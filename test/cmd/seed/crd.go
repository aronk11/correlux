package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

// crdSpec describes one synthetic CRD. Each one declares additionalPrinterColumns,
// because that is the part kubeui relies on: the API server renders the table
// and kubeui shows those columns without a line of resource-specific code.
type crdSpec struct {
	plural   string
	singular string
	kind     string
	listKind string
	short    string
}

func crdSpecs(n int) []crdSpec {
	catalog := []crdSpec{
		{"widgets", "widget", "Widget", "WidgetList", "wid"},
		{"gadgets", "gadget", "Gadget", "GadgetList", "gad"},
		{"pipelines", "pipeline", "Pipeline", "PipelineList", "pl"},
		{"sprockets", "sprocket", "Sprocket", "SprocketList", "spr"},
		{"cogs", "cog", "Cog", "CogList", "cg"},
	}
	if n > len(catalog) {
		n = len(catalog)
	}
	return catalog[:n]
}

func gvr(spec crdSpec) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: crdGroup, Version: "v1alpha1", Resource: spec.plural}
}

func ensureCRDs(ctx context.Context, c *clients, opts options) error {
	specs := crdSpecs(opts.crds)
	for _, spec := range specs {
		if err := created(createCRD(ctx, c, spec)); err != nil {
			return fmt.Errorf("crd %s: %w", spec.plural, err)
		}
	}
	// A CRD is not usable the instant it is created: the API server has to
	// serve its endpoint first. Waiting here is what makes the integration
	// tests deterministic instead of flaky.
	for _, spec := range specs {
		if err := waitForEstablished(ctx, c, spec); err != nil {
			return err
		}
	}
	return nil
}

func createCRD(ctx context.Context, c *clients, spec crdSpec) error {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.plural + "." + crdGroup,
			Labels: labels(nil),
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: crdGroup,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     spec.plural,
				Singular:   spec.singular,
				Kind:       spec.kind,
				ListKind:   spec.listKind,
				ShortNames: []string{spec.short},
				Categories: []string{"kubeui-load"},
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type: "object",
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"size":  {Type: "integer"},
									"owner": {Type: "string"},
								},
							},
							"status": {
								Type: "object",
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"phase": {Type: "string"},
								},
							},
						},
					},
				},
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				AdditionalPrinterColumns: []apiextensionsv1.CustomResourceColumnDefinition{
					{Name: "Phase", Type: "string", JSONPath: ".status.phase"},
					{Name: "Size", Type: "integer", JSONPath: ".spec.size"},
					{Name: "Owner", Type: "string", JSONPath: ".spec.owner", Priority: 1},
					{Name: "Age", Type: "date", JSONPath: ".metadata.creationTimestamp"},
				},
			}},
		},
	}
	_, err := c.ext.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	return err
}

func waitForEstablished(ctx context.Context, c *clients, spec crdSpec) error {
	name := spec.plural + "." + crdGroup
	return wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 60*time.Second, true,
		func(ctx context.Context) (bool, error) {
			crd, err := c.ext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		})
}

// seedCustomResources creates the custom objects for one CRD in one namespace.
func seedCustomResources(ctx context.Context, c *clients, opts options, namespace string, crdIndex int) error {
	specs := crdSpecs(opts.crds)
	if crdIndex >= len(specs) {
		return nil
	}
	spec := specs[crdIndex]
	client := c.dynamic.Resource(gvr(spec)).Namespace(namespace)

	phases := []string{"Ready", "Ready", "Ready", "Progressing", "Failed"}
	for i := 0; i < opts.customResources; i++ {
		name := fmt.Sprintf("%s-%04d", spec.singular, i)
		obj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": crdGroup + "/v1alpha1",
			"kind":       spec.kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"labels":    toAnyMap(labels(nil)),
			},
			"spec": map[string]any{
				"size":  int64(i%16 + 1),
				"owner": "team-" + strconv.Itoa(i%4),
			},
		}}
		result, err := client.Create(ctx, obj, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("custom resource %s/%s: %w", namespace, name, err)
		}

		// Write the status too, so the printer column has something to show.
		if err := unstructuredSetStatus(result, phases[i%len(phases)]); err != nil {
			return err
		}
		if _, err := client.UpdateStatus(ctx, result, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("custom resource status %s/%s: %w", namespace, name, err)
		}
	}
	return nil
}

func unstructuredSetStatus(obj *unstructured.Unstructured, phase string) error {
	status, ok := obj.Object["status"].(map[string]any)
	if !ok {
		status = map[string]any{}
	}
	status["phase"] = phase
	obj.Object["status"] = status
	return nil
}

func toAnyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

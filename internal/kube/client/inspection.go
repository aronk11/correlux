package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/aronk11/correlux/internal/domain/describe"
	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/kube/discovery"
)

// ReferencedBy scans bounded pages of workload definitions in the target's
// namespace. It never interprets an incomplete scan as a complete inventory.
func (f *Factory) ReferencedBy(ctx context.Context, cluster string, target inspection.Ref) (inspection.Report, error) {
	report := inspection.Report{Title: "What references " + target.Kind + "/" + target.Name, Summary: "Workload definitions in " + target.Namespace + "; explicit references only; first 200 of each kind", At: time.Now()}
	if target.Namespace == "" {
		return report, errors.New("select a namespaced configuration, identity, or volume resource")
	}
	cs, err := f.Clientset(cluster)
	if err != nil {
		return report, err
	}
	section := inspection.Section{Title: "Referencing workloads", Columns: []string{"Kind", "Namespace", "Name", "Reference"}, Empty: "no references in the scanned workload pages"}
	for _, entry := range [][4]string{{"", "v1", "pods", "Pod"}, {"apps", "v1", "deployments", "Deployment"}, {"apps", "v1", "statefulsets", "StatefulSet"}, {"apps", "v1", "daemonsets", "DaemonSet"}, {"batch", "v1", "jobs", "Job"}, {"batch", "v1", "cronjobs", "CronJob"}} {
		path := "/api/v1"
		if entry[0] != "" {
			path = "/apis/" + entry[0] + "/" + entry[1]
		}
		path += "/namespaces/" + target.Namespace + "/" + entry[2]
		raw, readErr := cs.Discovery().RESTClient().Get().AbsPath(path).Param("limit", "200").SetHeader("Accept", "application/json").DoRaw(ctx)
		if readErr != nil {
			report.Gaps = append(report.Gaps, entry[3]+": "+readErr.Error())
			continue
		}
		var list struct {
			Metadata struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []json.RawMessage `json:"items"`
		}
		if decodeErr := json.Unmarshal(raw, &list); decodeErr != nil {
			report.Gaps = append(report.Gaps, entry[3]+": could not decode list")
			continue
		}
		if list.Metadata.Continue != "" {
			report.Gaps = append(report.Gaps, entry[3]+": more than 200 objects; references outside this page were not scanned")
		}
		resource := entry[2]
		if entry[0] != "" {
			resource += "." + entry[0]
		}
		for _, item := range list.Items {
			var meta struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
			}
			if json.Unmarshal(item, &meta) != nil {
				continue
			}
			for _, link := range describe.Links(entry[3], item) {
				if link.Kind == target.Kind && link.Name == target.Name && link.Namespace == target.Namespace {
					section.Link(inspection.Ref{Kind: entry[3], Name: meta.Metadata.Name, Namespace: target.Namespace, Resource: resource}, entry[3], target.Namespace, meta.Metadata.Name, link.Detail)
				}
			}
		}
	}
	report.Sections = []inspection.Section{section}
	return report, nil
}

// ServiceRouting connects a service to selectors, EndpointSlices and applicable
// policy objects. It is configuration evidence, not a simulated packet verdict.
func (f *Factory) ServiceRouting(ctx context.Context, cluster, namespace, name, sourceNamespace, sourcePod string) (inspection.Report, error) {
	report := inspection.Report{Title: "Traffic to Service/" + name, Summary: "Configuration evidence; use a probe to measure connectivity from an explicit source", At: time.Now()}
	cs, err := f.Clientset(cluster)
	if err != nil {
		return report, err
	}
	service, err := cs.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return report, err
	}
	routing := inspection.Section{Title: "Service routing", Columns: []string{"Field", "Value"}}
	routing.Add("Service", namespace+"/"+name)
	routing.Add("Type", string(service.Spec.Type))
	routing.Add("Cluster IP", service.Spec.ClusterIP)
	routing.Add("Selector", labels.Set(service.Spec.Selector).String())
	routing.Add("External name", service.Spec.ExternalName)
	for _, port := range service.Spec.Ports {
		routing.Add("Port "+port.Name, fmt.Sprintf("%d/%s → %s", port.Port, port.Protocol, port.TargetPort.String()))
	}
	report.Sections = append(report.Sections, routing)
	var pods *corev1.PodList
	var readErr error
	if len(service.Spec.Selector) > 0 {
		pods, readErr = cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set(service.Spec.Selector).String(), Limit: 200})
	}
	if readErr != nil {
		report.Gaps = append(report.Gaps, "selected pods: "+readErr.Error())
	}
	selected := inspection.Section{Title: "Selected pods", Columns: []string{"Pod", "Node", "Phase", "Ready"}, Empty: "no pods in the loaded selector results"}
	if pods != nil {
		if pods.Continue != "" {
			report.Gaps = append(report.Gaps, "selected pods: first 200 only")
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			ready := "unknown"
			for _, condition := range pod.Status.Conditions {
				if condition.Type == "Ready" {
					ready = string(condition.Status)
				}
			}
			selected.Link(inspection.Ref{Kind: "Pod", Name: pod.Name, Namespace: namespace, Resource: "pods"}, pod.Name, pod.Spec.NodeName, string(pod.Status.Phase), ready)
		}
	}
	report.Sections = append(report.Sections, selected)
	endpoints := inspection.Section{Title: "EndpointSlices", Columns: []string{"Slice", "Address", "Ready", "Serving", "Terminating", "Pod"}, Empty: "no published EndpointSlices in the loaded page"}
	slices, readErr := cs.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set{"kubernetes.io/service-name": name}.String(), Limit: 200})
	if readErr != nil {
		report.Gaps = append(report.Gaps, "EndpointSlices: "+readErr.Error())
	} else {
		if slices.Continue != "" {
			report.Gaps = append(report.Gaps, "EndpointSlices: first 200 only")
		}
		for i := range slices.Items {
			slice := &slices.Items[i]
			for _, endpoint := range slice.Endpoints {
				pod := ""
				if endpoint.TargetRef != nil {
					pod = endpoint.TargetRef.Name
				}
				endpoints.Link(inspection.Ref{Kind: "EndpointSlice", Name: slice.Name, Namespace: namespace, Resource: "endpointslices.discovery.k8s.io"}, slice.Name, strings.Join(endpoint.Addresses, ","), boolValue(endpoint.Conditions.Ready), boolValue(endpoint.Conditions.Serving), boolValue(endpoint.Conditions.Terminating), pod)
			}
		}
	}
	report.Sections = append(report.Sections, endpoints)
	policies := inspection.Section{Title: "Relevant NetworkPolicies", Columns: []string{"Direction", "Namespace", "Policy", "Selected pod", "Rule sets"}, Empty: "no selecting policies in the loaded pages; CNI and mesh enforcement are not verified"}
	addPolicies := func(ns, podName string, podLabels map[string]string, direction string) {
		items, listErr := cs.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{Limit: 200})
		if listErr != nil {
			report.Gaps = append(report.Gaps, "NetworkPolicies in "+ns+": "+listErr.Error())
			return
		}
		if items.Continue != "" {
			report.Gaps = append(report.Gaps, "NetworkPolicies in "+ns+": first 200 only")
		}
		for i := range items.Items {
			policy := &items.Items[i]
			wanted := "Ingress"
			if strings.HasPrefix(direction, "source") {
				wanted = "Egress"
			}
			applies := len(policy.Spec.PolicyTypes) == 0 && (wanted == "Ingress" || len(policy.Spec.Egress) > 0)
			for _, policyType := range policy.Spec.PolicyTypes {
				if string(policyType) == wanted {
					applies = true
				}
			}
			if !applies {
				continue
			}
			selector, selectorErr := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
			if selectorErr != nil || !selector.Matches(labels.Set(podLabels)) {
				continue
			}
			types := []string{}
			for _, t := range policy.Spec.PolicyTypes {
				types = append(types, string(t))
			}
			policies.Link(inspection.Ref{Kind: "NetworkPolicy", Name: policy.Name, Namespace: ns, Resource: "networkpolicies.networking.k8s.io"}, direction, ns, policy.Name, podName, strings.Join(types, ",")+fmt.Sprintf("; ingress %d, egress %d", len(policy.Spec.Ingress), len(policy.Spec.Egress)))
		}
	}
	if sourcePod != "" {
		source, sourceErr := cs.CoreV1().Pods(sourceNamespace).Get(ctx, sourcePod, metav1.GetOptions{})
		if sourceErr != nil {
			report.Gaps = append(report.Gaps, "source pod: "+sourceErr.Error())
		} else {
			sourceSection := inspection.Section{Title: "Source", Columns: []string{"Field", "Value"}}
			sourceSection.Link(inspection.Ref{Kind: "Pod", Name: source.Name, Namespace: source.Namespace, Resource: "pods"}, "Pod", source.Namespace+"/"+source.Name)
			sourceSection.Add("DNS policy", string(source.Spec.DNSPolicy))
			sourceSection.Add("Service account", source.Spec.ServiceAccountName)
			sourceSection.Add("Node", source.Spec.NodeName)
			sourceSection.Add("Pod IP", source.Status.PodIP)
			report.Sections = append(report.Sections, sourceSection)
			addPolicies(source.Namespace, source.Name, source.Labels, "source egress")
		}
	}
	// Bound policy reads to one destination and disclose that other label sets
	// can select different policies.
	if pods != nil && len(pods.Items) > 0 {
		first := &pods.Items[0]
		addPolicies(namespace, first.Name, first.Labels, "destination ingress")
		if len(pods.Items) > 1 {
			report.Gaps = append(report.Gaps, "destination policies are evaluated for "+first.Name+" only; other selected pods can have different labels")
		}
	}
	report.Sections = append(report.Sections, policies)
	return report, nil
}

func boolValue(v *bool) string {
	if v == nil {
		return "not reported"
	}
	if *v {
		return "true"
	}
	return "false"
}

// CompareObjects fetches the explicitly mapped object from two selected clusters.
func (f *Factory) CompareObjects(ctx context.Context, leftCluster, rightCluster string, left, right inspection.Ref, res discovery.Resource) (inspection.Report, error) {
	report := inspection.Report{Title: "Compare " + left.Kind + "/" + left.Name, Summary: leftCluster + " / " + left.Namespace + " / " + left.Name + " → " + rightCluster + " / " + right.Namespace + " / " + right.Name + "; metadata noise, status and credential fields excluded", At: time.Now()}
	// Discover the target independently: a different cluster can serve a different version.
	catalog, err := f.Catalog(ctx, rightCluster)
	if err != nil {
		return report, err
	}
	other, ok := catalog.Lookup(res.FullName())
	if !ok {
		return report, fmt.Errorf("target cluster does not serve %s", res.FullName())
	}
	if other.Namespaced != (right.Namespace != "") {
		return report, errors.New("target namespace must match the resource scope")
	}
	before, err := f.GetObject(ctx, leftCluster, res, left.Namespace, left.Name)
	if err != nil {
		return report, err
	}
	after, err := f.GetObject(ctx, rightCluster, other, right.Namespace, right.Name)
	if err != nil {
		return report, err
	}
	section, err := inspection.Comparison(before.Raw, after.Raw)
	if err != nil {
		return report, err
	}
	report.Sections = []inspection.Section{section}
	if res.GVR.Version != other.GVR.Version {
		report.Gaps = append(report.Gaps, "API versions differ: "+res.GroupVersion()+" vs "+other.GroupVersion()+"; schema differences may explain the diff")
	}
	return report, nil
}

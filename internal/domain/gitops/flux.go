// Package gitops describes the capabilities of explicitly identified Flux APIs.
package gitops

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Resource resolves a supported Flux kind to its fully qualified resource name.
func Resource(kind string) string {
	switch kind {
	case "GitRepository":
		return "gitrepositories.source.toolkit.fluxcd.io"
	case "OCIRepository":
		return "ocirepositories.source.toolkit.fluxcd.io"
	case "Bucket":
		return "buckets.source.toolkit.fluxcd.io"
	case "HelmRepository":
		return "helmrepositories.source.toolkit.fluxcd.io"
	case "HelmChart":
		return "helmcharts.source.toolkit.fluxcd.io"
	case "Kustomization":
		return "kustomizations.kustomize.toolkit.fluxcd.io"
	case "HelmRelease":
		return "helmreleases.helm.toolkit.fluxcd.io"
	case "ImageRepository":
		return "imagerepositories.image.toolkit.fluxcd.io"
	case "ImagePolicy":
		return "imagepolicies.image.toolkit.fluxcd.io"
	case "ImageUpdateAutomation":
		return "imageupdateautomations.image.toolkit.fluxcd.io"
	case "Alert":
		return "alerts.notification.toolkit.fluxcd.io"
	case "Provider":
		return "providers.notification.toolkit.fluxcd.io"
	case "Receiver":
		return "receivers.notification.toolkit.fluxcd.io"
	}
	return ""
}

// Identifies checks the API group as well as the kind; kind names are not unique.
func Identifies(apiVersion, kind string) bool {
	resource := Resource(kind)
	_, group, ok := strings.Cut(resource, ".")
	return ok && strings.HasPrefix(apiVersion, group+"/")
}

// Actions lists only operations supported by the corresponding controller.
func Actions(raw []byte) []string {
	var doc struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Spec       struct {
			Suspend bool   `json:"suspend"`
			Type    string `json:"type"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &doc) != nil || !Identifies(doc.APIVersion, doc.Kind) {
		return nil
	}
	switch doc.Kind {
	case "GitRepository", "OCIRepository", "Bucket", "HelmRepository", "HelmChart", "Kustomization", "HelmRelease", "ImageRepository", "ImageUpdateAutomation":
	default:
		return nil
	}
	// OCI HelmRepository is a data object; source-controller does not reconcile it.
	if doc.Kind == "HelmRepository" && doc.Spec.Type == "oci" {
		return nil
	}
	if doc.Spec.Suspend {
		return []string{"resume"}
	}
	actions := []string{"reconcile", "suspend"}
	if doc.Kind == "HelmRelease" {
		actions = append(actions, "reset", "force")
	}
	return actions
}

// Patch builds a minimal merge patch with optimistic concurrency protection.
func Patch(raw []byte, action string, at time.Time) ([]byte, error) {
	allowed := false
	for _, candidate := range Actions(raw) {
		if candidate == action {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("this Flux object does not support that action in its current state")
	}
	var doc struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Metadata.ResourceVersion == "" {
		return nil, errors.New("refresh this object before changing it: resourceVersion is missing")
	}
	meta := map[string]any{"resourceVersion": doc.Metadata.ResourceVersion}
	patch := map[string]any{"metadata": meta}
	if action == "suspend" || action == "resume" {
		patch["spec"] = map[string]any{"suspend": action == "suspend"}
	} else {
		token := at.UTC().Format(time.RFC3339Nano)
		annotations := map[string]string{"reconcile.fluxcd.io/requestedAt": token}
		if action == "force" {
			annotations["reconcile.fluxcd.io/forceAt"] = token
		}
		if action == "reset" {
			annotations["reconcile.fluxcd.io/resetAt"] = token
		}
		meta["annotations"] = annotations
	}
	return json.Marshal(patch)
}

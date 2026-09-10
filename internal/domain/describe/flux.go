package describe

import (
	"sort"
	"strings"

	"github.com/aronk11/correlux/internal/domain/gitops"
)

func describeFlux(kind string, doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")
	overview := Section{Title: "Flux reconciliation", Columns: []string{"Field", "Value"}}
	overview.add("Suspended", yesNo(boolean(spec, "suspend")))
	for _, field := range []string{"interval", "retryInterval", "timeout", "serviceAccountName", "targetNamespace", "storageNamespace", "releaseName", "path", "url", "type", "provider"} {
		overview.add(field, value(spec[field]))
	}
	for _, field := range []string{"observedGeneration", "lastAppliedRevision", "lastAttemptedRevision", "lastHandledReconcileAt", "lastHandledForceAt", "lastHandledResetAt", "failures", "installFailures", "upgradeFailures", "lastReleaseRevision", "lastPushCommit", "lastAutomationRunTime"} {
		overview.add(field, value(status[field]))
	}
	overview.add("Generation", value(child(doc, "metadata")["generation"]))
	if child(spec, "kubeConfig") != nil {
		overview.add("Target cluster", "remote kubeConfig; managed resources are not linked into the local cluster")
	}
	if value(child(doc, "metadata")["generation"]) != value(status["observedGeneration"]) {
		overview.add("Freshness", "current generation not reported as observed; inspect condition generations")
	}
	for _, field := range []string{"ref", "chartRef", "sourceRef", "policy", "filterTags", "update", "git", "driftDetection", "install", "upgrade", "rollback", "uninstall"} {
		for _, key := range sortedMapKeys(child(spec, field)) {
			overview.add(field+"."+key, value(child(spec, field)[key]))
		}
	}
	if kind == "Kustomization" {
		overview.add("Prune", yesNo(boolean(spec, "prune")))
		overview.add("Wait for health", yesNo(boolean(spec, "wait")))
		overview.add("Deletion policy", str(spec, "deletionPolicy"))
		overview.add("Inventory entries", value(float64(len(slice(child(status, "inventory"), "entries")))))
	}
	artifact := Section{Title: "Source artifact", Columns: []string{"Field", "Value"}}
	for _, field := range []string{"revision", "digest", "lastUpdateTime", "size", "url"} {
		artifact.add(field, value(child(status, "artifact")[field]))
	}
	out := []Section{overview}
	if len(artifact.Rows) > 0 {
		out = append(out, artifact)
	}
	history := Section{Title: "Helm release history", Columns: []string{"Revision", "Chart", "Version", "Status", "First deployed", "Last deployed"}}
	for _, item := range slice(status, "history") {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		history.Rows = append(history.Rows, []string{value(entry["version"]), str(entry, "chartName"), str(entry, "chartVersion"), str(entry, "status"), str(entry, "firstDeployed"), str(entry, "lastDeployed")})
	}
	if len(history.Rows) > 0 {
		out = append(out, history)
	}
	return out
}

func sortedMapKeys(m map[string]any) []string {
	// splitLabels already gives stable ordering, but keys must retain '=' characters.
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fluxLinks(kind string, doc map[string]any, namespace string) []Link {
	spec, status := child(doc, "spec"), child(doc, "status")
	var out []Link
	add := func(kind, name, ns, detail string) {
		if name == "" || kind == "" {
			return
		}
		if ns == "" {
			ns = namespace
		}
		resource := gitops.Resource(kind)
		if kind == "Secret" {
			resource = "secrets"
		}
		if kind == "ConfigMap" {
			resource = "configmaps"
		}
		if kind == "ServiceAccount" {
			resource = "serviceaccounts"
		}
		out = append(out, Link{Kind: kind, Name: name, Namespace: ns, Resource: resource, Detail: detail})
	}
	ref := func(r map[string]any, fallback, detail string) {
		k := str(r, "kind")
		if k == "" {
			k = fallback
		}
		add(k, str(r, "name"), str(r, "namespace"), detail)
	}
	ref(child(spec, "sourceRef"), "GitRepository", "source artifact")
	ref(child(spec, "chartRef"), "OCIRepository", "chart artifact")
	ref(child(child(child(spec, "chart"), "spec"), "sourceRef"), "HelmRepository", "chart source")
	if chart := str(status, "helmChart"); chart != "" {
		ns, name, ok := strings.Cut(chart, "/")
		if ok {
			add("HelmChart", name, ns, "generated chart artifact")
		}
	}
	for _, item := range slice(spec, "dependsOn") {
		r, ok := item.(map[string]any)
		if ok {
			ref(r, kind, "reconciliation dependency")
		}
	}
	for _, item := range slice(spec, "valuesFrom") {
		r, ok := item.(map[string]any)
		if ok {
			ref(r, "ConfigMap", "Helm values ("+str(r, "valuesKey")+")")
		}
	}
	for _, item := range slice(child(spec, "postBuild"), "substituteFrom") {
		r, ok := item.(map[string]any)
		if ok {
			ref(r, "ConfigMap", "post-build substitution")
		}
	}
	for _, key := range []string{"secretRef", "certSecretRef", "proxySecretRef"} {
		ref(child(spec, key), "Secret", key)
	}
	ref(child(child(spec, "verify"), "secretRef"), "Secret", "signature verification")
	ref(child(child(spec, "decryption"), "secretRef"), "Secret", "decryption key")
	ref(child(child(spec, "kubeConfig"), "secretRef"), "Secret", "remote cluster credentials")
	ref(child(child(spec, "kubeConfig"), "configMapRef"), "ConfigMap", "remote cluster configuration")
	ref(child(spec, "imageRepositoryRef"), "ImageRepository", "image scan source")
	ref(child(spec, "providerRef"), "Provider", "notification provider")
	add("ServiceAccount", str(spec, "serviceAccountName"), "", "controller identity")
	for _, item := range slice(spec, "eventSources") {
		r, ok := item.(map[string]any)
		if ok && str(r, "name") != "*" {
			ref(r, "", "event source")
		}
	}
	for _, item := range slice(spec, "resources") {
		r, ok := item.(map[string]any)
		if ok && str(r, "name") != "*" {
			ref(r, "", "webhook target")
		}
	}
	// An inventory refers to the target cluster. Never navigate remote objects in the local cluster.
	if child(spec, "kubeConfig") == nil {
		for _, item := range slice(child(status, "inventory"), "entries") {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			parts := strings.Split(str(entry, "id"), "_")
			if len(parts) != 4 {
				continue
			}
			// API-group-qualified kinds are accepted by discovery without guessing plurals.
			lookup := parts[3]
			if parts[2] != "" {
				lookup += "." + parts[2]
			}
			out = append(out, Link{Kind: parts[3], Name: parts[1], Namespace: parts[0], Resource: lookup, Detail: "managed inventory (reported by Flux)"})
		}
		for _, item := range slice(spec, "healthChecks") {
			r, ok := item.(map[string]any)
			if ok {
				ref(r, "", "health check")
			}
		}
	}
	return uniqueLinks(out)
}

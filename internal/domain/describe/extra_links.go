package describe

import "strings"

func operationalLinks(kind string, doc map[string]any, namespace string) []Link {
	spec := child(doc, "spec")
	var out []Link
	add := func(kind, name, ns, resource, detail string) {
		if name != "" {
			out = append(out, Link{Kind: kind, Name: name, Namespace: ns, Resource: resource, Detail: detail})
		}
	}
	switch kind {
	case "Pod":
		add("Node", str(spec, "nodeName"), "", "nodes", "scheduled on")
	case "PersistentVolume":
		ref := child(spec, "claimRef")
		add("PersistentVolumeClaim", str(ref, "name"), str(ref, "namespace"), "persistentvolumeclaims", "bound claim")
		add("StorageClass", str(spec, "storageClassName"), "", "storageclasses.storage.k8s.io", "provisioning class")
	case "VolumeAttachment":
		add("PersistentVolume", str(child(spec, "source"), "persistentVolumeName"), "", "persistentvolumes", "attached volume")
		add("Node", str(spec, "nodeName"), "", "nodes", "attachment target")
	case "HorizontalPodAutoscaler":
		ref := child(spec, "scaleTargetRef")
		group, _, qualified := strings.Cut(str(ref, "apiVersion"), "/")
		lookup := str(ref, "kind")
		if qualified {
			lookup += "." + group
		}
		add(str(ref, "kind"), str(ref, "name"), namespace, lookup, "scaling target")
	case "RoleBinding", "ClusterRoleBinding":
		ref := child(doc, "roleRef")
		ns := namespace
		if str(ref, "kind") == "ClusterRole" {
			ns = ""
		}
		add(str(ref, "kind"), str(ref, "name"), ns, str(ref, "kind")+"."+str(ref, "apiGroup"), "permission rules")
		for _, item := range slice(doc, "subjects") {
			subject, ok := item.(map[string]any)
			if !ok || str(subject, "kind") != "ServiceAccount" {
				continue
			}
			ns := str(subject, "namespace")
			if ns == "" {
				ns = namespace
			}
			add("ServiceAccount", str(subject, "name"), ns, "serviceaccounts", "granted identity")
		}
	case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute", "UDPRoute":
		if !strings.HasPrefix(str(doc, "apiVersion"), "gateway.networking.k8s.io/") {
			return nil
		}
		for _, item := range slice(spec, "parentRefs") {
			ref, ok := item.(map[string]any)
			if !ok {
				continue
			}
			k := str(ref, "kind")
			if k == "" {
				k = "Gateway"
			}
			group := str(ref, "group")
			if group == "" {
				group = "gateway.networking.k8s.io"
			}
			ns := str(ref, "namespace")
			if ns == "" {
				ns = namespace
			}
			add(k, str(ref, "name"), ns, k+"."+group, "parent route attachment")
		}
		for _, item := range slice(spec, "rules") {
			rule, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, backend := range slice(rule, "backendRefs") {
				ref, ok := backend.(map[string]any)
				if !ok {
					continue
				}
				k := str(ref, "kind")
				if k == "" {
					k = "Service"
				}
				lookup := k
				if group := str(ref, "group"); group != "" {
					lookup += "." + group
				}
				ns := str(ref, "namespace")
				if ns == "" {
					ns = namespace
				}
				add(k, str(ref, "name"), ns, lookup, "route backend; permission/ReferenceGrant not verified")
			}
		}
	}
	return out
}

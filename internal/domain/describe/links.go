package describe

import "encoding/json"

// Link is an object explicitly referenced by another object's document.
// Unlike owner relationships these point sideways: a Pod mounts a claim or a
// ConfigMap, and a claim binds a PersistentVolume.
type Link struct {
	Kind      string
	Name      string
	Namespace string
	Detail    string
}

// Links returns only references the document states directly. It never infers
// a relationship from a similar name or label.
func Links(kind string, raw []byte) []Link {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	namespace := str(child(doc, "metadata"), "namespace")
	switch kind {
	case "Pod":
		return podLinks(child(doc, "spec"), namespace)
	case "PersistentVolumeClaim":
		spec := child(doc, "spec")
		var out []Link
		if name := str(spec, "volumeName"); name != "" {
			out = append(out, Link{Kind: "PersistentVolume", Name: name, Detail: "bound volume"})
		}
		if name := str(spec, "storageClassName"); name != "" {
			out = append(out, Link{Kind: "StorageClass", Name: name, Detail: "provisioning class"})
		}
		return out
	}
	return nil
}

func podLinks(spec map[string]any, namespace string) []Link {
	var out []Link
	add := func(kind, name, detail string) {
		if name != "" {
			out = append(out, Link{Kind: kind, Name: name, Namespace: namespace, Detail: detail})
		}
	}
	if name := str(spec, "serviceAccountName"); name != "" && name != "default" {
		add("ServiceAccount", name, "used by pod")
	}
	for _, item := range slice(spec, "imagePullSecrets") {
		secret, ok := item.(map[string]any)
		if ok {
			add("Secret", str(secret, "name"), "image pull credentials")
		}
	}
	for _, item := range slice(spec, "volumes") {
		volume, ok := item.(map[string]any)
		if !ok {
			continue
		}
		volumeName := str(volume, "name")
		out = append(out, volumeLinks(volume, namespace, volumeName)...)
	}
	for _, group := range []string{"initContainers", "containers"} {
		for _, item := range slice(spec, group) {
			container, ok := item.(map[string]any)
			if !ok {
				continue
			}
			detail := "environment for " + str(container, "name")
			for _, fromItem := range slice(container, "envFrom") {
				from, ok := fromItem.(map[string]any)
				if !ok {
					continue
				}
				add("ConfigMap", str(child(from, "configMapRef"), "name"), detail)
				add("Secret", str(child(from, "secretRef"), "name"), detail)
			}
			for _, envItem := range slice(container, "env") {
				env, ok := envItem.(map[string]any)
				if !ok {
					continue
				}
				valueFrom := child(env, "valueFrom")
				add("ConfigMap", str(child(valueFrom, "configMapKeyRef"), "name"), detail)
				add("Secret", str(child(valueFrom, "secretKeyRef"), "name"), detail)
			}
		}
	}
	return uniqueLinks(out)
}

func volumeLinks(volume map[string]any, namespace, volumeName string) []Link {
	var out []Link
	add := func(kind, name string) {
		if name != "" {
			out = append(out, Link{Kind: kind, Name: name, Namespace: namespace, Detail: "mounted as " + volumeName})
		}
	}
	add("PersistentVolumeClaim", str(child(volume, "persistentVolumeClaim"), "claimName"))
	add("ConfigMap", str(child(volume, "configMap"), "name"))
	add("Secret", str(child(volume, "secret"), "secretName"))
	for _, item := range slice(child(volume, "projected"), "sources") {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		add("ConfigMap", str(child(source, "configMap"), "name"))
		add("Secret", str(child(source, "secret"), "name"))
	}
	return out
}

func uniqueLinks(in []Link) []Link {
	seen := map[string]bool{}
	out := make([]Link, 0, len(in))
	for _, link := range in {
		key := link.Kind + "\x00" + link.Namespace + "\x00" + link.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, link)
	}
	return out
}

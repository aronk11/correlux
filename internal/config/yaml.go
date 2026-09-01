package config

import "sigs.k8s.io/yaml"

// unmarshal decodes YAML into v using the Kubernetes YAML↔JSON bridge, so the
// struct tags stay consistent with the rest of the Kubernetes ecosystem.
func unmarshal(data []byte, v any) error {
	return yaml.UnmarshalStrict(data, v)
}

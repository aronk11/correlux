package describe

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// The readers below walk a decoded JSON document without ever asserting that it
// has the shape it ought to. A CustomResourceDefinition can declare any schema
// it likes, an API server can add a field in a minor release, and a half-written
// object can be missing anything at all: reading a description must never be the
// thing that takes the UI down (SPEC 34).

func child(doc map[string]any, key string) map[string]any {
	if doc == nil {
		return nil
	}
	if nested, ok := doc[key].(map[string]any); ok {
		return nested
	}
	return nil
}

func slice(doc map[string]any, key string) []any {
	if doc == nil {
		return nil
	}
	if items, ok := doc[key].([]any); ok {
		return items
	}
	return nil
}

func str(doc map[string]any, key string) string {
	if doc == nil {
		return ""
	}
	if s, ok := doc[key].(string); ok {
		return s
	}
	return ""
}

func number(doc map[string]any, key string) int {
	if doc == nil {
		return 0
	}
	switch v := doc[key].(type) {
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func boolean(doc map[string]any, key string) bool {
	if doc == nil {
		return false
	}
	b, _ := doc[key].(bool)
	return b
}

// value renders any JSON value as one line. A map or a list is summarised
// rather than dumped: the full document is one keystroke away.
func value(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, value(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		return "{" + strconv.Itoa(len(typed)) + " fields}"
	default:
		return ""
	}
}

// selectorOf renders a LabelSelector, matchExpressions included, the way
// kubectl prints one.
func selectorOf(selector map[string]any) string {
	parts := splitLabels(child(selector, "matchLabels"))
	for _, e := range slice(selector, "matchExpressions") {
		expr, ok := e.(map[string]any)
		if !ok {
			continue
		}
		var values []string
		for _, v := range slice(expr, "values") {
			values = append(values, value(v))
		}
		clause := str(expr, "key") + " " + strings.ToLower(str(expr, "operator"))
		if len(values) > 0 {
			clause += " (" + strings.Join(values, ", ") + ")"
		}
		parts = append(parts, clause)
	}
	return strings.Join(parts, ",")
}

// selectorMap renders a plain label map, which is what a Service selector is.
func selectorMap(selector map[string]any) string {
	return strings.Join(splitLabels(selector), ",")
}

func splitLabels(labels map[string]any) []string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+value(v))
	}
	sort.Strings(parts)
	return parts
}

// resourceList renders CPU and memory as "cpu=100m, memory=128Mi".
func resourceList(resources map[string]any) string {
	if len(resources) == 0 {
		return "—"
	}
	return strings.Join(splitLabels(resources), ", ")
}

func containerPorts(container map[string]any) string {
	ports := slice(container, "ports")
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		port, ok := p.(map[string]any)
		if !ok {
			continue
		}
		rendered := strconv.Itoa(number(port, "containerPort"))
		if protocol := str(port, "protocol"); protocol != "" && protocol != "TCP" {
			rendered += "/" + protocol
		}
		parts = append(parts, rendered)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ",")
}

// volumeSource names where a volume comes from, which is the only part of a
// volume anybody reads.
func volumeSource(volume map[string]any) string {
	known := map[string]string{
		"persistentVolumeClaim": "claim",
		"configMap":             "configMap",
		"secret":                "secret",
		"emptyDir":              "emptyDir",
		"hostPath":              "hostPath",
		"projected":             "projected",
		"downwardAPI":           "downwardAPI",
		"nfs":                   "nfs",
		"csi":                   "csi",
	}
	keys := make([]string, 0, len(volume))
	for k := range volume {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if key == "name" {
			continue
		}
		label, ok := known[key]
		if !ok {
			label = key
		}
		source := child(volume, key)
		for _, field := range []string{"claimName", "name", "secretName", "path", "driver"} {
			if v := str(source, field); v != "" {
				return label + " " + v
			}
		}
		return label
	}
	return "—"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" || s == "0" {
		return "—"
	}
	return s
}

package inspection

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/aronk11/correlux/internal/domain/diff"
)

// Normalized removes controller bookkeeping and redacts credential-bearing
// fields before comparison. Status is separate from desired configuration.
func Normalized(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("expected an object document")
	}
	delete(doc, "status")
	if meta, ok := doc["metadata"].(map[string]any); ok {
		for _, key := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink", "namespace", "name"} {
			delete(meta, key)
		}
		if annotations, ok := meta["annotations"].(map[string]any); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			if len(annotations) == 0 {
				delete(meta, "annotations")
			}
		}
	}
	redact(doc)
	return json.MarshalIndent(doc, "", "  ")
}

func redact(doc map[string]any) {
	for key, value := range doc {
		lower := strings.ToLower(key)
		hidden := lower == "data" || lower == "binarydata" || lower == "stringdata" || lower == "values" || lower == "value" || lower == "token" || strings.Contains(lower, "password") || strings.Contains(lower, "privatekey") || lower == "credentials" || lower == "clientsecret"
		if hidden {
			doc[key] = "<redacted; not compared>"
			continue
		}
		switch value := value.(type) {
		case map[string]any:
			redact(value)
		case []any:
			for _, item := range value {
				if nested, ok := item.(map[string]any); ok {
					redact(nested)
				}
			}
		}
	}
}

// Comparison shows only differences in the reviewed, normalized projection.
func Comparison(before, after []byte) (Section, error) {
	left, err := Normalized(before)
	if err != nil {
		return Section{}, err
	}
	right, err := Normalized(after)
	if err != nil {
		return Section{}, err
	}
	section := Section{Title: "Configuration differences", Columns: []string{"Change", "Content"}, Empty: "no differences in compared fields; redacted fields are not compared"}
	changes := diff.Lines(strings.Split(string(left), "\n"), strings.Split(string(right), "\n"))
	for _, line := range diff.Hunks(changes, 2) {
		switch line.Op {
		case diff.Add:
			section.Add("+ target", line.Text)
		case diff.Remove:
			section.Add("- source", line.Text)
		default:
			section.Add("", line.Text)
		}
	}
	return section, nil
}

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/aronk11/correlux/internal/ui/screens"
)

// helmManifestSection follows identities in Helm's stored rendered manifest.
// It describes the saved revision, not a claim that every object still exists.
func (m *Model) helmManifestSection(raw []byte, namespace string, target func(objectRef) int) screens.DetailSection {
	section := screens.DetailSection{Title: "Resources in stored manifest", Columns: []string{"Kind", "Namespace", "Name"}, Empty: "no resource identities in the stored manifest"}
	var release struct {
		Manifest string `json:"manifest"`
	}
	if json.Unmarshal(raw, &release) != nil {
		return section
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(release.Manifest), 4096)
	for {
		var doc struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if err := decoder.Decode(&doc); err != nil {
			if !errors.Is(err, io.EOF) {
				section.Empty = "stored manifest could not be fully decoded"
			}
			break
		}
		if doc.Metadata.Name == "" || doc.Kind == "" {
			continue
		}
		ref := objectRef{Kind: doc.Kind, Name: doc.Metadata.Name, Namespace: doc.Metadata.Namespace}
		if group, _, ok := strings.Cut(doc.APIVersion, "/"); ok {
			ref.Resource = doc.Kind + "." + group
		} else {
			ref.Resource = doc.Kind
		}
		if res, ok := m.resourceFor(ref); ok {
			ref.Resource = res.FullName()
			if !res.Namespaced {
				ref.Namespace = ""
			} else if ref.Namespace == "" {
				ref.Namespace = namespace
			}
		} else if ref.Namespace == "" {
			ref.Namespace = namespace
		}
		section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{ref.Kind, ref.Namespace, ref.Name}, Target: target(ref)})
	}
	return section
}

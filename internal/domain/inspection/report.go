// Package inspection builds read-only investigation reports from observed data.
package inspection

import (
	"encoding/json"
	"time"
)

// Ref identifies a navigable Kubernetes object without guessing its API group.
type Ref struct{ Kind, Name, Namespace, Resource string }

// Row contains facts and an optional object to inspect.
type Row struct {
	Cells []string
	Ref   *Ref
}

// Section is one table in an investigation.
type Section struct {
	Title   string
	Columns []string
	Rows    []Row
	Empty   string
}

// Report records scope, time, facts and unavailable evidence explicitly.
type Report struct {
	Snapshot       *Snapshot `json:"snapshot,omitempty"`
	Title, Summary string
	At             time.Time
	Sections       []Section
	Gaps           []string
}

// Add appends a non-navigable fact row.
func (s *Section) Add(cells ...string) { s.Rows = append(s.Rows, Row{Cells: cells}) }

// Link appends an attributed, navigable fact row.
func (s *Section) Link(ref Ref, cells ...string) {
	s.Rows = append(s.Rows, Row{Cells: cells, Ref: &ref})
}

// Snapshot is a portable, redacted configuration projection with provenance.
type Snapshot struct {
	Version  int             `json:"version"`
	Cluster  string          `json:"cluster"`
	Ref      Ref             `json:"ref"`
	At       time.Time       `json:"at"`
	Document json.RawMessage `json:"document"`
}

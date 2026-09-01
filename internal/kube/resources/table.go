// Package resources lists any Kubernetes resource — native or custom — as a
// table.
//
// The columns come from the API server's own printer (the `Table` content
// type), which is what `kubectl get` uses. That single decision is what makes
// CRD support real rather than nominal: a CustomResourceDefinition that
// declares `additionalPrinterColumns` renders with those columns in kubeui,
// for free, with no per-resource code. It also moves the formatting work to the
// server and keeps the payload small.
package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// tableAccept asks for a server-rendered table, falling back to plain JSON on
// an API server too old to print one.
const tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io," +
	"application/json;as=Table;v=v1beta1;g=meta.k8s.io," +
	"application/json"

// DefaultPageSize bounds a single request. Large clusters are paged through
// rather than loaded whole (see ADR 6).
const DefaultPageSize int64 = 500

// Column is one table column as the server described it.
type Column struct {
	Name        string
	Type        string
	Description string
	// Priority above zero marks a column that `kubectl get -o wide` shows and
	// the default view hides.
	Priority int32
}

// Wide reports whether this column belongs to the wide view only.
func (c Column) Wide() bool { return c.Priority > 0 }

// Row is one object, already rendered to cells by the API server.
type Row struct {
	Cells     []string
	Name      string
	Namespace string
	// CreatedAt is the object's creation timestamp, zero when the server did
	// not include object metadata.
	CreatedAt time.Time
}

// Table is a page of results.
type Table struct {
	Columns []Column
	Rows    []Row
	// Continue is the token for the next page, empty when this is the last one.
	Continue string
	// Remaining is the server's estimate of the objects after this page, or -1
	// when it did not say.
	Remaining int64
}

// HasMore reports whether another page exists.
func (t *Table) HasMore() bool { return t.Continue != "" }

// ListOptions selects what to list.
type ListOptions struct {
	// Namespace scopes a namespaced resource; empty means all namespaces.
	Namespace string
	// Limit bounds the page; zero means DefaultPageSize.
	Limit int64
	// Continue resumes a previous page.
	Continue string
	// LabelSelector and FieldSelector are passed to the server unchanged.
	LabelSelector string
	FieldSelector string
}

// Target identifies what to list. It is the subset of a discovered resource
// that listing needs, so this package does not depend on discovery.
type Target struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// List fetches one page of a resource as a server-rendered table.
func List(ctx context.Context, client rest.Interface, target Target, opts ListOptions) (*Table, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}

	req := client.Get().
		AbsPath(path(target, opts.Namespace)).
		SetHeader("Accept", tableAccept).
		Param("limit", strconv.FormatInt(limit, 10))

	if opts.Continue != "" {
		req = req.Param("continue", opts.Continue)
	}
	if opts.LabelSelector != "" {
		req = req.Param("labelSelector", opts.LabelSelector)
	}
	if opts.FieldSelector != "" {
		req = req.Param("fieldSelector", opts.FieldSelector)
	}

	raw, err := req.DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return decode(raw)
}

// path builds the REST path for a resource, in the core group or otherwise.
func path(target Target, namespace string) string {
	var b strings.Builder
	if target.GVR.Group == "" {
		b.WriteString("/api/" + target.GVR.Version)
	} else {
		b.WriteString("/apis/" + target.GVR.Group + "/" + target.GVR.Version)
	}
	if target.Namespaced && namespace != "" {
		b.WriteString("/namespaces/" + namespace)
	}
	b.WriteString("/" + target.GVR.Resource)
	return b.String()
}

// objectMeta is the slice of an object's metadata a table row carries.
type objectMeta struct {
	Metadata struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
}

func decode(raw []byte) (*Table, error) {
	var src metav1.Table
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("decode table: %w", err)
	}
	if src.Kind != "" && src.Kind != "Table" {
		return nil, fmt.Errorf("server returned %s, not a Table; the cluster may be too old", src.Kind)
	}

	out := &Table{
		Columns:   make([]Column, 0, len(src.ColumnDefinitions)),
		Rows:      make([]Row, 0, len(src.Rows)),
		Continue:  src.Continue,
		Remaining: -1,
	}
	if src.RemainingItemCount != nil {
		out.Remaining = *src.RemainingItemCount
	}
	for _, c := range src.ColumnDefinitions {
		out.Columns = append(out.Columns, Column{
			Name:        c.Name,
			Type:        c.Type,
			Description: c.Description,
			Priority:    c.Priority,
		})
	}
	for i := range src.Rows {
		row := Row{Cells: make([]string, 0, len(src.Rows[i].Cells))}
		for _, cell := range src.Rows[i].Cells {
			row.Cells = append(row.Cells, formatCell(cell))
		}
		if obj := src.Rows[i].Object.Raw; len(obj) > 0 {
			var meta objectMeta
			if err := json.Unmarshal(obj, &meta); err == nil {
				row.Name = meta.Metadata.Name
				row.Namespace = meta.Metadata.Namespace
				row.CreatedAt = meta.Metadata.CreationTimestamp
			}
		}
		if row.Name == "" && len(row.Cells) > 0 {
			// Every Kubernetes printer puts the name first; fall back to it
			// when the server did not include object metadata.
			row.Name = row.Cells[0]
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// formatCell renders a JSON cell the way kubectl does, without ever panicking
// on a shape we did not expect — a custom resource's printer column can contain
// anything its author put there.
func formatCell(v any) string {
	switch value := v.(type) {
	case nil:
		return "<none>"
	case string:
		if value == "" {
			return "<none>"
		}
		return value
	case bool:
		return strconv.FormatBool(value)
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(value, 10)
	case json.Number:
		return value.String()
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, formatCell(item))
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", value)
	}
}

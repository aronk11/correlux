package resources

// Merging tables from several clusters is not a matter of concatenating rows.
//
// Each cluster renders its own table, and they need not agree: a CRD may be at
// a different version with different printer columns, and even a built-in kind
// gains columns between Kubernetes releases. What must never happen is a cell
// landing under the wrong heading, so rows are mapped by column *name* and a
// column one cluster does not have is left empty rather than shifted.

// Part is one cluster's answer.
type Part struct {
	// Source names where it came from, and becomes the first column.
	Source string
	Table  *Table
	// Err reports that this cluster could not be listed. Its rows are missing;
	// saying so is the whole reason this field exists.
	Err error
}

// MergedRow is one object, tagged with the cluster it lives in.
type MergedRow struct {
	Source    string
	Cells     []string
	Name      string
	Namespace string
}

// Merged is several clusters' tables as one.
type Merged struct {
	Columns []Column
	Rows    []MergedRow
	// Failures are the sources that could not be listed, in the order they were
	// given, so the UI can name them.
	Failures []Part
	// Truncated is true when any source had more rows than it returned.
	Truncated bool
}

// SourceColumn is the heading of the column that says which cluster a row is
// from.
const SourceColumn = "Cluster"

// NamespaceColumn is added for a namespaced resource, because a table listed
// across every namespace of every cluster is unreadable without it.
const NamespaceColumn = "Namespace"

// Merge combines the parts into one table.
//
// The column order is the first part's, with any column a later part
// contributes appended: the cluster that answered first decides what the table
// looks like, and nothing another cluster returned is thrown away.
func Merge(parts []Part, namespaced bool) Merged {
	out := Merged{
		Columns: []Column{{Name: SourceColumn, Type: "string"}},
	}
	if namespaced {
		out.Columns = append(out.Columns, Column{Name: NamespaceColumn, Type: "string"})
	}
	index := map[string]int{}
	for _, part := range parts {
		if part.Err != nil || part.Table == nil {
			continue
		}
		for _, column := range part.Table.Columns {
			if _, seen := index[column.Name]; seen {
				continue
			}
			index[column.Name] = len(out.Columns)
			out.Columns = append(out.Columns, column)
		}
	}

	for _, part := range parts {
		if part.Err != nil {
			out.Failures = append(out.Failures, part)
			continue
		}
		if part.Table == nil {
			continue
		}
		if part.Table.HasMore() {
			out.Truncated = true
		}

		for i := range part.Table.Rows {
			row := &part.Table.Rows[i]
			cells := make([]string, len(out.Columns))
			cells[0] = part.Source
			if namespaced {
				cells[1] = row.Namespace
			}
			for c, column := range part.Table.Columns {
				if c >= len(row.Cells) {
					break
				}
				cells[index[column.Name]] = row.Cells[c]
			}
			out.Rows = append(out.Rows, MergedRow{
				Source:    part.Source,
				Cells:     cells,
				Name:      row.Name,
				Namespace: row.Namespace,
			})
		}
	}

	return out
}

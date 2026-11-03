package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// ObjectData is one Kubernetes object on screen.
//
// It has two modes and they answer different questions. The details are what
// Correlux knows about the object and where it can take you next; the YAML is
// what the server actually holds, unedited and unabridged.
type ObjectData struct {
	Kind      string
	Name      string
	Namespace string
	// Subtitle carries the API version and the age.
	Subtitle string
	// Status colours the headline when the object is in a state worth marking.
	Status theme.Status
	Glyph  string
	// Headline is the one-line summary of the object's state.
	Headline string

	Sections []DetailSection
	// YAML is the document as the server returned it, already split into lines.
	YAML     []string
	ShowYAML bool
	// Tree is the document as a foldable tree; when it is set, the YAML view
	// draws it instead of the flat lines, with TreeCursor on one of them.
	Tree       []TreeLine
	TreeCursor int

	// Selected is the target index under the cursor, or -1 for none.
	Selected int
	Offset   int

	Message       string
	MessageStatus theme.Status
}

// RenderObject draws one object.
func RenderObject(t *theme.Theme, d ObjectData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}

	lines, _ := objectLines(t, d, width)
	offset := clamp(d.Offset, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// TreeLine is one visible node of the document tree.
type TreeLine struct {
	Depth  int
	Branch bool
	Open   bool
	// Key is the field name; empty for one line of a multi-line string.
	Key     string
	Value   string
	Summary string
}

// TreeCursorLine reports which line the tree's cursor is drawn on.
func (d ObjectData) TreeCursorLine(width int) int {
	lines, _ := objectLines(nil, d, width)
	return len(lines) - len(d.Tree) + d.TreeCursor
}

// treeLine draws one node the way YAML reads, "key: value", behind a fold
// marker. A folded node says what it holds instead of its value.
func treeLine(t *theme.Theme, n *TreeLine, selected bool, width int) string {
	marker := "  "
	if n.Branch {
		folded, unfolded := "▸", "▾"
		if t != nil {
			folded, unfolded = t.Glyphs.Folded, t.Glyphs.Unfolded
		}
		marker = folded + " "
		if n.Open {
			marker = unfolded + " "
		}
	}
	indent := strings.Repeat("  ", n.Depth)
	key := ""
	if n.Key != "" {
		key = n.Key
		if !strings.HasPrefix(n.Key, "[") {
			key += ":"
		}
	}
	tail := n.Value
	if n.Branch && (!n.Open || strings.HasPrefix(n.Key, "[")) {
		tail = n.Summary
	}
	plain := indent + marker + strings.TrimSpace(key+" "+tail)
	if t == nil {
		return truncateTo(plain, width)
	}
	if selected {
		return t.SelectedRow.Render(padTo(truncateTo(plain, width), width))
	}
	head := indent + marker
	out := t.Muted.Render(head)
	room := width - lipgloss.Width(head)
	if key != "" {
		k := truncateTo(key, room)
		out += t.Emphasis.Render(k)
		room -= lipgloss.Width(k)
	}
	if tail != "" && room > 1 {
		style := t.Base
		if n.Branch {
			style = t.Muted
		}
		sep := " "
		if key == "" {
			sep = ""
		}
		out += style.Render(truncateTo(sep+tail, room))
	}
	return out
}

// LineCount reports how tall the view is, so the model can bound scrolling.
func (d ObjectData) LineCount(width int) int {
	lines, _ := objectLines(nil, d, width)
	return len(lines)
}

// TargetLines reports which line each navigable row landed on.
func (d ObjectData) TargetLines(width int) map[int]int {
	_, targets := objectLines(nil, d, width)
	return targets
}

func objectLines(t *theme.Theme, d ObjectData, width int) ([]string, map[int]int) {
	style := func(get func(*theme.Theme) lipgloss.Style, s string) string {
		if t == nil {
			return s
		}
		return get(t).Render(s)
	}

	title := d.Kind + "/" + d.Name
	if d.Namespace != "" {
		title += "  " + d.Namespace
	}
	lines := []string{style(func(t *theme.Theme) lipgloss.Style { return t.Title }, truncateTo(title, width))}

	if d.Subtitle != "" {
		lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Muted },
			truncateTo(d.Subtitle, width)))
	}
	if d.Headline != "" {
		head := strings.TrimSpace(d.Glyph + " " + d.Headline)
		if t != nil {
			head = t.Style(d.Status).Render(truncateTo(head, width))
		} else {
			head = truncateTo(head, width)
		}
		lines = append(lines, head)
	}

	if d.ShowYAML && len(d.Tree) > 0 {
		lines = append(lines, "")
		for i := range d.Tree {
			lines = append(lines, treeLine(t, &d.Tree[i], i == d.TreeCursor, width))
		}
		return lines, map[int]int{}
	}
	if d.ShowYAML {
		// The document is shown as it came: indentation carries meaning in
		// YAML, so lines are clipped at the edge rather than wrapped.
		lines = append(lines, "")
		for _, line := range d.YAML {
			lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Base },
				truncateTo(strings.ReplaceAll(line, "\t", "  "), width)))
		}
		return lines, map[int]int{}
	}

	body, targets := sectionLines(t, d.Sections, d.Selected, width, len(lines))
	return append(lines, body...), targets
}

package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// ObjectData is one Kubernetes object on screen.
//
// It has two modes and they answer different questions. The details are what
// kubeui knows about the object and where it can take you next; the YAML is
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

// LineCount reports how tall the view is, so the model can bound scrolling.
func (d ObjectData) LineCount(width int) int {
	lines, _ := objectLines(nil, d, width)
	return len(lines)
}

// LineOfTarget reports which line a navigable row landed on, or -1.
func (d ObjectData) LineOfTarget(width, target int) int {
	_, targets := objectLines(nil, d, width)
	if line, ok := targets[target]; ok {
		return line
	}
	return -1
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

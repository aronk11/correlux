package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// WhyFinding is one explanation, ready to render.
type WhyFinding struct {
	// Glyph and Status carry the severity as a symbol and a colour; Severity is
	// the word, so the meaning survives without either.
	Glyph    string
	Status   theme.Status
	Severity string
	// Problem is what is wrong, Cause is why. Cause is empty when the cluster
	// did not say, and the view shows that plainly rather than filling the gap.
	Problem string
	Cause   string
	// Chain is the path from the workload to the failure.
	Chain       []string
	Evidence    []WhyEvidence
	Suggestions []WhySuggestion
	Confidence  string
}

// WhyEvidence is one fact, attributed to the object that stated it.
type WhyEvidence struct {
	Source string
	Detail string
	// At renders as a relative age when the fact was an event.
	At string
}

// WhySuggestion is something to do next, with the command that does it.
type WhySuggestion struct {
	Text    string
	Command string
}

// WhyData is the WHY view.
type WhyData struct {
	Name         string
	Namespace    string
	Health       string
	HealthGlyph  string
	HealthStatus theme.Status
	Summary      string
	Findings     []WhyFinding
	// Notes qualify the answer itself: evidence that could not be read, or that
	// has not arrived yet.
	Notes  []string
	Offset int
	// Empty is shown when there is nothing to explain, which is its own answer.
	Empty         string
	Message       string
	MessageStatus theme.Status
}

// RenderWhy draws the explanation for one application.
//
// The order is the order a person asks: what is wrong, how the failure is
// connected, why it happens, what says so, and what to do about it. The
// confidence is printed with every finding, because an explanation that cannot
// be doubted is one nobody checks (SPEC 10).
func RenderWhy(t *theme.Theme, d WhyData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}

	lines := whyLines(t, d, width)
	offset := clamp(d.Offset, 0, max(len(lines)-1, 0))
	end := min(offset+height, len(lines))
	return strings.Join(lines[offset:end], "\n")
}

// LineCount reports how tall the rendered view is, so the model can bound
// scrolling without rendering twice.
func (d WhyData) LineCount(width int) int { return len(whyLines(nil, d, width)) }

func whyLines(t *theme.Theme, d WhyData, width int) []string {
	style := func(get func(*theme.Theme) lipgloss.Style, s string) string {
		if t == nil {
			return s
		}
		return get(t).Render(s)
	}
	muted := func(s string) string {
		return style(func(t *theme.Theme) lipgloss.Style { return t.Muted }, s)
	}

	title := d.Name
	if d.Namespace != "" {
		title += "  " + d.Namespace
	}
	state := strings.TrimSpace(d.HealthGlyph + " " + d.Health)
	if d.Summary != "" {
		state += "   " + d.Summary
	}
	if t != nil {
		state = t.Style(d.HealthStatus).Render(truncateTo(state, width))
	}

	lines := []string{
		style(func(t *theme.Theme) lipgloss.Style { return t.Title }, truncateTo(title, width)),
		state,
	}
	for _, note := range d.Notes {
		lines = append(lines, muted(truncateTo(note, width)))
	}

	if len(d.Findings) == 0 {
		return append(lines, "", muted(truncateTo(orDefault(d.Empty, "Nothing to explain."), width)))
	}

	for _, f := range d.Findings {
		head := f.Glyph + " " + f.Problem
		if t != nil {
			head = t.Style(f.Status).Render(truncateTo(head, width))
		} else {
			head = truncateTo(head, width)
		}
		lines = append(lines, "", head)

		if len(f.Chain) > 0 {
			arrow := " -> "
			if t != nil {
				arrow = " " + t.Glyphs.Arrow + " "
			}
			lines = append(lines, muted(truncateTo("  "+strings.Join(f.Chain, arrow), width)))
		}

		if f.Cause != "" {
			lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.PanelTitle }, "  WHY"))
			lines = append(lines, wrapInto(f.Cause, width-4, "    ", t, false)...)
		} else {
			lines = append(lines, muted("  The cluster did not say why."))
		}

		if len(f.Evidence) > 0 {
			lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.PanelTitle }, "  EVIDENCE"))
			for _, e := range f.Evidence {
				label := "    " + e.Source
				if e.At != "" {
					label += "  " + e.At
				}
				lines = append(lines, muted(truncateTo(label, width)))
				lines = append(lines, wrapInto(e.Detail, width-6, "      ", t, false)...)
			}
		}

		if len(f.Suggestions) > 0 {
			lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.PanelTitle }, "  WHAT TO CHECK"))
			for _, s := range f.Suggestions {
				lines = append(lines, wrapInto(s.Text, width-6, "    "+bullet(t)+" ", t, false)...)
				if s.Command != "" {
					lines = append(lines, style(func(t *theme.Theme) lipgloss.Style { return t.Key },
						truncateTo("      "+s.Command, width)))
				}
			}
		}

		lines = append(lines, muted(truncateTo("  confidence: "+f.Confidence, width)))
	}
	return lines
}

func bullet(t *theme.Theme) string {
	if t == nil {
		return "-"
	}
	return t.Glyphs.Bullet
}

// wrapInto breaks a sentence across lines at the given width. Causes and
// evidence are prose, and truncating prose loses exactly the part that
// explained something.
func wrapInto(s string, width int, indent string, t *theme.Theme, emphasise bool) []string {
	if width < 8 {
		width = 8
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	var (
		out  []string
		line string
	)
	flush := func() {
		if line == "" {
			return
		}
		text := indent + line
		if t != nil {
			if emphasise {
				text = t.Emphasis.Render(text)
			} else {
				text = t.Base.Render(text)
			}
		}
		out = append(out, text)
		line = ""
	}
	for _, w := range words {
		switch {
		case line == "":
			line = w
		case lipgloss.Width(line)+1+lipgloss.Width(w) <= width:
			line += " " + w
		default:
			flush()
			line = w
		}
	}
	flush()
	return out
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

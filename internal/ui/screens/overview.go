// Package screens renders Correlux's full-window views.
//
// A screen turns already-resolved data into text. It performs no I/O and holds
// no Kubernetes types, which keeps rendering fast, deterministic and testable.
package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// Field is one label/value row inside a panel.
type Field struct {
	Label string
	Value string
	// Status colours the value; theme.StatusUnknown renders it plain.
	Status theme.Status
	// Glyph renders the status glyph before the value.
	Glyph bool
	// Emphasize colours and bolds the whole row, including its label. It is
	// reserved for scan-first status rows; ordinary metadata stays quiet.
	Emphasize bool
}

// Panel is a titled group of fields.
type Panel struct {
	Title  string
	Fields []Field
	// Status colours the panel title and border. A glyph inside the panel still
	// carries the meaning when colour is unavailable.
	Status theme.Status
	// Note is a dimmed line under the fields.
	Note string
}

// OverviewData is the connection-oriented view Correlux shows before the
// application dashboard exists.
type OverviewData struct {
	Panels []Panel
	// Roadmap states plainly what this view does not do yet, so an empty area
	// is never mistaken for an empty cluster.
	Roadmap []string
}

// RenderOverview draws the panels stacked vertically, wrapping to two columns
// when the terminal is wide enough.
func RenderOverview(t *theme.Theme, d OverviewData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	const twoColumnMin = 108
	blocks := make([]string, 0, len(d.Panels))
	columnWidth := width
	if width >= twoColumnMin && len(d.Panels) > 1 {
		columnWidth = (width - 1) / 2
	}

	for _, p := range d.Panels {
		blocks = append(blocks, renderPanel(t, p, columnWidth))
	}

	var body string
	if columnWidth != width {
		var leftCol, rightCol []string
		for i, b := range blocks {
			if i%2 == 0 {
				leftCol = append(leftCol, b)
			} else {
				rightCol = append(rightCol, b)
			}
		}
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			strings.Join(leftCol, "\n"),
			" ",
			strings.Join(rightCol, "\n"),
		)
	} else {
		body = strings.Join(blocks, "\n")
	}

	if len(d.Roadmap) > 0 {
		lines := make([]string, 0, len(d.Roadmap)+1)
		lines = append(lines, t.Muted.Render("Not implemented yet"))
		for _, r := range d.Roadmap {
			lines = append(lines, t.Muted.Render("  "+t.Glyphs.Bullet+" "+r))
		}
		body += "\n" + strings.Join(lines, "\n")
	}

	return clipHeight(body, height)
}

func renderPanel(t *theme.Theme, p Panel, width int) string {
	// lipgloss counts the border and padding inside Style.Width, so the text
	// area is four cells narrower than the box.
	const chrome = 4
	inner := width - chrome
	if inner < 10 {
		inner = 10
	}

	labelWidth := 0
	for _, f := range p.Fields {
		if n := lipgloss.Width(f.Label); n > labelWidth {
			labelWidth = n
		}
	}
	labelWidth = min(labelWidth, max(inner/3, 8))

	var b strings.Builder
	panelStyle := t.Panel
	titleStyle := t.PanelTitle
	if p.Status != theme.StatusUnknown {
		titleStyle = t.Style(p.Status).Bold(true)
		if t.Caps.Color {
			panelStyle = panelStyle.BorderForeground(t.Style(p.Status).GetForeground())
		}
	}
	b.WriteString(titleStyle.Render(p.Title))
	for _, f := range p.Fields {
		b.WriteString("\n")
		fieldStyle := t.Style(f.Status)
		labelStyle := t.Muted
		if f.Emphasize {
			fieldStyle = fieldStyle.Bold(true)
			labelStyle = fieldStyle
		}
		label := labelStyle.Render(padRight(f.Label, labelWidth))
		value := f.Value
		if f.Glyph {
			value = t.Glyph(f.Status) + " " + value
		}
		value = fieldStyle.Render(truncateTo(value, max(inner-labelWidth-2, 4)))
		b.WriteString(label + "  " + value)
	}
	if p.Note != "" {
		// Notes wrap rather than truncate: they exist to explain something, and
		// half an explanation is worse than none.
		b.WriteString("\n" + t.Muted.Width(inner).Render(p.Note))
	}
	return panelStyle.Width(width).Render(b.String())
}

func clipHeight(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func truncateTo(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

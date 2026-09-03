package screens

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// LogLine is one line ready to render.
type LogLine struct {
	// Source names the container it came from; shown only when a view covers
	// more than one.
	Source string
	// Time is the server's timestamp, already formatted, or empty.
	Time string
	Text string
	// Status marks a line Correlux itself wrote — a source that failed — so it
	// is never mistaken for output from the container.
	Status theme.Status
}

// LogsData is the log view.
type LogsData struct {
	// Title names what is being read: a pod, a container, an application.
	Title string
	// Subtitle carries the state of the read: following, paused, how many
	// lines, what could not be read.
	Subtitle string
	Lines    []LogLine
	// ShowSource prefixes each line with the container it came from.
	ShowSource bool
	// Wrap breaks long lines instead of clipping them.
	Wrap bool
	// Offset is the first visible line; Follow pins the view to the end.
	Offset int
	Follow bool

	Message       string
	MessageStatus theme.Status
}

// RenderLogs draws the log view.
//
// Log output is the one thing in Correlux that is not Correlux's text: it is
// rendered as it came, clipped rather than reflowed unless asked, and never
// styled by content. A line that looks like an error because Correlux coloured
// it is a line that lies about what the container said.
func RenderLogs(t *theme.Theme, d LogsData, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if d.Message != "" {
		return t.Style(d.MessageStatus).Render(truncateTo(d.Message, width))
	}

	head := t.Title.Render(truncateTo(d.Title, width))
	if d.Subtitle != "" {
		head += "\n" + t.Muted.Render(truncateTo(d.Subtitle, width))
	}
	headLines := strings.Count(head, "\n") + 1

	body := max(height-headLines-1, 1)
	rendered := logLines(t, d, width)

	offset := d.Offset
	if d.Follow || offset > max(len(rendered)-body, 0) {
		offset = max(len(rendered)-body, 0)
	}
	offset = clamp(offset, max(len(rendered)-body, 0))
	end := min(offset+body, len(rendered))

	out := head + "\n"
	if len(rendered) == 0 {
		return out + t.Muted.Render("No output yet.")
	}
	return out + strings.Join(rendered[offset:end], "\n")
}

// LineCount reports how many rendered lines the view holds, which is what
// bounds scrolling.
func (d LogsData) LineCount(width int) int { return len(logLines(nil, d, width)) }

func logLines(t *theme.Theme, d LogsData, width int) []string {
	out := make([]string, 0, len(d.Lines))
	for _, line := range d.Lines {
		prefix := ""
		if line.Time != "" {
			prefix += line.Time + " "
		}
		if d.ShowSource && line.Source != "" {
			prefix += line.Source + "  "
		}

		text := strings.ReplaceAll(line.Text, "\t", "    ")
		if t != nil && prefix != "" {
			prefix = t.Muted.Render(prefix)
		}

		if !d.Wrap {
			rendered := prefix + text
			if t != nil && line.Status != theme.StatusUnknown {
				rendered = t.Style(line.Status).Render(rendered)
			}
			out = append(out, truncateTo(rendered, width))
			continue
		}

		// Wrapped: the prefix leads the first line and the rest is indented to
		// it, so a wrapped line still reads as one.
		indent := strings.Repeat(" ", min(lipgloss.Width(prefix), max(width/4, 0)))
		for i, chunk := range wrapText(text, max(width-lipgloss.Width(prefix), 8)) {
			lead := prefix
			if i > 0 {
				lead = indent
			}
			rendered := lead + chunk
			if t != nil && line.Status != theme.StatusUnknown {
				rendered = t.Style(line.Status).Render(rendered)
			}
			out = append(out, rendered)
		}
	}
	return out
}

// wrapText breaks a line at the given width, on spaces where it can and
// mid-word where it must — a base64 blob has no spaces and still has to fit.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	if lipgloss.Width(text) <= width {
		return []string{text}
	}

	var out []string
	line := ""
	for _, word := range strings.Split(text, " ") {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= width:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
		for lipgloss.Width(line) > width {
			out = append(out, truncateTo(line, width))
			line = dropCells(line, width)
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// dropCells removes the first width display cells of a string.
func dropCells(s string, width int) string {
	runes := []rune(s)
	for i := range runes {
		if lipgloss.Width(string(runes[:i])) >= width {
			return string(runes[i:])
		}
	}
	return ""
}

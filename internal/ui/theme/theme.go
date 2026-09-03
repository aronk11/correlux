package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/config"
)

// Capabilities describes what the current terminal can do.
type Capabilities struct {
	Color   bool
	Unicode bool
	Dark    bool
	// Attributes reports whether the sink understands text attributes (bold,
	// reverse, underline). A monochrome terminal still does; a file or a CI log
	// does not, and must receive plain text.
	Attributes bool
}

// DetectCapabilities inspects the environment. The terminal may later report
// its real background colour, in which case the caller updates Dark and
// rebuilds the theme.
func DetectCapabilities(env Env) Capabilities {
	return Capabilities{
		Color:      DetectColor(env),
		Unicode:    DetectUnicode(env),
		Dark:       DetectDark(env),
		Attributes: true,
	}
}

// Theme holds every style Correlux renders with. It is rebuilt (cheaply)
// whenever capabilities change; styles are never constructed in render paths.
type Theme struct {
	Caps   Capabilities
	Glyphs Glyphs

	// Base chrome.
	Base       lipgloss.Style
	Panel      lipgloss.Style
	PanelTitle lipgloss.Style
	Header     lipgloss.Style
	StatusBar  lipgloss.Style
	Key        lipgloss.Style
	KeyDesc    lipgloss.Style
	Muted      lipgloss.Style
	Emphasis   lipgloss.Style
	Title      lipgloss.Style

	// Semantic states. Each is paired with a glyph so colour is never the only
	// carrier of meaning.
	Healthy  lipgloss.Style
	Warning  lipgloss.Style
	Critical lipgloss.Style
	Info     lipgloss.Style

	// Context badges.
	ContextProd lipgloss.Style
	ContextSafe lipgloss.Style

	// Overlays.
	Overlay        lipgloss.Style
	OverlayTitle   lipgloss.Style
	SelectedRow    lipgloss.Style
	MatchHighlight lipgloss.Style
	InputPrompt    lipgloss.Style

	// Styles for text drawn *inside* the selected row. A row under the cursor
	// sits on a background of its own, so a subtitle or a badge has to be
	// legible against that rather than against the screen behind it — the
	// muted grey that reads well on the terminal's own background is close
	// enough to the selection to disappear into it.
	//
	// Each carries the selection background itself. A nested style ends with a
	// reset, which otherwise punches a hole in the highlight halfway along the
	// line and leaves the row looking torn.
	SelectedMuted    lipgloss.Style
	SelectedEmphasis lipgloss.Style
	selectedStatus   [4]lipgloss.Style
}

// OnSelection returns the style for a status as drawn inside the selected row.
// Callers rendering a badge or a coloured cell in a row under the cursor use
// this instead of Style, which is mixed for the screen's own background.
func (t *Theme) OnSelection(s Status) lipgloss.Style {
	if s < 0 || int(s) >= len(t.selectedStatus) {
		return t.SelectedMuted
	}
	return t.selectedStatus[s]
}

type palette struct {
	fg, muted, subtle           color.Color
	accent, healthy, warn, crit color.Color
	border, borderFocus         color.Color
	selectedBG, selectedFG      color.Color
	prodBG, prodFG              color.Color
	// The same roles again, mixed for the selection background rather than for
	// the screen's. Only the ones that would fall below the contrast floor
	// differ from their counterparts above; the rest are repeated so that the
	// contrast test has one place to read every pair it has to check.
	selMuted, selAccent          color.Color
	selHealthy, selWarn, selCrit color.Color
}

func darkPalette() palette {
	return palette{
		fg:          lipgloss.Color("#e6e6e6"),
		muted:       lipgloss.Color("#8a8f98"),
		subtle:      lipgloss.Color("#5c626b"),
		accent:      lipgloss.Color("#7aa2f7"),
		healthy:     lipgloss.Color("#4ec9a5"),
		warn:        lipgloss.Color("#e0af68"),
		crit:        lipgloss.Color("#f7768e"),
		border:      lipgloss.Color("#3b4048"),
		borderFocus: lipgloss.Color("#7aa2f7"),
		selectedBG:  lipgloss.Color("#2c3245"),
		selectedFG:  lipgloss.Color("#ffffff"),
		prodBG:      lipgloss.Color("#7d2233"),
		prodFG:      lipgloss.Color("#ffffff"),
		// muted lifted from #8a8f98, which reads at 3.9:1 on the selection and
		// is the grey a subtitle under the cursor was disappearing into.
		selMuted:   lipgloss.Color("#a4abb8"),
		selAccent:  lipgloss.Color("#a8c2ff"),
		selHealthy: lipgloss.Color("#4ec9a5"),
		selWarn:    lipgloss.Color("#e0af68"),
		selCrit:    lipgloss.Color("#f7768e"),
	}
}

func lightPalette() palette {
	return palette{
		fg:          lipgloss.Color("#1f2328"),
		muted:       lipgloss.Color("#6a737d"),
		subtle:      lipgloss.Color("#98a0a8"),
		accent:      lipgloss.Color("#1f5fbf"),
		healthy:     lipgloss.Color("#1a7f4b"),
		warn:        lipgloss.Color("#9a6700"),
		crit:        lipgloss.Color("#c02637"),
		border:      lipgloss.Color("#d0d7de"),
		borderFocus: lipgloss.Color("#1f5fbf"),
		selectedBG:  lipgloss.Color("#dbe6f7"),
		selectedFG:  lipgloss.Color("#10151a"),
		prodBG:      lipgloss.Color("#c02637"),
		prodFG:      lipgloss.Color("#ffffff"),
		// Green and amber are the two that fail against a pale blue selection;
		// both are darkened rather than shifted in hue, so the row still reads
		// as the same colour it does anywhere else on the screen.
		selMuted:   lipgloss.Color("#56606b"),
		selAccent:  lipgloss.Color("#1f5fbf"),
		selHealthy: lipgloss.Color("#0f6338"),
		selWarn:    lipgloss.Color("#7f5500"),
		selCrit:    lipgloss.Color("#c02637"),
	}
}

// New builds a theme for the given capabilities and configured preference.
func New(caps Capabilities, pref config.Theme) *Theme {
	switch pref {
	case config.ThemeDark:
		caps.Dark = true
	case config.ThemeLight:
		caps.Dark = false
	}

	p := lightPalette()
	if caps.Dark {
		p = darkPalette()
	}

	t := &Theme{Caps: caps, Glyphs: GlyphsFor(caps.Unicode)}
	base := lipgloss.NewStyle()

	if !caps.Color && !caps.Attributes {
		// Plain text: a file, a pipe or a CI log. Emit no escape sequences at
		// all, so `correlux doctor > report.txt` stays readable.
		for _, style := range []*lipgloss.Style{
			&t.Base, &t.Panel, &t.PanelTitle, &t.Header, &t.StatusBar, &t.Key, &t.KeyDesc,
			&t.Muted, &t.Emphasis, &t.Title, &t.Healthy, &t.Warning, &t.Critical, &t.Info,
			&t.ContextProd, &t.ContextSafe, &t.Overlay, &t.OverlayTitle, &t.SelectedRow,
			&t.MatchHighlight, &t.InputPrompt, &t.SelectedMuted, &t.SelectedEmphasis,
		} {
			*style = base
		}
		for i := range t.selectedStatus {
			t.selectedStatus[i] = base
		}
		return t
	}

	if !caps.Color {
		// Monochrome: keep structure through weight and borders only.
		t.Base = base
		t.Panel = base.Border(lipgloss.RoundedBorder()).Padding(0, 1)
		t.PanelTitle = base.Bold(true)
		t.Header = base.Bold(true)
		t.StatusBar = base
		t.Key = base.Bold(true)
		t.KeyDesc = base
		t.Muted = base
		t.Emphasis = base.Bold(true)
		t.Title = base.Bold(true)
		t.Healthy = base
		t.Warning = base
		t.Critical = base.Bold(true)
		t.Info = base
		t.ContextProd = base.Bold(true).Underline(true)
		t.ContextSafe = base.Bold(true)
		t.Overlay = base.Border(lipgloss.RoundedBorder()).Padding(0, 1)
		t.OverlayTitle = base.Bold(true)
		t.SelectedRow = base.Reverse(true)
		t.MatchHighlight = base.Underline(true)
		t.InputPrompt = base.Bold(true)
		// Reversed video is the selection here, so everything inside the row
		// has to be reversed too or it reads as a gap rather than as a cell.
		t.SelectedMuted = base.Reverse(true)
		t.SelectedEmphasis = base.Reverse(true).Bold(true)
		for i := range t.selectedStatus {
			t.selectedStatus[i] = base.Reverse(true)
		}
		t.selectedStatus[StatusCritical] = base.Reverse(true).Bold(true)
		return t
	}

	t.Base = base.Foreground(p.fg)
	t.Panel = base.Border(lipgloss.RoundedBorder()).BorderForeground(p.border).Padding(0, 1)
	t.PanelTitle = base.Foreground(p.accent).Bold(true)
	t.Header = base.Foreground(p.fg).Bold(true)
	t.StatusBar = base.Foreground(p.muted)
	t.Key = base.Foreground(p.accent).Bold(true)
	t.KeyDesc = base.Foreground(p.muted)
	t.Muted = base.Foreground(p.muted)
	t.Emphasis = base.Foreground(p.fg).Bold(true)
	t.Title = base.Foreground(p.accent).Bold(true)
	t.Healthy = base.Foreground(p.healthy)
	t.Warning = base.Foreground(p.warn)
	t.Critical = base.Foreground(p.crit)
	t.Info = base.Foreground(p.accent)
	t.ContextProd = base.Foreground(p.prodFG).Background(p.prodBG).Bold(true).Padding(0, 1)
	t.ContextSafe = base.Foreground(p.fg).Bold(true).Padding(0, 1)
	t.Overlay = base.Border(lipgloss.RoundedBorder()).BorderForeground(p.borderFocus).Padding(0, 1)
	t.OverlayTitle = base.Foreground(p.accent).Bold(true)
	t.SelectedRow = base.Foreground(p.selectedFG).Background(p.selectedBG).Bold(true)
	t.MatchHighlight = base.Foreground(p.accent).Bold(true)
	t.InputPrompt = base.Foreground(p.accent).Bold(true)
	onSelection := func(fg color.Color) lipgloss.Style {
		return base.Foreground(fg).Background(p.selectedBG)
	}
	t.SelectedMuted = onSelection(p.selMuted)
	t.SelectedEmphasis = onSelection(p.selAccent).Bold(true)
	t.selectedStatus = [4]lipgloss.Style{
		StatusUnknown:  t.SelectedMuted,
		StatusHealthy:  onSelection(p.selHealthy),
		StatusWarning:  onSelection(p.selWarn),
		StatusCritical: onSelection(p.selCrit),
	}
	return t
}

// StatusStyle maps a severity to its style, and StatusGlyph to its symbol, so
// callers always render the pair together.
type Status int

const (
	StatusUnknown Status = iota
	StatusHealthy
	StatusWarning
	StatusCritical
)

// Style returns the style for a status.
func (t *Theme) Style(s Status) lipgloss.Style {
	switch s {
	case StatusHealthy:
		return t.Healthy
	case StatusWarning:
		return t.Warning
	case StatusCritical:
		return t.Critical
	default:
		return t.Muted
	}
}

// Glyph returns the symbol for a status.
func (t *Theme) Glyph(s Status) string {
	switch s {
	case StatusHealthy:
		return t.Glyphs.Healthy
	case StatusWarning:
		return t.Glyphs.Warning
	case StatusCritical:
		return t.Glyphs.Critical
	default:
		return t.Glyphs.Unknown
	}
}

// Badge renders "<glyph> <text>" in the status colour: never colour alone.
func (t *Theme) Badge(s Status, text string) string {
	return t.Style(s).Render(t.Glyph(s) + " " + text)
}

// Bar draws a proportion as a run of glyphs, width characters wide.
//
// It illustrates a number; it never carries one. Callers print the percentage
// next to it, because a bar is unreadable on a screen reader, in a piped
// capture and to anyone counting blocks — and because a proportion past 100%
// has nowhere left to grow (ADR 9). Anything over the width is drawn full, and
// the printed number is what says by how much.
func (t *Theme) Bar(percent, width int) string {
	if width <= 0 {
		return ""
	}
	filled := 0
	if percent > 0 {
		filled = (percent*width + 50) / 100
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat(t.Glyphs.BarFull, filled) +
		strings.Repeat(t.Glyphs.BarEmpty, width-filled)
}

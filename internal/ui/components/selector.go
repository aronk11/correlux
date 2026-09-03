package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/correlux/internal/ui/theme"
)

// Item is one row in a Selector.
type Item struct {
	// ID identifies the row to the caller when it is chosen.
	ID string
	// Title is the primary label.
	Title string
	// Subtitle is secondary detail, rendered dimmed after the title.
	Subtitle string
	// Right is right-aligned metadata, e.g. "current" or a shortcut.
	Right string
	// Badge is a short marker rendered before the title, e.g. "PROD".
	Badge string
	// BadgeStatus selects the badge's colour and glyph.
	BadgeStatus theme.Status
	// Highlight are rune indices in Title that matched the query.
	Highlight []int
	// Disabled rows are shown but cannot be chosen.
	Disabled bool
	// Note explains a disabled row.
	Note string
}

// FilterFunc narrows and ranks items for a query. Selector ships with a
// fuzzy default; the command palette supplies its own so ranking lives in one
// place.
type FilterFunc func(query string) []Item

// Selector is a filterable, scrollable list: the single interaction pattern
// behind the command palette, the cluster switcher and the namespace switcher.
// One component means one set of keys to learn and one place to fix a bug.
type Selector struct {
	Title       string
	Placeholder string
	// EmptyMessage is shown when the filter matches nothing.
	EmptyMessage string
	// Footer is an optional hint line under the list.
	Footer string

	input    Input
	filter   FilterFunc
	items    []Item
	cursor   int
	offset   int
	viewport int
}

// NewSelector creates a selector driven by filter.
func NewSelector(title, placeholder string, filter FilterFunc) *Selector {
	s := &Selector{
		Title:        title,
		Placeholder:  placeholder,
		EmptyMessage: "No matches.",
		filter:       filter,
	}
	s.input.Placeholder = placeholder
	s.Refresh()
	return s
}

// SetFilter replaces the filter function and re-runs it.
func (s *Selector) SetFilter(f FilterFunc) {
	s.filter = f
	s.Refresh()
}

// Refresh re-runs the filter for the current query, preserving the selected row
// where possible so a background data refresh does not move the user's cursor.
func (s *Selector) Refresh() {
	var selectedID string
	if it, ok := s.Selected(); ok {
		selectedID = it.ID
	}
	if s.filter != nil {
		s.items = s.filter(s.input.Value())
	} else {
		s.items = nil
	}
	s.resetCursor()
	if selectedID != "" {
		for i, it := range s.items {
			if it.ID == selectedID {
				s.cursor = i
				break
			}
		}
	}
	s.clampScroll()
}

// Reset clears the query and selection.
func (s *Selector) Reset() {
	s.input.Reset()
	s.offset = 0
	if s.filter != nil {
		s.items = s.filter("")
	}
	s.resetCursor()
	s.clampScroll()
}

// Query returns the current filter text.
func (s *Selector) Query() string { return s.input.Value() }

// SelectID moves the cursor to the row with the given ID, if present and
// selectable.
func (s *Selector) SelectID(id string) {
	for i, it := range s.items {
		if it.ID == id && !it.Disabled {
			s.cursor = i
			s.clampScroll()
			return
		}
	}
}

// Items returns the currently visible (filtered) items.
func (s *Selector) Items() []Item { return s.items }

// Selected returns the highlighted item.
func (s *Selector) Selected() (Item, bool) {
	if s.cursor < 0 || s.cursor >= len(s.items) {
		return Item{}, false
	}
	return s.items[s.cursor], true
}

// Move shifts the cursor by delta, skipping rows that cannot be chosen and
// clamping at both ends. Lists do not wrap: wrapping makes it easy to overshoot
// and act on the wrong row.
func (s *Selector) Move(delta int) {
	if len(s.items) == 0 || delta == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	for i := 0; i < delta; i++ {
		next := s.nextSelectable(s.cursor+step, step)
		if next < 0 {
			break
		}
		s.cursor = next
	}
	s.clampScroll()
}

// nextSelectable returns the first selectable index at or after from, walking in
// the given direction, or -1 when there is none.
func (s *Selector) nextSelectable(from, step int) int {
	for i := from; i >= 0 && i < len(s.items); i += step {
		if !s.items[i].Disabled {
			return i
		}
	}
	return -1
}

// resetCursor puts the cursor on the first selectable row, so informational
// rows ("loading…", "not permitted") never swallow an Enter press.
func (s *Selector) resetCursor() {
	if idx := s.nextSelectable(0, 1); idx >= 0 {
		s.cursor = idx
		return
	}
	s.cursor = 0
}

// HandleKey routes a keystroke. It returns handled=false for keys the owner
// must interpret (enter, esc, and its own global shortcuts).
func (s *Selector) HandleKey(keystroke, text string) (handled bool) {
	switch keystroke {
	case "up", "ctrl+p":
		s.Move(-1)
		return true
	case "down", "ctrl+n":
		s.Move(1)
		return true
	case "pgup":
		s.Move(-max(s.viewport-1, 1))
		return true
	case "pgdown":
		s.Move(max(s.viewport-1, 1))
		return true
	case "ctrl+home":
		s.Move(-len(s.items))
		return true
	case "ctrl+end":
		s.Move(len(s.items))
		return true
	case "tab":
		s.Move(1)
		return true
	case "shift+tab":
		s.Move(-1)
		return true
	}
	changed, handled := s.input.HandleKey(keystroke, text)
	if changed {
		s.offset = 0
		if s.filter != nil {
			s.items = s.filter(s.input.Value())
		}
		s.resetCursor()
		s.clampScroll()
	}
	return handled
}

// ScrollBy moves the cursor for mouse wheel input.
func (s *Selector) ScrollBy(lines int) { s.Move(lines) }

// ClickRow selects the row at the given y offset inside the list area and
// reports whether the click landed on a selectable row.
func (s *Selector) ClickRow(y int) bool {
	idx := s.offset + y
	if idx < 0 || idx >= len(s.items) {
		return false
	}
	s.cursor = idx
	return !s.items[idx].Disabled
}

func (s *Selector) clampScroll() {
	if s.viewport <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+s.viewport {
		s.offset = s.cursor - s.viewport + 1
	}
	maxOffset := max(len(s.items)-s.viewport, 0)
	s.offset = clampInt(s.offset, 0, maxOffset)
}

// Render draws the selector into a box of the given inner size.
func (s *Selector) Render(t *theme.Theme, width, height int) string {
	var b strings.Builder

	header := t.OverlayTitle.Render(truncate(s.Title, width))
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(s.input.Render(t, width, true))
	b.WriteString("\n")

	used := 2
	footerLines := 0
	if s.Footer != "" {
		footerLines = 1
	}
	s.viewport = max(height-used-footerLines, 1)
	s.clampScroll()

	if len(s.items) == 0 {
		b.WriteString(t.Muted.Render(truncate(s.EmptyMessage, width)))
		for i := 1; i < s.viewport; i++ {
			b.WriteString("\n")
		}
	} else {
		end := min(s.offset+s.viewport, len(s.items))
		for i := s.offset; i < end; i++ {
			if i > s.offset {
				b.WriteString("\n")
			}
			b.WriteString(s.renderRow(t, s.items[i], i == s.cursor, width))
		}
		for i := end - s.offset; i < s.viewport; i++ {
			b.WriteString("\n")
		}
	}

	if s.Footer != "" {
		b.WriteString("\n")
		footer := s.Footer
		if len(s.items) > s.viewport {
			footer += "  " + t.Glyphs.Bullet + " " + positionLabel(s.cursor+1, len(s.items))
		}
		b.WriteString(t.Muted.Render(truncate(footer, width)))
	}
	return b.String()
}

func (s *Selector) renderRow(t *theme.Theme, it Item, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = t.Glyphs.Selected + " "
	}

	// Every span of the row is styled on its own rather than composed plain and
	// wrapped once at the end.
	//
	// A nested style ends with a reset, and a reset cancels the row style the
	// wrap had opened — so a selected row rendered the outer way keeps its
	// highlight up to the first badge and loses it for everything after,
	// including the title. Styling each span, and the padding between them,
	// leaves the background unbroken across the line.
	//
	// The colours differ too: a subtitle mixed for the terminal's background
	// lands within a shade of the selection's and cannot be read on it.
	row, muted, status := t.Base, t.Muted, t.Style(it.BadgeStatus)
	switch {
	case it.Disabled:
		row, muted = t.Muted, t.Muted
	case selected:
		row, muted, status = t.SelectedRow, t.SelectedMuted, t.OnSelection(it.BadgeStatus)
	}

	badge := ""
	if it.Badge != "" {
		badge = status.Render(it.Badge) + row.Render(" ")
	}

	right := it.Right
	if it.Disabled && it.Note != "" {
		right = it.Note
	}
	rightWidth := 0
	if right != "" {
		rightWidth = min(lipgloss.Width(right)+2, max(width/3, 8))
	}

	markerWidth, badgeWidth := lipgloss.Width(marker), lipgloss.Width(badge)
	titleWidth := width - markerWidth - badgeWidth - rightWidth
	if titleWidth < 4 {
		rightWidth = 0
		titleWidth = max(width-markerWidth-badgeWidth, 1)
	}

	title := truncate(it.Title, titleWidth)
	used := markerWidth + badgeWidth + lipgloss.Width(title)
	body := row.Render(marker) + badge + highlightRunes(t, &row, title, it.Highlight, selected)
	if sub := it.Subtitle; sub != "" {
		if remaining := titleWidth - lipgloss.Width(title) - 2; remaining > 3 {
			sub = truncate(sub, remaining)
			body += row.Render("  ") + muted.Render(sub)
			used += 2 + lipgloss.Width(sub)
		}
	}
	// The gap is painted in the row style rather than left blank: unstyled
	// spaces in the middle of a highlighted row are the hole this function
	// exists to avoid.
	body += row.Render(strings.Repeat(" ", max(width-rightWidth-used, 0)))
	if rightWidth > 0 {
		body += muted.Render(padLeft(right, rightWidth))
	}
	// A last clamp for the degenerate widths — a window narrower than the
	// marker and the badge together. The row is one line, whatever happens.
	return truncate(body, width)
}

// highlightRunes emphasises the runes that matched the query. The runes that
// did not match are rendered in the row's own style rather than left bare, so
// the highlight behind them survives the matched runes' resets.
func highlightRunes(t *theme.Theme, row *lipgloss.Style, s string, positions []int, selected bool) string {
	if len(positions) == 0 {
		return row.Render(s)
	}
	set := make(map[int]struct{}, len(positions))
	for _, p := range positions {
		set[p] = struct{}{}
	}
	style := t.MatchHighlight
	if selected {
		style = t.SelectedEmphasis
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if _, ok := set[i]; ok {
			b.WriteString(style.Render(string(r)))
			continue
		}
		b.WriteString(row.Render(string(r)))
	}
	return b.String()
}

func positionLabel(pos, total int) string {
	return itoa(pos) + "/" + itoa(total)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

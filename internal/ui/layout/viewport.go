package layout

// Viewport is a window onto a list that does not fit the screen.
//
// Every scrollable screen in Correlux had the same two problems — which line to
// start drawing at, and where the selection is — and had grown its own answer
// to them. Six of those answers is five too many: they drifted, and the bug
// that came out of the drift (a page that snapped back to the selection on the
// next keypress) took a bug report to find.
//
// The rules, written once:
//
//   - The selection stays on screen. Moving it scrolls just far enough.
//   - Scrolling drags the selection with it, rather than leaving it behind for
//     the next keypress to jump back to.
//   - Nothing scrolls past the last screenful; a view scrolled into empty space
//     looks broken.
//   - When a list has nothing selectable on screen, the keys scroll instead.
type Viewport struct {
	// Offset is the first visible row or line.
	Offset int
	// Cursor is the selected row, or the selected target among the lines.
	Cursor int
}

// Reset puts the viewport back at the top.
func (v *Viewport) Reset() { v.Offset, v.Cursor = 0, 0 }

// MoveCursor moves the selection through a list of rows, scrolling to keep it
// visible.
func (v *Viewport) MoveCursor(delta, count, height int) {
	if count == 0 {
		v.Reset()
		return
	}
	height = usable(height)
	v.Cursor = within(v.Cursor+delta, count-1)
	v.follow(v.Cursor, count, height)
}

// ScrollRows moves the window over a list of rows and drags the selection into
// it.
func (v *Viewport) ScrollRows(delta, count, height int) {
	if count == 0 {
		v.Reset()
		return
	}
	height = usable(height)
	v.Offset = within(v.Offset+delta, max(count-height, 0))
	v.Cursor = within(v.Cursor, count-1)
	if v.Cursor < v.Offset {
		v.Cursor = v.Offset
	}
	if v.Cursor >= v.Offset+height {
		v.Cursor = v.Offset + height - 1
	}
}

// ScrollLines moves the window over rendered lines that have no selection.
func (v *Viewport) ScrollLines(delta, total, height int) {
	height = usable(height)
	v.Offset = within(v.Offset+delta, max(total-height, 0))
}

// MoveTarget moves the selection through the few selectable rows among many
// rendered lines — an application's objects among its events, say.
//
// It scrolls instead of moving when the selection has nowhere to go: when it is
// off screen because the page was scrolled away from it, and when it is already
// at either end. That is what keeps everything below the last selectable row
// reachable.
func (v *Viewport) MoveTarget(delta int, lines map[int]int, count, total, height int) {
	height = usable(height)
	if count == 0 || !v.onScreen(lines, v.Cursor, height) {
		v.ScrollLines(delta, total, height)
		return
	}

	next := within(v.Cursor+delta, count-1)
	if next == v.Cursor {
		v.ScrollLines(delta, total, height)
		return
	}
	v.Cursor = next
	if line, ok := lines[v.Cursor]; ok {
		v.follow(line, total, height)
	}
}

// ScrollTargets moves the window and takes the selection with it, choosing the
// nearest selectable row that is now on screen. When none is, the selection
// stays where it was and the arrows keep scrolling.
func (v *Viewport) ScrollTargets(delta int, lines map[int]int, count, total, height int) {
	height = usable(height)
	v.ScrollLines(delta, total, height)
	if count == 0 {
		return
	}
	v.Cursor = within(v.Cursor, count-1)
	if v.onScreen(lines, v.Cursor, height) {
		return
	}

	first, last, found := 0, 0, false
	for i := 0; i < count; i++ {
		if !v.onScreen(lines, i, height) {
			continue
		}
		if !found {
			first, found = i, true
		}
		last = i
	}
	if !found {
		return
	}
	if line, ok := lines[v.Cursor]; ok && line < v.Offset {
		v.Cursor = first
		return
	}
	v.Cursor = last
}

// KeepCursor puts the selection back on a row after the list changed underneath
// it, and scrolls to show it.
func (v *Viewport) KeepCursor(index, count, height int) {
	if count == 0 {
		v.Reset()
		return
	}
	if index >= 0 {
		v.Cursor = index
	}
	v.MoveCursor(0, count, height)
}

// onScreen reports whether a target's line is inside the window.
func (v *Viewport) onScreen(lines map[int]int, target, height int) bool {
	line, ok := lines[target]
	return ok && line >= v.Offset && line < v.Offset+height
}

// follow scrolls the window just far enough to show a position.
func (v *Viewport) follow(position, total, height int) {
	if position < v.Offset {
		v.Offset = position
	}
	if position >= v.Offset+height {
		v.Offset = position - height + 1
	}
	v.Offset = within(v.Offset, max(total-height, 0))
}

// within keeps a position inside [0, hi]. layout has a clamp already, but that
// one treats a zero bound as "no bound", which is exactly wrong for a list that
// legitimately has one row.
func within(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

// usable turns whatever height the terminal reported into one a viewport can
// divide a list by. A terminal mid-resize reports zero, and a window of zero
// rows would put every position off screen at once.
func usable(height int) int {
	if height < 1 {
		return 1
	}
	return height
}

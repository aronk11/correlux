package layout

import "testing"

// A viewport is pure arithmetic, so these tests state the rules directly: the
// selection stays visible, scrolling takes it along, and nothing runs past
// either end.

func TestTheSelectionStaysOnScreenWhileItMoves(t *testing.T) {
	var v Viewport
	const rows, height = 100, 10

	v.MoveCursor(15, rows, height)
	if v.Cursor != 15 {
		t.Fatalf("cursor = %d, want 15", v.Cursor)
	}
	if v.Offset > v.Cursor || v.Cursor >= v.Offset+height {
		t.Errorf("cursor %d is outside the window %d..%d", v.Cursor, v.Offset, v.Offset+height)
	}

	v.MoveCursor(-15, rows, height)
	if v.Cursor != 0 || v.Offset != 0 {
		t.Errorf("back at the top: cursor %d offset %d", v.Cursor, v.Offset)
	}
}

func TestTheSelectionDoesNotRunPastEitherEnd(t *testing.T) {
	var v Viewport
	v.MoveCursor(500, 10, 5)
	if v.Cursor != 9 {
		t.Errorf("cursor = %d, want the last row", v.Cursor)
	}
	v.MoveCursor(-500, 10, 5)
	if v.Cursor != 0 {
		t.Errorf("cursor = %d, want the first row", v.Cursor)
	}
}

func TestScrollingRowsTakesTheSelectionAlong(t *testing.T) {
	// The bug this rule exists for: scroll away from the selection, press a
	// key, and the page jumps back to it.
	var v Viewport
	const rows, height = 100, 10

	v.ScrollRows(40, rows, height)
	if v.Offset != 40 {
		t.Fatalf("offset = %d, want 40", v.Offset)
	}
	if v.Cursor < v.Offset || v.Cursor >= v.Offset+height {
		t.Errorf("the selection was left behind at %d, window %d..%d",
			v.Cursor, v.Offset, v.Offset+height)
	}

	before := v.Offset
	v.MoveCursor(1, rows, height)
	if v.Offset != before {
		t.Errorf("moving after a scroll jumped the page from %d to %d", before, v.Offset)
	}
}

func TestNothingScrollsPastTheLastScreenful(t *testing.T) {
	var v Viewport
	v.ScrollRows(1000, 30, 10)
	if v.Offset != 20 {
		t.Errorf("offset = %d, want the last screenful of 30 rows at height 10", v.Offset)
	}
	v.ScrollLines(1000, 30, 10)
	if v.Offset != 20 {
		t.Errorf("lines: offset = %d, want 20", v.Offset)
	}
}

func TestAnEmptyListIsAlwaysAtTheTop(t *testing.T) {
	v := Viewport{Offset: 5, Cursor: 3}
	v.MoveCursor(1, 0, 10)
	if v.Offset != 0 || v.Cursor != 0 {
		t.Errorf("viewport = %+v, want the top", v)
	}
}

// targets places three selectable rows among twenty lines.
func targets() map[int]int { return map[int]int{0: 2, 1: 3, 2: 15} }

func TestMovingBetweenTargetsScrollsToShowThem(t *testing.T) {
	var v Viewport
	const total, height = 20, 6

	v.MoveTarget(1, targets(), 3, total, height) // 0 -> 1, both already visible
	if v.Cursor != 1 || v.Offset != 0 {
		t.Fatalf("viewport = %+v, want the second target with no scrolling", v)
	}

	v.MoveTarget(1, targets(), 3, total, height) // 1 -> 2, on line 15
	if v.Cursor != 2 {
		t.Fatalf("cursor = %d, want the third target", v.Cursor)
	}
	if 15 < v.Offset || 15 >= v.Offset+height {
		t.Errorf("line 15 is outside the window %d..%d", v.Offset, v.Offset+height)
	}
}

func TestPastTheLastTargetTheKeysScroll(t *testing.T) {
	// Everything below the last selectable row — the events under an
	// application — has to stay reachable.
	var v Viewport
	const total, height = 40, 6
	v.Cursor = 2

	v.follow(15, total, height)
	before := v.Offset
	v.MoveTarget(1, targets(), 3, total, height)

	if v.Cursor != 2 {
		t.Errorf("cursor = %d, want it to stay on the last target", v.Cursor)
	}
	if v.Offset <= before {
		t.Errorf("offset = %d, want the page to have moved past %d", v.Offset, before)
	}
}

func TestScrollingTargetsPicksTheNearestVisibleOne(t *testing.T) {
	var v Viewport
	const total, height = 40, 6

	// Scroll away from targets 0 and 1 (lines 2 and 3) towards line 15.
	v.ScrollTargets(12, targets(), 3, total, height)
	if v.Cursor != 2 {
		t.Errorf("cursor = %d, want the target that is now on screen", v.Cursor)
	}

	// Scrolling back up leaves the selection below the window, so the nearest
	// one is the last that is visible — not the first.
	v.ScrollTargets(-12, targets(), 3, total, height)
	if v.Cursor != 1 {
		t.Errorf("cursor = %d, want the nearest visible target above it", v.Cursor)
	}
}

func TestAScreenfulWithNoTargetLeavesTheSelectionAlone(t *testing.T) {
	var v Viewport
	const total, height = 40, 4
	v.Cursor = 1

	v.ScrollTargets(25, targets(), 3, total, height) // nothing selectable down there
	if v.Cursor != 1 {
		t.Errorf("cursor = %d, want it left where it was", v.Cursor)
	}
	// And the arrows keep scrolling from there.
	before := v.Offset
	v.MoveTarget(1, targets(), 3, total, height)
	if v.Offset <= before {
		t.Errorf("offset = %d, want scrolling to continue past %d", v.Offset, before)
	}
}

func TestKeepCursorPutsTheSelectionBackAfterTheListChanged(t *testing.T) {
	var v Viewport
	v.MoveCursor(20, 50, 10)

	// The row it was on is now at index 3.
	v.KeepCursor(3, 50, 10)
	if v.Cursor != 3 {
		t.Errorf("cursor = %d, want the row it was following", v.Cursor)
	}
	if 3 < v.Offset || 3 >= v.Offset+10 {
		t.Errorf("row 3 is outside the window %d..%d", v.Offset, v.Offset+10)
	}

	// And when the row is gone, the position is merely clamped.
	v.KeepCursor(-1, 2, 10)
	if v.Cursor > 1 {
		t.Errorf("cursor = %d, want it inside a list of two", v.Cursor)
	}
}

func TestAHeightOfZeroIsTreatedAsOne(t *testing.T) {
	// A terminal mid-resize reports nonsense; nothing here may divide by it or
	// loop forever on it.
	var v Viewport
	v.MoveCursor(5, 10, 0)
	v.ScrollRows(5, 10, 0)
	v.ScrollLines(5, 10, 0)
	v.MoveTarget(1, targets(), 3, 10, 0)
	v.ScrollTargets(1, targets(), 3, 10, 0)
}

// Package layout computes Correlux's screen geometry.
//
// Layout is pure arithmetic on a width and a height: no styles, no I/O, no
// Bubble Tea. That keeps the responsive behaviour — including the awkward
// cases, like an 80x10 terminal or a window dragged to 20 columns — testable
// without rendering anything.
package layout

// Minimum usable terminal size. Below this Correlux shows a "resize me" notice
// rather than drawing a broken frame.
const (
	MinWidth  = 60
	MinHeight = 12
)

// Rect is a screen region in cells.
type Rect struct {
	X, Y          int
	Width, Height int
}

// Empty reports whether the rect has no area.
func (r Rect) Empty() bool { return r.Width <= 0 || r.Height <= 0 }

// Contains reports whether a screen cell falls inside the rect, which is how a
// mouse position becomes the region it landed on.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// Screen is the resolved geometry for one frame.
type Screen struct {
	Width, Height int
	// TooSmall is true when the terminal cannot host the full frame.
	TooSmall bool

	// Header is the whole chrome block at the top: context, breadcrumb,
	// navigation and the rule that closes it.
	Header Rect
	// Nav is the navigation row inside the header, kept separately because it
	// is the only part of the chrome a click can land on.
	Nav    Rect
	Body   Rect
	Status Rect
}

const (
	// The chrome block: context, breadcrumb, navigation — and one rule to
	// close it. The rule costs a row and earns it: without a line between the
	// chrome and the content, a table header reads as a fourth header row.
	headerHeight = 3
	ruleHeight   = 1
	statusHeight = 1
)

// Compute lays out the main frame: the header block and its closing rule, a
// single-line status bar, and everything in between for the body.
func Compute(width, height int) Screen {
	s := Screen{Width: width, Height: height}
	if width < MinWidth || height < MinHeight {
		s.TooSmall = true
		s.Body = Rect{X: 0, Y: 0, Width: max(width, 0), Height: max(height, 0)}
		return s
	}
	s.Header = Rect{X: 0, Y: 0, Width: width, Height: headerHeight + ruleHeight}
	s.Nav = Rect{X: 0, Y: headerHeight - 1, Width: width, Height: 1}
	s.Status = Rect{X: 0, Y: height - statusHeight, Width: width, Height: statusHeight}
	s.Body = Rect{
		X:      0,
		Y:      s.Header.Height,
		Width:  width,
		Height: height - s.Header.Height - statusHeight,
	}
	return s
}

// OverlayOptions describes the desired size of a floating panel.
type OverlayOptions struct {
	// WidthRatio is the fraction of the screen width to aim for (0..1).
	WidthRatio float64
	// HeightRatio is the fraction of the screen height to aim for (0..1).
	HeightRatio float64
	// MinWidth, MaxWidth bound the result; zero means unbounded.
	MinWidth, MaxWidth int
	// MinHeight, MaxHeight bound the result; zero means unbounded.
	MinHeight, MaxHeight int
}

// Overlay centres a floating panel on the screen, clamping it so it always fits
// inside the terminal even when the terminal is tiny.
func Overlay(screen Screen, opts OverlayOptions) Rect {
	w := scale(screen.Width, opts.WidthRatio)
	h := scale(screen.Height, opts.HeightRatio)

	w = clamp(w, opts.MinWidth, opts.MaxWidth)
	h = clamp(h, opts.MinHeight, opts.MaxHeight)

	// Always leave a one-cell frame around the overlay when there is room.
	w = min(w, max(screen.Width-2, 1))
	h = min(h, max(screen.Height-2, 1))
	w = min(w, screen.Width)
	h = min(h, screen.Height)

	return Rect{
		X:      max((screen.Width-w)/2, 0),
		Y:      max((screen.Height-h)/2, 0),
		Width:  max(w, 1),
		Height: max(h, 1),
	}
}

func scale(size int, ratio float64) int {
	if ratio <= 0 {
		return size
	}
	return int(float64(size) * ratio)
}

func clamp(v, lo, hi int) int {
	if lo > 0 && v < lo {
		v = lo
	}
	if hi > 0 && v > hi {
		v = hi
	}
	return v
}

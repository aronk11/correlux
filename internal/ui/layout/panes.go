package layout

// Split is which way the screen is divided once there is more than one pane.
type Split int

const (
	// SplitAuto puts the panes side by side where the terminal is wide enough
	// to read two of them, and above one another where it is not.
	SplitAuto Split = iota
	// SplitRight keeps the first pane on the left and stacks the rest beside it.
	SplitRight
	// SplitBelow keeps the first pane on top and lines the rest up under it.
	SplitBelow
)

const (
	// PaneMinWidth and PaneMinHeight are the smallest pane worth drawing: a
	// name, a status and a reason on a line, and a title over a handful of
	// them. Anything smaller shows less than the pane's own title promises.
	PaneMinWidth  = 34
	PaneMinHeight = 5

	// sideBySideWidth is where SplitAuto stops stacking. Below it two panes
	// next to each other each lose the columns that say why something is
	// broken, which is the one thing the screen is for.
	sideBySideWidth = 120
)

// PaneLayout is where each pane goes, and the lines drawn between them.
type PaneLayout struct {
	// Frames holds one rectangle per pane, in the order they were asked for.
	// The first row of each is its title.
	Frames []Rect
	// Rules are the one-cell-wide vertical lines between panes that sit next
	// to each other.
	Rules []Rect
	// Split is the direction that was used, which is the answer to SplitAuto.
	Split Split
	// OK is false when the body cannot hold that many panes at a readable
	// size. The frames are then not worth drawing, and the caller shows one
	// pane at a time instead.
	OK bool
}

// Panes divides body among n panes: the first keeps one side of the screen to
// itself and the others share the remaining side.
//
// One arrangement rather than a tree of arbitrary splits is deliberate. The
// first pane is where something is picked, and the others show more about it:
// a layout that keeps saying so is one nobody has to arrange by hand.
func Panes(body Rect, n int, split Split) PaneLayout {
	if n <= 1 {
		return PaneLayout{Frames: []Rect{body}, Split: split, OK: !body.Empty()}
	}
	if split == SplitAuto {
		split = SplitBelow
		if body.Width >= sideBySideWidth {
			split = SplitRight
		}
	}
	out := PaneLayout{Split: split, Frames: make([]Rect, 0, n)}
	rest := n - 1

	if split == SplitRight {
		// The list keeps a little less than half: what is beside it — a log,
		// a document — is the part that is cut off first.
		main := (body.Width - 1) * 45 / 100
		side := body.Width - 1 - main
		out.Frames = append(out.Frames, Rect{X: body.X, Y: body.Y, Width: main, Height: body.Height})
		out.Rules = append(out.Rules, Rect{X: body.X + main, Y: body.Y, Width: 1, Height: body.Height})
		y := body.Y
		for i := range rest {
			h := share(body.Height, rest, i)
			out.Frames = append(out.Frames, Rect{X: body.X + main + 1, Y: y, Width: side, Height: h})
			y += h
		}
	} else {
		main := body.Height / 2
		if rest > 1 {
			main = body.Height * 45 / 100
		}
		below := body.Height - main
		out.Frames = append(out.Frames, Rect{X: body.X, Y: body.Y, Width: body.Width, Height: main})
		// The panes below are separated by a rule each, which costs a column.
		usable := body.Width - (rest - 1)
		x := body.X
		for i := range rest {
			w := share(usable, rest, i)
			out.Frames = append(out.Frames, Rect{X: x, Y: body.Y + main, Width: w, Height: below})
			x += w
			if i < rest-1 {
				out.Rules = append(out.Rules, Rect{X: x, Y: body.Y + main, Width: 1, Height: below})
				x++
			}
		}
	}

	out.OK = true
	for _, f := range out.Frames {
		if f.Width < PaneMinWidth || f.Height < PaneMinHeight {
			out.OK = false
		}
	}
	return out
}

// share is the i-th of n parts of total, with the remainder handed to the
// first parts so the parts always add up to total.
func share(total, n, i int) int {
	part := total / n
	if i < total%n {
		part++
	}
	return part
}

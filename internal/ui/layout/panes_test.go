package layout

import "testing"

func TestPanesOneIsTheWholeBody(t *testing.T) {
	body := Rect{X: 0, Y: 4, Width: 100, Height: 30}
	got := Panes(body, 1, SplitAuto)
	if !got.OK || len(got.Frames) != 1 || got.Frames[0] != body || len(got.Rules) != 0 {
		t.Fatalf("one pane must be the body, untouched: %+v", got)
	}
}

func TestPanesAutoFollowsTheWidth(t *testing.T) {
	if got := Panes(Rect{Width: 160, Height: 40}, 2, SplitAuto); got.Split != SplitRight {
		t.Fatalf("a wide terminal must split side by side, got %v", got.Split)
	}
	if got := Panes(Rect{Width: 100, Height: 40}, 2, SplitAuto); got.Split != SplitBelow {
		t.Fatalf("a narrow terminal must stack, got %v", got.Split)
	}
}

// Every cell of the body belongs to exactly one pane or one rule: a gap is a
// column of stale characters, and an overlap is one pane drawing over another.
func TestPanesTileTheBody(t *testing.T) {
	for _, split := range []Split{SplitRight, SplitBelow} {
		for n := 2; n <= 4; n++ {
			for _, body := range []Rect{
				{X: 0, Y: 4, Width: 160, Height: 40},
				{X: 0, Y: 4, Width: 121, Height: 33},
				{X: 3, Y: 2, Width: 80, Height: 24},
			} {
				got := Panes(body, n, split)
				if len(got.Frames) != n {
					t.Fatalf("split %v n=%d: %d frames", split, n, len(got.Frames))
				}
				seen := make(map[[2]int]int)
				for _, r := range append(append([]Rect{}, got.Frames...), got.Rules...) {
					for y := r.Y; y < r.Y+r.Height; y++ {
						for x := r.X; x < r.X+r.Width; x++ {
							seen[[2]int{x, y}]++
						}
					}
				}
				for y := body.Y; y < body.Y+body.Height; y++ {
					for x := body.X; x < body.X+body.Width; x++ {
						if seen[[2]int{x, y}] != 1 {
							t.Fatalf("split %v n=%d body %+v: cell %d,%d covered %d times",
								split, n, body, x, y, seen[[2]int{x, y}])
						}
					}
				}
				if len(seen) != body.Width*body.Height {
					t.Fatalf("split %v n=%d: panes reach outside the body", split, n)
				}
			}
		}
	}
}

func TestPanesSayWhenTheyDoNotFit(t *testing.T) {
	if got := Panes(Rect{Width: 60, Height: 20}, 2, SplitRight); got.OK {
		t.Fatal("two panes beside each other in 60 columns are not readable")
	}
	if got := Panes(Rect{Width: 60, Height: 20}, 2, SplitBelow); !got.OK {
		t.Fatal("two panes above each other in 20 rows are readable")
	}
	if got := Panes(Rect{Width: 160, Height: 12}, 4, SplitRight); got.OK {
		t.Fatal("three panes stacked in 12 rows are not readable")
	}
}

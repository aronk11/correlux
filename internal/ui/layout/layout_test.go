package layout

import "testing"

func TestComputeFillsTheScreen(t *testing.T) {
	s := Compute(120, 40)
	if s.TooSmall {
		t.Fatal("120x40 must be usable")
	}
	if s.Header.Height+s.Body.Height+s.Status.Height != 40 {
		t.Errorf("regions must tile the screen exactly: %+v", s)
	}
	if s.Body.Y != s.Header.Height {
		t.Errorf("body must start below the header: %+v", s)
	}
	if s.Status.Y != 39 {
		t.Errorf("status bar must sit on the last row, got %d", s.Status.Y)
	}
}

func TestComputeMarksTinyTerminals(t *testing.T) {
	for _, size := range [][2]int{{MinWidth - 1, MinHeight}, {MinWidth, MinHeight - 1}, {0, 0}, {-5, 3}} {
		s := Compute(size[0], size[1])
		if !s.TooSmall {
			t.Errorf("Compute(%d, %d) must be marked too small", size[0], size[1])
		}
		if s.Body.Width < 0 || s.Body.Height < 0 {
			t.Errorf("Compute(%d, %d) produced a negative region: %+v", size[0], size[1], s)
		}
	}
}

func TestOverlayStaysInsideTheScreen(t *testing.T) {
	opts := OverlayOptions{WidthRatio: 0.7, HeightRatio: 0.6, MinWidth: 44, MaxWidth: 96, MinHeight: 8, MaxHeight: 22}

	for _, size := range [][2]int{{200, 60}, {120, 40}, {80, 24}, {60, 12}, {40, 10}, {20, 6}} {
		screen := Compute(size[0], size[1])
		screen.Width, screen.Height = size[0], size[1]
		r := Overlay(screen, opts)

		if r.X < 0 || r.Y < 0 {
			t.Errorf("%dx%d: overlay starts off-screen: %+v", size[0], size[1], r)
		}
		if r.X+r.Width > size[0] || r.Y+r.Height > size[1] {
			t.Errorf("%dx%d: overlay overflows the screen: %+v", size[0], size[1], r)
		}
		if r.Width < 1 || r.Height < 1 {
			t.Errorf("%dx%d: overlay has no area: %+v", size[0], size[1], r)
		}
	}
}

func TestOverlayIsCentred(t *testing.T) {
	screen := Compute(100, 40)
	r := Overlay(screen, OverlayOptions{WidthRatio: 0.5, HeightRatio: 0.5})
	if left, right := r.X, screen.Width-(r.X+r.Width); abs(left-right) > 1 {
		t.Errorf("overlay is not horizontally centred: left=%d right=%d", left, right)
	}
	if top, bottom := r.Y, screen.Height-(r.Y+r.Height); abs(top-bottom) > 1 {
		t.Errorf("overlay is not vertically centred: top=%d bottom=%d", top, bottom)
	}
}

func TestRectEmpty(t *testing.T) {
	if !(Rect{Width: 0, Height: 5}).Empty() {
		t.Error("a zero-width rect is empty")
	}
	if (Rect{Width: 3, Height: 2}).Empty() {
		t.Error("a 3x2 rect is not empty")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

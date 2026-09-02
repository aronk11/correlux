package components

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aronk11/kubeui/internal/ui/theme"
)

// HeaderData is everything the header renders. It is plain data: the header has
// no idea what a Kubernetes client is.
type HeaderData struct {
	Context    string
	Production bool
	Scope      string
	ConnStatus theme.Status
	// ConnGlyph overrides the status glyph, e.g. to show progress rather than
	// an unknown state while the first probe is in flight.
	ConnGlyph  string
	ConnLabel  string
	ConnDetail string
	Breadcrumb []string
	Version    string
	// Auto names the timed reload when it is running ("auto 10s"). It is on the
	// header rather than in a menu because a screen that changes on its own has
	// to say so.
	Auto string
}

// RenderHeader draws the two-line header: identity on top, position below.
//
// The context is always visible and a production context is always marked, in
// text as well as colour — the single most important piece of information on
// screen is which cluster the next keystroke will hit.
func RenderHeader(t *theme.Theme, d HeaderData, width int) string {
	badge := d.Context
	if badge == "" {
		badge = "no context"
	}
	if d.Production {
		badge = t.ContextProd.Render(t.Glyphs.Prod + " PROD " + badge)
	} else {
		badge = t.ContextSafe.Render(badge)
	}

	scope := t.Muted.Render("scope ") + t.Emphasis.Render(orDash(d.Scope))

	conn := t.Badge(d.ConnStatus, d.ConnLabel)
	if d.ConnGlyph != "" {
		conn = t.Style(d.ConnStatus).Render(d.ConnGlyph + " " + d.ConnLabel)
	}
	if d.ConnDetail != "" {
		conn += t.Muted.Render(" " + d.ConnDetail)
	}

	left := strings.Join([]string{badge, scope, conn}, t.Muted.Render("  "+t.Glyphs.Bullet+"  "))
	right := t.Muted.Render(d.Version)
	if d.Auto != "" {
		right = t.Info.Render(d.Auto) + t.Muted.Render("  "+d.Version)
	}

	line1 := joinEnds(left, right, width)

	crumbs := make([]string, 0, len(d.Breadcrumb))
	for i, c := range d.Breadcrumb {
		if i == len(d.Breadcrumb)-1 {
			crumbs = append(crumbs, t.Emphasis.Render(c))
			continue
		}
		crumbs = append(crumbs, t.Muted.Render(c))
	}
	line2 := strings.Join(crumbs, t.Muted.Render(" "+t.Glyphs.Arrow+" "))
	line2 = truncate(line2, width)

	return line1 + "\n" + pad(line2, width)
}

// joinEnds places left and right on one line of the given width, dropping the
// right-hand side when there is no room for it.
func joinEnds(left, right string, width int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if rw == 0 || lw+rw+2 > width {
		return pad(left, width)
	}
	return left + fill(width-lw-rw) + right
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

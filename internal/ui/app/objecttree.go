package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/tree"
)

// The YAML view is a tree: a large object is something to walk to the part
// that matters, not something to scroll past. Which nodes are open is kept by
// path, so a refresh that brings a new document keeps the reader where they
// were.

// treeState is the tree of the open object.
type treeState struct {
	// document is what roots were parsed from, so a frame does not parse it
	// again.
	document string
	roots    []*tree.Node
	// open overrides tree.DefaultOpen for the paths the user folded or
	// unfolded.
	open map[string]bool
	// cursor is the path under the cursor.
	cursor string
}

func (m *Model) resetTree() { m.tree = treeState{} }

// treeRoots parses the document on screen, once per document.
func (m *Model) treeRoots(document string) []*tree.Node {
	if m.tree.document != document || m.tree.roots == nil {
		m.tree.document = document
		m.tree.roots = tree.Parse(document)
	}
	return m.tree.roots
}

func (m *Model) treeOpen(n *tree.Node) bool {
	if open, ok := m.tree.open[n.Path]; ok {
		return open
	}
	return tree.DefaultOpen(n)
}

func (m *Model) setTreeOpen(n *tree.Node, open bool) {
	if m.tree.open == nil {
		m.tree.open = map[string]bool{}
	}
	m.tree.open[n.Path] = open
}

// treeVisible is the nodes on screen and the index of the cursor among them.
// A cursor whose node went away, folded into a parent or removed by a reload,
// lands on its nearest visible ancestor.
func (m *Model) treeVisible() ([]*tree.Node, int) {
	visible := tree.Visible(m.tree.roots, m.treeOpen)
	if len(visible) == 0 {
		return nil, 0
	}
	at := map[string]int{}
	for i, n := range visible {
		at[n.Path] = i
	}
	if i, ok := at[m.tree.cursor]; ok {
		return visible, i
	}
	for n := tree.Find(m.tree.roots, m.tree.cursor); n != nil; n = n.Parent {
		if i, ok := at[n.Path]; ok {
			m.tree.cursor = n.Path
			return visible, i
		}
	}
	m.tree.cursor = visible[0].Path
	return visible, 0
}

// treeLines is the tree as the screen draws it.
func (m *Model) treeLines(document string) ([]screens.TreeLine, int) {
	if len(m.treeRoots(document)) == 0 {
		return nil, 0
	}
	visible, cursor := m.treeVisible()
	lines := make([]screens.TreeLine, len(visible))
	for i, n := range visible {
		lines[i] = screens.TreeLine{
			Depth: n.Depth, Branch: n.Branch(), Open: n.Branch() && m.treeOpen(n),
			Key: n.Key, Value: n.Value, Summary: n.Summary,
		}
	}
	return lines, cursor
}

// treeActive reports whether the object view is showing the tree.
func (m *Model) treeActive() bool {
	if !m.objectYAML {
		return false
	}
	d, _ := m.objectView()
	return len(d.Tree) > 0
}

// handleTreeKey walks the tree: up and down move, right opens or steps in,
// left folds or steps out, space toggles, + and - open or fold everything.
func (m *Model) handleTreeKey(keystroke string) (tea.Cmd, bool) {
	visible, cursor := m.treeVisible()
	if len(visible) == 0 {
		return nil, false
	}
	page := max(m.screen.Body.Height-1, 1)
	move := func(i int) {
		m.tree.cursor = visible[clampInt(i, len(visible)-1)].Path
	}
	n := visible[cursor]
	switch keystroke {
	case "up", "k":
		move(cursor - 1)
	case "down", "j":
		move(cursor + 1)
	case "pgup":
		move(cursor - page)
	case "pgdown":
		move(cursor + page)
	case "home", "g":
		move(0)
	case "end", "G":
		move(len(visible) - 1)
	case "right", "enter":
		switch {
		case n.Branch() && !m.treeOpen(n):
			m.setTreeOpen(n, true)
		case n.Branch():
			m.tree.cursor = n.Children[0].Path
		}
	case "left", "h":
		switch {
		case n.Branch() && m.treeOpen(n):
			m.setTreeOpen(n, false)
		case n.Parent != nil:
			m.tree.cursor = n.Parent.Path
		default:
			return m.backFromObject(), true
		}
	case " ":
		if n.Branch() {
			m.setTreeOpen(n, !m.treeOpen(n))
		}
	case "+", "=":
		tree.Walk(m.tree.roots, func(b *tree.Node) {
			if b.Branch() {
				m.setTreeOpen(b, true)
			}
		})
	case "-":
		tree.Walk(m.tree.roots, func(b *tree.Node) {
			if b.Branch() {
				m.setTreeOpen(b, false)
			}
		})
	default:
		return nil, false
	}
	m.followTreeCursor()
	return nil, true
}

// followTreeCursor scrolls just enough to keep the cursor on screen, and to
// the very top when it is on the first node, so the title is not lost.
func (m *Model) followTreeCursor() {
	d, _ := m.objectView()
	if len(d.Tree) == 0 {
		return
	}
	height := max(m.bodyHeight(), 1)
	line := d.TreeCursorLine(m.screen.Body.Width)
	switch {
	case d.TreeCursor == 0:
		m.objectPort.Offset = 0
	case line < m.objectPort.Offset:
		m.objectPort.Offset = line
	case line >= m.objectPort.Offset+height:
		m.objectPort.Offset = line - height + 1
	}
}

// clickTree puts the cursor on the node that was clicked and toggles it.
func (m *Model) clickTree(row int) tea.Cmd {
	d, _ := m.objectView()
	start := d.TreeCursorLine(m.screen.Body.Width) - d.TreeCursor
	i := m.objectPort.Offset + row - start
	visible, _ := m.treeVisible()
	if i < 0 || i >= len(visible) {
		return nil
	}
	n := visible[i]
	m.tree.cursor = n.Path
	if n.Branch() {
		m.setTreeOpen(n, !m.treeOpen(n))
	}
	return nil
}

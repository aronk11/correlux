package app

import (
	"context"
	"reflect"

	tea "charm.land/bubbletea/v2"

	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/logs"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/components"
	"github.com/aronk11/correlux/internal/ui/layout"
	"github.com/aronk11/correlux/internal/ui/screens"
)

// pane is one window onto the cluster: the view it shows, where that view is
// scrolled to, and the remote values only it is looking at.
//
// What a session knows about the cluster as a whole — the applications, the
// evidence, the catalog — stays on the Model and is shared: two panes on one
// cluster must never disagree about what is deployed there. What differs from
// one pane to the next is which part of it each one has open (ADR 33).
type pane struct {
	// id addresses the pane in a message. It is never reused, so an answer for
	// a pane that has since been closed cannot land in the one that took its
	// place.
	id int
	// frame is everything the pane draws into; body is frame without the
	// title row a pane has once there is more than one of them.
	frame, body layout.Rect
	// follow is what a companion pane shows of whatever is selected in the
	// pane beside it; followNone is a pane that is navigated on its own.
	follow followKind
	// pinned holds a companion on what it shows now, whatever is selected
	// next door.
	pinned bool
	// subject names what a companion was last pointed at, so moving the cursor
	// along a row it already shows does not restart anything.
	subject string
	// subjectLabel is the same thing, for a person to read.
	subjectLabel string
	// blank is what a companion says when the selection gives it nothing to
	// show: a ConfigMap has no logs, and a pane that stayed empty would read
	// as one still loading.
	blank string
	// logSuspended marks a log whose stream was stopped because its tab went
	// out of sight, and is to be opened again when the tab is back.
	logSuspended bool

	table  async.Value[*resources.Table]
	object async.Value[*resources.Object]
	// One viewport per scrollable screen. The rules they follow live in
	// layout.Viewport, written once (ADR 4: arithmetic below the UI).
	appPort        layout.Viewport
	tablePort      layout.Viewport
	detailPort     layout.Viewport
	objectPort     layout.Viewport
	whyPort        layout.Viewport
	logPort        layout.Viewport
	usagePort      layout.Viewport
	activityPort   layout.Viewport
	fleetPort      layout.Viewport
	fleetTablePort layout.Viewport
	helpPort       layout.Viewport
	// savedViewports saves scroll positions when leaving a view, restoring them
	// on return: land where the user left off (ADR 25).
	savedViewports map[viewKind]layout.Viewport
	// usageDrilledIn records that the active namespace was entered from the
	// cluster-wide usage screen, which is what Esc walks back out of there. A
	// session that simply started in a namespace has nothing to walk back to,
	// and Esc must leave the screen the way it does everywhere else.
	usageDrilledIn bool
	// selectedApp is the application the detail view shows, addressed by key
	// rather than by index: a reload re-sorts the list, and a cursor position
	// would then follow the ranking instead of the application.
	selectedApp string
	// groupingShown toggles the section that explains why each object landed
	// in this application: which signal decided it, and how sure that signal
	// is (ADR 16 — correlation must be explainable).
	groupingShown bool
	// whyFrom is the view Diagnosis was opened from, so Esc returns there
	// rather than to a fixed screen — the dashboard when it was opened from
	// there directly, the application otherwise (ADR 23).
	whyFrom viewKind
	// whySubject is the application whyPort's scroll was last reset for. It is
	// its own field rather than a comparison against selectedApp because
	// explainApplication sets selectedApp (via openApplication) before it
	// calls explain — comparing against selectedApp there would always read
	// the new value, never the old one.
	whySubject string
	// activityFrom is the same idea for Events (ADR 23).
	activityFrom viewKind
	// The object inspector.
	objectTarget objectRef
	// objectTrail is the way back out: opening an owner or a child pushes the
	// object it was opened from, so Esc retraces the path instead of dumping
	// the user at the top.
	objectTrail []objectRef
	// objectFrom is the view the inspector was opened from, so Esc returns
	// there rather than to a fixed screen.
	objectFrom viewKind
	objectYAML bool
	// objectDecode shows the document's base64 values as what they decode to.
	// It changes what is drawn and nothing else: an edit still sends the
	// document the server holds.
	objectDecode bool
	// tree is the YAML view's document as a foldable tree.
	tree treeState
	// The log view. The stream lives as long as the view does; leaving it
	// cancels the context and the connections go with it.
	logFrom       viewKind // return to the investigation that opened logs
	logTitle      string
	logTargets    []logs.Source
	logLines      []logs.Line
	logStream     <-chan logs.Event
	logGeneration uint64
	logFollow     bool
	logTimestamps bool
	logPrevious   bool
	logWrap       bool
	logClosed     bool
	logDropped    int
	logFailed     []string
	logErr        error
	// The resource browser.
	view        viewKind
	resource    kubediscovery.Resource
	tableWide   screens.WideMode
	loadingMore bool
	// The order each table is in. They are kept apart on purpose: ordering the
	// dashboard by restarts says nothing about how somebody wants a list of
	// nodes, and one screen quietly reordering another is a change nobody
	// asked for.
	appSort tableSort
	// appGroup is what the dashboard is grouped by, if anything.
	appGroup  groupMode
	tableSort tableSort
	fleetSort tableSort
	// frameTable caches the table the frame being rendered draws, so the body,
	// the status bar and the palette do not each filter and sort every row
	// again. It is valid for one View and cleared around it.
	frameTable    *screens.TableData
	frameCaching  bool
	objectLoading bool
	tableLoading  bool
	cancelTable   context.CancelFunc
	cancelObject  context.CancelFunc
	cancelLogs    context.CancelFunc
	// The filter over whatever list is on screen.
	search    components.Input
	searching bool
}

// followKind is what a companion pane shows of the selection next door.
type followKind int

const (
	// followNone is a pane with a mind of its own.
	followNone followKind = iota
	followLogs
	followWhy
	followDetails
	followYAML
	followEvents
)

// newPane returns a pane on the dashboard, which is where every pane starts.
func newPane(id int) *pane {
	return &pane{
		id:             id,
		savedViewports: make(map[viewKind]layout.Viewport),
		// The placeholder is where the comparison syntax is discovered:
		// nobody reads a help screen to find out that a filter can do
		// arithmetic.
		search: newInput("filter…  or restarts>5, age<1h, ns=shop"),
	}
}

// paneMsg is an answer addressed to one pane. A message without the envelope
// belongs to the first pane, which is the only one a session without splits
// ever has.
type paneMsg struct {
	msg  tea.Msg
	pane int
}

// activate makes p the pane every promoted field and every geometry question
// refers to.
func (m *Model) activate(p *pane) {
	m.pane = p
	if !p.body.Empty() {
		m.screen.Body = p.body
	}
}

// withPane runs fn as if p had the focus and then puts things back.
func (m *Model) withPane(p *pane, fn func()) {
	previous, body := m.pane, m.screen.Body
	if p == previous {
		fn()
		return
	}
	m.activate(p)
	fn()
	// Not simply the saved rectangle: fn may have laid the screen out again,
	// and the pane coming back has to see where it now is.
	m.pane, m.screen.Body = previous, body
	if !previous.body.Empty() {
		m.screen.Body = previous.body
	}
}

// inPane runs fn as if p had the focus, and addresses whatever fn starts to p,
// so the answer comes back to the pane that asked.
func (m *Model) inPane(p *pane, fn func() tea.Cmd) tea.Cmd {
	if p == m.pane {
		return tagCmd(p.id, fn())
	}
	var cmd tea.Cmd
	m.withPane(p, func() { cmd = fn() })
	m.paneTouched = true
	return tagCmd(p.id, cmd)
}

// eachPane runs fn once for every pane with that pane active. It is for the
// bookkeeping shared data needs done in each window onto it.
func (m *Model) eachPane(fn func()) {
	for _, p := range m.allPanes() {
		m.withPane(p, fn)
	}
}

// allPanes lists every pane the session holds, in every tab, on screen or
// not.
func (m *Model) allPanes() []*pane {
	if len(m.tabs) < 2 {
		return m.panes
	}
	var all []*pane
	for i, t := range m.tabs {
		if i == m.tabIndex {
			all = append(all, m.panes...)
			continue
		}
		all = append(all, t.panes...)
	}
	return all
}

// paneByID finds a pane that still exists.
func (m *Model) paneByID(id int) *pane {
	for _, p := range m.allPanes() {
		if p.id == id {
			return p
		}
	}
	return nil
}

// tagCmd addresses the messages cmd produces to pane id. The first pane's go
// out bare: a session that never splits sends exactly the messages it always
// did.
func tagCmd(id int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil || id == 0 {
		return cmd
	}
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case nil:
			return nil
		case paneMsg:
			return msg
		case tea.BatchMsg:
			out := make(tea.BatchMsg, 0, len(msg))
			for _, c := range msg {
				if c != nil {
					out = append(out, tagCmd(id, c))
				}
			}
			return out
		default:
			if !ownMsg(msg) {
				// Bubble Tea's own messages — quit, hand over the terminal,
				// ask for the background colour — are for the program.
				return msg
			}
			return paneMsg{pane: id, msg: msg}
		}
	}
}

var ownPkg = reflect.TypeOf(paneMsg{}).PkgPath()

// ownMsg reports whether msg is one of Correlux's.
func ownMsg(msg tea.Msg) bool {
	t := reflect.TypeOf(msg)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t != nil && t.PkgPath() == ownPkg
}

// paneScoped reports whether msg carries a value only the pane that asked for
// it can use. An answer like that is dropped when its pane is gone; anything
// else is about the session and is handled by whichever pane has the focus.
func paneScoped(msg tea.Msg) bool {
	switch msg.(type) {
	case tableLoadedMsg, objectLoadedMsg, logsStartedMsg, logBatchMsg:
		return true
	}
	return false
}

package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/domain/usage"
	"github.com/aronk11/correlux/internal/kube/workloads"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// usageBarWidth is how many characters a proportion is drawn in. Six is enough
// to see a difference at a glance and narrow enough that four of them fit next
// to a node name on eighty columns.
const usageBarWidth = 6

// maxUsageNodes and maxUsageApps bound the two lists. A thousand-node cluster
// is a scrolling problem rather than a reading one, and the rows worth reading
// sort first.
const (
	maxUsageNodes      = 100
	maxUsageNamespaces = 100
	maxUsageApps       = 100
)

// openUsage shows where the pods are and what they use. Pressing the key again
// returns to the dashboard, the way the fleet key does.
func (m *Model) openUsage() tea.Cmd {
	if m.view == viewUsage {
		return m.backToApplications()
	}
	m.view = viewUsage
	m.usagePort.Offset, m.usagePort.Cursor = 0, 0
	m.usageDrilledIn = false
	m.rebuildCommands()

	cmds := []tea.Cmd{m.loadUsage()}
	if m.apps.State() == async.Idle {
		// The pods, and with them every request and limit on this screen, come
		// from the dashboard's snapshot. Opening the view before the dashboard
		// has loaded is normal: from the palette, or with a key on the first
		// frame.
		cmds = append(cmds, m.loadApplications())
	}
	return tea.Batch(cmds...)
}

// usageSubtitleForPalette says what the command will show, and — once it has
// been shown at least once — whether the numbers behind it are live.
func (m *Model) usageSubtitleForPalette() string {
	if m.usage.State() != async.Ready {
		return "where the pods are, and what they use against what they asked for"
	}
	report := m.usage.Get()
	return itoa(report.Totals.Nodes) + " " + plural(report.Totals.Nodes, "node") + ", " +
		metricsPhrase(report.Metrics, time.Now())
}

// loadUsage fetches the two things the dashboard's snapshot does not carry:
// the machines, and whatever the metrics API is willing to say.
//
// It is deliberately not part of the dashboard's load. That one runs on a timer
// over every scope; this runs when somebody opens the usage view (ADR 6).
func (m *Model) loadUsage() tea.Cmd {
	if m.cancelUsage != nil {
		m.cancelUsage()
	}
	gen := m.usage.Start()
	m.usageLoading = true

	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelUsage = cancel

	factory := m.factory
	name := m.contextName
	opts := workloads.Options{}
	if !m.allNamespaces {
		opts.Namespace = m.namespace
	}

	return func() tea.Msg {
		defer cancel()
		live, err := factory.Usage(ctx, name, opts)
		return usageLoadedMsg{gen: gen, live: live, err: err}
	}
}

// applyUsage stores what the cluster said and rolls it up.
func (m *Model) applyUsage(msg usageLoadedMsg) {
	if m.usage.Accepts(msg.gen) {
		m.usageLoading = false
	}
	if msg.err != nil {
		if m.usage.Fail(msg.gen, msg.err) {
			m.refreshFailures++
		}
		return
	}
	if !m.usage.Accepts(msg.gen) {
		return
	}
	m.usageLive = msg.live
	m.refreshFailures = 0
	m.usage.Succeed(msg.gen, m.buildUsage())
}

// resetUsage forgets the reading. A cluster or a scope the numbers were not
// measured in must never be described by them.
func (m *Model) resetUsage() {
	if m.cancelUsage != nil {
		m.cancelUsage()
		m.cancelUsage = nil
	}
	m.usage.Reset()
	m.usageLive = usage.Live{}
	m.usagePort.Offset, m.usagePort.Cursor = 0, 0
}

// recomputeUsage rolls the same live reading up against a newer snapshot. The
// dashboard reloads on its own timer, and a usage view still counting the pods
// of two minutes ago would be worse than one that says nothing.
func (m *Model) recomputeUsage() {
	if m.usage.State() != async.Ready {
		return
	}
	m.usage.Succeed(m.usage.Generation(), m.buildUsage())
}

// buildUsage does the rollup once per load rather than once per frame: View has
// to stay a cheap pure function, and a scope can hold ten thousand pods.
func (m *Model) buildUsage() usage.Report {
	list := m.apps.Get()
	return usage.Build(m.usageLive, list.Snapshot, list.Apps)
}

// usageData assembles the view.
func (m *Model) usageData() screens.UsageData {
	d, _ := m.usageView()
	return d
}

// usageView assembles the screen and its drill-down targets in the same pass.
// Namespace rows exist only cluster-wide; application rows exist only after a
// namespace has been selected, keeping the first screen useful at 80 columns
// and avoiding a flat list of hundreds of applications.
func (m *Model) usageView() (screens.UsageData, []objectRef) {
	d := screens.UsageData{
		Title: "Resource usage in " + m.scopeLabel(), Offset: m.usagePort.Offset,
		Selected: m.usagePort.Cursor,
	}

	// The pods come from one request and the machines from another, and a
	// screen that cannot say which of them it is still waiting for is a screen
	// that reads as broken (ADR 5).
	switch {
	case m.apps.State() == async.Idle, m.apps.State() == async.Loading:
		d.Message = "Looking for pods in " + m.scopeLabel() + "…"
		return d, nil
	case m.apps.State() == async.Failed:
		d.Message = "Could not read " + m.scopeLabel() + ": " + shortError(m.apps.Err())
		d.MessageStatus = theme.StatusCritical
		return d, nil
	case m.usage.State() == async.Idle, m.usage.State() == async.Loading:
		d.Message = "Measuring " + m.scopeLabel() + "…"
		return d, nil
	case m.usage.State() == async.Failed:
		d.Message = "Could not read the nodes: " + shortError(m.usage.Err())
		d.MessageStatus = theme.StatusCritical
		return d, nil
	}

	report := m.usage.Get()
	d.Subtitle = m.usageSubtitle(&report, time.Now())
	d.Notes = m.usageNotes(&report)

	sections := []detailSection{m.usageNodeSection(&report), m.usageUnscheduledSection(&report)}
	if m.allNamespaces {
		sections = append([]detailSection{m.usageNamespaceSection(&report)}, sections...)
	} else {
		sections = append(sections, m.usageApplicationSection(&report))
	}
	sections = append(sections, m.usageTotalsSection(&report))
	rendered, targets := numberTargets(sections)
	d.Sections = rendered
	return d, targets
}

// usageNamespaceSection is the cluster-wide entry point. Namespace resource
// use is shown as absolute values: there is no honest percentage without a
// ResourceQuota, and a node's allocatable is shared by every namespace.
func (m *Model) usageNamespaceSection(report *usage.Report) detailSection {
	live := report.Metrics.Available
	section := detailSection{
		Title: "Namespaces", Empty: "no namespace is running a pod",
		Columns: []string{"Namespace", "Applications", "Pods", "Nodes", cpuHeading(live), memoryHeading(live)},
	}
	for i := range report.Namespaces {
		if i >= maxUsageNamespaces {
			break
		}
		ns := &report.Namespaces[i]
		row := detailRow{
			Cells: []string{
				ns.Name, itoa(ns.Apps), itoa(ns.Pods), itoa(len(ns.Nodes)),
				triple(live, ns.Used, ns.Requests, ns.Limits, cpuOf),
				triple(live, ns.Used, ns.Requests, ns.Limits, memoryOf),
			},
			Ref: objectRef{Kind: "Namespace", Name: ns.Name},
		}
		if ns.Unscheduled > 0 || ns.Unsized == ns.Pods {
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	if extra := len(report.Namespaces) - maxUsageNamespaces; extra > 0 {
		section.Rows = append(section.Rows, detailRow{Cells: []string{
			m.theme.Glyphs.Ellipsis + " " + itoa(extra) + " more namespaces",
		}})
	}
	return section
}

// usageSubtitle answers the first question in one line: how many machines, how
// many pods, and whether any number below it is live.
func (m *Model) usageSubtitle(report *usage.Report, now time.Time) string {
	pods := itoa(report.Totals.Pods) + " " + plural(report.Totals.Pods, "pod")
	if slots := report.Totals.Allocatable.Pods; slots > 0 {
		pods += " of " + itoa64(slots) + " slots"
	}
	parts := []string{
		itoa(report.Totals.Nodes) + " " + plural(report.Totals.Nodes, "node"),
		pods,
	}
	if report.Totals.Unscheduled > 0 {
		parts = append(parts, itoa(report.Totals.Unscheduled)+" not scheduled")
	}
	return strings.Join(append(parts, metricsPhrase(report.Metrics, now)), "   ")
}

// metricsPhrase says where the live numbers came from, or plainly that there
// are none. Metrics are optional (SPEC 23), and the distance between "not
// installed" and "zero" is the whole reason to say it out loud.
func metricsPhrase(mx usage.Metrics, now time.Time) string {
	if !mx.Available {
		reason := mx.Reason
		if reason == "" {
			reason = "the metrics API did not answer"
		}
		return "no live usage — " + reason
	}
	phrase := "live usage"
	if !mx.At.IsZero() {
		phrase += " measured " + formatAge(mx.At, now) + " ago"
	}
	if mx.Window > 0 {
		phrase += " over " + mx.Window.String()
	}
	return phrase
}

// usageNotes qualify every number below them.
func (m *Model) usageNotes(report *usage.Report) []string {
	notes := append([]string(nil), report.Notes...)
	if !report.Metrics.Available {
		return append(notes, "Requests, limits and capacity below come from the pod specs "+
			"and the nodes themselves; none of them needs Metrics Server.")
	}
	if unmeasured := report.Totals.Pods - report.Totals.Measured; unmeasured > 0 {
		notes = append(notes, itoa(unmeasured)+" "+plural(unmeasured, "pod")+
			" had no live sample, so every used total covering one is a floor.")
	}
	return notes
}

// usageNodeSection answers the first question: where are the pods, and how full
// is each machine they landed on.
func (m *Model) usageNodeSection(report *usage.Report) detailSection {
	live := report.Metrics.Available
	columns := []string{"Node", "State", "Pods"}
	if live {
		columns = append(columns, "CPU used")
	}
	columns = append(columns, "CPU requested")
	if live {
		columns = append(columns, "Mem used")
	}
	columns = append(columns, "Mem requested")

	section := detailSection{Title: "Nodes", Columns: columns, Empty: m.nodesEmpty()}
	for i := range report.Nodes {
		if i >= maxUsageNodes {
			break
		}
		n := &report.Nodes[i]
		state, status := nodeCondition(&n.Node)
		cells := []string{
			n.Node.Name,
			m.theme.Glyph(status) + " " + state,
			podsCell(n.Pods, n.Node.Allocatable.Pods),
		}
		if live {
			cells = append(cells, m.sampleShare(n.Used.HasCPU, n.Used.CPUMilli,
				n.Node.Allocatable.CPUMilli, formatCPU))
		}
		cells = append(cells, m.requestShare(n.Requests.HasCPU, n.Requests.CPUMilli,
			n.Node.Allocatable.CPUMilli, formatCPU))
		if live {
			cells = append(cells, m.sampleShare(n.Used.HasMemory, n.Used.MemoryBytes,
				n.Node.Allocatable.MemoryBytes, formatMemory))
		}
		cells = append(cells, m.requestShare(n.Requests.HasMemory, n.Requests.MemoryBytes,
			n.Node.Allocatable.MemoryBytes, formatMemory))

		row := detailRow{Cells: cells}
		if status != theme.StatusHealthy {
			row.Status = status
		}
		section.Rows = append(section.Rows, row)
	}
	if extra := len(report.Nodes) - maxUsageNodes; extra > 0 {
		section.Rows = append(section.Rows, detailRow{Cells: []string{
			m.theme.Glyphs.Ellipsis + " " + itoa(extra) + " more nodes",
		}})
	}
	return section
}

// nodesEmpty says which nothing this is: a cluster whose machines this user may
// not see, or one that really reports none.
func (m *Model) nodesEmpty() string {
	if reason := m.usageLive.NodesReason; reason != "" {
		return "not readable — " + reason
	}
	return "this cluster reports no nodes"
}

// usageUnscheduledSection is the other half of "where are the pods": the ones
// that are nowhere, which is the answer nobody goes looking for and everybody
// needs.
func (m *Model) usageUnscheduledSection(report *usage.Report) detailSection {
	section := detailSection{
		Title:   "Not scheduled",
		Columns: []string{"Pod", "Namespace", "Requests", "Reason"},
		Empty:   "every pod has a node",
	}
	for i := range report.Unscheduled {
		p := &report.Unscheduled[i]
		section.Rows = append(section.Rows, detailRow{
			Cells:  []string{p.Name, p.Namespace, amountPair(p.Requests), orNone(p.Reason)},
			Status: theme.StatusWarning,
		})
	}
	if extra := report.Totals.Unscheduled - len(report.Unscheduled); extra > 0 {
		section.Rows = append(section.Rows, detailRow{Cells: []string{
			m.theme.Glyphs.Ellipsis + " " + itoa(extra) + " more pods with no node",
		}})
	}
	return section
}

// usageApplicationSection answers the second question the way an operator asks
// it: is this application using what it reserved, and what may it grow into.
func (m *Model) usageApplicationSection(report *usage.Report) detailSection {
	live := report.Metrics.Available
	columns := []string{"Application"}
	if m.allNamespaces {
		columns = append(columns, "Namespace")
	}
	columns = append(columns, "Pods", "Nodes", cpuHeading(live), memoryHeading(live))

	section := detailSection{
		Title:   "Applications",
		Columns: columns,
		Empty:   "no application in " + m.scopeLabel() + " is running a pod",
	}
	for i := range report.Apps {
		if i >= maxUsageApps {
			break
		}
		a := &report.Apps[i]
		cells := []string{a.Name}
		if m.allNamespaces {
			cells = append(cells, a.Namespace)
		}
		cells = append(cells,
			itoa(a.Pods),
			itoa(len(a.Nodes)),
			triple(live, a.Used, a.Requests, a.Limits, cpuOf),
			triple(live, a.Used, a.Requests, a.Limits, memoryOf),
		)
		row := detailRow{
			Cells: cells,
			Ref:   objectRef{Kind: "Application", Name: a.Name, Namespace: a.Namespace},
		}
		if a.Unsized == a.Pods {
			// Every pod unsized: the scheduler is placing this application
			// blind, which is worth noticing before anything goes wrong.
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	if extra := len(report.Apps) - maxUsageApps; extra > 0 {
		section.Rows = append(section.Rows, detailRow{Cells: []string{
			m.theme.Glyphs.Ellipsis + " " + itoa(extra) + " more applications",
		}})
	}
	return section
}

// usageTotalsSection is the scope in absolute numbers, which is what every bar
// above it is a proportion of.
func (m *Model) usageTotalsSection(report *usage.Report) detailSection {
	live := report.Metrics.Available
	totals := &report.Totals

	columns := []string{"Resource"}
	if live {
		columns = append(columns, "Used")
	}
	columns = append(columns, "Requested", "Limits", "Allocatable", "Requested share")
	section := detailSection{Title: "This scope", Columns: columns}

	cpu := []string{"CPU"}
	if live {
		cpu = append(cpu, cpuOf(totals.Used))
	}
	cpu = append(cpu,
		cpuOf(totals.Requests), cpuOf(totals.Limits),
		known(totals.Allocatable.CPUMilli > 0, formatCPU(totals.Allocatable.CPUMilli)),
		m.requestShare(totals.Requests.HasCPU, totals.Requests.CPUMilli,
			totals.Allocatable.CPUMilli, formatCPU),
	)

	memory := []string{"Memory"}
	if live {
		memory = append(memory, memoryOf(totals.Used))
	}
	memory = append(memory,
		memoryOf(totals.Requests), memoryOf(totals.Limits),
		known(totals.Allocatable.MemoryBytes > 0, formatMemory(totals.Allocatable.MemoryBytes)),
		m.requestShare(totals.Requests.HasMemory, totals.Requests.MemoryBytes,
			totals.Allocatable.MemoryBytes, formatMemory),
	)

	// A pod occupies a slot on the node whatever it requests, and a node runs
	// out of slots long before it runs out of either of the other two.
	pods := []string{"Pods"}
	if live {
		pods = append(pods, itoa(totals.Pods))
	}
	pods = append(pods, itoa(totals.Pods), "—",
		known(totals.Allocatable.Pods > 0, itoa64(totals.Allocatable.Pods)),
		m.share(int64(totals.Pods), totals.Allocatable.Pods, itoa(totals.Pods)),
	)

	section.Rows = []detailRow{{Cells: cpu}, {Cells: memory}, {Cells: pods}}
	return section
}

// share renders one number as a proportion of another: a bar, and the number
// the bar stands for. With no denominator there is no proportion, so the
// absolute value is shown rather than a bar that would imply one (ADR 9).
func (m *Model) share(part, whole int64, absolute string) string {
	percent := usage.Percent(part, whole)
	if percent < 0 {
		return absolute
	}
	return m.theme.Bar(percent, usageBarWidth) + " " + itoa(percent) + "%"
}

// sampleShare renders a live measurement. "no sample" is not "0": the metrics
// API describes a node it has heard from and says nothing about one it has not.
func (m *Model) sampleShare(measured bool, part, whole int64, format func(int64) string) string {
	if !measured {
		return "no sample"
	}
	return m.share(part, whole, format(part))
}

// requestShare renders what was asked for. "none set" is not "0" either: a node
// whose pods reserve nothing is a node the scheduler will fill to the brim.
func (m *Model) requestShare(set bool, part, whole int64, format func(int64) string) string {
	if !set {
		return "none set"
	}
	return m.share(part, whole, format(part))
}

// triple renders used, requested and allowed in one cell, in that order. A dash
// is a fact — nothing was asked for, or nothing caps it — and never a zero.
func triple(live bool, used, requests, limits application.Amounts,
	of func(application.Amounts) string,
) string {
	parts := make([]string, 0, 3)
	if live {
		parts = append(parts, of(used))
	}
	return strings.Join(append(parts, of(requests), of(limits)), " / ")
}

func cpuHeading(live bool) string {
	if live {
		return "CPU used/req/limit"
	}
	return "CPU req/limit"
}

func memoryHeading(live bool) string {
	if live {
		return "Memory used/req/limit"
	}
	return "Memory req/limit"
}

func cpuOf(a application.Amounts) string {
	if !a.HasCPU {
		return "—"
	}
	return formatCPU(a.CPUMilli)
}

func memoryOf(a application.Amounts) string {
	if !a.HasMemory {
		return "—"
	}
	return formatMemory(a.MemoryBytes)
}

// amountPair renders one pod's request as a single cell.
func amountPair(a application.Amounts) string {
	if !a.HasCPU && !a.HasMemory {
		return "none set"
	}
	return cpuOf(a) + " / " + memoryOf(a)
}

func podsCell(running int, slots int64) string {
	if slots <= 0 {
		return itoa(running)
	}
	return itoa(running) + "/" + itoa64(slots)
}

// formatCPU renders millicores the way `kubectl top` does, in one unit, so a
// column of them can be compared without arithmetic.
func formatCPU(milli int64) string { return strconv.FormatInt(milli, 10) + "m" }

// formatMemory renders bytes in the largest binary unit that still leaves a
// number worth reading.
func formatMemory(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return trimPointZero(strconv.FormatFloat(float64(bytes)/(1<<30), 'f', 1, 64)) + "Gi"
	case bytes >= 1<<20:
		return strconv.FormatInt(bytes>>20, 10) + "Mi"
	case bytes >= 1<<10:
		return strconv.FormatInt(bytes>>10, 10) + "Ki"
	default:
		return strconv.FormatInt(bytes, 10) + "B"
	}
}

func trimPointZero(s string) string { return strings.TrimSuffix(s, ".0") }

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

func known(ok bool, value string) string {
	if !ok {
		return "—"
	}
	return value
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// nodeState names what is wrong with a machine, as a word and a severity. The
// glyph is added where the cell is built, so the state never travels as colour
// alone (ADR 9).
func nodeCondition(n *application.Node) (string, theme.Status) {
	switch {
	case !n.Ready:
		return "NotReady", theme.StatusCritical
	case len(n.Pressure) > 0:
		return strings.Join(n.Pressure, ","), theme.StatusCritical
	case n.Unschedulable:
		return "cordoned", theme.StatusWarning
	default:
		return "Ready", theme.StatusHealthy
	}
}

// handleUsageKey moves across the namespace/application drill-down and scrolls
// the surrounding evidence without losing the selected row.
func (m *Model) handleUsageKey(keystroke string) (tea.Cmd, bool) {
	d, targets := m.usageView()
	page := max(m.screen.Body.Height-1, 1)
	switch keystroke {
	case "up", "k":
		m.usagePort.MoveTarget(-1, d.TargetLines(m.screen.Body.Width), len(targets),
			d.LineCount(m.screen.Body.Width), m.bodyHeight())
	case "down", "j":
		m.usagePort.MoveTarget(1, d.TargetLines(m.screen.Body.Width), len(targets),
			d.LineCount(m.screen.Body.Width), m.bodyHeight())
	case "pgup":
		m.scrollUsage(-page)
	case "pgdown", " ":
		m.scrollUsage(page)
	case "home", "g":
		m.usagePort.Offset = 0
	case "end", "G":
		m.scrollUsage(m.usageLines())
	case "enter":
		if len(targets) == 0 {
			return nil, true
		}
		target := targets[clampInt(m.usagePort.Cursor, len(targets)-1)]
		switch target.Kind {
		case "Namespace":
			// The scope change resets the drill-down state along with every
			// other scoped view, so the flag is set after it, not before.
			cmd := m.switchNamespace(target.Name)
			m.usageDrilledIn = true
			return cmd, true
		case "Application":
			return m.openApplication(target.Name), true
		}
	case "esc", "left", "h":
		// Esc goes back the way it came. Widening the scope is only going back
		// for somebody who narrowed it here; for anyone who opened the screen
		// in a namespace it would be a scope change they never asked for.
		if m.usageDrilledIn {
			return m.setAllNamespaces(true), true
		}
		return m.backToApplications(), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) usageLines() int {
	return m.usageData().LineCount(m.screen.Body.Width)
}

func (m *Model) scrollUsage(delta int) {
	d, targets := m.usageView()
	m.usagePort.ScrollTargets(delta, d.TargetLines(m.screen.Body.Width), len(targets),
		d.LineCount(m.screen.Body.Width), m.bodyHeight())
}

package app

import (
	"context"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	kubediscovery "github.com/aronk11/correlux/internal/kube/discovery"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

// fleetPartMsg carries one cluster's page of a resource.
type fleetPartMsg struct {
	gen  uint64
	part resources.Part
}

// fleetPartsMsg hands the model the channel the pages arrive on.
type fleetPartsMsg struct {
	gen   uint64
	parts <-chan resources.Part
}

// openFleetResource lists one kind across every cluster in the fleet.
//
// The kind is resolved once, in the cluster the session is in, and that same
// group/version/resource is asked of every member. A cluster that serves a
// different version of it answers with a 404, which is reported as itself
// rather than guessed around: Correlux does not quietly show you a different
// API than the one you asked for.
func (m *Model) openFleetResource(res kubediscovery.Resource) tea.Cmd {
	contexts := m.fleetContexts()
	if len(contexts) == 0 {
		m.notice("No fleet configured", theme.StatusWarning)
		return m.expireNotice()
	}

	m.fleetResource = res
	m.fleetParts = nil
	m.fleetTable = resources.Merged{}
	m.fleetTablePort.Cursor, m.fleetTablePort.Offset = 0, 0
	m.view = viewFleetResource
	m.rebuildCommands()
	return m.startFleetResource(contexts, res)
}

// openFleetResourceByName resolves a kubectl-style name and browses it across
// the fleet.
func (m *Model) openFleetResourceByName(name string) tea.Cmd {
	catalog := m.catalog.Get()
	if catalog == nil {
		m.notice("Resource kinds are not discovered yet", theme.StatusWarning)
		return tea.Batch(m.loadCatalog(), m.expireNotice())
	}
	res, ok := catalog.Lookup(name)
	if !ok {
		m.notice("Unknown resource "+name, theme.StatusWarning)
		return m.expireNotice()
	}
	return m.openFleetResource(res)
}

// fleetRead is one list call: one kind, in one cluster, in one scope.
//
// A fleet scoped to namespaces asks each cluster once per namespace rather than
// once for everything, which is both less to read and the only version an
// account that may not read the whole cluster can make at all.
type fleetRead struct {
	context   string
	namespace string
}

// fleetReads is the work the table is built from, in the order it is handed
// out: every cluster, and within it every namespace of the fleet's scope.
func (m *Model) fleetReads(contexts []string, res kubediscovery.Resource) []fleetRead {
	namespaces := m.fleetNamespaces()
	if !res.Namespaced || len(namespaces) == 0 {
		// A cluster-scoped kind has no namespace to be narrowed to, and saying
		// nothing about the nodes of a namespace-scoped fleet would be worse
		// than reading them.
		reads := make([]fleetRead, 0, len(contexts))
		for _, name := range contexts {
			reads = append(reads, fleetRead{context: name})
		}
		return reads
	}
	reads := make([]fleetRead, 0, len(contexts)*len(namespaces))
	for _, name := range contexts {
		for _, namespace := range namespaces {
			reads = append(reads, fleetRead{context: name, namespace: namespace})
		}
	}
	return reads
}

// startFleetResource reads the kind from every member, four at a time.
func (m *Model) startFleetResource(contexts []string, res kubediscovery.Resource) tea.Cmd {
	if m.cancelFleet != nil {
		m.cancelFleet()
	}
	gen := m.fleetGeneration + 1
	m.fleetGeneration = gen

	reads := m.fleetReads(contexts, res)
	m.fleetPending = len(reads)
	m.fleetClusters = len(contexts)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFleet = cancel

	factory := m.factory
	parts := make(chan resources.Part, len(reads))

	return func() tea.Msg {
		go func() {
			defer close(parts)

			work := make(chan fleetRead)
			var wg sync.WaitGroup
			for i := 0; i < min(fleetConcurrency, len(reads)); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for read := range work {
						readCtx, readCancel := context.WithTimeout(ctx, factory.Timeout())
						table, err := factory.ListTable(readCtx, read.context, res,
							resources.ListOptions{Namespace: read.namespace})
						readCancel()

						select {
						case parts <- resources.Part{Source: read.context, Table: table, Err: err}:
						case <-ctx.Done():
							return
						}
					}
				}()
			}
			for _, read := range reads {
				select {
				case work <- read:
				case <-ctx.Done():
					close(work)
					wg.Wait()
					return
				}
			}
			close(work)
			wg.Wait()
		}()

		return fleetPartsMsg{gen: gen, parts: parts}
	}
}

// waitForFleetPart delivers the next cluster's page.
func waitForFleetPart(gen uint64, parts <-chan resources.Part) tea.Cmd {
	return func() tea.Msg {
		part, open := <-parts
		if !open {
			return nil
		}
		return fleetPartMsg{gen: gen, part: part}
	}
}

// applyFleetPart merges one cluster's page in and waits for the next.
//
// The table is rebuilt from every part rather than appended to, because the
// columns can change when a cluster contributes one nobody else had: a row
// already on screen has to move with them.
func (m *Model) applyFleetPart(msg fleetPartMsg) tea.Cmd {
	if msg.gen != m.fleetGeneration {
		return nil
	}
	m.fleetParts = append(m.fleetParts, msg.part)
	m.fleetPending = max(m.fleetPending-1, 0)
	m.fleetTable = resources.Merge(m.fleetParts, m.fleetResource.Namespaced)
	m.fleetTablePort.Cursor = clampInt(m.fleetTablePort.Cursor, max(len(m.visibleFleetRows())-1, 0))
	return waitForFleetPart(msg.gen, m.fleetPartsChan)
}

// fleetResourceData renders the merged table.
func (m *Model) fleetResourceData() screens.TableData {
	d := screens.TableData{
		Cursor:   m.fleetTablePort.Cursor,
		Offset:   m.fleetTablePort.Offset,
		Wide:     m.wideMode(),
		Sort:     m.fleetSort.column,
		SortDesc: m.fleetSort.desc,
	}

	rows := m.visibleFleetRows()
	if len(rows) == 0 && m.filtering() {
		d.Message = m.emptyFilterMessage(len(m.fleetTable.Rows), m.fleetResource.Plural())
		return d
	}
	if len(rows) == 0 {
		switch {
		case m.fleetPending > 0:
			waiting := m.fleetClustersPending()
			d.Message = "Reading " + m.fleetResource.Plural() + " from " +
				itoa(waiting) + " " + clusterWord(waiting) + "…"
		case len(m.fleetTable.Failures) > 0:
			d.Message = "No " + m.fleetResource.Plural() + " anywhere. " + m.fleetFailureNote()
			d.MessageStatus = theme.StatusWarning
		default:
			d.Message = "No " + m.fleetResource.Plural() + " in any cluster of the fleet."
		}
		return d
	}

	d.Columns = make([]screens.TableColumn, 0, len(m.fleetTable.Columns))
	for _, c := range m.fleetTable.Columns {
		d.Columns = append(d.Columns, screens.TableColumn{
			Title: c.Name,
			Wide:  c.Wide(),
			Right: c.Type == "integer" || c.Type == "number",
		})
	}
	d.Rows = make([]screens.TableRow, 0, len(rows))
	for i := range rows {
		d.Rows = append(d.Rows, screens.TableRow{
			Cells:  rows[i].Cells,
			Status: rowStatus(rows[i].Cells),
		})
	}
	return d
}

// fleetClustersPending is how many clusters have said nothing yet, which is not
// the same as how many requests are out once the fleet is scoped to namespaces.
func (m *Model) fleetClustersPending() int {
	answered := map[string]bool{}
	for _, part := range m.fleetParts {
		answered[part.Source] = true
	}
	return max(m.fleetClusters-len(answered), 0)
}

// fleetResourceLabel says what is on screen and what it does not cover.
func (m *Model) fleetResourceLabel() string {
	parts := []string{itoa(len(m.fleetTable.Rows)) + " " + m.fleetResource.Plural()}
	if namespaces := m.fleetNamespaces(); len(namespaces) > 0 && m.fleetResource.Namespaced {
		parts = append(parts, "in "+strings.Join(namespaces, ", "))
	}

	// Clusters, counted as clusters: with a scope of three namespaces a cluster
	// contributes three parts, and printing those as clusters would invent
	// members the fleet does not have. A cluster that answered for one
	// namespace and was refused for another has answered — the refusal is named
	// in the failure note rather than counted as a silent member.
	answered, failed := map[string]bool{}, map[string]bool{}
	for _, part := range m.fleetParts {
		if part.Err != nil {
			failed[part.Source] = true
			continue
		}
		answered[part.Source] = true
	}
	for name := range answered {
		delete(failed, name)
	}
	// Never fewer clusters than have already spoken: a stale counter must not
	// produce "from 2 of 0".
	total := max(m.fleetClusters, len(answered)+len(failed))
	if m.fleetPending > 0 || len(failed) > 0 {
		parts = append(parts,
			"from "+itoa(len(answered))+" of "+itoa(total)+" "+clusterWord(total))
	}
	if m.fleetPending > 0 {
		parts = append(parts, itoa(m.fleetPending)+" still reading")
	}
	if note := m.fleetFailureNote(); note != "" {
		parts = append(parts, note)
	}
	if m.fleetTable.Truncated {
		parts = append(parts, "more rows left unread")
	}
	if note := sortedAmongLoaded(m.fleetSort.active(), m.fleetPending > 0 || m.fleetTable.Truncated); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, "   ")
}

// fleetFailureNote names the clusters that could not answer, and why the first
// of them could not.
func (m *Model) fleetFailureNote() string {
	if len(m.fleetTable.Failures) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.fleetTable.Failures))
	seen := map[string]bool{}
	for _, failure := range m.fleetTable.Failures {
		// One cluster refusing three of the fleet's namespaces is one cluster
		// to name, not three.
		if seen[failure.Source] {
			continue
		}
		seen[failure.Source] = true
		names = append(names, failure.Source)
	}
	return "not listed in " + strings.Join(names, ", ") + ": " +
		shortError(m.fleetTable.Failures[0].Err)
}

// openFleetRow leaves the fleet for the object under the cursor: its cluster,
// its namespace, the object itself.
func (m *Model) openFleetRow() tea.Cmd {
	rows := m.visibleFleetRows()
	if m.fleetTablePort.Cursor < 0 || m.fleetTablePort.Cursor >= len(rows) {
		return nil
	}
	row := rows[m.fleetTablePort.Cursor]
	res := m.fleetResource

	m.stopFleet()
	m.pendingObject = objectRef{
		Kind: res.Kind(), Name: row.Name, Namespace: row.Namespace, Resource: res.FullName(),
	}

	if row.Source == m.contextName {
		ref := m.pendingObject
		m.pendingObject = objectRef{}
		return m.openObject(ref)
	}
	return m.switchContextScoped(row.Source, row.Namespace)
}

// moveFleetTableCursor scrolls the merged table.
func (m *Model) moveFleetTableCursor(delta int) {
	m.fleetTablePort.MoveCursor(delta, len(m.visibleFleetRows()), m.rowsPerScreen())
}

// scrollFleetTable moves the viewport and drags the selection with it.
func (m *Model) scrollFleetTable(delta int) {
	m.fleetTablePort.ScrollRows(delta, len(m.visibleFleetRows()), m.rowsPerScreen())
}

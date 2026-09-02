package app

import (
	"context"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/screens"
	"github.com/aronk11/kubeui/internal/ui/theme"
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
// rather than guessed around: kubeui does not quietly show you a different API
// than the one you asked for.
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

// startFleetResource reads the kind from every member, four at a time.
func (m *Model) startFleetResource(contexts []string, res kubediscovery.Resource) tea.Cmd {
	if m.cancelFleet != nil {
		m.cancelFleet()
	}
	gen := m.fleetGeneration + 1
	m.fleetGeneration = gen
	m.fleetPending = len(contexts)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFleet = cancel

	factory := m.factory
	parts := make(chan resources.Part, len(contexts))

	return func() tea.Msg {
		go func() {
			defer close(parts)

			work := make(chan string)
			var wg sync.WaitGroup
			for i := 0; i < min(fleetConcurrency, len(contexts)); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for name := range work {
						readCtx, readCancel := context.WithTimeout(ctx, factory.Timeout())
						table, err := factory.ListTable(readCtx, name, res, resources.ListOptions{})
						readCancel()

						select {
						case parts <- resources.Part{Source: name, Table: table, Err: err}:
						case <-ctx.Done():
							return
						}
					}
				}()
			}
			for _, name := range contexts {
				select {
				case work <- name:
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
		ShowWide: m.tableWide,
	}

	rows := m.visibleFleetRows()
	if len(rows) == 0 && m.filtering() {
		d.Message = "Nothing matches " + m.query() + " among " +
			itoa(len(m.fleetTable.Rows)) + " " + m.fleetResource.Plural() + "."
		return d
	}
	if len(rows) == 0 {
		switch {
		case m.fleetPending > 0:
			d.Message = "Reading " + m.fleetResource.Plural() + " from " +
				itoa(m.fleetPending) + " " + clusterWord(m.fleetPending) + "…"
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

// fleetResourceLabel says what is on screen and what it does not cover.
func (m *Model) fleetResourceLabel() string {
	parts := []string{itoa(len(m.fleetTable.Rows)) + " " + m.fleetResource.Plural()}
	answered := len(m.fleetParts) - len(m.fleetTable.Failures)
	total := answered + len(m.fleetTable.Failures) + m.fleetPending
	if m.fleetPending > 0 || len(m.fleetTable.Failures) > 0 {
		parts = append(parts, "from "+itoa(answered)+" of "+itoa(total)+" "+clusterWord(total))
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
	return strings.Join(parts, "   ")
}

// fleetFailureNote names the clusters that could not answer, and why the first
// of them could not.
func (m *Model) fleetFailureNote() string {
	if len(m.fleetTable.Failures) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.fleetTable.Failures))
	for _, failure := range m.fleetTable.Failures {
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

package app

import (
	"strings"

	"github.com/sahilm/fuzzy"

	kubediscovery "github.com/aronk11/kubeui/internal/kube/discovery"
	"github.com/aronk11/kubeui/internal/ui/async"
	"github.com/aronk11/kubeui/internal/ui/components"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// allNamespacesLabel is what the cluster-wide row is matched against, so that
// typing "all" keeps it in the list while typing a namespace name drops it.
const allNamespacesLabel = "all namespaces"

// paletteLimit bounds how many rows the palette considers. Ranking every entry
// stays cheap, but rendering thousands would not.
const paletteLimit = 200

// filterCommands feeds the command palette from the registry.
func (m *Model) filterCommands(query string) []components.Item {
	matches := m.registry.Search(query, paletteLimit)
	items := make([]components.Item, 0, len(matches))
	for _, match := range matches {
		c := match.Command
		right := c.Shortcut
		if right == "" {
			right = c.Category
		}
		items = append(items, components.Item{
			ID:        c.ID,
			Title:     c.Title,
			Subtitle:  c.Subtitle,
			Right:     right,
			Highlight: match.TitlePositions,
			Disabled:  !c.Enabled,
			Note:      c.DisabledReason,
		})
	}
	return items
}

// filterContexts lists kubeconfig contexts, marking production ones in text as
// well as colour.
func (m *Model) filterContexts(query string) []components.Item {
	contexts := m.kubeconfig.Contexts
	names := make([]string, len(contexts))
	for i, c := range contexts {
		names[i] = c.Name
	}

	order := make([]int, 0, len(contexts))
	highlights := make(map[int][]int, len(contexts))
	if strings.TrimSpace(query) == "" {
		for i := range contexts {
			order = append(order, i)
		}
	} else {
		for _, res := range fuzzy.Find(query, names) {
			order = append(order, res.Index)
			highlights[res.Index] = res.MatchedIndexes
		}
	}

	items := make([]components.Item, 0, len(order))
	for _, idx := range order {
		c := contexts[idx]
		item := components.Item{
			ID:        c.Name,
			Title:     c.Name,
			Subtitle:  c.Server,
			Highlight: highlights[idx],
		}
		if c.Production {
			item.Badge = "PROD"
			item.BadgeStatus = theme.StatusCritical
		}
		switch {
		case c.Name == m.contextName:
			item.Right = "active"
		case c.Current:
			item.Right = "kubeconfig default"
		}
		items = append(items, item)
	}
	return items
}

// filterNamespaces lists namespaces for the active context. It distinguishes
// "still loading", "you may not list these" and "there really are none",
// because acting on the wrong assumption wastes a user's time.
func (m *Model) filterNamespaces(query string) []components.Item {
	items := make([]components.Item, 0, len(m.namespaces.Get().Names)+2)

	// The cluster-wide row leads the list, but disappears once the user starts
	// typing something else: pressing Enter after a filter must never hit a row
	// the query did not ask for.
	query = strings.TrimSpace(query)
	if query == "" || strings.HasPrefix(allNamespacesLabel, strings.ToLower(query)) {
		row := components.Item{ID: allNamespacesID, Title: "All namespaces"}
		if m.allNamespaces {
			row.Right = "active"
		}
		items = append(items, row)
	}

	list := m.namespaces.Get()
	switch m.namespaces.State() {
	case async.Loading:
		items = append(items, components.Item{
			ID:       "__loading",
			Title:    "Loading namespaces…",
			Disabled: true,
		})
	case async.Failed:
		items = append(items, components.Item{
			ID:       "__error",
			Title:    "Could not list namespaces",
			Subtitle: shortError(m.namespaces.Err()),
			Disabled: true,
		})
	case async.Ready:
		if list.Restricted {
			items = append(items, components.Item{
				ID:       "__restricted",
				Title:    "Listing namespaces is not permitted for this user",
				Subtitle: "type a namespace name and press Enter",
				Disabled: true,
			})
		} else if len(list.Names) == 0 {
			items = append(items, components.Item{
				ID:       "__empty",
				Title:    "No namespaces returned",
				Disabled: true,
			})
		}
	}

	matched := list.Names
	var highlights map[int][]int
	if query != "" && len(list.Names) > 0 {
		highlights = make(map[int][]int)
		matched = nil
		for _, res := range fuzzy.Find(query, list.Names) {
			highlights[len(matched)] = res.MatchedIndexes
			matched = append(matched, list.Names[res.Index])
		}
	}

	for i, name := range matched {
		item := components.Item{ID: name, Title: name}
		if highlights != nil {
			item.Highlight = highlights[i]
		}
		if !m.allNamespaces && name == m.namespace {
			item.Right = "active"
		}
		items = append(items, item)
	}

	// Allow a namespace that is not in (or not visible in) the list.
	if query != "" && !containsString(matched, query) {
		items = append(items, components.Item{
			ID:       query,
			Title:    "Use namespace \"" + query + "\"",
			Subtitle: "not in the visible list",
		})
	}
	return items
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// filterResources lists the resource kinds the cluster serves. Custom resources
// are badged so a user can tell at a glance what came from a CRD, and the list
// is searchable by kind, plural name, short name and API group — the four
// things people actually remember.
func (m *Model) filterResources(query string) []components.Item {
	catalog := m.catalog.Get()

	switch m.catalog.State() {
	case async.Idle, async.Loading:
		return []components.Item{{
			ID:       "__loading",
			Title:    "Discovering resource kinds…",
			Disabled: true,
		}}
	case async.Failed:
		if catalog == nil {
			return []components.Item{{
				ID:       "__error",
				Title:    "Could not discover resource kinds",
				Subtitle: shortError(m.catalog.Err()),
				Disabled: true,
			}}
		}
	}
	if catalog == nil || catalog.Len() == 0 {
		return []components.Item{{
			ID:       "__empty",
			Title:    "The server returned no listable resources",
			Disabled: true,
		}}
	}

	items := make([]components.Item, 0, catalog.Len()+1)
	if catalog.Partial() {
		items = append(items, components.Item{
			ID:       "__partial",
			Title:    itoa(len(catalog.Failures)) + " API group(s) could not be discovered",
			Subtitle: firstFailure(catalog),
			Disabled: true,
		})
	}

	// Rank by the searchable label so "deploy" finds Deployments and "acme"
	// finds every kind in that group.
	labels := make([]string, 0, catalog.Len())
	for _, r := range catalog.Resources {
		labels = append(labels, r.Kind()+" "+r.FullName()+" "+strings.Join(r.ShortNames, " "))
	}

	order := make([]int, 0, len(labels))
	if strings.TrimSpace(query) == "" {
		for i := range labels {
			order = append(order, i)
		}
	} else {
		for _, res := range fuzzy.Find(query, labels) {
			order = append(order, res.Index)
		}
	}

	for _, idx := range order {
		r := catalog.Resources[idx]
		item := components.Item{
			ID:       r.FullName(),
			Title:    r.Kind(),
			Subtitle: r.GroupVersion(),
			Right:    r.Plural(),
		}
		if !r.Builtin {
			item.Badge = "CRD"
			item.BadgeStatus = theme.StatusWarning
		}
		if !r.Namespaced {
			item.Subtitle += "  cluster-scoped"
		}
		items = append(items, item)
	}
	return items
}

func firstFailure(catalog *kubediscovery.Catalog) string {
	for gv, msg := range catalog.Failures {
		return gv + ": " + msg
	}
	return ""
}

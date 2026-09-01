package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/ui/components"
	"github.com/aronk11/kubeui/internal/ui/layout"
)

// activeSelector returns the selector belonging to the open overlay, if any.
func (m *Model) activeSelector() *components.Selector {
	switch m.overlay {
	case overlayPalette:
		return m.cmdPal
	case overlayContexts:
		return m.ctxPicker
	case overlayNamespaces:
		return m.nsPicker
	case overlayResources:
		return m.resPicker
	default:
		return nil
	}
}

// openOverlay shows a modal, resetting its filter so it always opens in a
// predictable state, and pre-selecting the current value where that helps.
func (m *Model) openOverlay(kind overlayKind) tea.Cmd {
	m.overlay = kind
	switch kind {
	case overlayPalette:
		m.rebuildCommands()
		m.cmdPal.Reset()
	case overlayContexts:
		m.ctxPicker.Reset()
		m.ctxPicker.SelectID(m.contextName)
	case overlayNamespaces:
		m.nsPicker.Reset()
		if m.allNamespaces {
			m.nsPicker.SelectID(allNamespacesID)
		} else {
			m.nsPicker.SelectID(m.namespace)
		}
		// Namespaces may never have loaded (or may have failed); opening the
		// picker is a good moment to try again.
		if !m.namespaces.HasValue() {
			return m.loadNamespaces()
		}
	case overlayResources:
		m.resPicker.Reset()
		if m.view == viewTable {
			m.resPicker.SelectID(m.resource.FullName())
		}
		if !m.catalog.HasValue() {
			return m.loadCatalog()
		}
	}
	return nil
}

func (m *Model) closeOverlay() {
	m.overlay = overlayNone
}

// handleOverlayKey routes a keystroke to the open overlay. It returns
// handled=false when the key belongs to the application (global shortcuts).
func (m *Model) handleOverlayKey(keystroke, text string) (tea.Cmd, bool) {
	switch keystroke {
	case "esc", "ctrl+g":
		m.closeOverlay()
		return nil, true
	case "enter":
		return m.confirmSelection(), true
	}

	if m.overlay == overlayHelp {
		// The help overlay has no input of its own; any other key closes it.
		if keystroke == "?" || keystroke == "q" {
			m.closeOverlay()
			return nil, true
		}
		return nil, false
	}

	sel := m.activeSelector()
	if sel == nil {
		return nil, false
	}
	return nil, sel.HandleKey(keystroke, text)
}

// confirmSelection applies the highlighted row of the open overlay.
func (m *Model) confirmSelection() tea.Cmd {
	sel := m.activeSelector()
	if sel == nil {
		m.closeOverlay()
		return nil
	}
	item, ok := sel.Selected()
	if !ok || item.Disabled {
		return nil
	}

	switch m.overlay {
	case overlayPalette:
		return m.runCommand(item.ID)
	case overlayContexts:
		m.closeOverlay()
		return m.switchContext(item.ID)
	case overlayNamespaces:
		m.closeOverlay()
		return m.switchNamespace(item.ID)
	case overlayResources:
		m.closeOverlay()
		return m.openResource(item.ID)
	}
	m.closeOverlay()
	return nil
}

// overlayRect is the geometry of the open overlay, used for rendering and for
// translating mouse coordinates into list rows.
func (m *Model) overlayRect() layout.Rect {
	switch m.overlay {
	case overlayPalette:
		return layout.Overlay(m.screen, layout.OverlayOptions{
			WidthRatio: 0.7, HeightRatio: 0.6,
			MinWidth: 44, MaxWidth: 96, MinHeight: 8, MaxHeight: 22,
		})
	case overlayContexts, overlayNamespaces:
		return layout.Overlay(m.screen, layout.OverlayOptions{
			WidthRatio: 0.6, HeightRatio: 0.55,
			MinWidth: 40, MaxWidth: 84, MinHeight: 8, MaxHeight: 20,
		})
	case overlayResources:
		return layout.Overlay(m.screen, layout.OverlayOptions{
			WidthRatio: 0.6, HeightRatio: 0.65,
			MinWidth: 44, MaxWidth: 88, MinHeight: 10, MaxHeight: 24,
		})
	case overlayHelp:
		return layout.Overlay(m.screen, layout.OverlayOptions{
			WidthRatio: 0.6, HeightRatio: 0.7,
			MinWidth: 46, MaxWidth: 76, MinHeight: 10, MaxHeight: 24,
		})
	default:
		return layout.Rect{}
	}
}

# Correlux UX review — 2026-09-08

Reviewed the terminal UI implementation against the incident workflow in SPEC.md:
identify an unhealthy application, diagnose it, inspect resources and logs,
choose an action, and verify recovery. This is a code and rendered-fixture
review, not a usability study with engineers or a live production-cluster test.

## Implemented

| Problem | Change | Operator benefit |
| --- | --- | --- |
| Menus could replace the visible workspace: independently composed Lip Gloss layers ignored their offsets. | Compose the base and positioned overlay through the compositor. | Cluster identity and background stay visible; clicks agree with the rendered menu. |
| Main destinations were discoverable mostly through a crowded shortcut footer. | Persistent compact navigation below context and breadcrumbs, with mouse targets and configured shortcut labels on wide terminals. | Predictable locations for applications, resources, usage, events, fleet and commands. |
| Commands competed with the investigation for the centre of the screen. | Bottom command dock with the selected subject in its title. | The top of the current workspace remains visible; Escape returns immediately. |
| Generic cluster navigation ranked ahead of relevant inspection actions. | Boost applicable WHY, logs, YAML and copy commands. | Faster access to evidence without promoting destructive actions. |
| WHY and pod logs indexed unfiltered data using the filtered cursor. | Resolve the visible row; disable generic WHY without an application target. | Diagnosis and logs belong to the resource the engineer actually selected. |
| The header block ran into the table below it, and the count of what the breadcrumb pointed at was glued into the crumb itself. | Close the block with a rule; print the count at the end of the breadcrumb line, muted. | The frame reads as chrome and content rather than as five header rows, and the crumb names a place again. |
| Navigation labels, shortcuts and the context badge were each spaced differently, and the badge indented the first line past the two below it. | One gutter between navigation items, shortcuts in the key style used everywhere else, no indent on the context. | The header block has one left edge and one rhythm. |
| The browser demo sized its terminal from a single measured glyph, so the frame was a column too wide and the right-hand end of the header was clipped — and below sixty columns it could only be scrolled sideways. | Measure a long run of glyphs, size from the viewport's real inner width, and shrink the type when the phone is narrower than the sixty-column floor. | The whole frame is visible on a laptop and on a phone. |
| The demo repeated the terminal's own footer as on-screen buttons on machines that have a keyboard. | Show the on-screen keys only on narrow or touch devices. | Desktop readers see the interface, not a second toolbar. |
| Escape meant "one step back" on some screens and "jump home" on others: it left the WHY explanation for the dashboard, skipping the application it explains. | One back key with one meaning, everywhere; home is the Apps key. | Leaving a screen no longer risks losing the investigation. |
| The bottom row said "Esc Back" — or nothing at all — so the only way to learn where back went was to press it. | The bar names the destination: "Esc payments", "Esc Pods", "Esc Applications". | The way out is legible before it is taken. |
| The navigation bar and the status bar advertised the same shortcuts, and the help listed four different, partly wrong descriptions of Escape. | A shortcut is printed once, in the bar that has room for it; the help states the rule once. | The bottom row is free for what this screen does. |
| An unreachable cluster still offered Enter, WHY, grouping and the filter, with the cluster switcher buried among them. | Keys that act on a row are offered only while there is a row. | On a broken connection, every key on screen does something. |
| Menu clicks could reach entries below the visible list. | Bound both selector viewport and overlay hit area. | Footers and borders cannot execute invisible commands. |
| Leaving logs jumped to an object or the dashboard. | Remember the originating view and return there on Escape. | WHY → logs → WHY and table → logs → table preserve the investigation. |
| Navigation to applications or usage could leave streams running. | Cancel log and fleet streams when leaving for those destinations. | Background requests follow the operator's workspace. |

## Design decisions

Keep a horizontal navigation bar rather than a permanent sidebar: terminal
columns are valuable for resource names, namespaces, failure reasons and logs.
Reserve colour and explicit status words for operational meaning. The active
navigation item also has brackets, so it remains identifiable without colour.

Commands, resource pickers and confirmations remain transient UI. Logs and
large YAML documents retain the full body area because their content benefits
from the available width. Returning from logs must be reliable before adding
multiple simultaneous panes.

## Follow-up priorities

These remain product work, not claims made by this change:

1. Make the investigation itself a stable workspace with resource, diagnosis,
   events and logs tabs. Keep selection, filters and scroll per view, with a
   wide-terminal split view as an option. Avoid silently interpreting a filter
   against unrelated columns after navigation.
2. Extend mouse support to application/resource rows, relationship links and
   sorting headers. A click should select; opening or mutation should remain
   a separate deliberate action.
3. Add contextual help that scrolls. The current fixed-height help can clip
   later sections; commands remain searchable in the palette.
4. Show data freshness and evidence gaps beside affected results. Distinguish
   timed reloads from watches, missing metrics from zero usage, and Kubernetes
   events from observed changes.
5. Validate with cloud engineers on incident tasks: time to correct diagnosis,
   wrong-target actions, backtracking, keyboard discovery and recovery checks.
   Include an 80-column SSH session, restricted RBAC, unavailable metrics and
   large resource lists.

The regression suite covers 60×12, 80×24, 110×32 and 180×50 layouts, rendered
palette position, mouse isolation, filtered action targets and log return paths.

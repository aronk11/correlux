# 23. A detour from an investigation remembers where it was opened from

- Status: accepted
- Date: 2026-09-09

## Context

Investigating an unhealthy application means moving between four different
readings of it: what it is made of (the application detail), why it is
unhealthy (Diagnosis, `Ctrl+W`), what happened to it recently (Events, `E`),
and what it is saying right now (Logs, `l`). Each is its own full-window view,
reached by its own key from wherever the investigation currently is — not a
separate destination the way the fleet or the resource browser are, but a
lateral move within the same one.

That lateral move behaved differently depending on which of the four it was,
in ways nobody chose on purpose:

- **Logs** remembers where it was opened from (`logFrom`, set in `openLogs()`,
  restored in `closeLogs()`) and returns there on Escape.
- **Diagnosis** hardcoded its return to the application detail regardless of
  where it was actually entered from — wrong when Diagnosis is opened straight
  from the dashboard cursor, which `explanationTarget()` has always allowed:
  Escape landed on the application detail the user never asked to see, not
  the dashboard they left.
- **Events** always returned to the dashboard, even when it was reached from
  Diagnosis or Logs.

The same three views disagreed about scroll, too. Diagnosis reset its scroll
to the top on every open, including a lateral return to the same application's
diagnosis after a detour through Events. Events did the same to its own
scroll and selection. The one case that happened to preserve state — Why to
Logs and back — worked only because `closeLogs()` restores the view directly
rather than calling `explain()` again; it was a side effect of how Logs is
built, not a rule anyone had written down.

Reachability was asymmetric on top of that: from the application detail, all
three others are one keypress away. From Diagnosis, Events and Logs both work
directly. From Logs, Events works but Diagnosis does not. From Events,
neither works — the only way out was Escape to the dashboard and back in.

None of this is a case for a bigger redesign. `docs/ux-review.md`'s open
backlog already names the goal — treat these as siblings of one investigation,
each keeping its own state — and the wide-terminal, split-pane version of that
goal is deliberately out of scope here: the terminal floor Correlux supports
(`layout.MinHeight = 12`) leaves five usable rows in Logs before a second pane
would ever be drawn, and the review's own "Design decisions" already deferred
a multi-pane layout once, explicitly pending logs returning reliably — which
this record is part of finishing, not going beyond.

## Decision

A view reached as a lateral detour from an investigation records where it was
opened from, and Escape returns there — generalizing the pattern `logFrom`
already established rather than inventing a second one:

- Diagnosis gained `whyFrom`, Events gained `activityFrom`, set the same way
  `logFrom` is: to the view being left, unless that view is already the
  detour's own (a re-entry must not overwrite a real origin with itself).
- `goBack()`'s hardcoded Diagnosis-to-application special case now assigns
  `m.view = m.whyFrom`; `handleActivityKey`'s Escape case and the `E` toggle
  both now go through a small `leaveActivity()` that does the same for
  `activityFrom`. Neither guards against a stale origin — `closeLogs()`
  never has, either; it assigns the view and trusts the destination to
  tolerate a subject that disappeared since, which every view already has to
  do independent of this.
- Scroll resets when the subject changes, not on every open. Diagnosis
  compares against a dedicated `whySubject` field rather than the existing
  `selectedApp` — `explainApplication` sets `selectedApp` (via
  `openApplication`) *before* it calls `explain`, so a comparison against
  `selectedApp` inside `explain` would always see the value just written, never
  the one it replaced. Events compares against `m.evidence.State()`: a real
  scope change already calls `evidence.Reset()` elsewhere
  (`switchContextScoped`, `reloadScopedViews`), which is what leaves the state
  `Idle`; a lateral return finds it already `Ready` (or `Failed` with a
  previous value) and leaves the scroll alone. The application detail's own
  `detailPort` gained the same rule in `openApplication`, for the same reason:
  a lateral round trip back to it, now that Diagnosis and Events correctly
  return there, must not discard where the user was either.
- Events gained one more direct move: `l` now opens logs for the object an
  Events row names, closing the one asymmetric gap that was cheap to close.
  The reverse — `Ctrl+W` resolving a *diagnosis* for an arbitrary Events row —
  is not: every existing resolution path answers "an application", keyed by
  `selectedApp`, and an Events row names an object with no guaranteed owning
  application and no existing reverse lookup from one to the other. Building
  that lookup is new logic, not wiring, and belongs in its own change if it
  turns out to be worth it.

## Consequences

- Four views now agree on one rule instead of implementing three. A future
  view that behaves like a detour from an investigation should follow it too:
  record an origin the way `logFrom`/`whyFrom`/`activityFrom` do, and reset
  state on a subject change rather than on every open.
- This does not add a visible affordance for the relationship — no tab row,
  no indicator that these four are siblings — only the underlying behavior.
  Whether that is worth building, and what a wide-terminal split view of it
  would look like, is a separate design question; `screens/overview.go`'s
  `twoColumnMin` two-column split and `screens/table.go`'s fit-tested
  `WideAuto` are the two existing patterns to start from when it is asked.
- `Ctrl+W` from Events still shows "Select an application first" rather than
  working, same as today. That is a known, named gap, not a silent one.

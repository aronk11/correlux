# 18. A diagnosis is built from evidence fetched on demand, not from a cache

- Status: accepted
- Date: 2026-09-02

## Context

The WHY feature ([ADR 10](0010-deterministic-diagnosis-before-ai.md)) answers
"why is this broken?" from what the cluster reports. Its rules need more than
the dashboard does: events, endpoint slices, node conditions and volume claims.

Those four are not free. Events in a busy namespace run to thousands of objects
and turn over constantly. Nodes are cluster-scoped and often denied to a scoped
service account. On a large cluster, adding them to the dashboard's pass would
multiply the cost of a screen that refreshes on a timer
([ADR 17](0017-timed-refresh-not-watches.md)) — and almost all of it would be
spent on applications nobody is looking at.

The alternative shape is a cache: keep events and endpoints for the scope in
memory, updated by watches, and diagnose from it. That is what a controller
does, and it is the wrong shape here for the same reasons watches are.

## Decision

Evidence is fetched **per scope, at the moment it is needed**, in a second pass
that is separate from the dashboard's.

- The dashboard's pass reads workloads, pods, services and ingresses. It stays
  cheap enough to run on a timer.
- The evidence pass reads events (one page), endpoint slices, nodes and volume
  claims. It runs when an application is opened or `Ctrl+W` is pressed, and it
  refreshes on the timer only while one of those screens is on.
- Both passes are concurrent, bounded and gap-tolerant: a kind that cannot be
  read is recorded and named on screen rather than failing the pass.

The engine is built for this: **every rule degrades with the evidence
available**. With pod states alone it still explains a crash loop from the
previous run's exit code; with events it can quote the probe that failed; with
endpoints it can tell "the selector matches nothing" from "the pods are not
ready". A rule that cannot support a cause says what it observed and lowers its
confidence instead of guessing convincingly.

Findings are computed when data arrives, not while rendering, so `View` stays a
pure function cheap enough to run on every keystroke.

## Consequences

- Opening Correlux costs one pass; asking why costs a second one. Nobody pays
  for the second unless they ask a question.
- An explanation can be shown before its evidence arrives, and it says so on
  screen ("Events, endpoints and node state have not been read yet"). It then
  improves in place. A partial answer that admits what it is missing is more
  useful during an incident than a spinner.
- Evidence is a snapshot, not a stream: an event that arrives a second after the
  pass is missed until the next one. For explaining a state that has already
  lasted long enough for someone to notice it, that is not a limitation.
- The rules stay testable without a cluster, because they take evidence as data.
  A test writes down the objects and asserts the sentence.

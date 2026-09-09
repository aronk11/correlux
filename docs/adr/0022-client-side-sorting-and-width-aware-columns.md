# 22. A table is ordered on the client, and its columns follow the width

- Status: accepted
- Date: 2026-09-09

## Context

Correlux renders every resource table from the API server's own output
([ADR 13](0013-server-side-tables.md)). That decision answers *which columns*
and *what is in them*. It left two questions unanswered, and both of them turn
out to be the reader's rather than the server's.

**In what order?** The filter answers "which rows": `restarts>5` is "show me
the ones that keep dying". Nothing answered "in what order", so the only way to
find the worst pod in a namespace was to guess a threshold in advance and
narrow until few enough rows were left to read. During an incident that is the
wrong shape of question — you do not know the threshold yet, which is why you
are looking.

The API server cannot help here. `list` offers no ordering, and the printer
columns a CRD declares are strings rendered for display: there is no field
selector that says "sort by the third `additionalPrinterColumn`". Ordering
either happens on the client or it does not happen.

**At what width?** Kubernetes marks a printer column with a `priority`, and
`kubectl` reads that as "hide unless `-o wide`". Correlux inherited the
reading, and it is the wrong one for a full-screen terminal application.
`kubectl` prints into a stream and cannot know how wide the reader's terminal
is. Correlux lays out a frame and knows exactly. Carrying `kubectl`'s guess
across meant a 200-column terminal drew the same four columns an 80-column one
drew and left the rest of the row blank, with the node, the IP and the manager
that deployed the workload one keystroke away for no reason but a flag's
default.

## Decision

**Tables are sorted on the client, over the rows already loaded.** `s` picks a
column, clicking a heading does the same, and choosing the column a table is
already in reverses it.

Sorting reads a cell exactly as the filter compares one — the same
`internal/domain/query` parsers, so an age column is a duration and every other
one is a number with the suffixes Kubernetes prints. This is not a convenience;
it is the whole reason the feature can exist without per-kind code. A table
where `age<1h` and *order by age* disagreed about what an age is would be worse
than one that could not sort at all, and `500m` genuinely means half a CPU in
one column and five hundred minutes in another.

Three rules follow from wanting the result to be readable rather than merely
ordered:

- A cell with no value — empty, `—`, `<none>` — sorts last in **both**
  directions. An unset value is not a small one, and a column sorted worst-first
  must not open on a screen of blanks.
- The sort is stable, so rows the column cannot tell apart keep the order the
  server gave them. A table that reshuffled its equal rows on every timed reload
  would be unusable at a two-second interval.
- Counted columns — restarts, age — are entered descending. The reason to order
  a table by restarts is never to find the pod that has not restarted.

Each screen keeps its own order, and the default order stays what that screen is
for: worst-first on the application dashboard, the server's own order in a
resource table.

**Columns follow the terminal's width.** A table draws every column that fits.
`priority` is read as "first to go when space runs out", not "hidden while space
remains". The toggle stays and flips away from what is *on screen* rather than
from the last value of a flag, so it always does something; asked for
explicitly, the wide columns narrow their neighbours rather than answering the
key with no visible change at all.

## Consequences

- **A sort covers the rows Correlux has, not the ones it has not fetched.**
  This is the honest limit of a client-side sort over a paged list
  ([ADR 6](0006-lazy-scoped-loading.md)), and it is stated on screen rather
  than left to be inferred: a sorted table with pages outstanding says
  `sorted among those loaded`, and the keystroke that chose the order says so
  too. The alternative — reading every page before ordering — is an unbounded
  LIST against somebody's production API server, which is exactly what ADR 6
  exists to prevent. Correlux would rather narrow a claim than widen a query.
- Sorting needs no code per resource kind, and works on a custom resource's own
  printer columns, for the same reason the filter does.
- The order on screen is never something to remember having asked for: the
  heading carries `↑`/`↓` (`^`/`v` where the terminal cannot do better), and the
  direction is also written in words when it is chosen
  ([ADR 9](0009-accessibility-and-terminal-capabilities.md)).
- The frame costs one extra column-layout pass, so the status bar can report
  whether the wide columns are actually drawn rather than what a flag was last
  set to. Measured at ~0.7 ms for a 5,000-row table against a 150 ms budget, and
  guarded by `BenchmarkViewLargeTable`. The table a frame draws is built once
  and shared within that frame; before, three callers each filtered and sorted
  every row again.
- Marking a column wide is now a statement about *layout priority* rather than
  about visibility. A column that should genuinely never be shown by default
  does not exist in this model — and none of the ones Kubernetes marks are that.

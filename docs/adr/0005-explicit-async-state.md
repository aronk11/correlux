# 5. Remote data carries an explicit lifecycle and a generation counter

- Status: accepted
- Date: 2026-09-01

## Context

The two most damaging lies a Kubernetes UI can tell are:

1. "There is nothing here" when the request is still in flight.
2. "Here is your data" when the data belongs to the cluster you just left.

Both come from modelling remote data as a plain slice. An empty slice means
"empty" and "not loaded yet" at the same time, and a response that arrives after
a context switch has nothing stopping it from overwriting fresh state.

During an incident, the first lie sends someone looking for a deleted workload
that is actually running; the second shows them a healthy cluster while the
broken one is on fire.

## Decision

Every value fetched from Kubernetes is wrapped in `async.Value[T]`, which holds:

- an explicit state: `Idle`, `Loading`, `Ready` or `Failed`;
- the last known value, retained across a failure so a refresh does not blank
  the screen;
- the error;
- a monotonic **generation** counter.

`Start()` bumps the generation and returns it. A response may only be applied if
its generation is still current, so answers for a superseded request — a
previous context, a cancelled refresh — are dropped rather than displayed.

The UI must render the four states differently. "Loading…", "none visible",
"listing not permitted for this user" and "unavailable — connection refused" are
four different sentences, never one blank area.

## Consequences

- Every remote field costs a few lines more than a bare slice.
- Stale-response bugs become impossible by construction rather than by review
  attention, and the behaviour is covered by tests that would otherwise require
  a racing cluster to reproduce.
- In-flight requests are also cancelled through their `context.Context` when they
  become irrelevant, so a dead cluster does not hold connections open.

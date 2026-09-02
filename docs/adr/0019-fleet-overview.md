# 19. A fleet overview across several kubeconfig contexts, read-only

- Status: accepted
- Date: 2026-09-02

## Context

kubeui is a single-cluster tool by construction: one context is active, one
scope is active, and every keystroke acts on that one place
([ADR 7](0007-session-local-context-switching.md)). That is what makes it safe.

The question is whether there should also be a mode that answers "is anything
broken *anywhere*?" across several contexts at once.

The case for it is real. People run three to thirty clusters — dev, staging,
production, per region — and during an incident or a fleet-wide rollout the
first question is not about one cluster. Checking them one at a time means
switching context, waiting for the dashboard, reading it, and holding the
result in your head while you do it again. That is precisely the work kubeui
exists to remove, one level up.

The application-first model is what makes it worth building here rather than
anywhere else: an application deployed to five clusters is one logical thing,
and kubeui already infers it from each cluster the same way
([ADR 16](0016-application-inference.md)). A fleet view is that inference,
grouped by name across clusters, with health per cluster. No other terminal
Kubernetes tool does this, and it falls out of work already done.

The case against is cost and risk, and both are concrete:

- **Cost multiplies.** One dashboard pass is nine list calls per scope. Thirty
  contexts is 270, and with the timed refresh running that repeats.
- **Authentication is not free or silent.** Many contexts use exec credential
  plugins that want a browser or a TTY. A view that touches every context on
  open would fan out to every credential helper the user has, and a TUI cannot
  answer an interactive prompt ([ADR 4](0004-layered-architecture.md) keeps
  `WarningHandler`/prompting out of the UI for exactly this reason).
- **Safety gets harder.** Production confirmations depend on knowing which
  cluster a keystroke hits. A screen showing eight clusters at once makes that
  ambiguous, and "which cluster was that?" is the mistake that ends careers.
- **An aggregate can lie.** "12 applications healthy" is false if two of eight
  clusters could not be read. kubeui's whole posture is that loading, empty and
  denied never look alike; a fleet view multiplies the ways to get that wrong.
- **Scope becomes two-dimensional.** Today a scope is a namespace (SPEC 8). A
  fleet scope is a set of context/namespace pairs, and namespaces differ
  between clusters.

## Decision

Build it, as an explicitly opt-in **read-only overview**, under five rules.

1. **Only the contexts the user names.** A fleet is configured, or assembled in
   a picker; never "every context in the kubeconfig" by default. Nobody should
   discover that opening kubeui authenticated against every production cluster
   they have credentials for.
2. **Read-only, and drilling in switches the session.** Enter on a row switches
   kubeui to that cluster and opens the application there. Nothing is ever
   mutated from the fleet view, and there is never a keystroke whose target
   cluster is ambiguous: either you are in the overview and looking, or you are
   in a cluster and acting.
3. **Per-cluster state is always visible.** Each context carries its own
   lifecycle — connecting, ok, unreachable, not permitted — exactly as remote
   values do today ([ADR 5](0005-explicit-async-state.md)). Counts state what
   they cover: "3 down across 6 of 8 clusters; 2 unreachable".
4. **Bounded fan-out.** Contexts are loaded through a small worker pool rather
   than all at once, each with its own cancellable command and timeout, so one
   dead cluster cannot delay the rest and thirty clusters cannot open hundreds
   of concurrent requests.
5. **No timed refresh by default**, and a longer interval when it is switched
   on: the cost of a tick is multiplied by the size of the fleet
   ([ADR 17](0017-timed-refresh-not-watches.md)).

A context that needs an interactive credential helper is reported as such and
left alone until the user opens it. Fleet mode never triggers a login it cannot
answer.

## Consequences

- SPEC 8 needs extending: a scope becomes a set of context/namespace pairs, and
  the configuration file grows a `fleets:` section. SPEC 5's navigation model
  gains one level above the cluster: Fleet → Cluster → Scope → Application.
- SPEC 31 says kubeui is not a cloud management platform. A read-only overview
  does not cross that line; the moment the fleet view could act on many clusters
  at once, it would.
- The domain layer needs nothing. `application.Group` already produces a list
  per snapshot; the fleet view groups those lists by application name and keeps
  the cluster on each row. The work is in the model (per-context async values, a
  bounded loader) and in one new screen.
- This is a phase of its own, and it is not urgent: it makes kubeui better for
  people who already use it, while object inspection and safe actions (phases 4
  and 5) are what make it usable at all. Sequence it after those.
- If it turns out that people mostly want "which of my clusters is on fire",
  a much smaller version — one row per cluster, no applications — would deliver
  most of that value for a fraction of the cost. That is the fallback if the
  full version proves too heavy in practice.

## What was built

The first version follows all five rules. The screen has two parts: one row per
cluster with its own state, and below it every application that is not healthy,
with the cluster it is not healthy in. Enter on either leaves the overview for
that cluster — a cluster row opens its dashboard, an application row opens the
application, in its namespace.

Members are read four at a time, each with its own timeout, and each answer
appears as it arrives rather than after the slowest cluster. Every total says
what it covers: "5 applications across 2 of 3" is what a fleet with an
unreachable member reports, and the member itself is listed with the reason it
could not be read.

Each member's nodes are read alongside its workloads — one more call, and the
most common thing that is wrong with a cluster belongs to no application at all.
Everything unusual reaches the default view: a node that is not ready, one under
pressure, one merely cordoned, and a kind that could not be read. Showing only
the worst of them is how the rest is discovered too late.

From the overview, `Ctrl+B` browses one resource kind across every member as a
single table. The columns are the API servers' own, merged by name rather than
by position: clusters need not agree — a CRD at two versions prints different
columns — and a cell landing under the wrong heading would be worse than a gap,
so a column one cluster lacks is left empty. A cluster that does not serve the
kind at all is named with the reason rather than quietly left out.

The timed refresh deliberately does not touch these screens. `Ctrl+R` reloads
them, which is the one moment a user has decided the cost is worth paying.

# 17. The screen reloads on a timer the user turns on, not on watches

- Status: accepted
- Date: 2026-09-02

## Context

A dashboard that shows a cluster's health has to be able to keep up with it:
watching a rollout recover by pressing refresh every few seconds is exactly the
workflow kubeui exists to replace.

The obvious answer is watches. `client-go` informers deliver changes as they
happen, which is what every controller in a cluster does. For a long-lived
process that reconciles everything, informers are unambiguously right.

kubeui is not that process. It shows one namespace at a time, for minutes, and
its scope changes whenever the user presses a key. An informer per kind per
scope means a cache of every object in that scope in memory, a resync storm on
every scope change, and a permanent open connection per kind to somebody's
production API server — nine of them for the application dashboard alone.
That is a large amount of machinery, and a large amount of load, for a screen
that is discarded when the user switches namespace.

## Decision

kubeui reloads on a timer, and the timer is off until the user turns it on
(`Ctrl+F`, the command palette, or `refresh.auto` in the config file).

The interval is configurable, defaults to ten seconds and is floored at two.
What makes a poll cheap enough to leave running:

- **Only what is on screen is refetched.** The dashboard on the dashboard, the
  table on a table, the connection probe on the session view. Never discovery,
  never the namespace list: neither changes on the timescale of a refresh, and
  re-discovering every API group every ten seconds would cost more than the
  screen is worth.
- **Requests never stack.** A tick that finds the previous request still in
  flight does nothing.
- **Nothing happens behind an overlay.** While a picker or the palette is open,
  the timer idles.
- **A table someone has paged into is left alone**, because refreshing the first
  page would delete the rows under them.
- **Failures back off exponentially.** An unreachable cluster stays unreachable
  for a while; polling it every ten seconds only produces the same error faster.
- **The cursor follows the object, not the row number**, in both the dashboard
  and the resource table. Without that, a list that re-sorts as health changes
  moves the selection under the user's hands.

The header says `auto 10s` while it is running: a screen that changes on its own
has to admit it.

## Consequences

- kubeui makes no requests at all when nobody is looking at it. Against a
  production API server, that is the difference between a tool that is welcome
  and one that is banned.
- Updates lag by up to one interval. For a human watching a rollout that is
  invisible; for a controller it would be unacceptable, and kubeui is not one.
- The implementation is a `tea.Tick` and two booleans rather than an informer
  factory, which keeps the concurrency story small enough to reason about.
- Watches are not ruled out. If a future screen genuinely needs sub-second
  updates for a bounded set of objects — a single application's pods during an
  exec session — a watch can back that one screen without the rest of the
  application growing a cache. This ADR would then be amended, not superseded.

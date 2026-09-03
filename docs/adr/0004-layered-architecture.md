# 4. The domain layer must not know that a terminal exists

- Status: accepted
- Date: 2026-09-01

## Context

TUIs rot in a predictable way: a widget calls the API "just this once", a render
function starts caching, and after a year the only way to test whether a
CrashLoopBackOff is diagnosed correctly is to press keys in a terminal and look.

Correlux's most valuable logic — application inference, diagnosis, topology —
must be testable exhaustively and cheaply, because its correctness is the
product.

## Decision

Dependencies point in one direction only:

```
Kubernetes API  →  kube/{client,kubeconfig}
                        ↓
                   domain model (application, topology, diagnosis, history)
                        ↓
                   ui/{screens,components,palette}
                        ↓
                   ui/app   (the only package that imports Bubble Tea)
```

Concretely:

- No package below `internal/ui/app` imports Bubble Tea.
- No Kubernetes call is made outside a `tea.Cmd` created in `internal/ui/app`.
- `internal/ui/components` and `internal/ui/screens` receive plain data
  structures and return strings. They contain no Kubernetes types.
- Layout arithmetic (`internal/ui/layout`) and palette ranking
  (`internal/ui/palette`) are pure functions with no UI dependency at all.

## Consequences

- The UI layer must translate Kubernetes state into view data, which is extra
  code. That translation is where "unknown" and "loading" get distinguished
  properly, so it earns its place.
- Rendering is a pure function of the model, so a frame can be produced in a
  test and asserted on — including the awkward sizes.
- A future non-terminal front end (a `correlux report` command, a web view) can
  reuse everything below `ui/app` unchanged.

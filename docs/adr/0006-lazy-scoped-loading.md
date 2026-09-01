# 6. No global cache at startup: load lazily, within the active scope

- Status: accepted
- Date: 2026-09-01

## Context

The obvious way to build a fast Kubernetes UI is to start informers for
everything and serve the UI from a local cache. It works beautifully on a
laptop's kind cluster and falls over on the clusters that matter: ten thousand
pods means hundreds of megabytes of memory, a multi-second stall before the
first frame, and a thundering-herd LIST against an API server that is often
already unhealthy — which is precisely when someone opens kubeui.

## Decision

- kubeui starts by reading the kubeconfig only. The first frame renders before
  any API call completes.
- Data is fetched for the active scope, on demand, and released when the scope
  changes.
- Watches are started per scope and per resource kind that is actually on
  screen, never cluster-wide "just in case".
- Lists are paginated, with an explicit bound; a truncated list says so rather
  than silently showing a prefix.
- Every request is cancellable and bounded by a timeout.
- Large-cluster behaviour is a benchmark, not an opinion: the test suite grows
  fake clusters of 1k/5k/10k pods and asserts that the UI keeps rendering.

## Consequences

- Switching scope costs a fetch. Caches are bounded and per-scope, so the common
  path (switching back and forth between two namespaces) stays fast.
- Some cross-cluster views (a global search across every namespace) are harder
  and must be built as explicit, cancellable operations with progress.
- kubeui works on a cluster where the user may only see one namespace, because
  nothing assumes cluster-wide read access.

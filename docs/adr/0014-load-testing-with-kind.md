# 14. Load is tested on a real API server, with pods that never run

- Status: accepted
- Date: 2026-09-02

## Context

"Responsive on large clusters" is kubeui's core technical claim
([ADR 6](0006-lazy-scoped-loading.md)). A claim that is never measured is a
wish. But measuring it is awkward: the failure modes we care about — an
unbounded LIST, a quadratic render, discovery that takes ten seconds, paging
that never terminates — only appear against a real API server with a real etcd
holding thousands of objects.

The options were: a fake clientset (fast, but tests our own mock rather than
Kubernetes), a real cloud cluster (accurate, but slow, costly and impossible to
run on a laptop or in a fork's CI), or a local cluster with synthetic load.

Running ten thousand real pods locally is not possible. Ten thousand *pod
objects* is trivial — for everything except the kubelet.

## Decision

Integration and load tests run against a local [kind](https://kind.sigs.k8s.io)
cluster, seeded by `test/cmd/seed`.

The seeder registers a **node object with no kubelet behind it** and binds pods
directly to it via `spec.nodeName`, then writes their status itself. The API
server and etcd carry exactly the load they would in production; nothing is ever
scheduled, pulled or started. Pods tolerate the `unreachable` and `not-ready`
taints without a timeout, so the node lifecycle controller does not evict the
load five minutes into a benchmark.

Around that:

- Deployments are created **paused**, with a ReplicaSet already at its desired
  count, so the real controllers observe a satisfied state and create nothing of
  their own. The ownership graph is genuine — Deployment → ReplicaSet → Pod —
  which is what Phase 2's application inference will be tested against.
- The seeded cluster is deliberately **not healthy**: a fixed, reproducible
  share of applications are degraded or down. A tool for finding what is broken
  must be tested on a cluster that has something broken in it.
- CRDs are installed with `additionalPrinterColumns`, including one with
  `priority: 1`, so the table rendering in
  [ADR 13](0013-server-side-tables.md) is verified end to end rather than
  against a hand-written fixture.
- One test deliberately breaks an aggregated API — the single most common cause
  of "my Kubernetes UI shows nothing" — and asserts that discovery degrades to
  "sixty kinds, one group unavailable".
- Everything the seeder creates carries `app.kubernetes.io/managed-by=kubeui-seed`,
  so `--clean` removes precisely the load and nothing else.

Integration tests are behind the `integration` build tag and need
`KUBEUI_TEST_KUBECONFIG`, so `go test ./...` stays fast and hermetic. A modest
seed runs on every pull request; the ten-thousand-pod benchmarks run nightly and
on demand.

## Consequences

- Contributors need Docker for the integration tasks. The unit tests, which are
  the bulk of the suite, need nothing.
- Synthetic pods are not identical to real ones: no kubelet means no real
  container statuses beyond what the seeder writes, and no metrics. That is the
  right trade — the code under test reads the API, and the API is real.
- Benchmarks measure a local kind cluster, so absolute numbers are not a
  production SLA. They are a regression signal, which is what they are for.

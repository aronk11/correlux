# 13. Resources are rendered from the API server's own tables

- Status: accepted
- Date: 2026-09-02

## Context

kubeui must show every resource in a cluster, not a hard-coded list of the
dozen kinds we thought of. On a real cluster, a large share of what an operator
works with comes from CRDs — Argo Applications, Flux Kustomizations, Cert
Manager Certificates, an in-house Deployment abstraction — and a tool that
treats those as second-class ("here is some YAML, good luck") is a tool people
leave open in a second terminal next to `kubectl`.

The naive implementation is a column layout per kind. That means code for every
resource we support, no support for anything we have not heard of, and a
guaranteed mismatch with what `kubectl get` shows for the same object.

## Decision

kubeui asks the API server to render the table, using the `Table` content type
that `kubectl get` itself uses:

```
Accept: application/json;as=Table;v=v1;g=meta.k8s.io
```

The server returns column definitions and pre-rendered cells for **any**
resource. For a CRD, those are exactly the `additionalPrinterColumns` its author
declared — including their `priority`, which is how `kubectl get -o wide`
decides what to hide.

Consequences of that single decision:

- CRD support requires no per-resource code, and a CRD installed while kubeui is
  running works after a refresh.
- kubeui's columns match `kubectl get` for every kind, so what a user sees here
  is what they will see in their terminal and in their runbook.
- The formatting work happens on the server; kubeui transfers cells, not whole
  objects. A page of 500 pods costs a fraction of what fetching 500 PodSpecs
  would.
- Paging is the server's `limit`/`continue`, so the memory cost is a page, not a
  collection ([ADR 6](0006-lazy-scoped-loading.md)).

Cells are formatted defensively: a printer column on a custom resource can
contain any JSON its author chose, and an unexpected shape must render as text,
never panic.

## Consequences

- kubeui shows what the server chose to print. Where a column is genuinely
  missing for an operational task, the answer is a purpose-built view for that
  case (the application dashboard, the diagnosis panel), not a per-resource
  table layout.
- Very old API servers that cannot print tables are detected and reported.
  Kubernetes has served tables since 1.10; this is a clear message, not a
  fallback path we maintain forever.
- The row objects are `PartialObjectMetadata`, so anything needing the full
  object (YAML, describe, diagnosis) fetches it on demand — which is the right
  shape anyway.

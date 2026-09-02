# 3. Applications, not resource types, are the primary navigation model

- Status: accepted
- Date: 2026-09-01

## Context

Existing terminal tools present Kubernetes the way the API presents itself: a
list of resource types, each containing objects. That mirrors the API server
faithfully, and it is the wrong first screen for the job people actually do.

When something is broken, nobody wants a list of ReplicaSets. They want to know
which application is unhealthy, why, and what is safe to do about it. The
resource-type view forces the operator to do the join themselves — Deployment to
ReplicaSet to Pod to Service to Endpoints — repeatedly, under time pressure,
from memory.

## Decision

The primary navigation model is:

```
Cluster → Scope → Application → Problem / resource
```

Applications are inferred from Kubernetes relationships (owner references,
selectors, endpoints, common labels), not declared by the user and not requiring
a CRD. The raw resource views remain available and complete; they are a drill-
down, not the entry point.

The product metric this optimises is the time from "something is wrong" to "I
know why".

## Consequences

- Inference can be wrong. Grouping must therefore be explainable ("these objects
  are one application because …") and overridable, never silent.
- The domain model needs a real `application` package with its own tests, rather
  than a view that happens to group rows.
- Users arriving from other tools must be able to reach the familiar
  resource-type lists immediately, or Correlux will feel like it hides things.
- The dashboard cannot be built from a single API call, which forces the lazy,
  scoped loading strategy in ADR 6.

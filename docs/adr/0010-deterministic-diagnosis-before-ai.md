# 10. The diagnosis engine is deterministic; AI is an optional explanation layer

- Status: accepted
- Date: 2026-09-01

## Context

The `WHY` feature — telling an operator the root cause of a failure — is the
core of Correlux's value. The tempting implementation is to send the cluster
state to a language model and print the answer.

That fails the requirements the feature actually has: it must work offline and
in air-gapped clusters, it must be identical for the same input, it must be
testable, it must not send cluster state to a third party by default, and it
must never be confidently wrong about why production is down.

## Decision

- The diagnosis engine is a set of deterministic rules over observable
  Kubernetes state: pod conditions, container states, restart counts, events,
  owner references, selectors, endpoints, probes, scheduling failures, node
  conditions, PVC state, rollout state, HPA and PDB state.
- Every diagnosis produces: problem, cause, evidence, related resources,
  recommended action and an explicit confidence level. Evidence is mandatory —
  a conclusion the user cannot verify is a guess.
- Every rule has unit tests over fixture objects.
- AI is optional, off by default, and never a dependency of the core product.
  When enabled, it receives a **pre-assembled context package** (the diagnosis,
  the relevant resources, events, topology) — never unrestricted cluster access —
  and its role is to explain, not to decide.

## Consequences

- Rule coverage grows slowly and deliberately; the engine will say "I don't
  know" more often than a language model would, which is the correct behaviour
  for a tool people trust during an incident.
- The rules are portable: the same engine can back a `correlux why` command or a
  CI check, because it is a pure function of cluster state.

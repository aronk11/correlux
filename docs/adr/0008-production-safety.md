# 8. Production contexts are classified heuristically and guarded by default

- Status: accepted
- Date: 2026-09-01

## Context

kubeui will eventually offer destructive actions: restart, scale, delete, edit.
A terminal UI where a single unlabelled keystroke can delete a production
Deployment is a liability, and "the user should pay attention" is not a design.

Kubernetes itself has no notion of "this cluster is production". The information
exists only in naming conventions and in the operator's head.

## Decision

- kubeui classifies a context as production by matching case-insensitive
  patterns against the context name, the cluster name **and the API server
  URL** — a context named `eu` pointing at `api.prod.example.com` is production.
  The default pattern set covers `prod`, `prd`, `production` and `live` as whole
  words, so `reproducer` and `product-team` do not match.
- Patterns and an explicit list of context names are configurable, because no
  heuristic survives every organisation's naming.
- A false positive costs one extra confirmation; a false negative costs an
  outage. When in doubt, classify as production.
- Production status is shown as a text badge (`PROD`), never as colour alone.
- Mutating actions are explicit, never bound to a bare letter key, and
  production contexts require a stronger confirmation by default
  (`dangerousActions.productionConfirmation`, on unless turned off).

## Consequences

- Users with unconventional naming must configure patterns, or they will see the
  weaker confirmation on a cluster that matters. The classification is therefore
  visible in the UI (`doctor` reports it too) rather than hidden.
- Every destructive action added later must state its blast radius in the
  confirmation ("this will remove 3 replicas"), which is a requirement on the
  action, not on the dialog.

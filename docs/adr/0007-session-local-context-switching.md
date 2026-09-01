# 7. Switching context inside kubeui never writes to the kubeconfig

- Status: accepted
- Date: 2026-09-01

## Context

A Kubernetes UI that switches context by running the equivalent of
`kubectl config use-context` changes global state for every other shell, script
and tool the user has open. The classic incident is: an engineer browses
production in a TUI, closes it, runs `kubectl delete` in a terminal they believe
is pointed at staging, and is wrong.

## Decision

Context and namespace selection inside kubeui is session state. kubeui reads the
kubeconfig and never writes to it. A REST client is built per context from the
in-memory merged configuration, and an external `kubectl` keeps pointing exactly
where the user left it.

The active context is therefore always displayed, and production contexts are
badged in text as well as colour (ADR 8), because the user can no longer infer
the target from their shell.

## Consequences

- The UI carries the burden of making the target obvious at all times. The
  header is never allowed to be ambiguous, and shells opened from kubeui
  (`exec`) inherit the kubeui context explicitly rather than the ambient one.
- Users who want kubeui to change their shell context must do it themselves;
  we may later offer an explicit, clearly labelled action, but never as a
  side effect of navigation.

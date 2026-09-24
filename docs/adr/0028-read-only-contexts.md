# 28. A read-only context is refused at the transport, not only in the UI

- Status: accepted
- Date: 2026-09-23
- Amends: [8](0008-production-safety.md), [20](0020-changes-go-through-one-gate.md)

## Context

The production confirmation ([ADR 8](0008-production-safety.md)) makes a
change deliberate. It does not make a change impossible, and that is what
many platform teams ask of a tool before it is allowed near production at
all: a laptop that may look at every cluster and touch none of them, or a
break-glass account that is used to read during an incident and not to act.

RBAC is the real answer, and remains so. But the credentials an engineer holds
are usually the ones they need for their own work, and a second kubeconfig per
cluster just to be unable to write is friction people route around.

## Decision

Correlux has a read-only mode, chosen per session or per context:

- `--read-only`, or `dangerousActions.readOnly: true`, covers every context;
- `dangerousActions.readOnlyProduction: true` covers the contexts classified
  as production — the ones the header marks `PROD`;
- `dangerousActions.readOnlyContexts` names individual contexts.

It is enforced twice.

**In the UI**, so that a refusal is legible. The keys that change a cluster or
open a session inside one — scale, restart, roll back, delete, cordon, edit,
shell — are not advertised, and pressed anyway they answer with the reason.
Palette entries stay listed, disabled, with the reason beside them. The header
says `read-only` next to the cluster's name. The confirmation gate refuses too,
so an entry point that forgets to ask is still stopped before anything is shown
as confirmable.

**At the transport**, so that it is true. Every client the factory builds for a
read-only context carries a round-tripper that refuses, before the request
leaves the machine, every method other than GET, HEAD and OPTIONS — and GETs on
`exec`, `attach` and `portforward`, because a websocket exec is a GET. The one
POST it lets through is a self-subject access or rules review, which is how
`correlux doctor` asks what the account may do. The refusal is `ErrReadOnly`.

Helm runs as a separate process with its own copy of the kubeconfig; it is kept
out by the UI half, since every Helm action that writes goes through the gate.

## Consequences

- A read-only session cannot open a shell, attach a debug container or forward
  a port. That is the point: each of those can do anything a change can.
- Nothing about RBAC changes. An account that may write still may, from
  `kubectl`; Correlux only declines to be the tool that does it.
- The transport decides from the same classifier and the same three names —
  context, cluster, server — the header's `PROD` badge is computed from, when
  the client is built. A kubeconfig reload resets the clients, so a context
  that appears in a reloaded file is classified like any other.

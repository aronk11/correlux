# 29. Rollout revisions answer "what changed?", and roll back through the gate

- Status: accepted
- Date: 2026-09-23
- Amends: [10](0010-deterministic-diagnosis-before-ai.md), [18](0018-evidence-on-demand.md), [20](0020-changes-go-through-one-gate.md)

## Context

"What changed?" is the first question in most incidents, and SPEC 1 lists it.
The WHY engine could say that pods crash, and why the kubelet thinks so, but
not that they started crashing four minutes after somebody changed the image.

Kubernetes keeps no audit log a client can rely on reading. It does keep one
memory of change: a Deployment leaves its previous ReplicaSets behind, each
stamped with a revision number and carrying the pod template it was made from,
until `revisionHistoryLimit` prunes them.

## Decision

**Revisions are evidence.** The on-demand evidence pass
([ADR 18](0018-evidence-on-demand.md)) also lists the scope's ReplicaSets, keeps
those a Deployment controls — its newest three, and any still running pods —
and flattens each template into the fields that change behaviour: images,
commands, arguments, environment, resources, probes, mounts, volumes and
placement. The pod-template-hash label is left out, because it differs on every
revision and would be the answer every time.

Two rules read them, and they make different claims:

- `workload.revisionfailing` — the newest revision's pods are not ready while
  the previous revision's are. That is a comparison the cluster shows, stated
  with medium confidence, with the changed fields as evidence.
- `workload.changed` — the application is failing and its template changed in
  the last six hours. That is a coincidence in time, stated as one: low
  confidence, and an `Unknown` saying the cluster cannot establish causation.

Neither outranks a pod finding: a crash loop's reason is still the first thing
on screen, and the change is the next. Environment values are compared and
never quoted, because WHY is read on shared screens.

A third rule, `workload.stalled`, quotes the Deployment controller when it has
given up on a rollout (`Progressing=False`, usually `ProgressDeadlineExceeded`),
and counts as a consequence whenever pods are failing.

**Rolling back is a change like any other.** `U` on a Deployment reads its
revisions, asks which one — the previous by default — and says, while the
number is typed, what that revision changes back. The confirmation carries the
same field list and the template diff, and passes through the gate
([ADR 20](0020-changes-go-through-one-gate.md)). The write is what
`kubectl rollout undo` does — the revision's template, without its hash label,
replaces the Deployment's — as a JSON patch whose first operation tests the
`resourceVersion` that was read, so a Deployment somebody changed in between is
refused rather than overwritten. A paused Deployment is refused, as kubectl
refuses it.

## Consequences

- The evidence pass costs one more paged list per scope, and memory bounded by
  three flattened templates per Deployment.
- StatefulSets and DaemonSets keep their history as ControllerRevisions. They
  are not rolled back by Correlux yet, and the key says so by name rather than
  attempting half of it.
- A Deployment managed by Flux or Argo CD will be reconciled back to its
  source. The confirmation says so; the durable fix is in Git.

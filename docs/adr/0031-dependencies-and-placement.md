# 31. Dependencies are read from configuration, and placement is a finding before it is an outage

- Status: accepted
- Date: 2026-11-14
- Amends: [10](0010-deterministic-diagnosis-before-ai.md), [16](0016-application-inference.md)

## Context

SPEC 10 opens with the example every operator recognises: payments is down
*because* PostgreSQL has no ready endpoints. The WHY engine explained each
application from its own objects and stopped at its edge, so the two failures
were two rows and the join stayed in the reader's head.

Kubernetes records no dependencies. A service mesh or eBPF would know the
traffic; Correlux installs neither (SPEC 2).

Separately, "3/3 healthy" says nothing about where the three are. Replicas on
one node, or in one zone of a three-zone cluster, are an outage that has not
happened yet, and the usage view could list nodes but not answer which pool
is full or whether one zone carries everything.

## Decision

**Dependencies come from two places that state them.** The
`a8r.io/dependencies` annotation, and the literal environment of the pods. A
variable counts when its value is a URL, a `.svc` name, or when its own name
says it holds an address. The host is resolved the way cluster DNS would
against Services in the snapshot; what resolves to nothing is dropped. Only
the variable's name and the host are kept — the value is where credentials
live. Variables filled from Secrets or ConfigMaps are not followed: reading
every Secret to draw an arrow is not a trade Correlux makes.

**A dependency is evidence of configuration, never of causation.** The
finding `dependency.unhealthy` is informational on a healthy application, a
warning at medium confidence when both are failing, and never critical: it
must not outrank what the pods report or change how unhealthy an application
is counted. Its `Unknown` always says that the cluster shows where the
application points, not that the calls fail.

**Placement is a fact worth stating early.** `placement.concentrated` fires,
informationally and with high confidence, when every running pod of a
replicated workload shares a node while another schedulable node exists, or a
zone while the cluster has another. It says whether the pod template asked to
be spread, because the fix differs. It is silent where there was no choice.

**Zones and pools are roll-ups of the nodes already read.** No extra request:
the usage view groups the same node rows by the labels the clouds write,
fullest first, and draws a roll-up only where there are two or more groups to
compare.

## Consequences

- The graph is incomplete by construction: dependencies configured through a
  ConfigMap, a Secret or a mounted file are invisible. A missing arrow means
  "not stated in the pod", never "does not exist".
- An environment variable can name a Service the application never calls. The
  evidence line names the variable so the reader can judge.
- Scoped to one namespace, a dependency in another namespace is not resolved,
  because its Service is not in the snapshot.
- Placement findings appear on healthy rows. They are informational and do
  not change health, but a cluster that deliberately packs replicas will see
  them until its workloads say so with a spread rule.

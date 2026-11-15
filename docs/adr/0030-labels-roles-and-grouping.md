# 30. Labels are read, roles are recognised by name, and the dashboard groups by both

- Status: accepted
- Date: 2026-11-14
- Amends: [16](0016-application-inference.md)

## Context

The dashboard answered "what is broken" and nothing that starts with
"everything that…": everything of the checkout system, everything one team
owns, everything the platform itself is made of. Those are the questions a
platform engineer asks of a cluster with two hundred applications in it, and
the cluster already holds the answers as labels.

It also printed the ingress controller and a shop frontend on consecutive
lines as if they weighed the same. One is a team's incident and the other is
everybody's.

Grouping itself had gaps that showed on real clusters: kubeadm's static pods
were filed under an application named after their node, every Knative revision
and every pipeline run was its own application, and a Service disappeared from
an application the moment that application had no pods left — which is when it
is opened.

## Decision

**Traits are read, never inferred.** `part-of`, component, version, team,
environment and the `a8r.io` runbook, repository and chat annotations are
copied from the workloads, in the order the conventions rank them. A label
nobody set is an empty field and no column. The one fallback is the version,
which is the image tag of the pods' first container when no label states one,
because that is a fact about what is running.

**A role is recognised by whole names against a list.** The software's own
name label, then the application's name, then the image it runs, matched
exactly or by a family prefix that ends at a dash. Never by substring: `kong`
is an ingress controller and `kongregate-api` is somebody's application. The
evidence is kept and shown, so the claim can be checked against the object.
An application not on the list has no role. A role is a fact about a name; it
carries a sentence about what leans on it, shown only while the application is
unwell, and that sentence never claims the failure has already had the effect.

**Grouping keeps the order it was given.** The dashboard can be grouped by
namespace, role, system, team or manager. Groups appear in the order of their
first member and members keep theirs, so worst-first stays worst-first and a
sort stays a sort. The group is one leading cell, printed once with a count and
how many are unwell; rows stay one line each, so scrolling, selection and
clicking need no second set of rules. A grouping no application has a label
for is skipped.

**Operator labels are a grouping signal**, ranked after the two recommended
labels and before the loose `app`: they are written by a controller on exactly
the objects of one cluster, pipeline or service. Static pods are named by
their `component` label or their own name without the node's. A Service joins
an application without pods when a workload's own selector contains every pair
the Service selects on.

## Consequences

- The list of names is maintenance. It is one map, and a missing entry costs a
  missing word rather than a wrong one.
- Team and environment labels have no standard. A handful of common spellings
  are read; an organisation with its own spelling gets no column until that is
  made configurable.
- Namespace labels are not read. Teams often label the namespace rather than
  the workload, and that needs namespaces in the snapshot.

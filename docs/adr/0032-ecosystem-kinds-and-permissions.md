# 32. Ecosystem kinds are read where served, and permissions are asked rather than tried

- Status: accepted
- Date: 2026-11-14
- Amends: [16](0016-application-inference.md), [17](0017-timed-refresh-not-watches.md), [18](0018-evidence-on-demand.md)

## Context

Three kinds outside core Kubernetes decide what an application is on the
clusters Correlux is used on: Gateway API routes are replacing Ingress, an Argo
Rollout stands where a Deployment would, and an Argo CD Application is what a
Flux Kustomization is elsewhere. Without them an application lost its way in,
its replica count, or the reason Git was not being applied.

Separately, the only way to learn that an account may not restart a workload
was to confirm a restart.

## Decision

**Routes and Rollouts are read on the dashboard pass, into the slots that
exist.** A route is an `Ingress` with its kind kept; a Rollout is a `Workload`.
Every screen and rule that handles the core kind handles them, and nothing
branches on a list of known kinds beyond the two conversions.

**An API that is not served is asked once.** The dashboard pass can run twice
a second ([ADR 17](0017-timed-refresh-not-watches.md)). A 404 for
`gateway.networking.k8s.io` is remembered per cluster for the session, so a
cluster without it costs one request, not one per refresh. A Gateway API
installed mid-session is seen after a restart; that is the price. A refusal is
treated the same way: the API server authorises before it looks a resource up,
so an account whose role does not mention the group is answered 403 whether or
not the group exists, and reporting that put a gap on every application of
clusters that never had a route.

**Argo CD Applications are evidence**, read with the Flux objects on the
on-demand pass ([ADR 18](0018-evidence-on-demand.md)), in the application's
namespace and in Argo CD's own. Its health roll-up is not a finding: it is
computed from the pods the other rules already explain.

**Permissions are asked with SelfSubjectAccessReview** when the session screen
opens, for the actions Correlux offers, against Deployments and Pods. An
unanswered question is shown as unanswered. The answers inform; they do not
yet hide keys, because a wrong "no" from a webhook authoriser would remove an
action somebody is entitled to.

## Consequences

- Sizing remarks, placement and permissions are all single readings. Each says
  so where it is shown.
- The permission questions name Deployments and Pods. An account allowed to
  scale StatefulSets and not Deployments reads as refused.
- Rollouts cannot be rolled back with `U`; that stays a Deployment action,
  since a Rollout's own controller owns promotion and abort.

# 16. Applications are inferred from the cluster, not declared to kubeui

- Status: accepted
- Date: 2026-09-02

## Context

kubeui's first screen is a list of applications ([ADR 3](0003-application-first-navigation.md)).
Kubernetes has no application object to list. It has Deployments, ReplicaSets,
Pods, Services and Ingresses, and the relationships between them are expressed
three different ways at once: owner references, label selectors and naming
conventions.

Every tool in this space picks one of three answers:

1. **Ask the user.** A config file that maps applications to resources. It is
   exact, it is always out of date, and a tool that needs configuration before
   it is useful during an incident is not useful during an incident.
2. **Require an operator or a CRD.** Correct, and it means installing something
   into a production cluster before kubeui can show anything. That contradicts
   the product: kubeconfig in, information out.
3. **Infer it.** What an operator already does in their head, done once and
   consistently.

## Decision

kubeui infers applications, in `internal/domain/application`, from what is
already in the cluster:

1. **Ownership first.** Pods are walked up their owner references to the
   controller that is not itself owned: Pod → ReplicaSet → Deployment, Pod →
   Job → CronJob. Ownership is the only relationship Kubernetes itself
   guarantees, so it outranks every convention.
2. **Labels next.** The root controller is named by `app.kubernetes.io/instance`,
   then `app.kubernetes.io/name`, then `app`, then `k8s-app`. The instance label
   comes first because it identifies one *installation* — a Helm release, an
   Argo application — which is what a person means by "the payments app"; the
   name label would merge two independent installations of the same chart.
3. **Selectors and backends last.** A Service joins the application whose pods
   its selector actually matches; an Ingress joins through the Service it routes
   to.

Two rules keep the result trustworthy:

- **Nothing is invented.** An object that matches nothing is left out of the
  dashboard rather than turned into an application of one. It remains fully
  visible in the resource browser.
- **Nothing is interpreted.** Health is derived from replica counts and pod
  states as the cluster reports them, and stops there. "Two pods are in
  CrashLoopBackOff" is an observation; "the database is unreachable" is a
  diagnosis, and belongs to the engine in
  [ADR 10](0010-deterministic-diagnosis-before-ai.md).

The inference is a pure function of a snapshot. Fetching lives in
`internal/kube/workloads`, so every rule is testable by writing down objects and
the grouping they should produce, with no cluster and no fixtures to record.

## Consequences

- kubeui is useful on any cluster immediately, including one whose owners never
  heard of it.
- The grouping is a heuristic and will occasionally surprise. That is acceptable
  because it is never load-bearing: the application view is a lens over the
  objects, and every object is reachable through the resource browser
  regardless of where the lens put it.
- A cluster with no labels at all still produces a sensible list, because
  ownership alone names an application after its controller.
- A chart that labels twenty workloads with one release name produces one large
  application. That is the correct reading of "instance", and the detail view
  shows what it is made of.
- Custom controllers are handled for free when they set owner references, which
  well-behaved ones do. When they do not, their pods fall back to labels.

# Permissions Correlux needs

Correlux uses your kubeconfig and asks the API server for exactly what the
screen you are looking at needs. Nothing runs in the cluster, nothing is
installed, and no permission is required before you start: an account that may
read nothing still opens the session screen and says so, and every feature you
lack access to reports that individually rather than failing the application.

The roles below are derived from the calls Correlux actually makes, not from a
convenient superset. They are a starting point to narrow, not a specification:
which resources a given team should see is your decision, and the resources a
custom operator serves are not knowable from here.

`correlux doctor` asks the API server what the current account may do, so you
can check a binding without granting it first.

## Reading

This is what the application dashboard, the WHY engine, the resource browser,
the object inspections and the fleet overview read. `get` and `list` are the
only verbs in it.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: correlux-read
rules:
  # The dashboard infers applications from ownership, labels and selectors, so
  # it reads the controllers and the pods underneath them together.
  - apiGroups: [""]
    resources:
      - namespaces
      - nodes
      - pods
      - services
      - events
      - persistentvolumeclaims
      - persistentvolumes
      - resourcequotas
      - limitranges
    verbs: ["get", "list"]
  # Logs are a subresource, which means an account can be allowed to see that a
  # pod is unhealthy without being allowed to read what it printed.
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
    verbs: ["get", "list"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses", "networkpolicies"]
    verbs: ["get", "list"]
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "list"]
  # A service that is up with no endpoints behind it is one of the answers the
  # WHY engine gives, and these three are where the rest of that answer lives.
  - apiGroups: ["policy"]
    resources: ["poddisruptionbudgets"]
    verbs: ["get", "list"]
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get", "list"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments"]
    verbs: ["get", "list"]
  # Usage is optional: without the Metrics API the rest of Correlux works and
  # the usage view says the numbers are unavailable.
  - apiGroups: ["metrics.k8s.io"]
    resources: ["nodes", "pods"]
    verbs: ["list"]
  # This is what `correlux doctor` asks, and it is why it can report what you
  # may do instead of attempting operations and interpreting the failures.
  - apiGroups: ["authorization.k8s.io"]
    resources: ["selfsubjectaccessreviews"]
    verbs: ["create"]
```

### No `watch`

Correlux never opens a watch. The screen reloads on a timer the user turns on,
refetching only what is on screen
([ADR 17](adr/0017-timed-refresh-not-watches.md)), so `list` with no `watch` is
not a restriction that degrades anything — it is the whole read path. A role
that grants `watch` grants something Correlux will not use.

### Cluster-scoped or namespaced

Bind `correlux-read` with a `ClusterRoleBinding` for an operator who moves
between namespaces, or with a `RoleBinding` per namespace for a team that owns
a few. A namespaced binding never grants the cluster-scoped kinds in the list
above — `namespaces`, `nodes`, `persistentvolumes`, `storageclasses`,
`volumeattachments` and node metrics — and those reads then fail individually:
the namespace picker falls back to the kubeconfig's namespace, the fleet
overview reports the node count it could not read, and a PVC's inspection stops
at the claim instead of following it to its volume. Scoping the fleet overview
with `fleetNamespaces` keeps it from asking for cluster-wide reads at all.

### Secrets and ConfigMaps are not in it

Correlux reads neither on its own: no screen loads them to render something
else. They appear in the resource browser only because it lists every kind the
cluster serves, and an object inspection links to them by name without opening
them. Add them if you want them browsable — descriptions redact Secret values
and show a TLS certificate's public fields without decoding its key — and leave
them out if reading a Secret should stay a separate, audited decision.

### Discovery

Every screen that is not the dashboard depends on the API server's discovery
documents (`/api`, `/apis` and the group listings), which is how custom
resources are first-class without Correlux hard-coding any of them. Kubernetes
grants this to `system:authenticated` through the `system:discovery` role, so
there is normally nothing to add. A cluster that has removed that binding has
to grant it back, or Correlux sees no resource kinds at all.

### Custom resources have to be added per cluster

Nothing above names a CRD, because no two clusters serve the same ones. A
custom resource is read through the same discovery and table path as a
Deployment, and needs `get` and `list` on its own API group before it appears:

```yaml
  - apiGroups: ["cert-manager.io", "gateway.networking.k8s.io"]
    resources: ["*"]
    verbs: ["get", "list"]
```

Object references follow what an object points at — a route, a certificate, an
RBAC binding — so a group that is missing from the role shows as a reference
that cannot be opened rather than as a broken screen.

## Troubleshooting

These are the actions that create something or attach to something, each behind
a confirmation that names the cluster and the blast radius
([ADR 25](adr/0025-explicit-troubleshooting-sessions.md)). None of them is
needed to read a cluster, which is why they are a separate role.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: correlux-debug
rules:
  # Shells and port-forwards are subresources reached with POST, so `create` is
  # the verb even though neither creates an object.
  - apiGroups: [""]
    resources: ["pods/exec", "pods/portforward"]
    verbs: ["create"]
  # Correlux issues a PUT to attach an ephemeral container. `kubectl debug`
  # patches instead, so a role written for it grants `patch` and will refuse
  # this one.
  - apiGroups: [""]
    resources: ["pods/ephemeralcontainers"]
    verbs: ["update"]
  # Toolboxes and DNS/TCP/HTTP probes are bounded Jobs, and a session is
  # stopped by deleting its Job. Reading them back is already in correlux-read.
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "delete"]
```

Bind this per namespace, to the people who need it, for as long as they need
it. A shell in a pod is the pod's own permissions, not Correlux's: the
container's service account is what the commands you type run as.

## Delivery

Helm and Flux operations are namespaced by nature — a release lives in one
namespace and a Flux object is reconciled where it is — so this is a `Role`
rather than a `ClusterRole`, granted where a team may deploy.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: correlux-delivery
  namespace: team-api
rules:
  # Helm's default driver keeps each release revision in a Secret. Reading the
  # release browser, history, values and manifests needs the first two verbs;
  # upgrade, rollback, test and uninstall write new revisions and need the
  # rest.
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "create", "update", "delete"]
  # Reconcile, suspend, resume and Helm's reset/force are one merge patch
  # carrying the resourceVersion that was read, which is why `patch` is the
  # only verb Flux operations add.
  - apiGroups:
      - source.toolkit.fluxcd.io
      - kustomize.toolkit.fluxcd.io
      - helm.toolkit.fluxcd.io
      - image.toolkit.fluxcd.io
    resources: ["*"]
    verbs: ["get", "list", "patch"]
```

Two things this Role does not cover, both deliberately:

- **What the chart contains.** Helm applies a release with your credentials, so
  an upgrade or rollback needs permission on every kind in the chart, and a
  chart that installs a CRD or a ClusterRole needs cluster-scoped permission
  for it. There is no useful way to enumerate that here; it follows the chart.
- **Reading a Secret.** Granting `secrets` for Helm's release storage grants
  every Secret in the namespace, including application credentials.
  Kubernetes' RBAC cannot scope a verb to one Secret type, so a team that may
  operate releases can also read what those releases hold. If that is not
  acceptable, keep Helm operations out of Correlux for that namespace and leave
  the read-only role in place — the release browser is the only thing lost, and
  Flux reconciliation still works without the `secrets` rule.

## Changes to running workloads

Scale, restart, rollback, edit, delete and cordon are not in any role above.
They are ordinary writes on the object in question — `patch` for scale, restart
and a Deployment rollback, `update` for an edit, `delete`, `patch` on `nodes`
for cordon — and they should
be granted the same way you already grant them to the people who use `kubectl`,
per kind and per namespace. Correlux puts each behind one confirmation that
states the blast radius and names the cluster
([ADR 20](adr/0020-changes-go-through-one-gate.md)); it does not, and cannot,
substitute for the API server refusing what somebody may not do.

A rollback reads the Deployment's ReplicaSets first, which `correlux-read`
already grants. For an account that may write but should not from Correlux,
`--read-only` or `dangerousActions.readOnlyProduction` refuses every change at
the client ([ADR 28](adr/0028-read-only-contexts.md)); that is a guard on the
tool, and the role is still what the API server enforces.

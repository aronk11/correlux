# Correlux

**Don't show me Kubernetes. Show me what matters.**

Correlux is a terminal-native Kubernetes operations UI for macOS, Linux and
Windows. It runs against your existing kubeconfig — no server, no agent, no CRD,
no container, no browser.

It is inspired by how useful K9s is, and it is deliberately not a K9s clone: the
first screen is your **applications and their health**, not a list of resource
types.

> **Status: early.** Implemented so far: the application dashboard, which infers
> applications from the cluster's own ownership, labels and selectors and sorts
> them worst first; cluster and namespace switching; the command palette; an
> optional timed refresh; the deterministic WHY engine that explains an
> unhealthy application from the cluster's own evidence; and a resource browser
> that lists **every** kind the cluster serves — custom resources included —
> with the API server's own columns. Logs and exec are next. See
> [the roadmap](#roadmap). Nothing in the UI is a mock-up: if Correlux does not
> know something yet, it says so.

## Why

During an incident you do not want to run `kubectl get`, `describe`, `logs`,
`get events`, `get endpoints` and `rollout status` in sequence and hold the join
in your head. You want one screen that answers:

- What is broken?
- **Why** is it broken?
- What can I safely do about it?

That last question is the one Correlux optimises for. The primary metric is the
time from *"something is wrong"* to *"I know why"*.

## Install

```bash
go install github.com/aronk11/correlux/cmd/correlux@latest
```

Or download a binary from the
[releases page](https://github.com/aronk11/correlux/releases) and put it on your
`PATH`. Pre-built binaries are static: there is nothing else to install.

## Use

```bash
correlux                              # start in your current context
correlux --context prod-eu            # start somewhere specific
correlux -n payments                  # start in a namespace
correlux -A                           # start scoped to all namespaces
correlux doctor                       # why doesn't it work here?
correlux version
```

### Keys

| Key | Action |
|-----|--------|
| `Ctrl+P` | Command palette — every action, searchable by name |
| `Ctrl+A` | Back to the application dashboard |
| `F` | The fleet: every configured cluster at once |
| `Enter` | Open the application under the cursor |
| `Ctrl+W` | Why is this unhealthy? |
| `y` | Show the document the server holds, and back |
| `b` | Decode the base64 values in it — a Secret's, above all |
| `e` | Edit the open object in your editor |
| `S` | Scale the selected workload |
| `l` | Read the logs of the pod, workload or application in hand |
| `u` | Where the pods are, and what they use against what they asked for |
| `Ctrl+B` | Browse resource kinds, custom resources included |
| `Enter` | Open the object under the cursor |
| `Ctrl+K` | Switch cluster |
| `Ctrl+O` | Switch namespace |
| `/` | Filter the list on screen |
| `Ctrl+R` | Refresh |
| `Ctrl+F` | Refresh on a timer, until you turn it off (`auto 10s` appears in the header) |
| `w` | Toggle the wide columns in a resource table |
| `?` | Help |
| `Esc` | Back / close overlay |
| `Ctrl+C` / `q` | Quit |

You are not expected to memorise those. Press `Ctrl+P` and type what you want.

### Applications, not resource types

The first screen is what is deployed in the current scope, worst first:

```
STATUS      APPLICATION  PODS   AGE    DETAIL
✖ down      payments     0/3    1h30m  0 of 3 pods ready
⚠ degraded  worker       7/8    4d2h   1 CrashLoopBackOff
✓ healthy   api          12/12  9d
✓ healthy   frontend     6/6    9d
```

Kubernetes has no application object, so Correlux infers one: pods are walked up
their owner references to the controller that owns them, workloads sharing an
`app.kubernetes.io/instance` label are one release, services join by selector
and ingresses through the service they route to. Nothing has to be installed,
labelled or configured first, and `Enter` opens an application to show the
workloads, pods and network it is actually made of
([ADR 16](docs/adr/0016-application-inference.md)).

Health comes from what the cluster reports — replica counts and pod states —
and stops there. *Why* something is broken is the next section's question.

### Why is it broken?

`Ctrl+W` explains the selected application, deterministically and without a
model anywhere near it:

```
✖ 3 pods restart in a loop
  Deployment/payments → Pods → CrashLoopBackOff → OOMKilled
  WHY
    the container is killed for exceeding its memory limit (exit 137)
  EVIDENCE
    Pod/payments-7d8f-0
      container payments was killed for exceeding its memory limit, restarted 12 times
    Event/payments-7d8f-0  1m ago
      BackOff: Back-off restarting failed container payments (x41)
  WHAT TO CHECK
    • Read the logs of the run that failed, not of the one waiting to start
      kubectl logs -n shop payments-7d8f-0 -c payments --previous
  confidence: high
```

Thirteen rules cover crash loops, image pulls, missing config, OOM kills,
unschedulable pods, failing probes, missing replicas, services without
endpoints, ingresses without a backend, unhealthy nodes and unbound volumes.
Each one reads what the cluster reported and stops there: where the cluster did
not say why, Correlux says so and lowers its confidence rather than inventing a
plausible cause. Every finding carries the evidence it rests on, attributed to
the object that stated it.

The evidence — events, endpoints, node conditions, volume claims — is fetched
when you open an application or ask the question, never on the dashboard's
timer ([ADR 18](docs/adr/0018-evidence-on-demand.md)).

### From an application to the object, and back

`Enter` on an application opens what it is made of; `Enter` again opens the
object under the cursor. The same key does the same thing in the resource
browser, custom resources included. From there, its relations lead up to the controller
that made it and down to the objects it made, so Deployment to ReplicaSet to Pod
is three keystrokes in either direction, and `Esc` retraces the path it came in
by.

Each object is described from the document it came as — a pod by its containers,
their images, states, restarts and limits; a workload by its replicas and pod
template — and `y` shows that document unabridged. A kind Correlux has never
heard of is described from its own status and conditions, which is where a
custom controller reports itself anyway.

`b` decodes the base64 in that document, which is what makes a Secret readable
without copying a blob out to `base64 -d`:

```
Secret/database  shop
v1   age 9d   3 values decoded from base64

data:
  keystore.jks: <binary, 2048 bytes>
  password: hunter2
  tls.crt: |
    -----BEGIN CERTIFICATE-----
```

The scope comes from the document rather than from a list of kinds: `data` and
`binaryData` are decoded, a value that is not base64 stays exactly as it was,
and one that decodes to bytes nobody can read is shown as its size instead of
being dumped into the terminal. The subtitle says which of the two you are
reading, and the toggle changes only that: `e` still hands your editor — and the
cluster — the document the server holds.

### Logs

`l` reads the logs of whatever is in hand: a pod, or every pod of a workload or
an application at once, each line attributed to the container it came from. It
opens following; `f` pauses so you can read what is there, `p` switches to the
previous run of a container that restarted — the only log that explains a crash
loop — `t` adds the server's timestamps and `w` wraps long lines.

A container that cannot be read yet says so on its own line instead of silencing
the others, the oldest lines are dropped once the buffer is full and the header
admits it, and leaving the view closes every connection it opened.

### Where the pods are, and what they use

`u` answers the two questions a full cluster raises, in one screen: which
machine everything landed on, and how much of what it reserved each application
is actually using.

```
Resource usage in default
2 nodes   3 pods of 220 slots   live usage measured 30s ago over 30s

NODES
  NODE    STATE    PODS   CPU USED    CPU REQUESTED  MEM USED    MEM REQUESTED
  node-1  ✓ Ready  2/110  ████░░ 65%  ░░░░░░ 6%      ████░░ 63%  ░░░░░░ 6%
  node-2  ✓ Ready  1/110  no sample   ░░░░░░ 6%      no sample   ░░░░░░ 6%

APPLICATIONS
  APPLICATION  PODS  NODES  CPU USED/REQ/LIMIT  MEMORY USED/REQ/LIMIT
  api          2     2      — / 500m / 1000m    — / 1Gi / 2Gi
  batch        1     1      — / — / —           — / — / —
```

The live column comes from the metrics API, which is optional. Without Metrics
Server the column is not there at all and the rest of the screen still answers
from the pod specs and the machines themselves — requests, limits, allocatable
and how full each node is. A node the metrics API said nothing about reads `no
sample`, a pod that reserved nothing reads `none set`, and neither is ever drawn
as a zero: an unsized pod is placed anywhere and throttled by nothing, which is
the opposite of idle.

Pods that no node has taken get their own section with the scheduler's own
verdict, because "where are the pods" is not answered by the ones that are
somewhere. `Ctrl+R` measures again.

### Finding something

`/` narrows whatever list is on screen — a resource table, the dashboard, or one
kind across the whole fleet. The match is fuzzy and covers the whole row, so
typing `crashloop` finds the pods that are in it and `pay` finds `payments`
wherever the name sits.

```
/pay  2 of 4213 loaded rows   Ctrl+P Commands   Ctrl+K Cluster   ? Help
```

It is a filter, not a query: Correlux narrows the rows it has rather than asking
the server a different question, and the bar says how much it is showing — with
`loaded` when the table is paged and rows below have not been fetched. The order
never changes; a filtered list is the same list with fewer rows.

### Changing something

`S` scales the selected workload, `e` opens the object in `$EDITOR`. Both end at
the same screen, which states what the change does before it happens:

```
Scale Deployment/payments
This removes 2 replicas.
Deployment/payments in shop
3 replicas → 1 replica

Cluster   ⬤ PROD prod-eu   shop

This context is production. Type prod-eu to confirm.
❯
```

The consequence, the object, and the cluster — because the mistake worth
guarding against is not a mistyped number, it is a correct action on the wrong
cluster ([ADR 20](docs/adr/0020-changes-go-through-one-gate.md)). An edit shows
its diff first, is refused if it renames the object or is not valid YAML, and is
refused by the server if somebody else changed the object while it sat in the
editor.

### Several clusters at once

`F` opens the fleet: every cluster you named, read in parallel, with what is
broken in each of them.

```
Fleet
3 clusters   1 unreachable   5 applications across 2 of 3   3 not healthy   1 of 7 nodes not ready, 1 cordoned

CLUSTERS
  CLUSTER        STATE        APPLICATIONS  DETAIL
  staging        connected    2             1 degraded, 1 cordoned
  prod-eu  PROD  connected    3             1 down, 1 of 4 nodes not ready
  sandbox        unreachable  —             connection refused

NODES
  CLUSTER        NODE     STATE      DETAIL
  prod-eu  PROD  node-2   not ready  Kubelet stopped posting node status.
  staging        node-7   cordoned   no new pods will be placed here

WHAT IS BROKEN
  APPLICATION  CLUSTER        NAMESPACE  HEALTH    PODS  DETAIL
  payments     prod-eu  PROD  shop       down      0/3   3 CrashLoopBackOff
  payments     staging        shop       degraded  2/3   1 ImagePullBackOff
```

Everything unusual is on that one screen, not only what has failed: a node that
is merely cordoned is named too, because it is the reason a rollout will not
land there, and a kind Correlux was not allowed to read is counted rather than
passed over in silence.

It covers the contexts of the selected fleet group and nothing else — Correlux
never discovers on its own that opening it authenticated against every
production cluster you hold credentials for. Adding all of them is a command you
run, for one session.

Groups are named sets of contexts, which is how production, staging, a region or
a team stay apart without authenticating against anything outside the one you
opened:

```yaml
fleetGroups:
  - name: production
    contexts: [prod-eu, prod-us]
  - name: non-prod
    contexts: [staging, dev]
```

`Open fleet group …` in the command palette switches between them. Switching
cancels the reads in flight and never carries a session-only addition into the
next group, so a cluster you added by hand to one group cannot appear in
another.

A plain `fleet:` list still works, and is offered as the group named `default`:
a configuration written before groups existed opens on exactly the clusters it
always did, and stays reachable from the palette when named groups sit beside
it.

From the overview, `Ctrl+B` browses **one resource kind across every cluster** —
pods, deployments, or a custom resource — as one table:

```
Fleet → Deployment → 69 deployments   from 3 of 4 clusters   not listed in prod-ap: connection refused
CLUSTER             NAMESPACE          NAME     READY  UP-TO-DATE  AVAILABLE  AGE
kind-correlux-test  kube-system        coredns  2/2    2           2          3h59m
kind-correlux-test  correlux-load-000  app-00   0/3    3           0          3h18m
```

The columns are the API server's own, merged by name: a cluster running an older
version of a CRD contributes what it has and leaves the rest empty, and no cell
ever lands under the wrong heading. A cluster that does not serve the kind is
named with the reason. `Enter` opens that object, in its cluster.

The overview is **read-only**. Enter leaves it for the cluster the row is about,
which is what keeps every action unambiguous: either you are looking at the
fleet, or you are inside one cluster acting on it
([ADR 19](docs/adr/0019-fleet-overview.md)). Totals never cover a cluster that
did not answer, and the timer does not refresh this screen — `Ctrl+R` does, when
you decide the cost is worth paying.

### Helm, Flux and Argo CD

Correlux recognises their handwriting. Nothing is installed and nothing is asked
of them: the workloads those tools create carry labels and annotations saying
so, and an application reads them.

```
DELIVERED BY
  TOOL  OBJECT                NAMESPACE
  Flux  HelmRelease/payments  flux-system
```

`Enter` on that row opens the HelmRelease, Kustomization or Argo application
itself — with its reconciliation conditions, like any other object. Flux is
recognised ahead of the Helm it deploys through, because the object worth
looking at is the one that drives the release. An application nothing claims
says so, which on a cluster run by Flux is itself worth noticing.

### Keeping up with a rollout

`Ctrl+F` reloads the current screen on a timer until you turn it off, and the
header says `auto 10s` while it does. It is off by default, refetches only what
is on screen, never stacks requests, idles while a menu is open and backs off
when the cluster is unreachable — because polling somebody's production API
server is the user's decision, not a default
([ADR 17](docs/adr/0017-timed-refresh-not-watches.md)).

### Custom resources are not second-class

Correlux asks the API server to render every resource table, using the same
`Table` content type `kubectl get` uses. A CustomResourceDefinition that
declares `additionalPrinterColumns` shows exactly those columns — with the same
`-o wide` behaviour — and Correlux contains no code for it
([ADR 13](docs/adr/0013-server-side-tables.md)).

The same applies when a cluster is half broken: if an aggregated API server is
down, discovery degrades to "sixty kinds, one group unavailable" instead of
showing nothing.

### Switching context is session-local

Correlux **never writes to your kubeconfig**. Changing cluster or namespace
inside Correlux affects Correlux only; the `kubectl` in your other terminal
keeps pointing exactly where you left it
([ADR 7](docs/adr/0007-session-local-context-switching.md)).

Because of that, the active context is always on screen, and production contexts
carry a `PROD` badge — in text, not only in colour.

## Configuration

Optional. Correlux runs correctly with no config file.

- Linux/macOS: `~/.config/correlux/config.yaml` (`$XDG_CONFIG_HOME` is honoured)
- Windows: `%APPDATA%\correlux\config.yaml`

```yaml
theme: auto # auto | dark | light

startup:
  context: ""
  namespace: ""

# The clusters the fleet overview (F) covers. Empty means no fleet.
fleet: []

refresh:
  auto: false # start with the timed reload running
  every: 10s  # floored at 2s

dangerousActions:
  productionConfirmation: true
  # Matched case-insensitively against context name, cluster name and server URL.
  productionPatterns:
    - '(^|[-_./])(prod|prd|production|live)([-_./]|$)'
  productionContexts: []

keybindings:
  palette: ctrl+p
  applications: ctrl+a
  fleet: F
  search: "/"
  why: ctrl+w
  object.yaml: "y"
  object.decode: b
  edit: e
  scale: S
  logs: l
  logs.follow: f
  logs.previous: p
  logs.timestamps: t
  context.picker: ctrl+k
  namespace.picker: ctrl+o
  resource.picker: ctrl+b
  refresh: ctrl+r
  refresh.auto: ctrl+f
  help: "?"
  quit: ctrl+c
```

## Accessibility

Information is never carried by colour alone: every state is a glyph *and* a
word (`✓ healthy`, `⚠ degraded`, `✖ down`). `NO_COLOR`, `CLICOLOR=0` and
`TERM=dumb` are honoured, the glyph set falls back to ASCII when the terminal
cannot do better (or when `CORRELUX_ASCII=1` is set), everything is keyboard
reachable, and key bindings are configurable.
See [ADR 9](docs/adr/0009-accessibility-and-terminal-capabilities.md).

## Roadmap

| Phase | Scope | State |
|-------|-------|-------|
| 1 | TUI shell, kubeconfig, cluster/namespace switching, command palette | **done** |
| — | Resource browser for every kind, native and custom | **done** |
| — | kind-based integration and load testing harness | **done** |
| 2 | Application discovery and the application-first dashboard | **done** |
| — | Timed refresh, mouse-wheel scrolling | **done** |
| 3 | Deterministic WHY diagnosis engine | **done** |
| 4 | Object detail, describe and navigation between objects | **done** |
| — | Logs at pod, workload and application level | **done** |
| — | Fleet overview across several clusters, read-only | **done** |
| — | Helm, Flux and Argo CD recognised from what they write | **done** |
| — | Resource usage per node and application, metrics API optional | **done** |
| — | Exec and clipboard | next |
| 5 | Safe mutating actions: scale and edit | **done** |
| — | Further safe actions: delete, restart, cordon | planned |
| 6 | Large-cluster performance work, guided by the benchmarks | planned |
| 7 | Platform polish across macOS, Linux and Windows | planned |

The full product specification is in [SPEC.md](SPEC.md).

## Non-goals

Correlux is a terminal operations interface. It is not a web dashboard, a hosted
service, an operator, a `kubectl` replacement, a Helm replacement, a GitOps
system, a monitoring backend, or an AI-first product. AI, when it arrives, is an
optional explanation layer over a deterministic engine
([ADR 10](docs/adr/0010-deterministic-diagnosis-before-ai.md)).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). In short:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
task check           # vet, lint, race tests — everything CI runs
task kind:up
task kind:seed       # a local cluster with realistic, deliberately broken load
task run:kind
```

The load generator can fill a local cluster with thousands of pods, deployments,
services and custom resources without starting a single container, which is how
Correlux's large-cluster claims are measured rather than asserted
([ADR 14](docs/adr/0014-load-testing-with-kind.md)).

Architecture and the reasoning behind it: [docs/architecture.md](docs/architecture.md)
and [docs/adr](docs/adr/README.md).

## Licence

Apache 2.0 — see [LICENSE](LICENSE).

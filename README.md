# kubeui

**Don't show me Kubernetes. Show me what matters.**

kubeui is a terminal-native Kubernetes operations UI for macOS, Linux and
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
> [the roadmap](#roadmap). Nothing in the UI is a mock-up: if kubeui does not
> know something yet, it says so.

## Why

During an incident you do not want to run `kubectl get`, `describe`, `logs`,
`get events`, `get endpoints` and `rollout status` in sequence and hold the join
in your head. You want one screen that answers:

- What is broken?
- **Why** is it broken?
- What can I safely do about it?

That last question is the one kubeui optimises for. The primary metric is the
time from *"something is wrong"* to *"I know why"*.

## Install

```bash
go install github.com/aronk11/kubeui/cmd/kubeui@latest
```

Or download a binary from the [releases page](https://github.com/aronk11/kubeui/releases)
and put it on your `PATH`. Pre-built binaries are static: there is nothing else
to install.

## Use

```bash
kubeui                              # start in your current context
kubeui --context prod-eu            # start somewhere specific
kubeui -n payments                  # start in a namespace
kubeui -A                           # start scoped to all namespaces
kubeui doctor                       # why doesn't it work here?
kubeui version
```

### Keys

| Key | Action |
|-----|--------|
| `Ctrl+P` | Command palette — every action, searchable by name |
| `Ctrl+A` | Back to the application dashboard |
| `Enter` | Open the application under the cursor |
| `Ctrl+W` | Why is this unhealthy? |
| `y` | Show the document the server holds, and back |
| `e` | Edit the open object in your editor |
| `S` | Scale the selected workload |
| `Ctrl+B` | Browse resource kinds, custom resources included |
| `Enter` | Open the object under the cursor |
| `Ctrl+K` | Switch cluster |
| `Ctrl+O` | Switch namespace |
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

Kubernetes has no application object, so kubeui infers one: pods are walked up
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
not say why, kubeui says so and lowers its confidence rather than inventing a
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
template — and `y` shows that document unabridged. A kind kubeui has never heard
of is described from its own status and conditions, which is where a custom
controller reports itself anyway.

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

### Keeping up with a rollout

`Ctrl+F` reloads the current screen on a timer until you turn it off, and the
header says `auto 10s` while it does. It is off by default, refetches only what
is on screen, never stacks requests, idles while a menu is open and backs off
when the cluster is unreachable — because polling somebody's production API
server is the user's decision, not a default
([ADR 17](docs/adr/0017-timed-refresh-not-watches.md)).

### Custom resources are not second-class

kubeui asks the API server to render every resource table, using the same
`Table` content type `kubectl get` uses. A CustomResourceDefinition that
declares `additionalPrinterColumns` shows exactly those columns — with the same
`-o wide` behaviour — and kubeui contains no code for it
([ADR 13](docs/adr/0013-server-side-tables.md)).

The same applies when a cluster is half broken: if an aggregated API server is
down, discovery degrades to "sixty kinds, one group unavailable" instead of
showing nothing.

### Switching context is session-local

kubeui **never writes to your kubeconfig**. Changing cluster or namespace inside
kubeui affects kubeui only; the `kubectl` in your other terminal keeps pointing
exactly where you left it ([ADR 7](docs/adr/0007-session-local-context-switching.md)).

Because of that, the active context is always on screen, and production contexts
carry a `PROD` badge — in text, not only in colour.

## Configuration

Optional. kubeui runs correctly with no config file.

- Linux/macOS: `~/.config/kubeui/config.yaml` (`$XDG_CONFIG_HOME` is honoured)
- Windows: `%APPDATA%\kubeui\config.yaml`

```yaml
theme: auto # auto | dark | light

startup:
  context: ""
  namespace: ""

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
  why: ctrl+w
  object.yaml: "y"
  edit: e
  scale: S
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
cannot do better (or when `KUBEUI_ASCII=1` is set), everything is keyboard
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
| — | Logs, exec, clipboard | next |
| 5 | Safe mutating actions: scale and edit | **done** |
| — | Further safe actions: delete, restart, cordon | planned |
| 6 | Large-cluster performance work, guided by the benchmarks | planned |
| 7 | Platform polish across macOS, Linux and Windows | planned |

The full product specification is in [SPEC.md](SPEC.md).

## Non-goals

kubeui is a terminal operations interface. It is not a web dashboard, a hosted
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
kubeui's large-cluster claims are measured rather than asserted
([ADR 14](docs/adr/0014-load-testing-with-kind.md)).

Architecture and the reasoning behind it: [docs/architecture.md](docs/architecture.md)
and [docs/adr](docs/adr/README.md).

## Licence

Apache 2.0 — see [LICENSE](LICENSE).

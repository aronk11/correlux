# Correlux — Product & Engineering Specification

## 1. Product

Correlux is a modern, terminal-native Kubernetes operations UI for macOS, Linux
and Windows.

It is inspired by the usefulness of tools like K9s, and it is not a K9s clone.
The central philosophy is:

> Kubernetes should be presented around applications, health, relationships and
> problems — not around raw Kubernetes resource types.

Opening Correlux must immediately answer:

- What applications exist?
- Which are healthy?
- What is broken?
- Why is it broken?
- What changed recently?
- What can I safely do about it?

It works directly against the user's existing kubeconfig. No server-side
component is required.

## 2. Platforms

First-class: macOS, Linux, Windows. Distributed as a single native binary.

Not required: Docker, Node.js, Python, a browser, an operator, CRDs, an
in-cluster agent. The only external dependency is access to a Kubernetes API.

## 3. Core principles

### 3.1 Terminal first

Everything works inside the terminal, and it should feel like a modern terminal
application rather than a web app squeezed into one. Support: keyboard, mouse,
native clipboard, resizing, Unicode, colour and reduced-colour terminals,
Windows Terminal, standard Linux terminals, macOS Terminal and iTerm2.

### 3.2 Application first

The main screen is not a list of Pods, Deployments, Services, ConfigMaps,
Secrets and ReplicaSets. Applications are inferred from Kubernetes
relationships:

```
payments
├── Deployment/payments
├── ReplicaSet/payments-7d8f
├── Pods
├── Service/payments
├── Ingress/payments
├── HPA/payments
├── PDB/payments
├── ConfigMap/payments
└── Secret/payments
```

…and presented as one logical application:

```
payments    6/6 pods    healthy    18ms
```

Users drill down into the underlying resources when they need to.

## 4. Main screen

```
┌─ correlux ───────────────────────────────────────────────────────┐
│ 🔴 prod-eu / payments                                  CPU 42%   │
├──────────────────────────────────────────────────────────────────┤
│ APPLICATIONS                                                     │
│                                                                  │
│ ✓ api             12/12     18ms       healthy                   │
│ ✓ frontend         6/6      42ms       healthy                   │
│ ⚠ worker           7/8     820ms       degraded                  │
│ 🔴 payments        0/3       —          down                     │
├──────────────────────────────────────────────────────────────────┤
│ INCIDENTS                                                        │
│                                                                  │
│ 🔴 payments   PostgreSQL has 0 ready endpoints                   │
│ ⚠ worker      3 pods OOMKilled                                   │
├──────────────────────────────────────────────────────────────────┤
│ 1 incident   2 warnings   4 recent changes                       │
├──────────────────────────────────────────────────────────────────┤
│ ↑↓ Navigate  Enter Open  / Search  Ctrl+K Cluster  ? Help        │
└──────────────────────────────────────────────────────────────────┘
```

Do not overload the screen.

## 5. Navigation model

```
Cluster → Scope → Application → Problem / resource
```

Not `Cluster → Resource type → Object`.

## 6. Command palette

`Ctrl+P` (also `Cmd+P` on macOS) opens a global, fuzzy-searchable palette.
Commands must be discoverable; users must not have to memorise cryptic
shortcuts.

## 7. Cluster switching

`Ctrl+K` lists all kubeconfig contexts with fuzzy search. The current cluster is
displayed prominently at all times, and production contexts are visually
distinguishable.

## 8. Namespace / scope switching

A scope is more powerful than a single namespace: it may be one namespace, or a
named set (`payments`, `payments-staging`, `payments-dev`). The active scope is
always obvious.

## 9. Search

`/` searches the current scope across applications, pods, deployments, services,
ingresses, nodes, CRDs, namespaces and events.

## 10. The WHY feature

`Ctrl+W` explains why the selected object is unhealthy.

```
payments 🔴

WHY IS THIS DOWN?

Deployment → Pods → CrashLoopBackOff → connection refused
→ Service/postgres → 0 ready endpoints

ROOT CAUSE
PostgreSQL currently has no ready endpoints.

RELATED
Service/postgres   0 endpoints
Pod/postgres-0     CrashLoopBackOff

CONFIDENCE: HIGH
```

The first implementation is deterministic and must not depend on an LLM. It uses
pod conditions, container states, restart counts, events, owner references,
selectors, services, endpoints and EndpointSlices, probes, scheduling failures,
node conditions, resource pressure, PVC state, rollout state, ReplicaSets, Jobs,
HPA and PDB state.

## 11. Diagnosis engine

A standalone package, independent of the UI:

```go
type Diagnosis struct {
    Severity    Severity
    Problem     string
    Cause       string
    Evidence    []Evidence
    Suggestions []Suggestion
    Confidence  Confidence
}
```

Initial rules cover pods (CrashLoopBackOff, ImagePullBackOff, ErrImagePull,
OOMKilled, Pending, failed scheduling, probe failures, unexpected termination),
deployments (unavailable replicas, stalled rollout, old ReplicaSet still
serving, failed rollout), services (no endpoints, selector mismatch, endpoints
present but pods unhealthy), nodes (NotReady, memory/disk/PID pressure,
unschedulable), storage (PVC pending, attachment problems, filesystem issues)
and networking (no endpoints, ingress without backend, NetworkPolicy blocking).

Every rule has unit tests.

## 12. Application topology

Opening an application shows its relationships (Ingress → Service → Deployment →
Pods → dependencies). Readability in a small terminal beats completeness; this
is not a graph visualisation.

## 13. Change history

`Ctrl+H` shows recent changes derived from Kubernetes events and observed state
transitions. Correlux must clearly distinguish **observed by Correlux** from
**Kubernetes event**, and must not claim a complete audit log that Kubernetes
does not provide. V1 keeps an in-memory session history; the architecture allows
persistence later.

## 14. Resource views

Applications are primary, but raw resources remain fully inspectable: pods,
deployments, services, ingresses, nodes, ConfigMaps, Secrets, jobs, cronjobs,
PVCs, CRDs — with sorting, filtering, search, details, YAML, JSON, logs, events,
exec, describe and edit. Table and detail components are reusable rather than
duplicated per resource.

## 15. Logs

Logs work at application, pod and container level. Application-level logs follow
new pods automatically during a rollout; the user must not have to re-select
pods after a deployment.

## 16. Exec

`Ctrl+T` opens an interactive shell. Before a production shell, Correlux shows
the context, namespace, pod and container and requires confirmation. The shell
inherits the Correlux context, namespace and target explicitly — never the
ambient external kubectl context.

## 17. Safe destructive actions

Destructive actions are explicit, never a bare keystroke. Confirmations state
the blast radius ("this will remove 3 replicas"). Production contexts require a
stronger confirmation by default. The safety system is configurable.

## 18. Copy system

Clipboard is first-class: resource name, namespace/name, YAML, JSON, the
equivalent `kubectl` command, logs, and the current table. It must work on
macOS, Linux and Windows without assuming `pbcopy` or `xclip` exist.

## 19. Create / edit

Users can create and edit resources from the terminal: edit YAML → validate →
preview → apply, with a plan shown before applying.

## 20. Performance architecture

Correlux must stay responsive on large clusters.

- Do not load the whole cluster into memory at startup.
- Do not make the first frame depend on listing everything.
- Lazy loading, scoped watches, informers only where they pay for themselves,
  per-resource bounded caches, pagination, debounced rendering, asynchronous
  operations with cancellation.
- The UI never freezes waiting for Kubernetes.
- Every asynchronous operation has an explicit state: "Loading…" is never
  confused with "No resources found."

## 21. Rendering architecture

```
Kubernetes watch → state store → derived application state → diagnosis → UI
```

No Kubernetes calls inside UI components. Rendering is decoupled from API calls.
Resize events are debounced rather than triggering a full redraw each time.

## 22. AI integration

Optional, and not required by the core product. When enabled, the model receives
a pre-assembled context package (relevant resources, diagnosis, events, logs,
topology, recent changes, metrics if available) — never unrestricted cluster
access. AI is an explanation layer, not the product.

## 23. Metrics

Optional for V1. If the Metrics API is available, show CPU, memory and restarts.
If it is not, say so plainly and keep working.

## 24. Configuration

Minimal, in an OS-appropriate location (`~/.config/correlux/config.yaml`,
`%APPDATA%\correlux\config.yaml`): theme, dangerous-action policy, startup
context and namespace, scopes, keybindings.

## 25. Themes

Auto, dark, light. The interface stays usable without colour; critical
information is never encoded by colour alone.

## 26. Accessibility

No-colour and reduced-colour terminals, readable contrast, keyboard-only
navigation, configurable keybindings, minimal animation.

## 27. CLI

```
correlux
correlux --context prod-eu
correlux --namespace payments
correlux --all-namespaces
correlux version
correlux doctor
```

`doctor` checks kubeconfig, connectivity, permissions, terminal capabilities,
clipboard support and metrics availability.

## 28. Technology

Go, `client-go`, Cobra for the CLI, Bubble Tea and Lip Gloss for the TUI.
Chosen for cross-platform support, rendering quality, performance and
maintainability. Do not over-engineer.

## 29. Project structure

```
cmd/correlux/
internal/
  kube/{client,discovery,cache,watch}
  domain/{application,topology,diagnosis,history,scope}
  actions/{logs,exec,edit,create,delete,scale}
  ui/{app,components,screens,palette,tables,dialogs}
  config/  clipboard/  terminal/
```

Domain logic stays independent of the TUI.

## 30. MVP scope

Start-up; cluster and namespace selection; application-first dashboard;
application detail; pod/deployment/service inspection; deterministic WHY
diagnosis; logs; exec; YAML and describe; command palette; cross-platform
clipboard; safe actions; lazy loading. Everything else is postponed.

## 31. Non-goals

Not a web dashboard, a hosted SaaS, an operator, a `kubectl` replacement, a Helm
replacement, a GitOps system, a monitoring backend, an AI-first product, or a
cloud management platform.

## 32. Definition of success

An engineer can go from *see broken application* → *open it* → *press WHY* →
*understand the root cause* → *inspect logs and events* → *perform a safe
action* → *verify recovery*, without leaving Correlux, and faster than running
the equivalent `kubectl` sequence by hand.

The primary KPI: **time from "something is wrong" to "I know why".**

## 33. Development strategy

1. Go application and TUI shell: kubeconfig loading, context switching,
   namespace switching, responsive layout, command palette.
2. Application discovery.
3. Diagnosis engine, deterministic, with a unit test per rule.
4. Logs, exec, resource inspection.
5. Safe mutating actions.
6. Large-cluster performance, with fake-cluster benchmarks at 1k/5k/10k pods.
7. Platform polish on macOS, Linux and Windows.

The application stays runnable after every phase.

## 34. Engineering quality

Clean architecture, unit and integration tests, deterministic diagnosis,
cancellation, race-free state, cross-platform behaviour, graceful API failures,
clear error messages. Never panic because a resource is missing or unexpectedly
shaped. CRDs and unknown resource types must not crash the application.

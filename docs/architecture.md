# Architecture

How kubeui is put together today. The *why* lives in [the ADRs](adr/README.md).

## Data flow

```
kubeconfig ──▶ kube/kubeconfig ──▶ kube/client (REST config, clientset, probes)
                                          │
                                          ├──▶ kube/workloads   two bounded passes over a scope
                                          │            │         (objects; evidence on demand)
                                          │            ▼
                                          │    domain/application   grouping + health (pure)
                                          │            │
                                          │            ▼
                                          │    domain/diagnosis     rules → problem, cause, evidence
                                          │
                                          │  cancellable, generation-tagged
                                          ▼
                                   ui/app  (tea.Cmd)
                                          │
                                          ▼
                                   async.Value[T]   ── explicit Loading/Ready/Failed
                                          │
                                          ▼
                              ui/screens, ui/components  ── pure render functions
                                          │
                                          ▼
                                   terminal frame
```

The rule that keeps this honest: **no Kubernetes call happens outside a
`tea.Cmd`, and no package below `ui/app` imports Bubble Tea** (ADR 4).

## Packages

| Package | Responsibility |
|---------|----------------|
| `cmd/kubeui` | Process entry point; nothing but a call into `internal/cli`. |
| `internal/cli` | Flags, subcommands (`version`, `doctor`), start-up wiring, log silencing. |
| `internal/config` | User configuration: OS-appropriate paths, defaults, strict parsing. |
| `internal/buildinfo` | Version stamps, filled by ldflags or VCS metadata. |
| `internal/kube/kubeconfig` | Reads and merges the kubeconfig; classifies production contexts. Never writes. |
| `internal/kube/client` | REST configs and clientsets per context, connectivity probes, error classification, namespace listing, discovery and table listing. |
| `internal/kube/discovery` | The catalog of every resource kind the cluster serves, native and custom, tolerant of partially broken discovery. |
| `internal/kube/resources` | Lists any resource as a server-rendered table, paged and cancellable; reads, updates and scales a single object. |
| `internal/kube/logs` | Container logs as a bounded, cancellable stream, several containers merged into one. |
| `internal/kube/workloads` | One bounded, concurrent pass over a scope, converted into a domain snapshot. A kind that cannot be read becomes a gap, not a failure. |
| `internal/domain/application` | Infers applications from ownership, labels and selectors, and derives their health. Pure; knows nothing about client-go ([ADR 16](adr/0016-application-inference.md)). |
| `internal/domain/describe` | Turns the document an object came as into the facts worth reading. Works on raw JSON, so an unknown kind is described as well as a Pod is. |
| `internal/domain/diff` | Line comparison, so an edit can be shown before it is applied. |
| `internal/domain/diagnosis` | Thirteen deterministic rules that turn evidence into a problem, a cause, the facts behind it and what to check next. Degrades with the evidence available ([ADR 18](adr/0018-evidence-on-demand.md)). |
| `internal/ui/async` | `Value[T]`: lifecycle plus generation counter for every remote value. |
| `internal/ui/layout` | Screen geometry and the resize debouncer. Pure arithmetic. |
| `internal/ui/theme` | Colours, glyphs, terminal capability detection. |
| `internal/ui/palette` | Command registry and fuzzy ranking. No UI dependency. |
| `internal/ui/components` | Reusable widgets: input, selector, header, status bar. |
| `internal/ui/screens` | Full-window views (application dashboard, application detail, WHY, resource table, session); data in, string out. |
| `internal/ui/app` | The Bubble Tea model: keys, overlays, commands, rendering. |

## Start-up sequence

1. Silence `klog` — a stray library log line corrupts a full-screen frame.
2. Load the config file (missing is fine; malformed is reported, not fatal).
3. Load and merge the kubeconfig; classify contexts.
4. Resolve the starting context: `--context` → config `startup.context` →
   `current-context` → the only context.
5. Build the model and **render the first frame**. No API call has happened yet.
6. Probe the cluster, load namespaces, discover the resource catalog and collect
   the applications, as four independent asynchronous commands.

Step 5 is the important one: kubeui is usable and informative against a cluster
that is down, because reaching the cluster is never a precondition for drawing.

## Concurrency

- Every API call runs in a `tea.Cmd` goroutine with a `context.Context` bounded
  by the client timeout.
- Cancellation is explicit: switching context cancels the in-flight probe and
  namespace load.
- Results are tagged with a generation; stale answers are discarded (ADR 5).
- The model itself is only touched from `Update`, on Bubble Tea's own goroutine.
  Shared state below that (the client cache) is mutex-protected.
- CI runs the test suite under `-race`.

## Refreshing

Nothing polls until the user asks it to (`Ctrl+F`). When they do, a `tea.Tick`
loop reloads **only what the current screen shows**, never stacks a request on
one still in flight, idles behind an overlay, leaves a deeply paged table alone
and backs off while the cluster is unreachable. The cursor follows the object it
was on rather than the row number, because both the dashboard and a refreshed
table re-sort underneath it. The reasoning, and why this is not informers, is in
[ADR 17](adr/0017-timed-refresh-not-watches.md).

## Changing things

Every mutating action is a `pendingAction`: what it does in consequences, the
object, and the cluster it will hit — marked when that cluster is production,
where the confirmation demands the cluster's name be typed. Editing hands the
terminal to `$EDITOR` and compares what comes back
([ADR 20](adr/0020-changes-go-through-one-gate.md)).

## Rendering

- `View` is a pure function of the model. Nothing is fetched or mutated in it.
- Resize events are coalesced by `layout.Debouncer` (40 ms), because dragging a
  window emits dozens of events per second.
- Overlays are composed with Lip Gloss layers on a canvas, so the palette floats
  over the dashboard instead of replacing it.
- `task frames` renders the UI to plain text files — layout can be reviewed, and
  regression-tested, without a terminal.

## Testing

Two layers, with different jobs.

**Unit tests** (`go test ./...`, no cluster, no Docker):

- Pure packages (`layout`, `palette`, `async`, `theme`, `config`, `kubeconfig`,
  `discovery`) are tested directly.
- `kube/client` uses `client-go`'s fake clientset, including the
  denied-permission path; `kube/resources` uses an HTTP stub, so table decoding
  and path construction are covered without a cluster.
- `domain/application` is tested by writing down objects and the grouping they
  must produce: every inference rule is one table-free test.
- `domain/diagnosis` has a test per rule, including what each one does *without*
  evidence: a rule that cannot support a cause must say less, not guess.
- `kube/workloads` uses the fake clientset, including the denied-kind and
  page-budget paths.
- `ui/app` is tested by driving the model with synthetic key and message events
  and asserting on the rendered frame — including that no overlay ever overflows
  the terminal at any of five sizes, that loading never reads as empty, and that
  a timed refresh does not move the cursor.

**Integration tests** (`task test:integration`, behind the `integration` build
tag, against a seeded kind cluster):

- Discovery really finds CRDs; the API server really renders their printer
  columns; paging really terminates without repeating an object.
- Applications inferred from the seeded cluster own their real pods and
  services, no ReplicaSet is presented as a workload, and health agrees with the
  replica counts the cluster reports.
- The rules are run against the states a kubelet really writes, and every
  unhealthy application must produce a finding with evidence behind it.
- A deliberately broken aggregated API degrades discovery to a partial result
  instead of an empty screen.
- The application is driven end to end against the live cluster: commands are
  plain functions, so a test can run them and feed the messages back exactly as
  the runtime would.
- Benchmarks and budget assertions cover the first page, discovery, and a
  ten-thousand-row render. See
  [ADR 14](adr/0014-load-testing-with-kind.md).

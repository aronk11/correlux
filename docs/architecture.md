# Architecture

How kubeui is put together today. The *why* lives in [the ADRs](adr/README.md).

## Data flow

```
kubeconfig ──▶ kube/kubeconfig ──▶ kube/client (REST config, clientset, probes)
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
| `internal/kube/client` | REST configs and clientsets per context, connectivity probes, error classification, namespace listing. |
| `internal/ui/async` | `Value[T]`: lifecycle plus generation counter for every remote value. |
| `internal/ui/layout` | Screen geometry and the resize debouncer. Pure arithmetic. |
| `internal/ui/theme` | Colours, glyphs, terminal capability detection. |
| `internal/ui/palette` | Command registry and fuzzy ranking. No UI dependency. |
| `internal/ui/components` | Reusable widgets: input, selector, header, status bar. |
| `internal/ui/screens` | Full-window views; data in, string out. |
| `internal/ui/app` | The Bubble Tea model: keys, overlays, commands, rendering. |

## Start-up sequence

1. Silence `klog` — a stray library log line corrupts a full-screen frame.
2. Load the config file (missing is fine; malformed is reported, not fatal).
3. Load and merge the kubeconfig; classify contexts.
4. Resolve the starting context: `--context` → config `startup.context` →
   `current-context` → the only context.
5. Build the model and **render the first frame**. No API call has happened yet.
6. Probe the cluster and load namespaces as asynchronous commands.

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

## Rendering

- `View` is a pure function of the model. Nothing is fetched or mutated in it.
- Resize events are coalesced by `layout.Debouncer` (40 ms), because dragging a
  window emits dozens of events per second.
- Overlays are composed with Lip Gloss layers on a canvas, so the palette floats
  over the dashboard instead of replacing it.
- `make frames` renders the UI to plain text files — layout can be reviewed, and
  regression-tested, without a terminal.

## Testing

- Pure packages (`layout`, `palette`, `async`, `theme`, `config`, `kubeconfig`)
  are unit-tested directly.
- `kube/client` is tested against `client-go`'s fake clientset, including the
  denied-permission path.
- `ui/app` is tested by driving the model with synthetic key and message events
  and asserting on the rendered frame, including that no overlay ever overflows
  the terminal at any of five sizes.

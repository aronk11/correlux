# kubeui

**Don't show me Kubernetes. Show me what matters.**

kubeui is a terminal-native Kubernetes operations UI for macOS, Linux and
Windows. It runs against your existing kubeconfig — no server, no agent, no CRD,
no container, no browser.

It is inspired by how useful K9s is, and it is deliberately not a K9s clone: the
first screen is your **applications and their health**, not a list of resource
types.

> **Status: early.** Phase 1 (the shell) is implemented: kubeconfig loading,
> cluster and namespace switching, the command palette, connection diagnostics
> and a responsive, accessible TUI. The application dashboard, the WHY diagnosis
> engine, logs and exec are next. See [the roadmap](#roadmap). Nothing in the UI
> is a mock-up: if kubeui does not know something yet, it says so.

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
| `Ctrl+K` | Switch cluster |
| `Ctrl+O` | Switch namespace |
| `Ctrl+R` | Refresh |
| `?` | Help |
| `Esc` | Close overlay |
| `Ctrl+C` / `q` | Quit |

You are not expected to memorise those. Press `Ctrl+P` and type what you want.

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

dangerousActions:
  productionConfirmation: true
  # Matched case-insensitively against context name, cluster name and server URL.
  productionPatterns:
    - '(^|[-_./])(prod|prd|production|live)([-_./]|$)'
  productionContexts: []

keybindings:
  palette: ctrl+p
  context.picker: ctrl+k
  namespace.picker: ctrl+o
  refresh: ctrl+r
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
| 2 | Application discovery and the application-first dashboard | next |
| 3 | Deterministic WHY diagnosis engine | planned |
| 4 | Logs, exec, resource inspection, clipboard | planned |
| 5 | Safe mutating actions | planned |
| 6 | Large-cluster performance (1k/5k/10k pod benchmarks) | planned |
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
make check   # vet, lint, race tests — everything CI runs
```

Architecture and the reasoning behind it: [docs/architecture.md](docs/architecture.md)
and [docs/adr](docs/adr/README.md).

## Licence

Apache 2.0 — see [LICENSE](LICENSE).

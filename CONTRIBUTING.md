# Contributing to Correlux

Thanks for helping. This document is short on purpose; the parts that constrain
your change are the ones worth reading.

## Getting started

Requirements: Go 1.25 or newer, plus Docker (or, on Apple Silicon, Apple's own
`container` tool — see below) if you want to run the integration tests.
Nothing else — the tools are pinned and installed on demand into `.tools/`.

```bash
go install github.com/go-task/task/v3/cmd/task@latest

git clone https://github.com/aronk11/correlux
cd correlux
task              # list every task
task check        # vet + lint + race tests: exactly what CI runs on a PR
task run          # run against your current kubeconfig
task frames       # render the TUI into .frames/*.txt without a terminal
```

### A cluster to develop against

```bash
task kind:up                                   # local kind cluster
task kind:seed                                 # a small, realistically broken cluster
task kind:seed -- --namespaces 50 --pods-per-app 10   # thousands of pods
task run:kind                                  # correlux, pointed at it
task test:integration                          # the integration suite
task bench:cluster                             # the benchmarks
task kind:down                                 # tear it down
```

The seeder never runs a container: pods are attached to a node object with no
kubelet behind it, so ten thousand of them cost the API server what they would
in production and cost your laptop nothing
([ADR 14](docs/adr/0014-load-testing-with-kind.md)). Everything it creates is
labelled `app.kubernetes.io/managed-by=correlux-seed`, and `task kind:reset`
removes exactly that.

#### Apple Silicon without Docker Desktop

`kind` has no native provider for Apple's own [`container`](https://github.com/apple/container)
tool yet — Apple submitted one, but `kind`'s maintainers asked to hold off
while it matures ([kubernetes-sigs/kind#3958](https://github.com/kubernetes-sigs/kind/issues/3958)).
If you have `container` instead of Docker Desktop, OrbStack or Podman, `task
kind:apple:up` / `task kind:apple:down` stand in for `kind:up` / `kind:down`
using the community-verified workaround
([apple/container#92](https://github.com/apple/container/issues/92)): a
systemd-enabled Ubuntu VM, booted through `container`, with a real Docker
Engine installed inside it. `kind` runs inside that VM exactly as it would on
any other Docker host. See `test/kind/apple-container-compose.yaml` for what
that means in full, and the caveats.

```bash
brew install container-compose   # https://github.com/full-chaos/container-compose
task kind:apple:up
task run:kind                    # everything downstream reads .tools/kind.kubeconfig same as kind:up
task kind:apple:down
```

This is experimental: nothing here runs in CI (GitHub-hosted runners are
Linux; `container` needs macOS on Apple Silicon), so it is only as good as
what contributors report back. If the API server it publishes turns out not
to be reachable from the host, everything still works run from inside the VM
directly — `container-compose -f test/kind/apple-container-compose.yaml exec kind-host bash`
gets you a shell with `kind` and `kubectl` already on `PATH`.

### Signing

Every commit must be signed ([ADR 15](docs/adr/0015-signed-commits.md)):

```bash
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
gh ssh-key add ~/.ssh/id_ed25519.pub --type signing --title "$(hostname)"
```

## Workflow

We use trunk-based development ([ADR 11](docs/adr/0011-development-workflow.md)):

1. Branch off `main`: `feat/application-dashboard`, `fix/kubeconfig-reload`.
2. Keep the branch short-lived and the pull request reviewable.
3. Make sure `task check` passes.
4. Open a pull request. It is squash-merged, so **the pull request title becomes
   the commit message on `main`**.

### Conventional Commits

Every commit subject and every pull request title follows
[Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<optional scope>): <subject>
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`,
`ci`, `chore`, `revert`. A `!` before the colon marks a breaking change.

```
feat(palette): rank recently used commands first
fix(kubeconfig): keep the session context when the file is reloaded
perf(namespaces): page the namespace list instead of loading it whole
feat(cli)!: remove the deprecated --context-name flag
```

Check locally before pushing:

```bash
task commits   # conventional commits and signatures
```

## What reviewers will look for

These come from the ADRs and are not negotiable in review — but they *are*
negotiable by writing a new ADR.

- **No Kubernetes call outside a `tea.Cmd`,** and no Bubble Tea import below
  `internal/ui/app` ([ADR 4](docs/adr/0004-layered-architecture.md)).
- **Loading is not empty.** Remote data goes through `async.Value[T]`, and the
  UI renders "loading", "empty", "not permitted" and "failed" as four different
  things ([ADR 5](docs/adr/0005-explicit-async-state.md)).
- **Every request is cancellable and bounded.** No unbounded LIST, no
  cluster-wide watch "just in case" ([ADR 6](docs/adr/0006-lazy-scoped-loading.md)).
- **Colour is never the only signal.** Glyph plus word, always
  ([ADR 9](docs/adr/0009-accessibility-and-terminal-capabilities.md)).
- **Nothing hard-codes the set of known resource types.** Custom resources go
  through the same path as native ones
  ([ADR 13](docs/adr/0013-server-side-tables.md)).
- **Missing or oddly shaped resources must not panic.** CRDs and unknown types
  are normal. A cluster that returns something unexpected is a Tuesday.
- **No mock functionality.** If a view cannot show real data yet, it says so.
- **Errors are actionable.** A raw client-go error string is not an error
  message; classify it and add a hint the user can act on.

## Tests

- New behaviour needs a test. Diagnosis rules (once they land) need one per rule.
- Pure packages are tested directly; `kube/client` uses the fake clientset;
  `ui/app` is tested by driving the model with synthetic events and asserting on
  the rendered frame.
- `go test -race ./...` must pass. CI runs it on Linux and macOS.
- Behaviour that depends on a real API server — discovery, paging, printer
  columns — belongs in `test/integration`, behind the `integration` build tag,
  so `go test ./...` stays fast and needs no cluster.

## Reporting bugs and asking for features

[Open an issue](https://github.com/aronk11/correlux/issues/new/choose) and pick
the form that fits. The forms ask for the things a report is useless without —
for a bug, your OS and terminal, `correlux version` and the output of `correlux
doctor`; for a feature, the situation that was slow rather than the name of the
feature you have in mind.

Two things do not belong in a public issue: a security vulnerability, which
goes through [private reporting](SECURITY.md), and anything carrying secrets or
internal cluster names — redact those and say that you did.

Issues are labelled by type (`bug`, `enhancement`, `documentation`), by area
(`area/tui`, `area/fleet`, `area/kubernetes`, `area/config`, `area/release`)
and, until somebody has looked at them, `needs triage`. `good first issue`
means exactly that: scoped, and with the reasoning already written down.

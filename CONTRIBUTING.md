# Contributing to kubeui

Thanks for helping. This document is short on purpose; the parts that constrain
your change are the ones worth reading.

## Getting started

Requirements: Go 1.25 or newer. Nothing else.

```bash
git clone https://github.com/akiesel/kubeui
cd kubeui
make check       # vet + lint + race tests: exactly what CI runs
make run         # run against your current kubeconfig
make frames      # render the TUI into .frames/*.txt without a terminal
```

`make lint` downloads a pinned golangci-lint into `.tools/` on first use.

## Workflow

We use trunk-based development ([ADR 11](docs/adr/0011-development-workflow.md)):

1. Branch off `main`: `feat/application-dashboard`, `fix/kubeconfig-reload`.
2. Keep the branch short-lived and the pull request reviewable.
3. Make sure `make check` passes.
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
./scripts/check-conventional-commits.sh
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

## Reporting bugs

Include: your OS and terminal, `kubeui version`, the output of `kubeui doctor`
(it contains no secrets), and what you expected to see instead.

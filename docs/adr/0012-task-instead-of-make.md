# 12. Task instead of Make

- Status: accepted
- Date: 2026-09-02
- Supersedes the Makefile introduced in [ADR 11](0011-development-workflow.md)

## Context

Correlux targets macOS, Linux and Windows, and its contributors work on all
three. A Makefile does not: GNU Make is not present on a stock Windows machine,
recipes depend on a POSIX shell, and the tab-versus-space and `$$`-escaping
rules are a tax paid by everyone who touches the file. We were also about to
grow the automation substantially — a kind cluster, a load generator,
integration tests, benchmarks — which is exactly when a Makefile becomes a
collection of `.PHONY` targets with hand-rolled dependency logic.

## Decision

Automation lives in `Taskfile.yml`, run by [Task](https://taskup.dev).

- Task is a single Go binary, installable with `go install`, which means the
  project's only prerequisite stays "you have Go" on every platform.
- Its shell is [mvdan/sh](https://github.com/mvdan/sh), an interpreter written
  in Go, so the same recipe runs identically on Windows and on Linux.
- `status:` makes idempotence declarative: `task kind:up` is a no-op when the
  cluster already exists, without a hand-written `if` in every recipe.
- Tools (golangci-lint, kind) are pinned in one place and installed into
  `.tools/` on demand, so a contributor's first command works and CI uses the
  same versions.
- CI installs Task and runs the same tasks a human runs. There is no second,
  divergent definition of "what CI does".

## Consequences

- Contributors run `task` rather than `make`. `task` with no arguments lists
  everything, which is better discovery than a hand-maintained `help` target.
- Task must be installed. `go install github.com/go-task/task/v3/cmd/task@latest`
  is one line and needs nothing but the Go toolchain the project already
  requires.
- We depend on a third-party runner. It is a single static binary with a stable
  YAML schema; if it ever became a problem, the tasks translate back to shell
  scripts mechanically.

# 11. Trunk-based development with feature branches and Conventional Commits

- Status: accepted
- Date: 2026-09-01

## Context

Correlux is an open-source tool intended for production use. Contributors will
be strangers, releases must be reproducible, and the changelog has to be
trustworthy without anyone hand-writing it.

## Decision

- **`main` is always releasable.** It is protected; nothing lands without a
  pull request and a green pipeline.
- **Feature branches**, short-lived, named `<type>/<short-description>`
  (`feat/application-dashboard`, `fix/kubeconfig-reload`).
- **Conventional Commits** for every commit subject and every pull request
  title, validated in CI by `scripts/check-conventional-commits.sh`. The commit
  type drives the generated release notes, and `!` marks a breaking change.
- **Squash merge**, so the pull request title becomes the commit on `main` and
  the history reads as a list of changes rather than a list of typo fixes.
- **Tags drive releases**: pushing `vX.Y.Z` builds cross-platform binaries with
  GoReleaser and publishes checksums.
- CI runs on Linux, macOS and Windows, with the race detector where a C
  toolchain is available, plus lint, `go mod tidy` verification and
  `govulncheck`.

## Consequences

- Contributors must learn one commit convention; the pull request template and
  a runnable local script make that cheap.
- The changelog is generated, so a badly typed commit is a visible defect rather
  than a private annoyance.
- Cross-platform breakage is caught on the pull request, not by the first
  Windows user after a release.

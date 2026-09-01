# 1. Record architecture decisions

- Status: accepted
- Date: 2026-09-01

## Context

kubeui is meant to be maintained for years, by people who were not in the room
when its shape was decided. The expensive questions in a Kubernetes tool are not
"which function goes where" but "why does switching context not write to the
kubeconfig?", "why is there no informer cache at startup?", "why is the
diagnosis engine not an LLM call?". Answering those from the code alone is
guesswork, and guesswork leads to well-intentioned changes that quietly undo a
deliberate trade-off.

## Decision

Every decision that constrains the architecture, the product model or the
release process is recorded as a numbered Architecture Decision Record in
`docs/adr/`, using the format popularised by Michael Nygard: context, decision,
consequences.

An ADR is immutable once accepted. When a decision changes, a new ADR supersedes
the old one and both stay in the repository, so the history of the reasoning
survives.

ADRs are written for decisions, not for descriptions. How the packages are laid
out today belongs in `docs/architecture.md`; why they may not depend on each
other in the other direction belongs in an ADR.

## Consequences

- A pull request that changes an architectural constraint is expected to carry
  an ADR. The pull request template asks for it.
- Reviewers can reject a change by pointing at an ADR instead of re-arguing it.
- The record is honest about cost: each ADR lists what it makes harder, not only
  what it makes possible.

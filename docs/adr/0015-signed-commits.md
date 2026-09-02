# 15. Every commit is signed

- Status: accepted
- Date: 2026-09-02

## Context

Correlux is an operations tool: people will download a binary and point it at
production clusters with their own credentials. The supply chain that produces
that binary starts at a commit. Git's author field is a string anybody can set,
so an unsigned history proves nothing about who wrote what.

## Decision

Every commit in this repository is cryptographically signed.

- SSH signing is the default (`gpg.format = ssh`): contributors already have an
  SSH key for GitHub, it needs no keyserver, no expiry management and no agent
  gymnastics, and GitHub verifies it once the same public key is registered as a
  *signing* key.
- GPG remains perfectly acceptable for anyone who prefers it; the requirement is
  a signature, not a particular format.
- `scripts/check-signed-commits.sh` fails a pull request that contains an
  unsigned commit. CI cannot verify *whose* signature it is — that needs the
  signer's public key — so the repository additionally enables GitHub's
  "require signed commits" rule, which checks the signature against the
  contributor's registered keys.
- Release tags are signed too, and release binaries are published with
  checksums.

## Consequences

- A contributor must configure signing once. The failure message from the check
  script contains the exact three commands.
- Commits made through the GitHub web UI are signed by GitHub, which satisfies
  the rule.
- History rewrites (a rebase to fix an unsigned commit) change commit hashes.
  That is acceptable on a feature branch and forbidden on `main`.

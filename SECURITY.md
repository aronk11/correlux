# Security policy

## Supported versions

Correlux is pre-1.0. Security fixes are made against the latest released minor
version and `main`.

## Verifying a release

Releases are signed and carry an SBOM and build provenance, so that "did this
binary come from this project?" is a question you can answer yourself rather
than infer from the URL you downloaded it from. Signing is keyless: the
certificate belongs to the release workflow's own identity, and no private key
exists to be stolen or to expire unnoticed.

With the GitHub CLI it is one command:

```sh
gh attestation verify correlux_0.12.0_linux_amd64.tar.gz --repo aronk11/correlux
```

[docs/verifying-releases.md](docs/verifying-releases.md) has the full
`cosign verify-blob` invocation — including the certificate identity flags that
are what make a signature mean anything — and what the SBOM is good for.

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/aronk11/correlux/security/advisories/new)
rather than in a public issue.

Include the version (`correlux version`), your platform, and the steps to
reproduce. You can expect an acknowledgement within a few days and an assessment
with a plan shortly after.

## Threat model

Correlux runs locally, with the user's own Kubernetes credentials, and holds the
terminal. Points worth knowing:

- **Credentials.** Correlux never reads, stores or transmits credentials itself.
  Authentication is delegated entirely to `client-go`, including exec credential
  plugins, which run with the user's privileges exactly as they do for
  `kubectl`.
- **The kubeconfig is read-only.** Correlux never writes to it
  ([ADR 7](docs/adr/0007-session-local-context-switching.md)).
- **Permissions are the cluster's to decide.** Correlux can do only what the
  account in your kubeconfig may do, and it asks for no more than the screen in
  front of you needs. [docs/rbac.md](docs/rbac.md) lists what that is, derived
  from the calls the code makes, so a least-privilege role can be written
  without granting a superset first.
- **No telemetry.** Correlux collects nothing about you, your clusters or your
  session, and sends nothing anywhere. It makes exactly one connection that is
  not to a Kubernetes API server: once a day it asks GitHub's public release
  feed whether a newer Correlux exists — one unauthenticated `GET` to
  `api.github.com`, carrying nothing but a `User-Agent` naming the version
  `correlux version` already prints, and no identifier, context name, cluster
  or counter. The answer is a version number and a link, cached on disk beside
  your config. `update.check: false` stops it, `airGapped: true` blocks manual
  checks as well, and the session screen says which of the two is in effect
  ([ADR 21](docs/adr/0021-update-check.md),
  [air-gapped operation](docs/air-gapped.md)).
- **Cluster data stays local.** Nothing is sent to a third party. The optional
  AI layer, when it exists, will be off by default, will require explicit opt-in,
  and will send only a bounded, pre-assembled context package
  ([ADR 10](docs/adr/0010-deterministic-diagnosis-before-ai.md)).
- **Cluster data is untrusted input.** Resource names, labels and log lines come
  from the cluster and are rendered as text; they are never executed or
  interpreted as commands.

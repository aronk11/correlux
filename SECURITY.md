# Security policy

## Supported versions

kubeui is pre-1.0. Security fixes are made against the latest released minor
version and `main`.

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/akiesel/kubeui/security/advisories/new)
rather than in a public issue.

Include the version (`kubeui version`), your platform, and the steps to
reproduce. You can expect an acknowledgement within a few days and an assessment
with a plan shortly after.

## Threat model

kubeui runs locally, with the user's own Kubernetes credentials, and holds the
terminal. Points worth knowing:

- **Credentials.** kubeui never reads, stores or transmits credentials itself.
  Authentication is delegated entirely to `client-go`, including exec credential
  plugins, which run with the user's privileges exactly as they do for `kubectl`.
- **The kubeconfig is read-only.** kubeui never writes to it
  ([ADR 7](docs/adr/0007-session-local-context-switching.md)).
- **No telemetry.** kubeui makes no network connection other than to the
  Kubernetes API server of the selected context.
- **Cluster data stays local.** Nothing is sent to a third party. The optional
  AI layer, when it exists, will be off by default, will require explicit opt-in,
  and will send only a bounded, pre-assembled context package
  ([ADR 10](docs/adr/0010-deterministic-diagnosis-before-ai.md)).
- **Cluster data is untrusted input.** Resource names, labels and log lines come
  from the cluster and are rendered as text; they are never executed or
  interpreted as commands.

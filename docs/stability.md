# What is stable, and what is not

Correlux is pre-1.0. That is not a licence to change anything at any time, but
it does mean the guarantee is smaller than a 1.0 project's, and it is worth
writing down which half of the tool you can build a script, a runbook or a
cluster role against.

The rule for a 0.x series: **a patch release never changes anything on the
stable list.** A minor bump may, and says so in its release notes and in the
commit that does it, marked `!` per
[Conventional Commits](../CONTRIBUTING.md).

## Stable within a minor

### The command-line surface

`correlux`, `correlux doctor` and `correlux version` exist, take the arguments
they take now, and keep their meaning:

- The persistent flags `--kubeconfig`, `--context`, `--namespace`/`-n`,
  `--all-namespaces`/`-A`, `--config`, `--air-gapped` and `--read-only`.
- `correlux version --short` prints the version and nothing else. It is the one
  output here meant to be parsed, and it performs no network request, so it
  cannot hang in a script behind a firewall.
- A non-zero exit code means the command failed. `doctor` reporting a warning
  is not a failure.

The full output of `correlux version` and `correlux doctor` is for people to
read. The fields will grow; parse `--short`, not the report.

### Configuration keys

Every key documented in the README keeps its name and its meaning: `airGapped`,
`theme`, `startup`, `refresh`, `update`, `dangerousActions`, `debug`, `fleet`,
`fleetNamespaces`, `fleetGroups`, `keybindings` and `savedInvestigations`.

Correlux parses its configuration strictly — an unknown key stops startup
rather than being ignored, so that a typo in `update` is a message and not a
check that quietly stays on. The cost of that choice is that removing or
renaming a key breaks every config file that sets it, which is why it does not
happen inside a minor. New keys are additive and optional: a config file
written for an older Correlux keeps working, and a config file written for a
newer one fails loudly on the older binary rather than half-applying.

### What Correlux does to a cluster on its own

- It opens no watches; the screen reloads on a timer that is off until the user
  turns it on ([ADR 17](adr/0017-timed-refresh-not-watches.md)).
- It authenticates against no cluster nobody named: the fleet overview covers
  the contexts listed in the config and no others
  ([ADR 19](adr/0019-fleet-overview.md)).
- It never writes to your kubeconfig ([ADR 7](adr/0007-session-local-context-switching.md)).
- Every change to a cluster goes through one confirmation that names the
  cluster and states the blast radius
  ([ADR 20](adr/0020-changes-go-through-one-gate.md)).
- The only request that is not to a Kubernetes API server is the once-a-day
  release check, which `update.check: false` switches off
  ([ADR 21](adr/0021-update-check.md)).

### The permissions it asks for

The verbs in [docs/rbac.md](rbac.md) do not grow inside a minor. A feature that
needs a permission Correlux did not previously use lands in a minor bump, is
named in the release notes, and degrades to a feature that reports it is not
allowed rather than an application that fails to start.

## Not stable

### Keybindings

Default keystrokes get remapped as screens are added and the shape of the tool
settles. If a key matters to your muscle memory or to a runbook, pin it in
`keybindings` — that mapping is a stable config key even though the defaults
behind it are not.

### Exported files

The investigation report and the portable snapshot are for reading and for
loading back into Correlux, not for feeding to another tool. Their structure
changes with the screens they come from. A snapshot carries a format version
and Correlux refuses one it does not recognise, so a change shows up as a file
that will not load rather than as a field silently read wrong — but the version
it refuses may well be the one your last release wrote.

### The screens

Layout, column choices, wording, ordering and the text of any particular
message are not an interface. Nothing in the TUI is designed to be scraped.

### The Go packages

Everything but `cmd/correlux` lives under `internal/`, which is not an
accident: Correlux is a program, not a library. There is no importable API, no
plugin surface, and no support for `go get`-ing any part of it. That includes
the domain packages, whose internal structure exists to keep the reasoning
testable rather than to be reused.

## The Kubernetes versions it is tested against

Correlux talks to an API server, so the version that matters is Kubernetes',
not the operating system's. Every release is tested against the current
Kubernetes minor and the two before it — at the time of writing **v1.35, v1.36
and v1.37** — against real clusters in CI rather than against fakes.

Older versions are very likely to work and are not tested: Correlux reads
through discovery and server-rendered tables rather than against hard-coded
schemas ([ADR 13](adr/0013-server-side-tables.md)), which is exactly the part
of Kubernetes that changes slowest. What breaks first on an old cluster is a
resource that has since moved API group, and it breaks as one kind reporting an
error rather than as an application that will not open.

The window moves when Kubernetes releases. Dropping the oldest entry is not a
breaking change and does not get a minor bump of its own; it means the oldest
version is no longer *tested*, not that it is blocked.

Correlux builds with the Go version named in `go.mod`. Raising that floor is a
deliberate decision, taken in a minor bump, and CI tests the current Go release
separately so that raising it is never forced by a toolchain update.

## Deprecation

When something on the stable list has to go:

1. The replacement ships first, in a minor release, and both work.
2. The old one keeps working for at least one further minor, and says it is
   deprecated where the user meets it — a flag prints a warning, a config key
   is accepted and noted on the session screen.
3. It is removed no earlier than the second minor after the warning, in a
   commit marked `!`, named in the release notes.

A security fix is the exception. If something on this list has to change
immediately to close a vulnerability, it changes, and the advisory says what
and why ([SECURITY.md](../SECURITY.md)).

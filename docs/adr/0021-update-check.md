# 21. Correlux says when it is out of date, and that is the only thing it asks

- Status: accepted
- Date: 2026-09-07

## Context

Correlux ships as a single binary that people install once and then keep. There
is no package manager telling most of them a new version exists, no server to
push a notice, and no telemetry that would let anybody notice they are three
releases behind. In practice a user finds out that the fleet view learned to
scope namespaces when they happen to open the repository — which is to say,
mostly not at all.

The obvious fix is the one every CLI has: ask a public feed for the latest
version and mention it. The reason it is not obvious *here* is that until now
Correlux has never opened a connection to anything but a Kubernetes API server.
That is not an accident of implementation; it is close to the product's pitch —
"no server, no agent, no CRD, no container, no browser", and the rule that
Correlux never contacts a cluster nobody named ([ADR 19](0019-fleet-overview.md)).
`net/http` did not appear anywhere in the tree, and the people this tool is for
run it inside networks where an unexpected outbound request is a finding, not a
feature.

So the question is not whether an update check is useful. It is whether it can
exist without quietly turning a tool that talks to your cluster into a tool that
talks to the internet about you.

## Decision

Check once a day, in the background, and make every part of it findable.

1. **One request, to one public endpoint, carrying nothing.** GitHub's own
   "latest release" feed for this repository, unauthenticated, `GET`, five
   second timeout. The only thing sent that is not in the request line is a
   User-Agent naming the version `correlux version` already prints. No
   identifier, no context name, no cluster, no counter. The answer is a version
   number and a link.
2. **At most once a day, cached on disk** beside the configuration file. Asking
   on every start would be asking a hundred times a day on somebody's laptop,
   which is rude to a server nobody is paying for.
3. **It never delays or interrupts anything.** The check is a command like any
   other, fired last in `Init`, and a failure is stored rather than shown. A
   laptop on a train, a proxy that refuses, an air-gapped bastion: none of them
   are problems with Correlux, and none of them may put a banner in front of
   somebody looking at a broken cluster.
4. **All four states are distinguishable, in one place.** The session screen
   says which of "a newer version is available", "up to date", "could not
   check", and "the check is off" is true, next to the switch that turns it off
   ([ADR 5](0005-explicit-async-state.md)). The header only ever says the first
   one: a header that permanently reports "up to date" spends a line on the
   answer nobody needed.
5. **One line switches it off for good.** `update.check: false`, and Correlux
   contacts nothing but Kubernetes again — which the same screen then says.

`correlux version` prints what the last check learned and never performs one:
it is run by scripts, and a command that reaches for the network is a command
that hangs behind a firewall.

Only the numeric core of a version is compared. `go install` and local builds
stamp versions like `v0.10.0-3-gabc1234` — three commits *after* the tag —
which strict semantic versioning ranks *below* `v0.10.0` and would announce as
an update to a version the user is already past. Ignoring the suffix costs a
release candidate its notification and never tells anybody to install what they
are already running.

## Consequences

- The claim "the only external dependency is access to a Kubernetes API" needs
  one qualification in SPEC, and it is a qualification the user controls.
- `net/http` enters the tree. It stays in `internal/update`, which is the only
  package allowed to open a connection that is not a Kubernetes client, and
  which the layering already keeps out of the domain ([ADR 4](0004-layered-architecture.md)).
- The default is on. That is a deliberate trade: opt-in would mean the feature
  reaches nobody who does not already read the changelog, which is the group
  that needs it least. The cost of being wrong is one GET a day to a public
  endpoint, discoverable on the session screen and stoppable in one line.
- If a distribution ever wants a build that cannot check at all, the honest
  answer is a build-time default rather than a runtime setting. Nothing here
  prevents that; nobody has asked yet.

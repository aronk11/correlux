# 2. Go and Bubble Tea v2 for a single static binary

- Status: accepted
- Date: 2026-09-01

## Context

kubeui must run on macOS, Linux and Windows, and it must be usable on a laptop
that is halfway through an incident. That rules out anything requiring a runtime
to be installed, a container to be pulled or a browser to be opened. It also
needs a mature Kubernetes client: reimplementing kubeconfig merging, exec
credential plugins and API discovery is a multi-year mistake.

The realistic options were Go with `client-go`, Rust with `kube-rs`, and a
TypeScript TUI on Node.

## Decision

Go, with `client-go` for Kubernetes access and Bubble Tea v2 (`charm.land/bubbletea/v2`)
plus Lip Gloss v2 for the terminal UI.

- Go produces a single static binary per platform with `CGO_ENABLED=0`, which is
  the entire distribution story: download a file, run it.
- `client-go` is the reference implementation of everything kubeconfig-shaped.
  Exec plugins, OIDC refresh and API discovery behave exactly as `kubectl` does,
  because it is the same code.
- Bubble Tea is an Elm-style architecture: state changes happen in one function,
  and everything slow is a command that returns a message. That is precisely the
  discipline a UI needs when the thing it renders is a remote API that may be
  slow, unreachable or lying.
- Lip Gloss v2 provides a layer/canvas model, which gives real floating overlays
  (the command palette) instead of string surgery.

Node was rejected because it requires a runtime; Rust was rejected because
`kube-rs`, while good, would leave us reimplementing the long tail of kubeconfig
authentication behaviour that operators rely on.

## Consequences

- The UI code must obey Bubble Tea's rules: `Update` never blocks, and no
  Kubernetes call happens outside a command. This is enforced by review and by
  the package layering in ADR 4.
- Charm's v2 line is young. We pin exact versions and keep our own components
  (input, selector, header) rather than depending on the wider widget library,
  so an upstream API change is a contained edit.
- Binary size (~50 MB with `client-go`) is accepted. It is the price of behaving
  exactly like `kubectl` on authentication.

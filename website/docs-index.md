# The friendly manual

Welcome to Correlux. Less cluster, more clarity — and documentation you can actually read during an incident.

## Start here

[Getting started](getting-started.md) covers installation on macOS, Linux and Windows, cluster access, a first incident walkthrough and troubleshooting.

Already installed? Open the [user guide](../README.md) for the complete set of workflows, keyboard shortcuts and configuration options.

## Find your workflow

- [Applications and health](../README.md#applications-not-resource-types): how Correlux connects workloads, pods and networking.
- [The WHY engine](../README.md#why-is-it-broken): deterministic findings, attributed evidence and confidence.
- [Logs](../README.md#logs): multiple containers, following and previous runs.
- [Resource usage](../README.md#where-the-pods-are-and-what-they-use): reservations, live metrics and placement.
- [Fleet groups](../README.md#several-clusters-at-once): selected clusters, read-only triage and cross-cluster resource tables.
- [Safe changes](../README.md#changing-something): scale, edit and production confirmation.
- [Helm, Flux and Argo CD](../README.md#helm-flux-and-argo-cd): delivery metadata and navigation.
- [Configuration](../README.md#configuration): themes, startup, refresh, safety and keybindings.
- [Accessibility](../README.md#accessibility): keyboard navigation, ASCII fallback and colour controls.

## Understand the design

The [architecture](../docs/architecture.md) explains how data travels from kubeconfig to the terminal. The [architecture decision records](../docs/adr/README.md) explain why the system is built this way: evidence on demand, session-local context switching, server-rendered resource tables and a shared confirmation gate.

The [product specification](../SPEC.md) records the intended direction. Consult the [roadmap](../README.md#roadmap) and [release notes](https://github.com/aronk11/correlux/releases) to distinguish available functionality from planned work.

## Build with us

The [contribution guide](../CONTRIBUTING.md) covers local development, checks and testing against kind. Read the [code of conduct](../CODE_OF_CONDUCT.md) and [security policy](../SECURITY.md) before contributing or reporting a vulnerability.

## Search without leaving your flow

Use **Search docs** or press `Ctrl+K` (`⌘K` on macOS) to search the user guide, setup instructions, architecture and decisions. Search happens in your browser; no account or search service is involved.

These pages are built from the repository’s Markdown documents. Fixing a guide in the repository updates the website on its next deployment.

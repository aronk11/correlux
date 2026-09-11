# 27. Air-gapped operation is an explicit session policy

- Status: accepted
- Date: 2026-09-11
- Amends: [21](0021-update-check.md), [25](0025-explicit-troubleshooting-sessions.md)

## Context

Disabling automatic updates did not block a manual release check. Troubleshooting
prompts also supplied public images and left pull policy to Kubernetes, which
can cause registry requests even where preloaded images are intended.

## Decision

`airGapped: true` or `--air-gapped` enables a policy for the entire process.
Either enables it; a false flag cannot override a true configuration. Automatic
and forced release checks return no network command, the palette action is
disabled, and the session screen explains offline upgrades. Cached update
banners are suppressed. Invalid or unreadable configuration stops CLI startup
instead of reverting to online defaults; a missing file remains valid.

Public debug image defaults are omitted in this mode. Operators configure or
enter approved images explicitly, or set `debug.registryMirror`. The mirror
prefixes default Docker Hub repository paths (including `library/busybox`);
individual image overrides and references entered at prompts remain unchanged.
Invalid mirror syntax is a startup configuration error. The shared resolver
also supplies the effective images shown by doctor. Registry proxies must be
populated before losing upstream access; no public fallback is attempted. `debug.imagePullPolicy` applies to Jobs and
ephemeral containers, with `IfNotPresent` as the air-gapped default and `Never`
available for preloaded images. Normal connected defaulting is preserved.
Creation confirmations show the policy. Job image-pull secrets remain namespace
scoped; ephemeral containers retain the existing pod's pull secrets.

Release archives include a local installation guide and an example config.
Doctor reports the policy and incomplete debug-image configuration without
contacting a public service or pulling images.

## Boundaries

Air-gapped means no required Internet service, not no Kubernetes connectivity.
Correlux does not implement a firewall, import images, install authentication
helpers, or mirror Helm/Flux sources. Explicit image references, Helm chart
references, probe destinations, credential helpers, and workload controllers
remain subject to the operator's network policy. The guide describes both an
internal registry and preloaded images, including CA trust, optional binaries,
chart dependencies, and offline binary upgrades.

## Validation

Tests cover startup precedence and failure handling, automatic and forced update
suppression, cached-banner suppression, empty image prompts, explicit image and
pull-policy confirmation, and policy propagation into Jobs and the ephemeral
container API request. Existing connected-mode update behavior stays covered.

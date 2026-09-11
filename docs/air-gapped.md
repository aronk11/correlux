# Running Correlux in an air-gapped environment

Correlux runs on a workstation or bastion that can reach your Kubernetes API
servers. It does not need a hosted service, an agent, or Internet access to
browse resources, diagnose problems, read logs, open shells, or port-forward.
Air-gapped mode disables every Correlux release check, including manual checks,
and removes public-image defaults from troubleshooting prompts.

## Transfer and install

On a connected machine, download the archive for your operating system and CPU
and `checksums.txt` from the same [release](https://github.com/aronk11/correlux/releases).
Transfer them through your approved process. The archive includes the binary,
LICENSE, NOTICE, README, this guide, and `docs/examples/air-gapped.yaml`.

On the destination, calculate the archive's SHA-256 and compare it with the
matching entry in the transferred `checksums.txt` before extracting:

```sh
# Linux; substitute the actual downloaded archive name.
sha256sum correlux_VERSION_linux_amd64.tar.gz
# macOS
shasum -a 256 correlux_VERSION_darwin_arm64.tar.gz
```

On Windows use `Get-FileHash .\correlux_VERSION_windows_amd64.zip -Algorithm SHA256`.
The checksum detects transfer corruption; trust the release and checksum through
your organization's artifact approval process. Extract the archive and place
`correlux` (or `correlux.exe`) on `PATH`. No Go toolchain or package manager is
needed. On macOS, transferred binaries may require approval under your local
Gatekeeper policy. Keep this guide locally; it needs no online assets.

Enable the mode on the first launch:

```sh
correlux --air-gapped --kubeconfig /path/to/internal-kubeconfig
correlux --air-gapped --kubeconfig /path/to/internal-kubeconfig doctor
```

Or put `airGapped: true` in your Correlux config. Copy and adapt the supplied
[example](examples/air-gapped.yaml), then run `correlux --config /path/to/config.yaml`.
The default config locations are `~/.config/correlux/config.yaml` on Linux/macOS
(`XDG_CONFIG_HOME` is honoured) and `%APPDATA%\correlux\config.yaml` on Windows.
`CORRELUX_CONFIG_DIR` can point to a managed directory.

The flag enables the mode for that process. A config with `airGapped: true`
cannot be overridden with `--air-gapped=false`. The session screen and `doctor`
report the mode. The update command is disabled, and cached update banners are
hidden. `update.check: false` alone only disables automatic checks; use
`airGapped: true` to block manual checks too. Invalid or unreadable configuration
stops startup instead of silently selecting online defaults. An absent config
still uses defaults, so keep `--air-gapped` in your launcher when relying on it.

## Troubleshooting images

Resource browsing and diagnosis do not launch containers. Toolboxes,
DNS/HTTP/TCP probes, and ephemeral debug containers require images available to
the cluster. Correlux neither downloads nor imports these images itself.

For an internal registry, mirror approved images for the nodes' architectures,
configure the nodes' registry trust, and set:

```yaml
airGapped: true
debug:
  toolboxImage: registry.internal/ops/busybox:1.37.0
  curlImage: registry.internal/ops/curl:8.21.0
  networkImage: registry.internal/ops/netshoot:v0.16
  imagePullPolicy: IfNotPresent
  imagePullSecrets:
    - internal-registry
```

Alternatively, configure one registry mirror or registry proxy prefix for all
default images:

```yaml
airGapped: true
debug:
  registryMirror: registry.internal:5000/dockerhub
  imagePullPolicy: IfNotPresent
  # Optional override for just one image (never rewritten):
  toolboxImage: registry.internal/approved/toolbox:2
```

`registryMirror` is a registry host with an optional port and repository/project
path, not an HTTP proxy URL. Do not include `https://`, credentials, a tag or a
digest. Registry hosts need a dot, an explicit port, or the name `localhost`;
a bare `registry/cache` would be interpreted as a Docker Hub repository. A
trailing slash is accepted. Use `imagePullSecrets` for registry credentials;
TLS trust is configured on the Kubernetes nodes.

The precedence is: an explicit image reference, otherwise the mirror plus the
standard repository, otherwise the public default in connected mode. With no
mirror or override, air-gapped mode leaves the prompt empty. References entered
in the prompt are used exactly as entered. No retry falls back to a public image.

For the prefix `registry.internal:5000/dockerhub`, defaults resolve as follows:

| Use | Image |
| --- | --- |
| Toolbox, DNS, ephemeral container | `registry.internal:5000/dockerhub/library/busybox:1.37.0` |
| HTTP/TLS | `registry.internal:5000/dockerhub/curlimages/curl:8.21.0` |
| TCP | `registry.internal:5000/dockerhub/nicolaka/netshoot:v0.16` |

The mirror must expose those repository paths. For another layout or a different
image version, set the full `toolboxImage`, `curlImage`, or `networkImage` override.
This setting selects references only for Correlux's own debug containers; it
does not change the node runtime's mirror configuration or application images.
Default versions can change with Correlux releases; explicit overrides pin your
approved versions independently.

A registry proxy that fetches upstream on demand still needs its upstream
connection. For a fully disconnected environment, populate all required images
in the internal registry before disconnecting, or use preloaded node images.
Doctor shows the resolved default image references without contacting the registry.

These are example mirror names; populate your registry before using them.
Digest references are supported. Toolbox images need `/bin/sh` and `sleep`;
DNS probes need `nslookup`, HTTP/TLS probes need `curl`, and TCP probes need
`nc` with `-v -z -w` support. Images must run as UID 1000 with a read-only root
filesystem and dropped capabilities. Include your internal CA trust in the
curl image for TLS probes; Correlux does not disable certificate verification.

If there is no registry, transfer image archives and import them into the
Kubernetes container runtime on every eligible node using your runtime's
supported procedure. Set `debug.imagePullPolicy: Never` and use image references
that match the imported images exactly. Missing images fail visibly; there is
no fallback pull. A Docker image loaded only on your workstation is not an
image loaded on a Kubernetes node.

Air-gapped mode defaults to `IfNotPresent`, including images tagged `latest`.
`Never` prevents image pulls; `Always` remains available for an internal registry
that requires it. Outside air-gapped mode, an omitted policy keeps Kubernetes'
normal default. The policy applies to both Jobs and ephemeral containers and is
shown before confirmation. Admission policies can override or reject these
settings; verify the admitted pod and node configuration. See the Kubernetes
[image pull policy documentation](https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy).

Missing image settings without a mirror leave the prompt empty and require an explicit image.
Job pull secrets must exist in the target namespace. Ephemeral containers use
the existing pod's pull secrets; Correlux does not add the configured Job secrets
to that pod. Its configured pull policy still applies to the new container.

## Authentication and optional integrations

Use kubeconfig endpoints, DNS, CA certificates and credentials valid inside the
enclave. Referenced certificate files must be present. Install any kubeconfig
`exec` authentication helper locally, including its dependencies. If a helper
needs a public identity provider, supply an approved offline-capable credential
flow or reachable internal identity service. Air-gapped mode is not a network
sandbox for credential helpers, editors, Helm, or commands run inside pods.

Install Helm 3 or 4 from approved offline media on `PATH` to use Helm operations.
For upgrades, transfer a local chart archive or directory with dependencies
already bundled, or use a reachable internal chart/OCI registry. The upgrade
prompt accepts a local path such as `/opt/charts/api-1.2.3.tgz`; Helm supports
local charts as well as repository references. See [Helm upgrade](https://helm.sh/docs/helm/helm_upgrade/).
Mirror workload and hook images too. Inspection and rollback use release data
in the cluster; hooks and replacement pods still need their images available.

Flux runs in the cluster. Point its Git, OCI and Helm sources at internal
mirrors and make its controller images and credentials available there.
Correlux's reconciliation action does not rewrite sources. Metrics Server is
optional; mirror and install it separately if you need usage metrics. Without
it, the rest of Correlux works and usage reports that metrics are unavailable.

## Verify and upgrade offline

Run `correlux --air-gapped doctor`. It probes the selected Kubernetes API and
permissions, reports the release-check policy and missing image defaults, and
checks metrics availability. It does not pull images or test every registry,
helper, chart, or fleet member. Then browse resources, read pod logs, and create
a reviewed test toolbox to verify your image setup. Verify an ephemeral
container separately if you use that workflow. Use cluster egress controls to
enforce your network policy for helpers and workloads.

To upgrade, repeat the archive transfer and checksum verification, close
Correlux, replace the binary, and restart with the same config or flag. There
is no automatic download or self-updater. `correlux version` reads local version
metadata and any existing release cache only; it never makes a network request.

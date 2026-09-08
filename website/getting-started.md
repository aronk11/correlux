# From install to insight

Correlux is a terminal-native Kubernetes operations UI for macOS, Linux and Windows. It uses your existing kubeconfig and Kubernetes permissions. There is no cluster component to install.

## Install

### macOS with Homebrew

```bash
brew install aronk11/tap/correlux
correlux version
```

The Homebrew cask ships pre-built binaries for Apple Silicon and Intel Macs.

### Install with Go

Use Go **1.25.4 or later**, as specified by this repository’s `go.mod`:

```bash
go install github.com/aronk11/correlux/cmd/correlux@latest
```

Go installs executables to `GOBIN` when set, otherwise to the `bin` directory in `GOPATH` (normally `$HOME/go/bin` on Unix or `%USERPROFILE%\go\bin` on Windows). Add that directory to your `PATH`. `go env GOBIN GOPATH` shows the values on your machine.

### Download a binary

1. Open the [releases page](https://github.com/aronk11/correlux/releases).
2. Download the archive matching your OS and CPU: `darwin` for macOS, `linux` for Linux, or `windows` for Windows; `arm64` for ARM or `amd64` for x86-64.
3. Download `checksums.txt` from the same release and compare the SHA-256 checksum of your archive with its entry. On macOS use `shasum -a 256 <archive>`; on Linux use `sha256sum <archive>`; on PowerShell use `Get-FileHash <archive> -Algorithm SHA256`. Replace `<archive>` with the actual downloaded filename.
4. Extract the `.tar.gz` archive (macOS/Linux) or `.zip` archive (Windows).
5. Put `correlux` (or `correlux.exe`) in a directory on your `PATH`. On macOS/Linux, ensure the binary is executable with `chmod +x correlux`.
6. Run `correlux version` to verify the installation.

Pre-built binaries are static. You do not need Go, Docker or a runtime to use them. If macOS blocks an unsigned downloaded binary, the Homebrew install is the simplest supported alternative.

## Connect to your cluster

Correlux reads the kubeconfig selected by `--kubeconfig`, then `$KUBECONFIG`, then `~/.kube/config`. It uses the credentials in that configuration, including any required authentication plugins. You still need network access to the cluster and the RBAC permissions for the resources you want to read or change.

```bash
correlux                              # current context
correlux --context prod-eu            # a specific context
correlux -n payments                  # one namespace
correlux -A                           # all namespaces
correlux --kubeconfig ./cluster.yaml  # an explicit kubeconfig
correlux --config ./correlux.yaml     # an explicit Correlux config
```

Context and namespace switches inside Correlux are session-local. They never rewrite your kubeconfig or change the context of `kubectl` in another terminal.

## Your first incident, minus the guessing

1. Start Correlux. The application dashboard sorts applications worst first.
2. Select an unhealthy application. Press `Enter` to see the workloads, pods and network objects that make it up.
3. Press `Ctrl+W` to ask WHY. Read the finding, the attributed evidence, the confidence and the suggested checks.
4. Press `l` to open logs. For a restarting container, `p` reads the previous run. Use `f` to pause following, `t` for timestamps and `w` to wrap lines.
5. Open an object and press `y` to read its server document. `Esc` walks back through your navigation.
6. If you decide a change is appropriate, `S` opens scale and `e` opens edit. Review the consequence or diff and the target context before confirming.

Diagnosis rules establish only what the available evidence supports. A missing fact is not a licence to invent a root cause.

## The keys you will actually use

| Key                 | Action                                |
| ------------------- | ------------------------------------- |
| `Ctrl+P`            | Search every command by name          |
| `Ctrl+A`            | Application dashboard                 |
| `Enter` / `Esc`     | Open / go back one step               |
| `Ctrl+W`            | Explain an unhealthy application      |
| `Ctrl+K` / `Ctrl+O` | Switch cluster / namespace            |
| `Ctrl+B`            | Browse resource kinds, including CRDs |
| `F`                 | Open the selected fleet group         |
| `/`                 | Filter the loaded rows                |
| `l`                 | Read logs                             |
| `u`                 | Resource usage and pod placement      |
| `E`                 | Recent Kubernetes Events              |
| `Ctrl+R`            | Refresh now                           |
| `Ctrl+F`            | Toggle timed refresh                  |
| `?`                 | Help                                  |
| `Ctrl+C` / `q`      | Quit                                  |

No flash cards required. `Ctrl+P` is your way back to any command.

## Configuration is optional

The default configuration path is:

- Linux/macOS: `~/.config/correlux/config.yaml`, respecting `$XDG_CONFIG_HOME`.
- Windows: `%APPDATA%\correlux\config.yaml`.

A small starting point:

```yaml
theme: auto
startup:
  namespace: payments
refresh:
  auto: false
  every: 2s
fleetGroups:
  - name: production
    contexts: [prod-eu, prod-us]
  - name: development
    contexts: [staging, dev]
```

Replace the example context and namespace names with ones in your kubeconfig. Fleet groups contact only the contexts you choose; opening the normal dashboard does not connect to every context you have.

Timed refresh is off by default. Its minimum interval is two seconds. Detailed diagnosis evidence is loaded on demand, and the fleet overview refreshes manually. See the [full configuration reference](../README.md#configuration) and [fleet guide](../README.md#several-clusters-at-once).

## Production safeguards

The active context is visible throughout the UI. Contexts recognised as production have a textual `PROD` badge. By default, changing a production resource requires typing the context’s name. Editing shows a diff, refuses renaming an object and relies on the server to reject stale updates.

Review [the complete production configuration](../README.md#configuration) to explicitly mark contexts whose names do not match the built-in production patterns. RBAC remains the authority for every API request. The fleet overview is read-only; opening an object takes you into its specific cluster.

## When something does not work

```bash
correlux doctor
correlux --context prod-eu doctor
correlux --help
```

- **Command not found:** check that the binary’s directory is on `PATH` and open a fresh terminal after changing it.
- **Authentication or connectivity failure:** check the selected kubeconfig, network or VPN, and any credential plugin your provider requires.
- **Forbidden resources:** your credentials need RBAC permission for those resources. Partial permissions are shown as gaps rather than invented results.
- **No live CPU or memory sample:** the optional metrics API may be unavailable. Requests, limits, placement and node capacity remain useful without it.
- **Unexpected symbols or colours:** try `CORRELUX_ASCII=1` or `NO_COLOR=1`. Keybindings and themes are configurable.

For unresolved issues, check [existing issues](https://github.com/aronk11/correlux/issues) and include your Correlux version, OS and reproduction steps. Redact credentials and private cluster details before sharing diagnostics.

## Keep reading

- [User guide](../README.md): all workflows, keybindings, configuration, accessibility and the roadmap.
- [Architecture](../docs/architecture.md): data flow, concurrency, loading and rendering.
- [Architecture decisions](../docs/adr/README.md): the reasoning behind the product.
- [Contributing](../CONTRIBUTING.md): development checks and the kind test cluster.
- [Product specification](../SPEC.md): intended product direction; planned features are not necessarily released.

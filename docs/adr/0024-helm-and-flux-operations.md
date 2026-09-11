# 24. Inspect Helm releases through Helm and reconcile Flux through its APIs

- Status: accepted
- Date: 2026-09-10

## Context

Ownership labels identify a delivery tool, but they do not explain a failed
release or provide the operation needed to recover it. Helm releases are not
ordinary Kubernetes objects. Reimplementing their storage and lifecycle would
couple Correlux to transaction details which Helm already handles.

## Decision

Flux is read through discovered Kubernetes APIs. Dedicated descriptions and
references supplement the generic resource browser. API groups identify Flux;
a custom resource with the same kind in another group is not Flux.

Reconcile, suspend, resume, Helm retry reset and forced reconciliation go
through the existing confirmation gate. Minimal merge patches carry the read
resourceVersion. Confirmation means a request was accepted, not that a controller
finished deploying. Conditions remain the authority on completion.

Flux inventory references are followed only for local targets. A kubeConfig
reference is navigable, but remote resources must not be opened in the local
cluster. Helm release identity is taken from Flux's reported history instead
of reproducing its generated-name rules.

Helm inspection and lifecycle use an optional installed Helm CLI. Each process
gets explicit namespace/context flags and a private, minified copy of the
session's merged kubeconfig. It is deleted after the process exits. Inherited
HELM_KUBE settings cannot silently override the selected session. No shell is
used. Output is capped at 16 MiB and reads have the normal request timeout.
Lifecycle operations have a six-minute process deadline and a five-minute Helm
timeout. Release lists are paged; history states its retained-revision limit.

Helm mutations use the same confirmation gate and name the release namespace,
cluster, hooks and workload consequences. Flux-managed releases may be restored
by reconciliation; their source should normally be changed instead.

## Consequences

- Existing cluster functionality and Flux require no Helm or Flux binary.
- Helm needs the installed CLI, its supported storage driver and credentials.
- Helm errors, missing executables and partial discovery are visible failures.
- Helm commands may execute chart hooks and contact chart registries on upgrade.
- Secret-bearing values/manifests are read explicitly and are not logged.
- The release browser is single-cluster. Fleet resource browsing still supports
  Flux resources through the normal discovery/table path.
- New chart installation and Flux bootstrap are separate authoring workflows.

# 26. Bounded investigation reports with explicit evidence gaps

Date: 2026-09-10

Status: accepted

## Context

Application health, generic resources and troubleshooting sessions provide facts
but leave operators to assemble routing, references, constraints and differences
by hand. Cross-cluster reads and persisted observations can also suggest a level
of completeness that neither the API nor the current session provides.

## Decision

Use the existing object inspector to render structured investigation reports with
scope, observation time, navigable resource identities, and explicit evidence gaps.
API reads run inside cancellable commands using the existing factory timeout.
Workload, policy, event and attachment lists read at most 200 entries per kind and
report continuation tokens as incomplete coverage. No background cluster-wide graph
or history service is introduced.

Service routing reports join selectors, EndpointSlices and relevant source-egress
and destination-ingress policy objects. Policies are inspected for one destination
pod, with that limitation stated when more exist. These reports do not simulate
CNI, mesh, DNS or packet behavior. Troubleshooting probes remain explicit actions.
Reverse references scan six built-in workload kinds in the selected namespace.
Rollout/constraint and storage reports show observed controller and infrastructure
state without inferring a scheduling or attachment root cause.

Object comparison requires an explicit target context and namespace/name mapping.
The target is independently discovered. Fleet comparison uses the stated
namespace/application-name convention and compares loaded container images,
runtime image IDs, resource requests/limits and desired replica counts. Missing or
partial observations stay distinguishable from equality or absence.

Normalized comparisons exclude status, object identity/controller bookkeeping and
known credential-bearing fields, including Secret data, literal values and Helm
values. Redacted fields are explicitly not compared. This is a best-effort
projection, not a universal custom-resource secret classifier.

Retain eight observations per object, 32 objects and 32 snapshots, each normalized
document at most 64 KiB. History describes reads in this session, not audit history.
Reports are cached separately for back navigation. Portable snapshots include
version, cluster, object identity and timestamp. Exports preview the exact report,
create a private file without overwriting, and require review of custom fields.
Imports are bounded to 2 MiB and validate version, document size and resource group.
Saved investigations persist navigation/filter/scope only through the existing
atomic config writer, preserving other settings and comments.

## Consequences

Operators gain connected investigations without installing a collector or granting
additional cluster-wide permissions. Partial API access produces useful partial
reports. Public TLS certificate inspection never decodes the private key and labels
validity separately from trust/hostname verification.

This does not provide a complete arbitrary-CRD reverse graph, cluster audit log,
packet-policy evaluator, desired-state fleet manifest inventory, or automatic
custom-secret redaction. Those require separate evidence sources and explicit scope.

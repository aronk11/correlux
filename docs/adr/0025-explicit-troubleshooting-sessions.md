# 25. Troubleshooting sessions have explicit identity and bounded lifetimes

- Status: accepted
- Date: 2026-09-10

## Decision

Toolboxes and DNS/TCP/HTTP probes are opt-in, namespace-scoped Jobs. Creation
uses the existing confirmation gate and names the image, command, namespace,
cluster and lifetime. No application labels, mounted volumes, or service-account
token are copied. Containers use a non-root user, read-only root filesystem,
dropped capabilities and the runtime-default seccomp profile on Linux nodes.

A toolbox runs for at most 15 minutes. Probes have a 60-second Job deadline and
command-level timeouts where supported. Backoff is zero; Correlux never reruns
a failed probe automatically. Finished Jobs and their pods are eligible for
TTL-controller deletion after ten minutes, even if Correlux has disconnected.
The session browser fetches label-filtered, namespace-scoped pages of 200 Jobs.
A session is stopped by deleting its Job through the existing deletion gate.

These tests measure connectivity from the newly created pod. NetworkPolicy,
service mesh and workload identity can differ from the application's. A failed
probe is evidence of that failure, not proof of a particular root cause.

Ephemeral debugging is a separate action against one loaded, running pod and
one explicit target container. Its update retains UID/resourceVersion checks.
It shares the pod's network, requests no additional privileges, and exits after
15 minutes. The confirmation explains that the entry cannot subsequently be
removed from the pod. Image-pull settings come from that pod.

Port-forwards are process-local connections to an explicit pod, bound only to
127.0.0.1. Actual allocated local ports and the original cluster/namespace/pod
stay visible in the stop command. Changing context does not retarget them.
Stopping a forward or exiting the TUI cancels its connection. Establishment has
a timeout; established forwards last until stopped or the connection fails.

## Boundaries

Images are configurable and previewed before use; image availability and
admission restrictions remain the cluster's decision. Tags are defaults, not a
claim of immutable content; installations can use approved digest references.
Node-level privileged debugging and packet capture are not offered. Port
forwarding targets pods explicitly, not service load balancing. Results are
available through the Job conditions and pod logs; this is not a persisted
network-monitoring backend.

References: [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/),
[ephemeral debugging](https://kubernetes.io/docs/tasks/debug/debug-application/debug-running-pod/),
[NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/).

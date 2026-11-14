package application

import "strings"

// Role is what an application is to the cluster it runs in: its ingress
// controller, its DNS, its GitOps engine. Most applications have none — they
// are what the cluster is for, not what it is made of.
//
// The distinction matters most when something is down. Three replicas of a
// shop frontend failing is one team's incident; the ingress controller failing
// is everybody's, and a list sorted by health alone prints the two on
// consecutive lines as if they weighed the same.
type Role struct {
	// Name is the role in a word or two: "ingress", "service mesh".
	Name string
	// By is the evidence it was recognised from — a label and its value, or
	// an image — so the claim can be checked against the object and is never
	// more than the reading of a name (ADR 16).
	By string
}

// Known reports whether the application was recognised as anything.
func (r Role) Known() bool { return r.Name != "" }

// The roles. They are words an operator would use for a part of a platform,
// kept few enough to scan as a column.
const (
	RoleControlPlane = "control plane"
	RoleNetworking   = "networking"
	RoleDNS          = "dns"
	RoleIngress      = "ingress"
	RoleMesh         = "service mesh"
	RoleCertificates = "certificates"
	RoleGitOps       = "gitops"
	RoleCICD         = "ci/cd"
	RoleMonitoring   = "monitoring"
	RoleLogging      = "logging"
	RoleTracing      = "tracing"
	RoleAutoscaling  = "autoscaling"
	RoleStorage      = "storage"
	RoleBackup       = "backup"
	RoleSecrets      = "secrets"
	RolePolicy       = "policy"
	RoleSecurity     = "security"
	RoleIdentity     = "identity"
	RoleDatabase     = "database"
	RoleCache        = "cache"
	RoleMessaging    = "messaging"
	RoleRegistry     = "registry"
	RoleCost         = "cost"
)

// Impact says what stops working for everyone else while an application in
// this role is down. It is a fact about the role, not a diagnosis of the
// application: it is shown next to a failure so the reader can weigh it, and
// it never claims the failure has already had that effect.
func (r Role) Impact() string {
	switch r.Name {
	case RoleControlPlane:
		return "the cluster's own API and scheduling depend on it"
	case RoleNetworking:
		return "pod networking on the nodes depends on it"
	case RoleDNS:
		return "name lookups inside the cluster depend on it"
	case RoleIngress:
		return "traffic from outside the cluster enters through it"
	case RoleMesh:
		return "service-to-service traffic and sidecar injection depend on it"
	case RoleCertificates:
		return "certificates are not issued or renewed while it is down"
	case RoleGitOps:
		return "changes in Git do not reach the cluster while it is down"
	case RoleAutoscaling:
		return "capacity is not added or removed while it is down"
	case RoleStorage:
		return "volumes cannot be provisioned or attached while it is down"
	case RoleSecrets:
		return "secrets are not synced or injected while it is down"
	case RolePolicy:
		return "admission of every change may fail or go unchecked while it is down"
	case RoleMonitoring:
		return "alerts may not fire while it is down"
	case RoleLogging:
		return "logs are not collected while it is down"
	case RoleIdentity:
		return "logins that go through it fail while it is down"
	default:
		return ""
	}
}

// Platform reports whether the role is part of what runs the cluster, as
// opposed to a backing service an application talks to. A database is a role
// worth naming and filtering by, and it is still somebody's workload.
func (r Role) Platform() bool {
	switch r.Name {
	case "", RoleDatabase, RoleCache, RoleMessaging:
		return false
	default:
		return true
	}
}

// roleNames maps the names well-known software gives itself — in its
// app.kubernetes.io/name, k8s-app or app label, or as the last segment of its
// image — to a role. A name is matched whole, never as a substring: "kong" is
// an ingress controller and "kongregate-api" is somebody's application.
var roleNames = map[string]string{
	// control plane
	"kube-apiserver": RoleControlPlane, "kube-controller-manager": RoleControlPlane,
	"kube-scheduler": RoleControlPlane, "cloud-controller-manager": RoleControlPlane,
	"aws-cloud-controller-manager": RoleControlPlane, "konnectivity-agent": RoleControlPlane,
	"konnectivity-server": RoleControlPlane,

	// networking
	"cilium": RoleNetworking, "cilium-agent": RoleNetworking, "cilium-operator": RoleNetworking,
	"cilium-envoy": RoleNetworking, "hubble-relay": RoleNetworking, "hubble-ui": RoleNetworking,
	"calico-node": RoleNetworking, "calico-kube-controllers": RoleNetworking, "calico-typha": RoleNetworking,
	"calico-apiserver": RoleNetworking, "tigera-operator": RoleNetworking, "canal": RoleNetworking,
	"flannel": RoleNetworking, "kube-flannel": RoleNetworking, "weave-net": RoleNetworking,
	"antrea-agent": RoleNetworking, "antrea-controller": RoleNetworking, "kube-router": RoleNetworking,
	"kube-proxy": RoleNetworking, "kindnet": RoleNetworking, "aws-node": RoleNetworking,
	"amazon-vpc-cni-k8s": RoleNetworking, "amazon-k8s-cni": RoleNetworking, "azure-cni": RoleNetworking,
	"azure-ip-masq-agent": RoleNetworking, "ip-masq-agent": RoleNetworking, "multus": RoleNetworking,
	"kube-multus": RoleNetworking, "metallb": RoleNetworking, "kube-vip": RoleNetworking,
	"ovnkube-node": RoleNetworking,

	// dns
	"coredns": RoleDNS, "kube-dns": RoleDNS, "node-local-dns": RoleDNS, "nodelocaldns": RoleDNS,
	"k8s-dns-node-cache": RoleDNS, "external-dns": RoleDNS,

	// ingress
	"ingress-nginx": RoleIngress, "nginx-ingress": RoleIngress, "nginx-ingress-controller": RoleIngress,
	"nginx-gateway-fabric": RoleIngress, "traefik": RoleIngress, "haproxy-ingress": RoleIngress,
	"kubernetes-ingress": RoleIngress, "kong": RoleIngress, "kong-ingress-controller": RoleIngress,
	"contour": RoleIngress, "envoy-gateway": RoleIngress, "emissary-ingress": RoleIngress,
	"ambassador": RoleIngress, "aws-load-balancer-controller": RoleIngress,
	"istio-ingressgateway": RoleIngress, "ingress-gce": RoleIngress, "application-gateway-kubernetes-ingress": RoleIngress,
	"cloudflared": RoleIngress, "cloudflare-tunnel-ingress-controller": RoleIngress,

	// service mesh
	"istiod": RoleMesh, "istio-cni": RoleMesh, "istio-cni-node": RoleMesh, "ztunnel": RoleMesh,
	"linkerd-destination": RoleMesh, "linkerd-identity": RoleMesh,
	"linkerd-proxy-injector": RoleMesh, "linkerd-controller": RoleMesh, "kuma-control-plane": RoleMesh,
	"consul-connect-injector": RoleMesh,

	// certificates
	"cert-manager": RoleCertificates, "cainjector": RoleCertificates, "webhook.cert-manager": RoleCertificates,
	"cert-manager-webhook": RoleCertificates, "cert-manager-cainjector": RoleCertificates,
	"cert-manager-controller": RoleCertificates, "trust-manager": RoleCertificates,
	"cert-manager-csi-driver": RoleCertificates,

	// gitops and progressive delivery
	"argocd-server": RoleGitOps, "argocd-application-controller": RoleGitOps,
	"argocd-repo-server": RoleGitOps, "argocd-applicationset-controller": RoleGitOps,
	"argocd-notifications-controller": RoleGitOps, "argocd-dex-server": RoleGitOps,
	"argocd-redis": RoleGitOps, "argocd-image-updater": RoleGitOps, "argocd": RoleGitOps, "argo-cd": RoleGitOps,
	"source-controller": RoleGitOps, "kustomize-controller": RoleGitOps, "helm-controller": RoleGitOps,
	"notification-controller": RoleGitOps, "image-reflector-controller": RoleGitOps,
	"image-automation-controller": RoleGitOps, "flux": RoleGitOps, "flux-operator": RoleGitOps,
	"argo-rollouts": RoleGitOps, "flagger": RoleGitOps, "fleet-agent": RoleGitOps,

	// ci/cd
	"tekton-pipelines-controller": RoleCICD, "tekton-pipelines-webhook": RoleCICD,
	"tekton-triggers-controller": RoleCICD, "workflow-controller": RoleCICD, "argo-workflows": RoleCICD,
	"argo-server": RoleCICD, "gitlab-runner": RoleCICD, "actions-runner-controller": RoleCICD,
	"gha-runner-scale-set-controller": RoleCICD, "gha-rs-controller": RoleCICD, "jenkins": RoleCICD,
	"buildkitd": RoleCICD,

	// monitoring
	"prometheus": RoleMonitoring, "prometheus-operator": RoleMonitoring, "kube-prometheus-stack": RoleMonitoring,
	"kube-prometheus": RoleMonitoring, "alertmanager": RoleMonitoring, "grafana": RoleMonitoring,
	"kube-state-metrics": RoleMonitoring, "node-exporter": RoleMonitoring,
	"prometheus-node-exporter": RoleMonitoring, "metrics-server": RoleMonitoring,
	"prometheus-adapter": RoleMonitoring, "prometheus-pushgateway": RoleMonitoring, "pushgateway": RoleMonitoring,
	"prometheus-blackbox-exporter": RoleMonitoring, "blackbox-exporter": RoleMonitoring,
	"thanos": RoleMonitoring, "mimir": RoleMonitoring, "cortex": RoleMonitoring,
	"victoria-metrics": RoleMonitoring, "vmagent": RoleMonitoring, "vmalert": RoleMonitoring,
	"vmsingle": RoleMonitoring, "vmselect": RoleMonitoring, "vminsert": RoleMonitoring, "vmstorage": RoleMonitoring,
	"victoria-metrics-operator": RoleMonitoring, "datadog": RoleMonitoring, "datadog-agent": RoleMonitoring,
	"datadog-cluster-agent": RoleMonitoring, "newrelic-infrastructure": RoleMonitoring,
	"dynatrace-operator": RoleMonitoring, "opentelemetry-collector": RoleMonitoring,
	"opentelemetry-operator": RoleMonitoring, "otel-collector": RoleMonitoring, "grafana-agent": RoleMonitoring,
	"alloy": RoleMonitoring, "cadvisor": RoleMonitoring, "kubernetes-dashboard": RoleMonitoring,
	"headlamp": RoleMonitoring, "node-problem-detector": RoleMonitoring, "amazon-cloudwatch-agent": RoleMonitoring,
	"cloudwatch-agent": RoleMonitoring, "ama-metrics": RoleMonitoring, "gmp-operator": RoleMonitoring,

	// logging
	"loki": RoleLogging, "promtail": RoleLogging, "fluent-bit": RoleLogging, "fluentbit": RoleLogging,
	"fluentd": RoleLogging, "fluent-operator": RoleLogging, "vector": RoleLogging,
	"elasticsearch": RoleLogging, "kibana": RoleLogging, "logstash": RoleLogging, "filebeat": RoleLogging,
	"opensearch": RoleLogging, "opensearch-dashboards": RoleLogging, "elastic-operator": RoleLogging,
	"logging-operator": RoleLogging, "ama-logs": RoleLogging, "fluentbit-gke": RoleLogging,

	// tracing
	"jaeger": RoleTracing, "jaeger-operator": RoleTracing, "jaeger-collector": RoleTracing,
	"jaeger-query": RoleTracing, "jaeger-agent": RoleTracing, "tempo": RoleTracing, "zipkin": RoleTracing,

	// autoscaling
	"cluster-autoscaler": RoleAutoscaling, "aws-cluster-autoscaler": RoleAutoscaling,
	"karpenter": RoleAutoscaling, "keda": RoleAutoscaling, "keda-operator": RoleAutoscaling,
	"keda-operator-metrics-apiserver": RoleAutoscaling, "keda-admission-webhooks": RoleAutoscaling,
	"vertical-pod-autoscaler": RoleAutoscaling, "vpa-recommender": RoleAutoscaling,
	"vpa-updater": RoleAutoscaling, "vpa-admission-controller": RoleAutoscaling,
	"descheduler": RoleAutoscaling, "cluster-proportional-autoscaler": RoleAutoscaling,
	"goldilocks": RoleAutoscaling, "aws-node-termination-handler": RoleAutoscaling,

	// storage
	"aws-ebs-csi-driver": RoleStorage, "ebs-csi-controller": RoleStorage, "ebs-csi-node": RoleStorage,
	"aws-efs-csi-driver": RoleStorage, "efs-csi-controller": RoleStorage, "efs-csi-node": RoleStorage,
	"longhorn-manager": RoleStorage, "longhorn-driver-deployer": RoleStorage, "longhorn-ui": RoleStorage,
	"rook-ceph-operator": RoleStorage, "openebs": RoleStorage, "local-path-provisioner": RoleStorage,
	"nfs-subdir-external-provisioner": RoleStorage, "nfs-client-provisioner": RoleStorage,
	"snapshot-controller": RoleStorage, "minio": RoleStorage, "minio-operator": RoleStorage,
	"topolvm": RoleStorage, "portworx": RoleStorage, "secrets-store-csi-driver": RoleSecrets,

	// backup
	"velero": RoleBackup, "k10": RoleBackup, "kasten": RoleBackup,

	// secrets
	"external-secrets": RoleSecrets, "external-secrets-webhook": RoleSecrets,
	"external-secrets-cert-controller": RoleSecrets, "sealed-secrets": RoleSecrets,
	"sealed-secrets-controller": RoleSecrets, "vault": RoleSecrets, "vault-agent-injector": RoleSecrets,
	"vault-secrets-operator": RoleSecrets, "reloader": RoleSecrets, "csi-secrets-store": RoleSecrets,

	// policy
	"kyverno": RolePolicy, "kyverno-admission-controller": RolePolicy, "kyverno-background-controller": RolePolicy,
	"kyverno-cleanup-controller": RolePolicy, "kyverno-reports-controller": RolePolicy,
	"gatekeeper": RolePolicy, "gatekeeper-controller-manager": RolePolicy, "gatekeeper-audit": RolePolicy,
	"opa": RolePolicy, "kubewarden-controller": RolePolicy, "policy-controller": RolePolicy,

	// security
	"falco": RoleSecurity, "falcosidekick": RoleSecurity, "trivy-operator": RoleSecurity,
	"tetragon": RoleSecurity, "kube-bench": RoleSecurity, "neuvector": RoleSecurity,
	"falcon-sensor": RoleSecurity, "sysdig-agent": RoleSecurity, "kubescape": RoleSecurity,
	"starboard-operator": RoleSecurity,

	// identity
	"dex": RoleIdentity, "keycloak": RoleIdentity, "oauth2-proxy": RoleIdentity,
	"pinniped-concierge": RoleIdentity, "pinniped-supervisor": RoleIdentity, "authentik": RoleIdentity,
	"aws-pod-identity-webhook": RoleIdentity, "eks-pod-identity-agent": RoleIdentity,
	"azure-workload-identity-webhook": RoleIdentity,

	// databases
	"postgresql": RoleDatabase, "postgres": RoleDatabase, "postgresql-ha": RoleDatabase,
	"mysql": RoleDatabase, "mariadb": RoleDatabase, "mariadb-galera": RoleDatabase, "mongodb": RoleDatabase,
	"cockroachdb": RoleDatabase, "cassandra": RoleDatabase, "clickhouse": RoleDatabase,
	"cloudnative-pg": RoleDatabase, "cnpg-controller-manager": RoleDatabase, "postgres-operator": RoleDatabase,
	"mysql-operator": RoleDatabase, "tidb": RoleDatabase, "vitess": RoleDatabase, "etcd": RoleDatabase,
	"influxdb": RoleDatabase, "timescaledb": RoleDatabase, "couchdb": RoleDatabase, "neo4j": RoleDatabase,
	"qdrant": RoleDatabase, "weaviate": RoleDatabase, "milvus": RoleDatabase,

	// caches
	"redis": RoleCache, "redis-ha": RoleCache, "redis-cluster": RoleCache, "valkey": RoleCache,
	"memcached": RoleCache, "dragonfly": RoleCache, "keydb": RoleCache,

	// messaging
	"kafka": RoleMessaging, "strimzi-cluster-operator": RoleMessaging, "strimzi-kafka-operator": RoleMessaging,
	"zookeeper": RoleMessaging, "rabbitmq": RoleMessaging, "rabbitmq-cluster-operator": RoleMessaging,
	"nats": RoleMessaging, "pulsar": RoleMessaging, "redpanda": RoleMessaging, "emqx": RoleMessaging,
	"activemq": RoleMessaging, "kafka-connect": RoleMessaging, "schema-registry": RoleMessaging,

	// registries
	"harbor": RoleRegistry, "docker-registry": RoleRegistry, "registry": RoleRegistry, "zot": RoleRegistry,

	// cost
	"kubecost": RoleCost, "opencost": RoleCost, "cost-analyzer": RoleCost,
}

// rolePrefixes recognise families whose members are too many to list: every
// Argo CD component starts with "argocd-", every CSI driver has "csi" in its
// name. A prefix ends at a dash, so it still matches whole words only.
var rolePrefixes = []struct{ prefix, role string }{
	{"argocd-", RoleGitOps},
	{"rook-ceph-", RoleStorage},
	{"longhorn-", RoleStorage},
	{"csi-", RoleStorage},
	{"linkerd-", RoleMesh},
	{"istio-", RoleMesh},
	{"calico-", RoleNetworking},
	{"cilium-", RoleNetworking},
	{"hubble-", RoleNetworking},
	{"metallb-", RoleNetworking},
	{"thanos-", RoleMonitoring},
	{"kube-prometheus-stack-", RoleMonitoring},
	{"vpa-", RoleAutoscaling},
	{"kyverno-", RolePolicy},
	{"tekton-", RoleCICD},
	{"external-secrets-", RoleSecrets},
	{"cert-manager-", RoleCertificates},
}

// roleLabels say what an application is by their mere presence: an operator
// writes them on exactly the things it runs.
var roleLabels = []struct{ key, role string }{
	{"cnpg.io/cluster", RoleDatabase},
	{"postgres-operator.crunchydata.com/cluster", RoleDatabase},
	{"strimzi.io/cluster", RoleMessaging},
	{"linkerd.io/control-plane-component", RoleMesh},
}

// roleNameLabels are the labels whose value is the software's name, in the
// order they are trusted.
var roleNameLabels = []string{"app.kubernetes.io/name", "k8s-app", "app", "app.kubernetes.io/part-of"}

// lookupRole matches one name against the catalogue.
func lookupRole(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", false
	}
	if role, ok := roleNames[name]; ok {
		return role, true
	}
	for _, p := range rolePrefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.role, true
		}
	}
	if strings.Contains(name, "-csi-") || strings.HasSuffix(name, "-csi") || strings.HasSuffix(name, "-csi-driver") {
		return RoleStorage, true
	}
	return "", false
}

// roleOf recognises what an application is from the names it gives itself.
//
// The order is the order of trust: a label the software's own chart wrote,
// then the name of the application, then the image it runs — the last because
// an image is the one thing a chart cannot rename. Recognition is by exact
// name against a list, and an application nobody listed has no role rather
// than a guessed one.
func roleOf(a *Application) Role {
	metas := make([]Meta, 0, len(a.Workloads)+len(a.Pods))
	for i := range a.Workloads {
		metas = append(metas, a.Workloads[i].Meta)
	}
	for i := range a.Pods {
		metas = append(metas, a.Pods[i].Meta)
	}

	// kubeadm's static pods say so themselves.
	for i := range a.Pods {
		if _, ok := staticPodName(a.Pods[i].Meta); ok && a.Pods[i].Labels["tier"] == "control-plane" {
			return Role{Name: RoleControlPlane, By: "tier=control-plane"}
		}
	}
	for _, l := range roleLabels {
		for i := range metas {
			if v := metas[i].Labels[l.key]; v != "" {
				return Role{Name: l.role, By: l.key + "=" + v}
			}
		}
	}
	for _, key := range roleNameLabels {
		for i := range metas {
			if v := metas[i].Labels[key]; v != "" {
				if role, ok := lookupRole(v); ok {
					return Role{Name: role, By: key + "=" + v}
				}
			}
		}
	}
	if role, ok := lookupRole(a.Name); ok {
		return Role{Name: role, By: "name " + a.Name}
	}
	// The image of the main container only — the first regular one, by every
	// chart's convention. A Vault agent, a mesh proxy or a log shipper injected
	// beside an application says what the application uses, not what it is,
	// and reading sidecars made a "secrets" role of everything Vault injects
	// into.
	for i := range a.Pods {
		for j := range a.Pods[i].Containers {
			c := &a.Pods[i].Containers[j]
			if c.Init {
				continue
			}
			if role, ok := roleOfImage(c.Image); ok {
				return Role{Name: role, By: "image " + ImageRepository(c.Image)}
			}
			break
		}
	}
	return Role{}
}

// genericImageNames are last segments that name a part rather than a
// project: "ingress-nginx/controller", "calico/node". For those the project
// is the directory.
var genericImageNames = map[string]bool{
	"controller": true, "operator": true, "server": true, "agent": true, "node": true,
	"manager": true, "webhook": true, "daemon": true, "cni": true,
}

// roleOfImage recognises an image by its repository: the last segment, then
// — for a segment that only names a part — the directory, alone or joined to
// it, so "calico/node" is found as "calico-node".
func roleOfImage(image string) (string, bool) {
	repo := ImageRepository(image)
	if repo == "" {
		return "", false
	}
	segments := strings.Split(repo, "/")
	last := segments[len(segments)-1]
	if role, ok := lookupRole(last); ok {
		return role, true
	}
	if len(segments) < 2 || !genericImageNames[last] {
		return "", false
	}
	dir := segments[len(segments)-2]
	if role, ok := lookupRole(dir + "-" + last); ok {
		return role, true
	}
	return lookupRole(dir)
}

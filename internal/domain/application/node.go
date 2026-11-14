package application

import (
	"sort"
	"strings"
)

// The node labels worth reading. Kubernetes standardised zone, region and
// instance type; which pool a node belongs to and whether it can be taken
// away at two minutes' notice is still spelled differently by every cloud and
// every autoscaler, so each spelling is read.
var (
	nodeZoneLabels = []string{"topology.kubernetes.io/zone", "failure-domain.beta.kubernetes.io/zone"}
	nodeTypeLabels = []string{"node.kubernetes.io/instance-type", "beta.kubernetes.io/instance-type"}
	nodePoolLabels = []string{
		"karpenter.sh/nodepool",
		"karpenter.sh/provisioner-name",
		"eks.amazonaws.com/nodegroup",
		"alpha.eksctl.io/nodegroup-name",
		"cloud.google.com/gke-nodepool",
		"kubernetes.azure.com/agentpool",
		"agentpool",
		"node.kubernetes.io/instance-group",
		"kops.k8s.io/instancegroup",
		"cluster.x-k8s.io/deployment-name",
		"doks.digitalocean.com/node-pool",
		"node.cluster.x-k8s.io/pool",
		"nodepool",
		"pool",
	}
)

// Zone is the availability zone the node is in, empty when it does not say.
func (n *Node) Zone() string { return firstOf(n.Labels, nodeZoneLabels) }

// InstanceType is the machine type the cloud gave it.
func (n *Node) InstanceType() string { return firstOf(n.Labels, nodeTypeLabels) }

// Pool is the node pool, node group or Karpenter NodePool the node came from.
func (n *Node) Pool() string { return firstOf(n.Labels, nodePoolLabels) }

// Spot reports a node the cloud may reclaim at short notice. Pods vanishing
// from such a node is its nature rather than an incident, which is why it is
// worth a word next to the node's name.
func (n *Node) Spot() bool {
	if strings.EqualFold(n.Labels["karpenter.sh/capacity-type"], "spot") ||
		strings.EqualFold(n.Labels["eks.amazonaws.com/capacityType"], "spot") {
		return true
	}
	return n.Labels["cloud.google.com/gke-spot"] == "true" ||
		n.Labels["cloud.google.com/gke-preemptible"] == "true" ||
		n.Labels["kubernetes.azure.com/scalesetpriority"] == "spot" ||
		n.Labels["node.kubernetes.io/lifecycle"] == "spot"
}

// Roles are the node's roles as kubectl prints them: the suffixes of its
// node-role.kubernetes.io/ labels, sorted.
func (n *Node) Roles() []string {
	const prefix = "node-role.kubernetes.io/"
	var out []string
	for key := range n.Labels {
		if role := strings.TrimPrefix(key, prefix); role != key && role != "" {
			out = append(out, role)
		}
	}
	sort.Strings(out)
	return out
}

// Placement is the node's pool, machine type and capacity class on one line,
// each part only where the node says it: "default-pool · m5.large · spot".
func (n *Node) Placement() string {
	var parts []string
	if pool := n.Pool(); pool != "" {
		parts = append(parts, pool)
	} else if roles := n.Roles(); len(roles) > 0 {
		parts = append(parts, strings.Join(roles, ","))
	}
	if t := n.InstanceType(); t != "" {
		parts = append(parts, t)
	}
	if n.Spot() {
		parts = append(parts, "spot")
	}
	return strings.Join(parts, " · ")
}

func firstOf(labels map[string]string, keys []string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(labels[key]); v != "" {
			return v
		}
	}
	return ""
}

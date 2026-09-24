package workloads

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/aronk11/correlux/internal/domain/application"
)

// Annotations the Deployment controller and kubectl write on ReplicaSets.
const (
	revisionAnnotation    = "deployment.kubernetes.io/revision"
	changeCauseAnnotation = "kubernetes.io/change-cause"
	templateHashLabel     = appsv1.DefaultDeploymentUniqueLabelKey
)

// revisionsKept bounds how many old revisions of one Deployment are kept for a
// diagnosis: the one serving now and the one before it are what "what
// changed?" compares, and a third is what a rollback that already happened is
// compared against. Deployments keep ten by default, and a namespace with a
// hundred of them should not cost a thousand flattened templates.
const revisionsKept = 3

// fromReplicaSet reduces a ReplicaSet to the revision it records. Only those a
// Deployment controls are revisions; a bare ReplicaSet has nothing to compare
// itself against.
func fromReplicaSet(rs *appsv1.ReplicaSet) (application.Revision, bool) {
	m := meta("ReplicaSet", rs.ObjectMeta)
	owner, ok := m.Controller()
	if !ok || owner.Kind != "Deployment" {
		return application.Revision{}, false
	}
	number, _ := strconv.ParseInt(rs.Annotations[revisionAnnotation], 10, 64)
	return application.Revision{
		Meta:        m,
		Number:      number,
		Desired:     replicas(rs.Spec.Replicas),
		Ready:       rs.Status.ReadyReplicas,
		Template:    FlattenTemplate(&rs.Spec.Template),
		ChangeCause: rs.Annotations[changeCauseAnnotation],
	}, true
}

// pruneRevisions keeps, for every Deployment, its newest revisions and every
// revision still running pods — an old ReplicaSet that is serving is the one a
// stuck rollout is stuck next to.
func pruneRevisions(in []application.Revision) []application.Revision {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Owner() != in[j].Owner() {
			return in[i].Owner() < in[j].Owner()
		}
		return in[i].Number > in[j].Number
	})
	out := in[:0]
	seen := map[string]int{}
	for _, r := range in {
		owner := r.Owner()
		if seen[owner] < revisionsKept || r.Desired > 0 {
			out = append(out, r)
		}
		seen[owner]++
	}
	return out
}

// FlattenTemplate turns a pod template into the fields a person compares
// between two rollouts, one readable key each.
//
// It is deliberately a list of the fields that change behaviour — images,
// commands, environment, resources, probes, mounts, placement — rather than a
// generic walk of the document: a generic walk reports the pod-template-hash
// label as the change every single time, and "something in the spec changed"
// is the answer everyone already had.
func FlattenTemplate(t *corev1.PodTemplateSpec) map[string]string {
	out := map[string]string{}
	for k, v := range t.Labels {
		if k == templateHashLabel {
			continue
		}
		out["label "+k] = v
	}
	for k, v := range t.Annotations {
		out["annotation "+k] = v
	}
	spec := &t.Spec
	for i := range spec.InitContainers {
		flattenContainer(out, "init container "+spec.InitContainers[i].Name, &spec.InitContainers[i])
	}
	for i := range spec.Containers {
		flattenContainer(out, "container "+spec.Containers[i].Name, &spec.Containers[i])
	}
	set(out, "serviceAccountName", spec.ServiceAccountName)
	set(out, "priorityClassName", spec.PriorityClassName)
	set(out, "runtimeClassName", deref(spec.RuntimeClassName))
	for k, v := range spec.NodeSelector {
		out["nodeSelector "+k] = v
	}
	set(out, "affinity", compact(spec.Affinity))
	set(out, "tolerations", compact(spec.Tolerations))
	set(out, "topologySpreadConstraints", compact(spec.TopologySpreadConstraints))
	set(out, "securityContext", compact(spec.SecurityContext))
	for i := range spec.ImagePullSecrets {
		out["imagePullSecret "+spec.ImagePullSecrets[i].Name] = "referenced"
	}
	for i := range spec.Volumes {
		v := &spec.Volumes[i]
		switch {
		case v.ConfigMap != nil:
			out["volume "+v.Name] = "configMap " + v.ConfigMap.Name
		case v.Secret != nil:
			out["volume "+v.Name] = "secret " + v.Secret.SecretName
		case v.PersistentVolumeClaim != nil:
			out["volume "+v.Name] = "persistentVolumeClaim " + v.PersistentVolumeClaim.ClaimName
		default:
			out["volume "+v.Name] = compact(v.VolumeSource)
		}
	}
	return out
}

func flattenContainer(out map[string]string, prefix string, c *corev1.Container) {
	set(out, prefix+" image", c.Image)
	set(out, prefix+" command", strings.Join(c.Command, " "))
	set(out, prefix+" args", strings.Join(c.Args, " "))
	for i := range c.Env {
		e := &c.Env[i]
		key := prefix + " env " + e.Name
		switch {
		case e.ValueFrom == nil:
			out[key] = e.Value
		case e.ValueFrom.SecretKeyRef != nil:
			out[key] = "from secret " + e.ValueFrom.SecretKeyRef.Name + " key " + e.ValueFrom.SecretKeyRef.Key
		case e.ValueFrom.ConfigMapKeyRef != nil:
			out[key] = "from configMap " + e.ValueFrom.ConfigMapKeyRef.Name + " key " + e.ValueFrom.ConfigMapKeyRef.Key
		default:
			out[key] = compact(e.ValueFrom)
		}
	}
	for i := range c.EnvFrom {
		from := &c.EnvFrom[i]
		switch {
		case from.ConfigMapRef != nil:
			out[prefix+" envFrom configMap "+from.ConfigMapRef.Name] = "referenced"
		case from.SecretRef != nil:
			out[prefix+" envFrom secret "+from.SecretRef.Name] = "referenced"
		}
	}
	for name, q := range c.Resources.Requests {
		out[prefix+" requests "+string(name)] = q.String()
	}
	for name, q := range c.Resources.Limits {
		out[prefix+" limits "+string(name)] = q.String()
	}
	set(out, prefix+" livenessProbe", compact(c.LivenessProbe))
	set(out, prefix+" readinessProbe", compact(c.ReadinessProbe))
	set(out, prefix+" startupProbe", compact(c.StartupProbe))
	for i := range c.VolumeMounts {
		mount := &c.VolumeMounts[i]
		out[prefix+" mount "+mount.MountPath] = mount.Name
	}
	for i := range c.Ports {
		p := &c.Ports[i]
		out[prefix+" port "+strconv.Itoa(int(p.ContainerPort))+"/"+string(p.Protocol)] = p.Name
	}
	set(out, prefix+" securityContext", compact(c.SecurityContext))
}

// set records a field only when it has a value, so a field nobody wrote does
// not read as one that was removed.
func set(out map[string]string, key, value string) {
	if value != "" {
		out[key] = value
	}
}

// compact renders a structured field as one line of JSON, or "" when it is
// empty. Probes and affinities are compared whole: which part of a readiness
// probe changed is visible in the rollback diff, and here it is enough to know
// that it did.
func compact(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	switch s := string(data); s {
	case "null", "{}", "[]":
		return ""
	default:
		return s
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

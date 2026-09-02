// Package application infers applications from Kubernetes objects.
//
// Kubernetes has no concept of an application. It has Deployments, ReplicaSets,
// Pods, Services and Ingresses, and the relationship between them is expressed
// through owner references, label selectors and naming conventions. An operator
// reconstructs that relationship in their head every time they debug something.
// This package does it once, deterministically, so the first screen can be a
// list of applications rather than a list of resource types (SPEC 3.2).
//
// The package is pure: it takes a Snapshot of already-fetched objects and
// returns applications. It does not know about Kubernetes clients, contexts or
// the terminal, which is what makes every inference rule testable by writing
// down the objects and the expected grouping.
package application

import "time"

// OwnerRef is the part of an owner reference that grouping needs.
type OwnerRef struct {
	Kind string
	Name string
	UID  string
	// Controller marks the reference that actually manages the object. An
	// object may have several owners but at most one controller.
	Controller bool
}

// Meta is what every object contributes to grouping, whatever its kind.
type Meta struct {
	Kind      string
	Name      string
	Namespace string
	UID       string
	Labels    map[string]string
	Owners    []OwnerRef
	CreatedAt time.Time
}

// Controller returns the owner reference that manages this object.
func (m Meta) Controller() (OwnerRef, bool) {
	for _, o := range m.Owners {
		if o.Controller {
			return o, true
		}
	}
	return OwnerRef{}, false
}

// Workload is a controller that manages pods: Deployment, StatefulSet,
// DaemonSet, Job or CronJob.
//
// Desired and Ready come from the workload's own status, because that is the
// number the cluster is working towards — counting pods would report a rollout
// that has not created its pods yet as complete.
type Workload struct {
	Meta
	Desired  int32
	Ready    int32
	Updated  int32
	Selector map[string]string
	// Replicated is false for workloads that do not have a replica count at all
	// (a CronJob), so "0 of 0 ready" is never read as a broken application.
	Replicated bool
	// Paused (Deployment) and Suspended (CronJob, Job) mean the cluster is
	// deliberately not reconciling this workload. That is a state, not a fault.
	Paused    bool
	Suspended bool
}

// Pod is one pod, reduced to what health and grouping need.
type Pod struct {
	Meta
	// Phase is the pod phase: Running, Pending, Succeeded, Failed or Unknown.
	Phase string
	Ready bool
	// Reason names the observed state when it is not simply running:
	// CrashLoopBackOff, ImagePullBackOff, OOMKilled, Unschedulable, Evicted,
	// Terminating. It is what the cluster reports, never an interpretation of
	// it — explaining the cause is the diagnosis engine's job (SPEC 11).
	Reason   string
	Restarts int32
	Node     string
}

// Terminal reports whether the pod has finished and is not expected to run
// again, which is the normal end state for a Job's pods.
func (p Pod) Terminal() bool { return p.Phase == "Succeeded" }

// Service is one service and the pods it selects.
type Service struct {
	Meta
	Type      string
	ClusterIP string
	Selector  map[string]string
	Ports     []string
}

// Ingress is one ingress and the services it routes to.
type Ingress struct {
	Meta
	Hosts    []string
	Backends []string
}

// Gap records something the snapshot could not read: a kind the user may not
// list, an API that is not served, a group whose aggregated API server is down.
// The UI shows gaps, because "no ingresses" and "you may not list ingresses"
// must never look the same (SPEC 20).
type Gap struct {
	Kind   string
	Reason string
}

// Snapshot is one bounded, point-in-time read of a scope.
type Snapshot struct {
	// Scope is the namespace the snapshot covers, empty for all namespaces.
	Scope     string
	Workloads []Workload
	// Owners are objects that exist only to connect pods to their controller —
	// ReplicaSets, chiefly. They are never shown as workloads themselves,
	// because nobody thinks of "payments-7d8f" as an application.
	Owners    []Meta
	Pods      []Pod
	Services  []Service
	Ingresses []Ingress
	Gaps      []Gap
	// Truncated is true when a list hit its page limit, so the applications
	// derived from it are a subset rather than the whole scope.
	Truncated bool
	FetchedAt time.Time
}

// Problem is a pod state worth naming on the dashboard, with how many pods are
// in it.
type Problem struct {
	Reason string
	Count  int
}

// Application is one inferred application: everything that belongs together,
// with the health that follows from it.
type Application struct {
	Name      string
	Namespace string
	Health    Health
	// Summary states the observed facts behind Health — "0 of 3 pods ready" —
	// and never a cause.
	Summary string

	Workloads []Workload
	Pods      []Pod
	Services  []Service
	Ingresses []Ingress

	DesiredPods int32
	ReadyPods   int32
	Restarts    int32
	Problems    []Problem
	// CreatedAt is the age of the oldest object in the application.
	CreatedAt time.Time
}

// Key identifies an application within a cluster.
func (a Application) Key() string { return a.Namespace + "/" + a.Name }

// ProblemSummary renders the pod problems as one line, worst first.
func (a Application) ProblemSummary() string {
	out := ""
	for _, p := range a.Problems {
		if out != "" {
			out += ", "
		}
		out += itoa(p.Count) + " " + p.Reason
	}
	return out
}

// Counts summarises a set of applications by health, for a one-line status.
type Counts struct {
	Total    int
	Healthy  int
	Degraded int
	Down     int
	Unknown  int
}

// Summarise counts applications by health.
func Summarise(apps []Application) Counts {
	c := Counts{Total: len(apps)}
	for _, a := range apps {
		switch a.Health {
		case Healthy:
			c.Healthy++
		case Degraded:
			c.Degraded++
		case Down:
			c.Down++
		default:
			c.Unknown++
		}
	}
	return c
}

// itoa avoids pulling strconv into a file that formats exactly one number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

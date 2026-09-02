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

import (
	"sort"
	"time"
)

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
	// Containers carry the detail a diagnosis reasons about: which container,
	// which exit code, what the kubelet said about it.
	Containers []Container
	// Scheduled is false while no node has accepted the pod; the reason and
	// message then carry the scheduler's own verdict.
	Scheduled        bool
	ScheduledReason  string
	ScheduledMessage string
	// Claims are the PersistentVolumeClaims the pod mounts, in the order the
	// spec lists them.
	Claims []string
}

// Container is one container's state inside a pod.
//
// Both the current and the previous state are kept: a container in
// CrashLoopBackOff is *waiting* right now, and the only thing that explains why
// is how its last run ended.
type Container struct {
	Name  string
	Image string
	Ready bool
	// Init marks an init container, which blocks everything after it.
	Init     bool
	Restarts int32
	// State is "running", "waiting" or "terminated".
	State   string
	Reason  string
	Message string
	// ExitCode is meaningful only for a terminated container.
	ExitCode int32
	// LastReason and LastExitCode describe the previous run of a container that
	// has restarted.
	LastReason   string
	LastExitCode int32
	// OOMKilled is true when this container, or its previous run, was killed
	// for exceeding its memory limit.
	OOMKilled bool
}

// Terminal reports whether the pod has finished and is not expected to run
// again, which is the normal end state for a Job's pods.
//
// The receiver is a pointer only because a Pod is several hundred bytes and
// this is called once per pod per render.
func (p *Pod) Terminal() bool { return p.Phase == "Succeeded" }

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

// Event is one Kubernetes event, reduced to what an explanation needs.
//
// Events are the cluster's own account of what happened, and kubeui shows them
// as such: quoted, attributed and never rewritten into something the cluster
// did not say (SPEC 13).
type Event struct {
	Meta
	// Type is "Normal" or "Warning".
	Type    string
	Reason  string
	Message string
	Count   int32
	// About identifies the object the event is about.
	About ObjectRef
	// LastSeen is when the event last occurred.
	LastSeen time.Time
}

// ObjectRef points at an object an event or a diagnosis is about.
type ObjectRef struct {
	Kind string
	Name string
	UID  string
}

// EndpointSet is how many addresses one service currently routes to. Zero ready
// endpoints is the most common reason a healthy-looking application serves
// nothing at all.
type EndpointSet struct {
	Service   string
	Namespace string
	Ready     int
	NotReady  int
}

// Node is the state of one node an application's pods run on.
type Node struct {
	Meta
	Ready         bool
	Unschedulable bool
	// Pressure names the pressure conditions that are currently true
	// (MemoryPressure, DiskPressure, PIDPressure).
	Pressure []string
	// Reason and Message come from the Ready condition when it is not true.
	Reason  string
	Message string
}

// Claim is a PersistentVolumeClaim's binding state.
type Claim struct {
	Meta
	// Phase is Pending, Bound or Lost.
	Phase        string
	StorageClass string
	Volume       string
}

// Context is the extra evidence a diagnosis needs.
//
// It is deliberately separate from Snapshot: the dashboard refreshes on a timer
// and must stay cheap, while events, endpoints, nodes and claims are fetched
// for one application at the moment someone asks why it is broken (ADR 6).
type Context struct {
	// Scope is the namespace the context covers.
	Scope     string
	Events    []Event
	Endpoints []EndpointSet
	Nodes     []Node
	Claims    []Claim
	Gaps      []Gap
	FetchedAt time.Time
}

// Node returns the node with the given name.
func (c Context) Node(name string) (Node, bool) {
	for _, n := range c.Nodes {
		if n.Name == name {
			return n, true
		}
	}
	return Node{}, false
}

// EndpointsFor returns the endpoint counts of one service.
func (c Context) EndpointsFor(namespace, service string) (EndpointSet, bool) {
	for _, e := range c.Endpoints {
		if e.Namespace == namespace && e.Service == service {
			return e, true
		}
	}
	return EndpointSet{}, false
}

// ClaimByName returns one PersistentVolumeClaim.
func (c Context) ClaimByName(namespace, name string) (Claim, bool) {
	for _, claim := range c.Claims {
		if claim.Namespace == namespace && claim.Name == name {
			return claim, true
		}
	}
	return Claim{}, false
}

// EventsAbout returns the events about one object, newest first.
func (c Context) EventsAbout(uid, name string) []Event {
	var out []Event
	for _, e := range c.Events {
		if (uid != "" && e.About.UID == uid) || (e.About.UID == "" && e.About.Name == name) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
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

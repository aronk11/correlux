package application

import "strings"

// Signal names which rule matched an object into its application. The order
// here is the order Group itself tries them (ADR 16): ownership first, then
// the label conventions from most to least specific, then the relationships a
// Service or Ingress carries.
type Signal int

const (
	// SignalOwner means an owner reference connects the object to the
	// controller that names the application. It is the only signal
	// Kubernetes itself guarantees, which is why it is the only one Correlux
	// reports as certain rather than a guess.
	SignalOwner Signal = iota
	// SignalInstanceLabel and SignalNameLabel are Kubernetes' own recommended
	// labels: app.kubernetes.io/instance identifies one installation,
	// app.kubernetes.io/name the software it runs.
	SignalInstanceLabel
	SignalNameLabel
	// SignalAppLabel and SignalK8sAppLabel are the older, looser community
	// conventions ("app", "k8s-app"): widely used, and just as easily a
	// coincidence between two unrelated workloads.
	SignalAppLabel
	SignalK8sAppLabel
	// SignalSelector is a Service whose selector actually matches the
	// application's pods.
	SignalSelector
	// SignalBackend is an Ingress that routes to one of the application's
	// services.
	SignalBackend
	// SignalNone means nothing matched at all: the object is here only
	// because its own name was used as a last resort, so it appears
	// somewhere rather than vanishing (ADR 16).
	SignalNone
)

// String names the signal the way an operator would say it out loud.
func (s Signal) String() string {
	switch s {
	case SignalOwner:
		return "owner reference"
	case SignalInstanceLabel:
		return "instance label"
	case SignalNameLabel:
		return "name label"
	case SignalAppLabel:
		return "app label"
	case SignalK8sAppLabel:
		return "k8s-app label"
	case SignalSelector:
		return "service selector"
	case SignalBackend:
		return "ingress backend"
	default:
		return "no signal"
	}
}

// Reason is why one object belongs to its application: which signal decided
// it, and the concrete, bounded evidence behind that decision. It is captured
// once, at grouping time (Group), so the answer to "why is this part of
// payments?" is exactly what the grouper used rather than something
// reconstructed afterwards from a wider search.
//
// The fields are deliberately narrow: a label's key and value, a selector
// rendered as one short string, or the ownership path as "Kind/Name" per
// link. Never a whole label map and never an unbounded string, for the same
// reason Meta keeps only an annotation allowlist.
type Reason struct {
	Signal Signal
	// Key and Value carry the label that matched, when Signal is one of the
	// four label signals.
	Key   string
	Value string
	// Chain is the ownership path from the object up to the controller that
	// named the application, one "Kind/Name" per link, in the order walked.
	// Bounded by maxOwnerDepth, so it can never grow past a handful of
	// entries regardless of how deep an object's history goes. Set only when
	// Signal is SignalOwner.
	Chain []string
}

// Certain reports whether Kubernetes itself guarantees this relationship.
// An owner reference is the only one that does (ADR 16): every label, every
// selector match and every ingress backend is a convention Correlux is
// reading, which is a guess in exactly the sense a bare "app" label is —
// common, usually right, never guaranteed. Never present a guess as a fact:
// this is the one place that decides which is which.
func (r Reason) Certain() bool { return r.Signal == SignalOwner }

// Describe renders the reason as one line, in the vocabulary an operator
// would use to explain it themselves.
func (r Reason) Describe() string {
	switch r.Signal {
	case SignalOwner:
		if len(r.Chain) == 0 {
			return "owned by its controller"
		}
		return "owned by " + strings.Join(r.Chain, ", owned by ")
	case SignalSelector:
		return "selector matches " + r.Value
	case SignalBackend:
		return "backend references Service/" + r.Value
	case SignalNone:
		return "no owner, label, selector or backend matched — grouped by name"
	default: // the four label signals
		return r.Key + "=" + r.Value
	}
}

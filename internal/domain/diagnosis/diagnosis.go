// Package diagnosis explains why an application is unhealthy.
//
// The engine is deterministic and rule-based: no model, no heuristics that
// cannot be printed, no answer that cannot be traced back to something the
// cluster itself said (ADR 10). Every rule turns evidence into one sentence
// about the problem, one about the cause, the evidence it used, and what the
// user might do next.
//
// Two properties are not negotiable:
//
//   - Nothing is invented. A cause is stated only when the cluster stated it;
//     otherwise the rule says what it observed and lowers its confidence.
//   - Nothing is required. The rules degrade with the evidence available, so a
//     dashboard that has only pod states still gets an explanation, and one
//     with events, endpoints and nodes gets a better one.
package diagnosis

import (
	"sort"
	"time"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// Severity is how much attention a finding deserves.
type Severity int

const (
	// Info is a deliberate state worth knowing about: a paused rollout.
	Info Severity = iota
	// Warning is degraded: it serves, but not fully.
	Warning
	// Critical is not serving.
	Critical
)

// String renders the severity as the word shown next to the glyph.
func (s Severity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// Confidence says how sure the engine is, and it means something specific:
// High when the cluster stated the cause, Medium when the evidence allows only
// one reasonable reading, Low when the finding is an observation with a likely
// explanation attached.
type Confidence int

const (
	Low Confidence = iota
	Medium
	High
)

// String renders the confidence for display.
func (c Confidence) String() string {
	switch c {
	case High:
		return "high"
	case Medium:
		return "medium"
	default:
		return "low"
	}
}

// Evidence is one fact the finding rests on, quoted rather than paraphrased.
type Evidence struct {
	// Kind and Name identify where the fact came from.
	Kind string
	Name string
	// Detail is what that object said.
	Detail string
	// At is when it said it, zero when the fact is a current state rather than
	// an event.
	At time.Time
}

// Suggestion is something the user might do next.
type Suggestion struct {
	Text string
	// Command is the equivalent kubectl invocation, ready to run or copy. It is
	// always a read-only command: kubeui suggests looking, never acting
	// (SPEC 17).
	Command string
}

// Diagnosis is one finding about one object.
type Diagnosis struct {
	// Rule names the rule that produced this, which is what a test asserts on
	// and what lets the UI keep findings apart.
	Rule     string
	Severity Severity
	// Subject is the object the finding is about.
	Subject application.ObjectRef
	// Problem states what is wrong, in the user's terms.
	Problem string
	// Cause states why, when the evidence supports saying so.
	Cause string
	// Chain is the path from the workload to the failure, rendered as the
	// breadcrumb in the WHY view: Deployment → Pods → CrashLoopBackOff.
	Chain       []string
	Evidence    []Evidence
	Suggestions []Suggestion
	Confidence  Confidence
}

// Input is everything the rules may look at.
type Input struct {
	App application.Application
	// Context is the extra evidence, and may be empty: the rules degrade to
	// what they can see rather than refusing to answer.
	Context application.Context
	// Scope is the snapshot the application was grouped from. A few questions
	// cannot be answered from one application alone — whether an ingress points
	// at a service that exists at all, for one.
	Scope application.Snapshot
	// Now is the reference time, so tests are not clock-dependent.
	Now time.Time
}

// rule is one explanation, kept independent of every other so it can be read,
// tested and argued about on its own.
type rule func(*Input) []Diagnosis

// rules is the whole engine. Order does not matter: findings are ranked after
// they are produced.
var rules = []rule{
	crashLoop,
	imagePull,
	containerConfig,
	outOfMemory,
	unschedulable,
	nodeUnhealthy,
	podFailed,
	notReady,
	storageNotBound,
	replicasMissing,
	serviceWithoutEndpoints,
	ingressWithoutBackend,
	rolloutPaused,
}

// Diagnose explains an application, worst first.
//
// The input is taken by pointer because it carries the whole scope. The rules
// never modify it, and the default clock is applied to a copy so it cannot leak
// back to the caller.
func Diagnose(in *Input) []Diagnosis {
	if in == nil {
		return nil
	}
	input := *in
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	var out []Diagnosis
	for _, r := range rules {
		out = append(out, r(&input)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

// Primary returns the finding that best answers "what is wrong here?", which is
// the one the dashboard shows on the application's row.
func Primary(findings []Diagnosis) (Diagnosis, bool) {
	if len(findings) == 0 {
		return Diagnosis{}, false
	}
	return findings[0], true
}

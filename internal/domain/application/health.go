package application

import "sort"

// Health is how an application is doing, as far as the cluster's own status
// fields say. It is deliberately coarse: four states an operator can scan a
// screen for, not a score.
type Health int

const (
	// Unknown means kubeui cannot tell — nothing is running and nothing is
	// meant to be, or the objects that would say were not readable.
	Unknown Health = iota
	// Healthy means every replica the cluster wants is ready and no pod is in
	// a state worth naming.
	Healthy
	// Degraded means it is serving, but not fully: missing replicas, restarting
	// containers, pods that are not ready.
	Degraded
	// Down means nothing is ready although something is meant to be.
	Down
)

// String renders the health as the word shown next to the glyph, so meaning
// never rides on colour alone (SPEC 26).
func (h Health) String() string {
	switch h {
	case Healthy:
		return "healthy"
	case Degraded:
		return "degraded"
	case Down:
		return "down"
	default:
		return "unknown"
	}
}

// severity orders health for "worst first" sorting. Unknown sits above healthy:
// something kubeui cannot vouch for deserves a look before something it can.
func (h Health) severity() int {
	switch h {
	case Down:
		return 3
	case Degraded:
		return 2
	case Unknown:
		return 1
	default:
		return 0
	}
}

// evaluate derives health, replica counts and pod problems from the objects
// that were grouped into the application.
//
// The rules are deliberately mechanical. Every one of them is a fact the API
// server reported, never an inference about causes: an application whose pods
// are in CrashLoopBackOff is *degraded because two pods are not ready*, and why
// they crash is a question for the diagnosis engine (SPEC 10).
func (a *Application) evaluate() {
	var desired, ready int32
	replicated := false
	paused, suspended := 0, 0

	for _, w := range a.Workloads {
		if w.Paused {
			paused++
		}
		if w.Suspended {
			suspended++
		}
		if !w.Replicated {
			continue
		}
		replicated = true
		desired += w.Desired
		ready += min32(w.Ready, w.Desired)
	}

	problems := map[string]int{}
	// Counted as int32 because that is what a replica count is: converting a
	// pod tally into one would be a conversion the compiler cannot vouch for.
	var running, notReady int32
	for _, p := range a.Pods {
		a.Restarts += p.Restarts
		if p.Terminal() {
			// A completed Job pod is the successful end of its work, not a pod
			// that failed to become ready.
			continue
		}
		running++
		if !p.Ready {
			notReady++
		}
		if p.Reason != "" {
			problems[p.Reason]++
		}
	}

	// A workload without a replica count (a CronJob) still has pods, and they
	// are the only thing that can be counted for it.
	if !replicated {
		desired = running
		ready = running - notReady
	}

	a.DesiredPods, a.ReadyPods = desired, ready
	a.Problems = rankProblems(problems)
	a.Health, a.Summary = classify(state{
		desired:    desired,
		ready:      ready,
		replicated: replicated,
		pods:       running,
		notReady:   notReady,
		problems:   len(problems) > 0,
		paused:     paused,
		suspended:  suspended,
		workloads:  len(a.Workloads),
	})
}

// state is the evidence classify decides on, named so the rules read as rules.
type state struct {
	desired, ready    int32
	pods, notReady    int32
	problems          bool
	paused, suspended int
	workloads         int
	// replicated is false when no workload in the application declares a
	// replica count, so "0 of 0" describes a CronJob between runs rather than
	// something that was scaled away.
	replicated bool
}

func classify(s state) (Health, string) {
	// A paused or suspended workload is a deliberate state, not a fault: it
	// changes what the summary says, never what the health says. Colouring
	// every paused-but-fully-ready Deployment as degraded would train users to
	// ignore the colour.
	note := ""
	if s.paused > 0 {
		note = ", rollout paused"
	}
	if s.suspended > 0 {
		note = ", suspended"
	}

	switch {
	case s.workloads == 0 && s.pods == 0:
		return Unknown, "nothing running"
	case !s.replicated && s.pods == 0:
		return Unknown, "no pods running" + note
	case s.desired == 0 && s.pods == 0:
		// Scaled to zero on purpose. Calling that "down" would cry wolf on
		// every deliberately idle workload in the cluster.
		return Unknown, "scaled to zero"
	case s.desired > 0 && s.ready == 0:
		return Down, replicaSummary(s.ready, s.desired) + note
	case s.ready < s.desired, s.notReady > 0, s.problems:
		return Degraded, replicaSummary(s.ready, s.desired) + note
	default:
		return Healthy, replicaSummary(s.ready, s.desired) + note
	}
}

func replicaSummary(ready, desired int32) string {
	return itoa(int(ready)) + " of " + itoa(int(desired)) + " pods ready"
}

// rankProblems orders pod states by how many pods are in them, then by name so
// the dashboard does not reshuffle between two equally common problems.
func rankProblems(counts map[string]int) []Problem {
	if len(counts) == 0 {
		return nil
	}
	out := make([]Problem, 0, len(counts))
	for reason, n := range counts {
		out = append(out, Problem{Reason: reason, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

package diagnosis

import (
	"strconv"
	"strings"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

// recentChange is how long after a rollout a failing application is still
// worth pointing at it. An incident an hour after a deploy usually is the
// deploy; one a week later usually is not, and saying so every time would
// teach people to skip the line.
const recentChange = 6 * time.Hour

// maxChanges bounds how many template fields a finding quotes. A rollout that
// changed forty fields is a rewrite, and the rollback diff is where to read it.
const maxChanges = 5

// rolloutChanged answers the question every incident starts with — what
// changed? — from the one memory of change Kubernetes keeps: each Deployment's
// previous ReplicaSets, and the pod templates they were made from.
//
// It never claims the change caused the failure. Two readings are stated, and
// they are different claims:
//
//   - The new revision's pods fail while the previous revision's pods are
//     ready. That is a comparison the cluster itself makes visible, and the
//     difference between the two templates is the likeliest place to look.
//   - Everything fails and the template changed recently. That is a
//     coincidence in time, stated as one.
func rolloutChanged(in *Input) []Diagnosis {
	if in.App.Health == application.Healthy {
		return nil
	}
	var out []Diagnosis
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		if w.Kind != "Deployment" {
			continue
		}
		revs := in.Context.RevisionsOf(w.UID)
		if len(revs) < 2 {
			continue
		}
		current, previous := &revs[0], &revs[1]
		changes := application.TemplateChanges(previous.Template, current.Template)
		if len(changes) == 0 {
			continue
		}

		failing, total := revisionPods(&in.App, current)
		age := in.Now.Sub(current.CreatedAt)
		var d Diagnosis
		switch {
		case failing > 0 && previous.Ready > 0:
			d = Diagnosis{
				Rule:     "workload.revisionfailing",
				Severity: Warning,
				Problem: "revision " + revisionName(current) + " of " + w.Kind + "/" + w.Name +
					" is failing while revision " + revisionName(previous) + " still serves",
				Cause: strconv.Itoa(failing) + " of " + strconv.Itoa(total) + " pods of the new revision are not ready, and " +
					strconv.Itoa(int(previous.Ready)) + " of the previous one are, so the difference between the two templates is the likeliest place to look",
				Unknown:    "Which of the changed fields is responsible: Kubernetes records what changed, not what it broke.",
				Confidence: Medium,
			}
		case current.CreatedAt.IsZero() || age > recentChange:
			continue
		default:
			d = Diagnosis{
				Rule:       "workload.changed",
				Severity:   Info,
				Problem:    w.Kind + "/" + w.Name + " was changed " + since(age) + " ago, in revision " + revisionName(current),
				Cause:      "the change precedes the failure; that is the whole of what the cluster shows",
				Unknown:    "Whether the change caused it: the previous revision is no longer running to compare against.",
				Confidence: Low,
			}
		}
		d.Subject = application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID}
		d.Chain = chain(&in.App, "revision "+revisionName(previous)+" → "+revisionName(current), changes[0].String())
		d.Evidence = revisionEvidence(current, previous, changes)
		d.Suggestions = []Suggestion{
			{Text: "Read the revision that is rolling out, whole",
				Command: historyCommand(w, current)},
			{Text: "If the change is the cause, roll back to revision " + revisionName(previous) + " — compare the two first",
				Command: historyCommand(w, previous)},
		}
		out = append(out, d)
	}
	return out
}

// rolloutStalled quotes the controller when it has given up on a rollout. It is
// a consequence whenever pods are failing — they are why it stalled, and the
// pod rules say more about them than the deadline does.
func rolloutStalled(in *Input) []Diagnosis {
	var out []Diagnosis
	for i := range in.App.Workloads {
		w := &in.App.Workloads[i]
		if !w.Stalled {
			continue
		}
		cause := w.StalledMessage
		if cause == "" {
			cause = "the controller reports " + orUnknown(w.StalledReason)
		}
		out = append(out, Diagnosis{
			Rule:        "workload.stalled",
			Severity:    Warning,
			Subject:     application.ObjectRef{Kind: w.Kind, Name: w.Name, UID: w.UID},
			Problem:     w.Kind + "/" + w.Name + "'s rollout has stopped making progress",
			Cause:       cause,
			Confidence:  High,
			Consequence: anyPodNotReady(&in.App),
			Chain:       chain(&in.App, "rollout", orUnknown(w.StalledReason)),
			Evidence: []Evidence{{Kind: w.Kind, Name: w.Name,
				Detail: "Progressing=False: " + orUnknown(w.StalledReason) + ", " + strconv.Itoa(int(w.Updated)) +
					" of " + strconv.Itoa(int(w.Desired)) + " replicas updated"}},
			Suggestions: []Suggestion{{Text: "See where the rollout stopped",
				Command: "kubectl rollout status " + strings.ToLower(w.Kind) + "/" + w.Name + " -n " + w.Namespace + " --timeout=1s"}},
		})
	}
	return out
}

// revisionPods counts the live pods a revision's ReplicaSet created, and how
// many of them are not ready.
func revisionPods(app *application.Application, r *application.Revision) (failing, total int) {
	for _, p := range livePods(app) {
		owner, ok := p.Controller()
		if !ok || owner.UID != r.UID {
			continue
		}
		total++
		if !p.Ready {
			failing++
		}
	}
	return failing, total
}

func anyPodNotReady(app *application.Application) bool {
	for _, p := range livePods(app) {
		if !p.Ready {
			return true
		}
	}
	return false
}

func revisionEvidence(current, previous *application.Revision, changes []application.Change) []Evidence {
	out := []Evidence{
		{Kind: "ReplicaSet", Name: current.Name, At: current.CreatedAt,
			Detail: "revision " + revisionName(current) + ", " + replicaDetail(current) + changeCause(current)},
		{Kind: "ReplicaSet", Name: previous.Name,
			Detail: "revision " + revisionName(previous) + ", " + replicaDetail(previous) + changeCause(previous)},
	}
	for i, c := range changes {
		if i == maxChanges {
			out = append(out, Evidence{Kind: "ReplicaSet", Name: current.Name,
				Detail: strconv.Itoa(len(changes)-maxChanges) + " more fields changed"})
			break
		}
		out = append(out, Evidence{Kind: "ReplicaSet", Name: current.Name, Detail: c.String()})
	}
	return out
}

func replicaDetail(r *application.Revision) string {
	return strconv.Itoa(int(r.Ready)) + " of " + strconv.Itoa(int(r.Desired)) + " ready"
}

func changeCause(r *application.Revision) string {
	if r.ChangeCause == "" {
		return ""
	}
	return "; change-cause: " + r.ChangeCause
}

func revisionName(r *application.Revision) string {
	if r.Number == 0 {
		return r.Name
	}
	return strconv.FormatInt(r.Number, 10)
}

func historyCommand(w *application.Workload, r *application.Revision) string {
	cmd := "kubectl rollout history " + strings.ToLower(w.Kind) + "/" + w.Name + " -n " + w.Namespace
	if r.Number > 0 {
		cmd += " --revision=" + strconv.FormatInt(r.Number, 10)
	}
	return cmd
}

// since renders a duration the way the rest of the screen does: one unit,
// rounded down, because "4m" is read faster than "4m12s" and means the same
// thing during an incident.
func since(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(max(int(d/time.Second), 0)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
}

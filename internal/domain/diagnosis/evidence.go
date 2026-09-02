package diagnosis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// maxEvidence bounds how many objects a finding quotes. Three pods are enough
// to recognise a pattern; forty is a wall of text nobody reads during an
// incident.
const maxEvidence = 3

// livePods are the pods that are still meant to be running. A Job's completed
// pods are not a problem to explain.
func livePods(app application.Application) []*application.Pod {
	out := make([]*application.Pod, 0, len(app.Pods))
	for i := range app.Pods {
		if app.Pods[i].Terminal() {
			continue
		}
		out = append(out, &app.Pods[i])
	}
	return out
}

func podRef(p *application.Pod) application.ObjectRef {
	return application.ObjectRef{Kind: "Pod", Name: p.Name, UID: p.UID}
}

// chain renders the path from the workload to the failure, which is the line
// the WHY view leads with.
func chain(app application.Application, steps ...string) []string {
	out := make([]string, 0, len(steps)+2)
	if len(app.Workloads) > 0 {
		out = append(out, app.Workloads[0].Kind+"/"+app.Workloads[0].Name)
	} else {
		out = append(out, "Application/"+app.Name)
	}
	return append(out, steps...)
}

// warningsAbout returns the cluster's own complaints about an object, newest
// first. They are the difference between "the probe is failing" and "the probe
// is failing because the connection is refused".
func warningsAbout(in *Input, ref application.ObjectRef, reasons ...string) []application.Event {
	events := in.Context.EventsAbout(ref.UID, ref.Name)
	out := make([]application.Event, 0, len(events))
	for _, e := range events {
		if e.Type != "Warning" {
			continue
		}
		if len(reasons) > 0 && !contains(reasons, e.Reason) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// eventEvidence quotes an event as the cluster wrote it.
func eventEvidence(e application.Event) Evidence {
	detail := e.Reason + ": " + e.Message
	if e.Count > 1 {
		detail += " (x" + strconv.Itoa(int(e.Count)) + ")"
	}
	return Evidence{Kind: "Event", Name: e.About.Name, Detail: detail, At: e.LastSeen}
}

// podsPhrase renders "3 pods" or "1 pod", because a diagnosis that says
// "1 pods" reads like a machine wrote it.
func podsPhrase(n int) string {
	if n == 1 {
		return "1 pod"
	}
	return strconv.Itoa(n) + " pods"
}

// agree picks the form that goes with the count, so the rest of the sentence
// agrees with the subject podsPhrase produced.
func agree(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// containerOf names a container in a way that is useful in a sentence.
func containerOf(c *application.Container) string {
	if c.Init {
		return "init container " + c.Name
	}
	return "container " + c.Name
}

// logsCommand is the kubectl invocation that shows what actually failed.
func logsCommand(p *application.Pod, c *application.Container, previous bool) string {
	cmd := "kubectl logs -n " + p.Namespace + " " + p.Name
	if c != nil && c.Name != "" {
		cmd += " -c " + c.Name
	}
	if previous {
		cmd += " --previous"
	}
	return cmd
}

func describeCommand(kind string, namespace, name string) string {
	cmd := "kubectl describe " + strings.ToLower(kind) + " "
	if namespace != "" {
		cmd += "-n " + namespace + " "
	}
	return cmd + name
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// sortedNames keeps evidence order stable between two renders of the same
// cluster state.
func sortedNames(pods []*application.Pod) []*application.Pod {
	out := append([]*application.Pod(nil), pods...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

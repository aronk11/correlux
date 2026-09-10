package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/kube/debug"
	"github.com/aronk11/correlux/internal/kube/podexec"
	"github.com/aronk11/correlux/internal/kube/resources"
	"github.com/aronk11/correlux/internal/ui/palette"
	"github.com/aronk11/correlux/internal/ui/screens"
	"github.com/aronk11/correlux/internal/ui/theme"
)

const paletteDebug palette.ActionID = "debug.action"
const debugResource = "debug.correlux.internal"

func (m *Model) debugCommands() []palette.Command {
	if m.view == viewFleet || m.view == viewFleetResource {
		return nil
	}
	var cmds []palette.Command
	for _, entry := range [][2]string{{"sessions", "Manage troubleshooting sessions"}, {"toolbox", "Launch temporary toolbox"}, {"http", "Check HTTP / TLS connectivity"}, {"dns", "Check DNS resolution"}, {"tcp", "Check TCP connectivity"}} {
		cmds = append(cmds, palette.Command{ID: "debug." + entry[0], Action: paletteDebug, Arg: entry[0], Title: entry[1], Category: "Troubleshoot", Keywords: []string{"debug", "busybox", "curl", "network"}, Enabled: true})
	}
	if m.view == viewObject && m.objectTarget.Kind == "Pod" && m.object.HasValue() {
		cmds = append(cmds, palette.Command{ID: "debug.ephemeral", Action: paletteDebug, Arg: "ephemeral", Title: "Add debug container to this pod", Category: "Troubleshoot", Enabled: true})
		var pod corev1.Pod
		if obj := m.object.Get(); obj != nil && json.Unmarshal(obj.Raw, &pod) == nil {
			for i := range pod.Spec.EphemeralContainers {
				container := &pod.Spec.EphemeralContainers[i]
				if strings.HasPrefix(container.Name, "correlux-debug-") {
					cmds = append(cmds, palette.Command{ID: "debug.shell." + container.Name, Action: paletteDebug, Arg: "shell:" + container.Name, Title: "Shell in " + container.Name, Category: "Troubleshoot", Enabled: true})
				}
			}
		}
	}
	return cmds
}

func (m *Model) debugNamespace() string {
	if ref, ok := m.execRef(); ok && ref.Namespace != "" {
		return ref.Namespace
	}
	if !m.allNamespaces {
		return m.namespace
	}
	return ""
}

func (m *Model) openDebug(mode string) tea.Cmd {
	if mode == "sessions" {
		ns := m.namespace
		if m.allNamespaces {
			ns = ""
		}
		return m.openObject(objectRef{Kind: "Debug sessions", Name: "sessions", Namespace: ns, Resource: debugResource})
	}
	if strings.HasPrefix(mode, "shell:") {
		target := podexec.Target{Namespace: m.objectTarget.Namespace, Pod: m.objectTarget.Name, Container: strings.TrimPrefix(mode, "shell:")}
		return m.confirm(pendingAction{Title: "Open debug shell", Lines: []string{target.Label() + " in " + target.Namespace, "Commands execute inside this pod's debug container."}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd { return m.startExec(target) }})
	}
	if mode == "ephemeral" {
		return m.promptEphemeral()
	}
	ns := m.debugNamespace()
	if ns == "" {
		m.promptTitle = "Namespace for troubleshooting"
		m.promptNote = "Choose one namespace; the new pod uses that namespace's network policies."
		m.promptRef = objectRef{}
		m.promptError = ""
		m.promptInput.SetValue("")
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, value string) tea.Cmd {
			m.cancelPrompt()
			return m.promptDebugDestination(mode, strings.TrimSpace(value))
		}
		return nil
	}
	return m.promptDebugDestination(mode, ns)
}

func (m *Model) promptDebugDestination(mode, namespace string) tea.Cmd {
	opts := debug.Options{Namespace: namespace, Mode: mode, Seconds: 60, ImagePullSecrets: m.cfg.Debug.ImagePullSecrets}
	opts.Image = m.cfg.Debug.ToolboxImage
	if opts.Image == "" {
		opts.Image = "busybox:1.37.0"
	}
	if mode == "http" {
		opts.Image = m.cfg.Debug.CurlImage
		if opts.Image == "" {
			opts.Image = "curlimages/curl:8.21.0"
		}
	}
	if mode == "tcp" {
		opts.Image = m.cfg.Debug.NetworkImage
		if opts.Image == "" {
			opts.Image = "nicolaka/netshoot:v0.16"
		}
	}
	if mode == "toolbox" {
		opts.Seconds = 900
		return m.promptDebugImage(opts)
	}
	m.promptTitle = "Destination for " + mode + " check"
	m.promptNote = "Runs from a new pod in " + namespace + ". This does not reproduce the application's pod identity."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue("")
	m.overlay = overlayPrompt
	if ref, ok := m.execRef(); ok && ref.Kind == "Service" {
		host := ref.Name + "." + ref.Namespace + ".svc"
		if mode == "http" {
			host = "http://" + host
		}
		m.promptInput.SetValue(host)
	}
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		opts.Destination = strings.TrimSpace(value)
		if _, err := debug.Command(mode, opts.Destination, opts.Seconds); err != nil {
			m.promptError = err.Error()
			return nil
		}
		m.cancelPrompt()
		return m.promptDebugImage(opts)
	}
	return nil
}

func (m *Model) promptDebugImage(opts debug.Options) tea.Cmd {
	m.promptTitle = "Image for " + opts.Mode + " session"
	m.promptNote = "Use an approved image or private-registry reference. Runtime is " + strconv.FormatInt(opts.Seconds, 10) + " seconds."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue(opts.Image)
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		opts.Image = strings.TrimSpace(value)
		job, err := debug.Job(opts)
		if err != nil {
			m.promptError = err.Error()
			return nil
		}
		m.cancelPrompt()
		return m.confirmDebugJob(job)
	}
	return nil
}

type debugCreatedMsg struct {
	cluster string
	job     *batchv1.Job
	from    viewKind
	ref     objectRef
	err     error
}

func (m *Model) confirmDebugJob(job *batchv1.Job) tea.Cmd {
	cluster, factory, from, ref := m.contextName, m.factory, m.view, m.objectTarget
	return m.confirm(pendingAction{Title: "Create " + job.Annotations["correlux.dev/debug-mode"] + " troubleshooting session", Lines: []string{
		"Creates one unprivileged Linux pod in " + job.Namespace + ".", "Image: " + job.Spec.Template.Spec.Containers[0].Image,
		"Command: " + strings.Join(job.Spec.Template.Spec.Containers[0].Command, " "),
		"No service-account token or application labels are copied.",
		"Maximum runtime " + strconv.FormatInt(*job.Spec.ActiveDeadlineSeconds, 10) + "s; Job and pod expire 10 minutes after finishing.",
		"Network results describe this new pod, whose identity can differ from the application.",
	}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd {
		if m.contextName != cluster {
			return nil
		}
		m.notice("Creating troubleshooting session…", theme.StatusUnknown)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
			defer cancel()
			created, err := factory.CreateDebug(ctx, cluster, job)
			return debugCreatedMsg{cluster: cluster, job: created, from: from, ref: ref, err: err}
		}
	}})
}

func (m *Model) applyDebugCreated(msg *debugCreatedMsg) tea.Cmd {
	if msg.cluster != m.contextName {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not create troubleshooting session: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Session "+msg.job.Name+" created. Open its pod for logs or a shell; delete the Job to stop and clean up.", theme.StatusHealthy)
	if m.view == msg.from && m.objectTarget == msg.ref {
		return tea.Batch(m.openObject(objectRef{Kind: "Job", Name: msg.job.Name, Namespace: msg.job.Namespace, Resource: "jobs.batch"}), m.loadApplications(), m.expireNotice())
	}
	return tea.Batch(m.loadApplications(), m.expireNotice())
}

func (m *Model) loadDebugSessions(ref objectRef, gen uint64) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), m.factory.Timeout())
	m.cancelObject = cancel
	m.objectLoading = true
	factory, cluster := m.factory, m.contextName
	return func() tea.Msg {
		defer cancel()
		raw, err := factory.DebugSessions(ctx, cluster, ref.Namespace, ref.Continuation)
		if err != nil {
			return objectLoadedMsg{gen: gen, err: err}
		}
		document, _ := yaml.JSONToYAML(raw)
		return objectLoadedMsg{gen: gen, object: &resources.Object{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace, Raw: raw, YAML: string(document)}}
	}
}

func (m *Model) debugSessionsView(d screens.ObjectData, obj *resources.Object) (screens.ObjectData, []objectRef) {
	var list batchv1.JobList
	if err := json.Unmarshal(obj.Raw, &list); err != nil {
		d.Message = "Could not decode debug sessions: " + err.Error()
		return d, nil
	}
	d.Subtitle = "Correlux jobs in " + orNone(m.objectTarget.Namespace) + " • includes retained finished sessions"
	d.YAML = splitDocument(obj.YAML)
	section := screens.DetailSection{Title: "Sessions", Columns: []string{"Job", "Namespace", "Mode", "State", "Destination"}, Empty: "no troubleshooting Jobs in this scope"}
	var refs []objectRef
	for i := range list.Items {
		job := &list.Items[i]
		state := "pending"
		if job.Status.Active > 0 {
			state = "running"
		}
		if job.Status.Succeeded > 0 {
			state = "succeeded"
		}
		for _, c := range job.Status.Conditions {
			if c.Status == corev1.ConditionTrue && c.Type == batchv1.JobFailed {
				state = "failed: " + c.Reason
			}
		}
		refs = append(refs, objectRef{Kind: "Job", Name: job.Name, Namespace: job.Namespace, Resource: "jobs.batch"})
		section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{job.Name, job.Namespace, job.Annotations["correlux.dev/debug-mode"], state, job.Annotations["correlux.dev/debug-destination"]}, Target: len(refs) - 1})
	}
	if list.Continue != "" {
		ref := m.objectTarget
		ref.Continuation = list.Continue
		refs = append(refs, ref)
		section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{"Next page…"}, Target: len(refs) - 1})
		d.Subtitle += " • more sessions exist"
	}
	d.Sections = []screens.DetailSection{section}
	return d, refs
}

type ephemeralAddedMsg struct {
	cluster string
	ref     objectRef
	name    string
	err     error
}

func (m *Model) promptEphemeral() tea.Cmd {
	if m.view != viewObject || m.objectTarget.Kind != "Pod" || m.object.Get() == nil {
		return nil
	}
	ref := m.objectTarget
	var pod corev1.Pod
	if json.Unmarshal(m.object.Get().Raw, &pod) != nil || len(pod.Spec.Containers) == 0 {
		return nil
	}
	m.promptTitle = "Target container for debugging"
	m.promptNote = "Shares this pod's network; process targeting depends on the container runtime."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue(pod.Spec.Containers[0].Name)
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		found := false
		for i := range pod.Spec.Containers {
			c := &pod.Spec.Containers[i]
			if c.Name == value {
				found = true
			}
		}
		if !found {
			m.promptError = "Enter the name of a container in this pod"
			return nil
		}
		target := value
		m.cancelPrompt()
		m.promptTitle = "Debug container image"
		m.promptNote = "Requires /bin/sh and sleep. The container exits after 15 minutes; its entry remains until the pod is deleted."
		m.promptRef = objectRef{}
		m.promptError = ""
		image := m.cfg.Debug.ToolboxImage
		if image == "" {
			image = "busybox:1.37.0"
		}
		m.promptInput.SetValue(image)
		m.overlay = overlayPrompt
		m.promptAccept = func(m *Model, image string) tea.Cmd {
			image = strings.TrimSpace(image)
			if image == "" || strings.ContainsAny(image, " \t\r\n") {
				m.promptError = "Enter an image reference"
				return nil
			}
			m.cancelPrompt()
			cluster, factory := m.contextName, m.factory
			return m.confirm(pendingAction{Title: "Add ephemeral debug container to " + ref.Name, Lines: []string{"Image: " + image, "Pod " + ref.Name + " in " + ref.Namespace + ", target container " + target, "Shares the pod's network and uses its image-pull configuration.", "Runs unprivileged for 15 minutes. Its entry cannot be removed from this pod afterward."}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd {
				if m.contextName != cluster {
					return nil
				}
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
					defer cancel()
					name, err := factory.AddDebugContainer(ctx, cluster, ref.Namespace, ref.Name, string(pod.UID), pod.ResourceVersion, image, target)
					return ephemeralAddedMsg{cluster: cluster, ref: ref, name: name, err: err}
				}
			}})
		}
		return nil
	}
	return nil
}

func (m *Model) applyEphemeralAdded(msg *ephemeralAddedMsg) tea.Cmd {
	if msg.cluster != m.contextName {
		return nil
	}
	if msg.err != nil {
		m.notice("Could not add debug container: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Added "+msg.name+". Refresh when running, then choose its shell in the palette.", theme.StatusHealthy)
	if m.view == viewObject && m.objectTarget == msg.ref {
		return tea.Batch(m.loadObject(), m.expireNotice())
	}
	return m.expireNotice()
}

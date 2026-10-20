package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/aronk11/correlux/internal/kube/debug"
	"github.com/aronk11/correlux/internal/kube/logs"
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
	for _, entry := range [][2]string{{"sessions", "Manage troubleshooting sessions"}, {"toolbox", "Launch temporary toolbox"}, {"http", "Check HTTP / TLS connectivity"}, {"tls", "Inspect a TLS certificate"}, {"dns", "Check DNS resolution"}, {"tcp", "Check TCP connectivity"}} {
		cmds = append(cmds, palette.Command{ID: "debug." + entry[0], Action: paletteDebug, Arg: entry[0], Title: entry[1], Category: "Troubleshoot", Keywords: []string{"debug", "busybox", "curl", "network"}, Enabled: true})
	}
	if m.view == viewObject && m.objectTarget.Kind == "Node" && m.cfg.Debug.NodeShell {
		cmds = append(cmds, palette.Command{ID: "debug.node", Action: paletteDebug, Arg: "node", Title: "Open a root shell on this node",
			Subtitle: "privileged pod with the host at /host; enabled by debug.nodeShell", Category: "Troubleshoot",
			Keywords: []string{"debug", "node", "kubelet", "disk", "containerd", "chroot", "host"}, Enabled: true})
	}
	if m.view == viewObject && m.objectTarget.Kind == "Pod" && m.object.HasValue() {
		cmds = append(cmds, palette.Command{ID: "debug.ephemeral", Action: paletteDebug, Arg: "ephemeral", Title: "Add debug container to this pod", Category: "Troubleshoot", Enabled: true})
		for _, entry := range [][2]string{{"dns", "Check DNS from this pod"}, {"tcp", "Check TCP from this pod"}, {"http", "Check HTTP from this pod"}, {"tls", "Inspect a TLS certificate from this pod"}} {
			cmds = append(cmds, palette.Command{ID: "debug.probe." + entry[0], Action: paletteDebug, Arg: "probe:" + entry[0], Title: entry[1],
				Subtitle: "with the pod's own IP, labels and NetworkPolicies", Category: "Troubleshoot",
				Keywords: []string{"debug", "network", "connectivity", "policy", "egress", entry[0]}, Enabled: true})
		}
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
	if probe, ok := strings.CutPrefix(mode, "probe:"); ok {
		return m.promptProbe(probe)
	}
	if mode == "node" {
		return m.promptNodeShell()
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

// debugImage shares resolution with startup diagnostics.
func (m *Model) debugImage(mode string) string {
	return m.cfg.Debug.Image(mode, m.cfg.AirGapped)
}

func (m *Model) debugPullPolicy() (corev1.PullPolicy, error) {
	value := m.cfg.Debug.ImagePullPolicy
	if value == "" && m.cfg.AirGapped {
		value = string(corev1.PullIfNotPresent)
	}
	return debug.PullPolicy(value)
}

func pullPolicyLabel(policy corev1.PullPolicy) string {
	if policy == "" {
		return "Kubernetes default"
	}
	return string(policy)
}

func (m *Model) promptDebugDestination(mode, namespace string) tea.Cmd {
	policy, err := m.debugPullPolicy()
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	opts := debug.Options{Namespace: namespace, Mode: mode, Seconds: 60, Image: m.debugImage(mode), ImagePullSecrets: m.cfg.Debug.ImagePullSecrets, ImagePullPolicy: policy}
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
	if m.cfg.AirGapped {
		m.promptNote = "Air-gapped: enter an internal-registry or preloaded image. Pull policy: " + pullPolicyLabel(opts.ImagePullPolicy) + "."
	}
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
		"Image pull policy: " + pullPolicyLabel(job.Spec.Template.Spec.Containers[0].ImagePullPolicy),
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
	policy, err := m.debugPullPolicy()
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
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
		image := m.debugImage("toolbox")
		if m.cfg.AirGapped {
			m.promptNote += " Air-gapped: use an internal-registry or preloaded image."
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
			return m.confirm(pendingAction{Title: "Add ephemeral debug container to " + ref.Name, Lines: []string{"Image: " + image, "Image pull policy: " + pullPolicyLabel(policy), "Pod " + ref.Name + " in " + ref.Namespace + ", target container " + target, "Shares the pod's network and uses its existing image-pull secrets.", "Runs unprivileged for 15 minutes. Its entry cannot be removed from this pod afterward."}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd {
				if m.contextName != cluster {
					return nil
				}
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
					defer cancel()
					name, err := factory.AddDebugContainer(ctx, cluster, ref.Namespace, ref.Name, string(pod.UID), pod.ResourceVersion, image, target, policy)
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

type probeStartedMsg struct {
	cluster string
	ref     objectRef
	name    string
	err     error
}

// probeTimeout bounds adding the probe and waiting for its image to start.
const probeTimeout = 90 * time.Second

// promptProbe runs one check from inside the pod on screen. The checks from a
// new pod answer "can something in this namespace reach it?"; this answers the
// question an incident actually asks: can *this* pod reach it, with its own
// labels and therefore its own NetworkPolicies?
func (m *Model) promptProbe(mode string) tea.Cmd {
	if m.view != viewObject || m.objectTarget.Kind != "Pod" || m.object.Get() == nil {
		return nil
	}
	var pod corev1.Pod
	if json.Unmarshal(m.object.Get().Raw, &pod) != nil || len(pod.Spec.Containers) == 0 {
		return nil
	}
	if pod.Status.Phase != corev1.PodRunning {
		m.notice("Checks from a pod need a running pod", theme.StatusWarning)
		return m.expireNotice()
	}
	image := m.debugImage(mode)
	if image == "" {
		m.notice("No image configured for "+mode+" checks; set debug images or debug.registryMirror", theme.StatusWarning)
		return m.expireNotice()
	}
	policy, err := m.debugPullPolicy()
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	ref, target := m.objectTarget, pod.Spec.Containers[0].Name

	m.promptTitle = "Destination for " + mode + " check from " + ref.Name
	m.promptNote = "Runs inside this pod, beside " + target + ", with its network identity."
	m.promptRef = objectRef{}
	m.promptError = ""
	switch mode {
	case "tls":
		m.promptInput.SetValue("https://")
	case "http":
		m.promptInput.SetValue("http://")
	default:
		m.promptInput.SetValue("")
	}
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		destination := strings.TrimSpace(value)
		command, err := debug.Command(mode, destination, 0)
		if err != nil {
			m.promptError = err.Error()
			return nil
		}
		m.cancelPrompt()
		cluster, factory := m.contextName, m.factory
		return m.confirm(pendingAction{Title: "Check " + destination + " from " + ref.Name, Lines: []string{
			"Runs: " + strings.Join(command, " "),
			"As an ephemeral container in pod " + ref.Name + " (" + ref.Namespace + "), sharing its network, labels and NetworkPolicies.",
			"Image: " + image + " • pull policy " + pullPolicyLabel(policy),
			"Unprivileged. It exits when the check ends; its entry stays in the pod spec until the pod is replaced.",
		}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd {
			if m.contextName != cluster {
				return nil
			}
			m.notice("Starting the check inside "+ref.Name+"…", theme.StatusUnknown)
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
				defer cancel()
				name, err := factory.AddProbeContainer(ctx, cluster, ref.Namespace, ref.Name, string(pod.UID), pod.ResourceVersion, image, target, policy, command)
				if err == nil {
					err = factory.WaitForEphemeral(ctx, cluster, ref.Namespace, ref.Name, name)
				}
				return probeStartedMsg{cluster: cluster, ref: ref, name: name, err: err}
			}
		}})
	}
	return nil
}

// applyProbeStarted opens the probe's output, which is the whole point of it.
func (m *Model) applyProbeStarted(msg *probeStartedMsg) tea.Cmd {
	if msg.cluster != m.contextName {
		return nil
	}
	if msg.err != nil {
		m.notice("Check could not run: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.message = ""
	m.logPrevious = false
	return m.openLogsFor([]logs.Source{{Namespace: msg.ref.Namespace, Pod: msg.ref.Name, Container: msg.name}}, "Check from "+msg.ref.Name)
}

// nodeShellSeconds is how long the node pod lives: long enough to read logs
// and look around, short enough that a forgotten one does not stay root.
const nodeShellSeconds = 1800

// promptNodeShell creates the privileged node pod. It asks for the namespace,
// because Pod Security admission usually refuses privileged pods everywhere
// but a few namespaces, and the person at the keyboard knows which.
func (m *Model) promptNodeShell() tea.Cmd {
	if !m.cfg.Debug.NodeShell || m.view != viewObject || m.objectTarget.Kind != "Node" {
		return nil
	}
	policy, err := m.debugPullPolicy()
	if err != nil {
		m.notice(err.Error(), theme.StatusWarning)
		return m.expireNotice()
	}
	node := m.objectTarget.Name
	m.promptTitle = "Namespace for the node pod on " + node
	m.promptNote = "Pod Security admission must allow privileged pods there; kube-system usually does."
	m.promptRef = objectRef{}
	m.promptError = ""
	m.promptInput.SetValue("kube-system")
	m.overlay = overlayPrompt
	m.promptAccept = func(m *Model, value string) tea.Cmd {
		job, err := debug.NodeJob(debug.NodeOptions{Node: node, Namespace: strings.TrimSpace(value), Image: m.debugImage("toolbox"),
			ImagePullSecrets: m.cfg.Debug.ImagePullSecrets, ImagePullPolicy: policy, Seconds: nodeShellSeconds})
		if err != nil {
			m.promptError = err.Error()
			return nil
		}
		m.cancelPrompt()
		return m.confirmNodeJob(job)
	}
	return nil
}

func (m *Model) confirmNodeJob(job *batchv1.Job) tea.Cmd {
	cluster, factory, from, ref := m.contextName, m.factory, m.view, m.objectTarget
	spec := &job.Spec.Template.Spec
	return m.confirm(pendingAction{Title: "Open a root shell on node " + spec.NodeName, Lines: []string{
		"PRIVILEGED: root on " + spec.NodeName + ", in the host's PID, network and IPC namespaces.",
		"The node's filesystem is mounted at /host; run chroot /host to use the node's own tools.",
		"Pod in " + job.Namespace + " • image " + spec.Containers[0].Image + " • tolerates every taint.",
		"Lives at most " + strconv.Itoa(nodeShellSeconds/60) + " minutes; delete the Job to end it sooner.",
	}, Challenge: m.productionChallenge(), Run: func(m *Model) tea.Cmd {
		if m.contextName != cluster {
			return nil
		}
		m.notice("Creating node pod…", theme.StatusUnknown)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
			defer cancel()
			created, err := factory.CreateDebug(ctx, cluster, job)
			return debugCreatedMsg{cluster: cluster, job: created, from: from, ref: ref, err: err}
		}
	}})
}

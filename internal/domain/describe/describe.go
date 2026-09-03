// Package describe turns the document an object came as into the handful of
// facts a person actually looks for.
//
// It is deliberately not a reimplementation of `kubectl describe`: that command
// prints everything, and everything is what makes it hard to read at three in
// the morning. This prints what identifies the object, what state it is in, and
// what the cluster has to say about it.
//
// Everything here works on the raw JSON. Nothing is decoded into a typed
// struct, because a typed struct only knows the fields it was compiled with,
// and half of a cluster's objects are custom resources whose fields Correlux
// has never heard of. Reading a document generically means an unknown kind is
// described as well as a Pod is — and, per SPEC 34, that a strangely shaped
// object is a thin description rather than a panic.
package describe

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Section is one titled table of facts.
type Section struct {
	Title   string
	Columns []string
	Rows    [][]string
	// Empty is shown instead of the rows when there are none.
	Empty string
}

// Object describes any object, using whatever the document actually contains.
func Object(kind string, raw []byte) []Section {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	var sections []Section
	switch kind {
	case "Pod":
		sections = describePod(doc)
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		sections = describeWorkload(kind, doc)
	case "Service":
		sections = describeService(doc)
	case "Ingress":
		sections = describeIngress(doc)
	case "Job", "CronJob":
		sections = describeJob(kind, doc)
	case "Node":
		sections = describeNode(doc)
	case "PersistentVolumeClaim":
		sections = describeClaim(doc)
	default:
		// A kind with no rules of its own is still described: whatever its
		// author put in its status is what they wanted anybody to see.
		sections = append(sections, genericSection(doc))
	}

	// Conditions are how every controller in Kubernetes reports itself, custom
	// ones included, so they are worth showing whatever the kind is.
	if conditions := conditionsSection(doc); conditions != nil {
		sections = append(sections, *conditions)
	}
	return sections
}

func describePod(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Phase", str(status, "phase"))
	overview.add("Node", str(spec, "nodeName"))
	overview.add("Pod IP", str(status, "podIP"))
	overview.add("Host IP", str(status, "hostIP"))
	overview.add("QoS", str(status, "qosClass"))
	overview.add("Service account", str(spec, "serviceAccountName"))
	overview.add("Restart policy", str(spec, "restartPolicy"))
	overview.add("Started", str(status, "startTime"))

	containers := Section{
		Title:   "Containers",
		Columns: []string{"Name", "Image", "State", "Ready", "Restarts", "Requests", "Limits"},
		Empty:   "none reported",
	}
	states := containerStates(status)
	for _, group := range []struct{ field, label string }{
		{"initContainers", "init "},
		{"containers", ""},
	} {
		for _, c := range slice(spec, group.field) {
			container, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name := str(container, "name")
			state := states[name]
			containers.Rows = append(containers.Rows, []string{
				group.label + name,
				str(container, "image"),
				state.state,
				yesNo(state.ready),
				strconv.Itoa(state.restarts),
				resourceList(child(child(container, "resources"), "requests")),
				resourceList(child(child(container, "resources"), "limits")),
			})
		}
	}

	volumes := Section{Title: "Volumes", Columns: []string{"Name", "Source"}, Empty: "none"}
	for _, v := range slice(spec, "volumes") {
		volume, ok := v.(map[string]any)
		if !ok {
			continue
		}
		volumes.Rows = append(volumes.Rows, []string{str(volume, "name"), volumeSource(volume)})
	}

	return []Section{overview, containers, volumes}
}

// containerState is the part of a container's status a description needs.
type containerState struct {
	state    string
	ready    bool
	restarts int
}

func containerStates(status map[string]any) map[string]containerState {
	out := map[string]containerState{}
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, s := range slice(status, field) {
			cs, ok := s.(map[string]any)
			if !ok {
				continue
			}
			out[str(cs, "name")] = containerState{
				state:    stateOf(child(cs, "state")),
				ready:    boolean(cs, "ready"),
				restarts: number(cs, "restartCount"),
			}
		}
	}
	return out
}

// stateOf renders a container state the way the kubelet reports it: the phase
// plus the reason, because "waiting" alone explains nothing.
func stateOf(state map[string]any) string {
	for _, phase := range []string{"waiting", "terminated", "running"} {
		inner := child(state, phase)
		if inner == nil {
			continue
		}
		if reason := str(inner, "reason"); reason != "" {
			return phase + ": " + reason
		}
		if phase == "terminated" {
			return phase + ": exit " + strconv.Itoa(number(inner, "exitCode"))
		}
		return phase
	}
	return "—"
}

func describeWorkload(kind string, doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	switch kind {
	case "DaemonSet":
		overview.add("Desired", strconv.Itoa(number(status, "desiredNumberScheduled")))
		overview.add("Ready", strconv.Itoa(number(status, "numberReady")))
		overview.add("Up to date", strconv.Itoa(number(status, "updatedNumberScheduled")))
		overview.add("Available", strconv.Itoa(number(status, "numberAvailable")))
		overview.add("Misscheduled", strconv.Itoa(number(status, "numberMisscheduled")))
	default:
		overview.add("Replicas", strconv.Itoa(number(spec, "replicas")))
		overview.add("Ready", strconv.Itoa(number(status, "readyReplicas")))
		overview.add("Up to date", strconv.Itoa(number(status, "updatedReplicas")))
		overview.add("Available", strconv.Itoa(number(status, "availableReplicas")))
	}
	if strategy := child(spec, "strategy"); strategy != nil {
		overview.add("Strategy", str(strategy, "type"))
	}
	if str(spec, "serviceName") != "" {
		overview.add("Service", str(spec, "serviceName"))
	}
	if boolean(spec, "paused") {
		overview.add("Paused", "yes")
	}
	overview.add("Selector", selectorOf(child(spec, "selector")))
	overview.add("Generation", strconv.Itoa(number(child(doc, "metadata"), "generation")))

	return []Section{overview, templateSection(spec)}
}

// templateSection describes the pod template a controller creates pods from,
// which is where the image everyone is looking for lives.
func templateSection(spec map[string]any) Section {
	section := Section{
		Title:   "Pod template",
		Columns: []string{"Container", "Image", "Ports", "Requests", "Limits"},
		Empty:   "no pod template",
	}
	template := child(child(spec, "template"), "spec")
	for _, group := range []struct{ field, label string }{
		{"initContainers", "init "},
		{"containers", ""},
	} {
		for _, c := range slice(template, group.field) {
			container, ok := c.(map[string]any)
			if !ok {
				continue
			}
			section.Rows = append(section.Rows, []string{
				group.label + str(container, "name"),
				str(container, "image"),
				containerPorts(container),
				resourceList(child(child(container, "resources"), "requests")),
				resourceList(child(child(container, "resources"), "limits")),
			})
		}
	}
	return section
}

func describeService(doc map[string]any) []Section {
	spec := child(doc, "spec")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Type", str(spec, "type"))
	overview.add("Cluster IP", str(spec, "clusterIP"))
	overview.add("Session affinity", str(spec, "sessionAffinity"))
	overview.add("External traffic", str(spec, "externalTrafficPolicy"))
	overview.add("Selector", selectorMap(child(spec, "selector")))

	ports := Section{
		Title:   "Ports",
		Columns: []string{"Name", "Port", "Target", "Protocol", "Node port"},
		Empty:   "none",
	}
	for _, p := range slice(spec, "ports") {
		port, ok := p.(map[string]any)
		if !ok {
			continue
		}
		ports.Rows = append(ports.Rows, []string{
			orDash(str(port, "name")),
			strconv.Itoa(number(port, "port")),
			orDash(value(port["targetPort"])),
			str(port, "protocol"),
			orDash(strconv.Itoa(number(port, "nodePort"))),
		})
	}
	return []Section{overview, ports}
}

func describeIngress(doc map[string]any) []Section {
	spec := child(doc, "spec")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Class", str(spec, "ingressClassName"))
	lbs := slice(child(child(doc, "status"), "loadBalancer"), "ingress")
	addresses := make([]string, 0, len(lbs))
	for _, lb := range lbs {
		entry, ok := lb.(map[string]any)
		if !ok {
			continue
		}
		addresses = append(addresses, orDash(str(entry, "ip")+str(entry, "hostname")))
	}
	overview.add("Address", strings.Join(addresses, ", "))

	rules := Section{
		Title:   "Rules",
		Columns: []string{"Host", "Path", "Backend"},
		Empty:   "none",
	}
	for _, r := range slice(spec, "rules") {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		host := orDash(str(rule, "host"))
		for _, p := range slice(child(rule, "http"), "paths") {
			path, ok := p.(map[string]any)
			if !ok {
				continue
			}
			service := child(child(path, "backend"), "service")
			backend := str(service, "name")
			if port := child(service, "port"); port != nil {
				if n := number(port, "number"); n > 0 {
					backend += ":" + strconv.Itoa(n)
				}
			}
			rules.Rows = append(rules.Rows, []string{host, orDash(str(path, "path")), orDash(backend)})
		}
	}
	return []Section{overview, rules}
}

func describeJob(kind string, doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	if kind == "CronJob" {
		overview.add("Schedule", str(spec, "schedule"))
		overview.add("Suspended", yesNo(boolean(spec, "suspend")))
		overview.add("Last schedule", str(status, "lastScheduleTime"))
		overview.add("Active jobs", strconv.Itoa(len(slice(status, "active"))))
		overview.add("Concurrency", str(spec, "concurrencyPolicy"))
		return []Section{overview, templateSection(child(child(spec, "jobTemplate"), "spec"))}
	}
	overview.add("Completions", strconv.Itoa(number(spec, "completions")))
	overview.add("Parallelism", strconv.Itoa(number(spec, "parallelism")))
	overview.add("Active", strconv.Itoa(number(status, "active")))
	overview.add("Succeeded", strconv.Itoa(number(status, "succeeded")))
	overview.add("Failed", strconv.Itoa(number(status, "failed")))
	overview.add("Suspended", yesNo(boolean(spec, "suspend")))
	return []Section{overview, templateSection(spec)}
}

func describeNode(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")
	info := child(status, "nodeInfo")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Kubelet", str(info, "kubeletVersion"))
	overview.add("OS image", str(info, "osImage"))
	overview.add("Runtime", str(info, "containerRuntimeVersion"))
	overview.add("Unschedulable", yesNo(boolean(spec, "unschedulable")))
	overview.add("CPU", str(child(status, "allocatable"), "cpu")+" of "+str(child(status, "capacity"), "cpu"))
	overview.add("Memory", str(child(status, "allocatable"), "memory")+" of "+str(child(status, "capacity"), "memory"))
	overview.add("Pods", str(child(status, "allocatable"), "pods"))

	taints := Section{Title: "Taints", Columns: []string{"Key", "Value", "Effect"}, Empty: "none"}
	for _, t := range slice(spec, "taints") {
		taint, ok := t.(map[string]any)
		if !ok {
			continue
		}
		taints.Rows = append(taints.Rows, []string{
			str(taint, "key"), orDash(str(taint, "value")), str(taint, "effect"),
		})
	}
	return []Section{overview, taints}
}

func describeClaim(doc map[string]any) []Section {
	spec, status := child(doc, "spec"), child(doc, "status")

	overview := Section{Title: "Status", Columns: []string{"Field", "Value"}}
	overview.add("Phase", str(status, "phase"))
	overview.add("Volume", str(spec, "volumeName"))
	overview.add("Storage class", str(spec, "storageClassName"))
	overview.add("Requested", str(child(child(spec, "resources"), "requests"), "storage"))
	overview.add("Capacity", str(child(status, "capacity"), "storage"))
	accessModes := slice(spec, "accessModes")
	modes := make([]string, 0, len(accessModes))
	for _, m := range accessModes {
		modes = append(modes, value(m))
	}
	overview.add("Access modes", strings.Join(modes, ", "))
	return []Section{overview}
}

// conditionsSection renders whatever conditions an object carries. Every
// controller reports through them, so this works for a Deployment and for a
// custom resource nobody here has heard of.
func conditionsSection(doc map[string]any) *Section {
	conditions := slice(child(doc, "status"), "conditions")
	if len(conditions) == 0 {
		return nil
	}
	section := Section{
		Title:   "Conditions",
		Columns: []string{"Type", "Status", "Reason", "Message"},
	}
	for _, c := range conditions {
		condition, ok := c.(map[string]any)
		if !ok {
			continue
		}
		section.Rows = append(section.Rows, []string{
			str(condition, "type"),
			str(condition, "status"),
			orDash(str(condition, "reason")),
			orDash(str(condition, "message")),
		})
	}
	return &section
}

// genericSection is the fallback for a kind with no rules of its own: the
// scalar fields of its status, which is where a custom resource usually puts
// the thing its author wanted you to see.
func genericSection(doc map[string]any) Section {
	section := Section{
		Title:   "Status",
		Columns: []string{"Field", "Value"},
		Empty:   "the object reports no status fields",
	}
	status := child(doc, "status")
	keys := make([]string, 0, len(status))
	for k := range status {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if rendered := value(status[k]); rendered != "" && !strings.HasPrefix(rendered, "[") {
			section.Rows = append(section.Rows, []string{k, rendered})
		}
	}
	return section
}

func (s *Section) add(label, value string) {
	if strings.TrimSpace(value) == "" || value == "0" && label != "Replicas" {
		return
	}
	s.Rows = append(s.Rows, []string{label, value})
}

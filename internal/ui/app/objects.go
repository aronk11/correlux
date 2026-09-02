package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/application"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/async"
	"github.com/aronk11/kubeui/internal/ui/screens"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// objectRef addresses one object the way the user sees it: a kind, a name and
// a namespace. The API group is resolved from the discovery catalog when the
// object is fetched, so nothing above this layer has to know about GVRs.
type objectRef struct {
	Kind      string
	Name      string
	Namespace string
}

func (r objectRef) empty() bool { return r.Kind == "" || r.Name == "" }

func (r objectRef) label() string { return r.Kind + "/" + r.Name }

// openObject opens an object, remembering where it was opened from so Esc can
// walk back out the way it came in.
func (m *Model) openObject(ref objectRef) tea.Cmd {
	if ref.empty() {
		return nil
	}
	if m.view == viewObject {
		m.objectTrail = append(m.objectTrail, m.objectRef())
	} else {
		m.objectTrail = nil
	}
	return m.showObject(ref)
}

// showObject switches to an object without touching the trail.
func (m *Model) showObject(ref objectRef) tea.Cmd {
	m.object.Reset()
	m.objectTarget = ref
	m.view = viewObject
	m.objectOffset = 0
	m.objectCursor = 0
	m.objectYAML = false
	m.rebuildCommands()
	return m.loadObject()
}

// objectRef reports what the object view is showing.
func (m *Model) objectRef() objectRef { return m.objectTarget }

// backFromObject walks one step back: to the object it was opened from, or out
// to the application it belongs to.
func (m *Model) backFromObject() tea.Cmd {
	if len(m.objectTrail) > 0 {
		previous := m.objectTrail[len(m.objectTrail)-1]
		m.objectTrail = m.objectTrail[:len(m.objectTrail)-1]
		return m.showObject(previous)
	}
	if m.cancelObject != nil {
		m.cancelObject()
	}
	m.object.Reset()
	if m.selectedApp != "" {
		m.view = viewApplication
		m.rebuildCommands()
		return nil
	}
	return m.backToApplications()
}

// toggleObjectYAML switches between what kubeui knows about an object and what
// the server actually holds.
func (m *Model) toggleObjectYAML() {
	m.objectYAML = !m.objectYAML
	m.objectOffset = 0
	m.rebuildCommands()
}

// objectData assembles the object view.
func (m *Model) objectData() screens.ObjectData {
	d, _ := m.objectView()
	return d
}

// objectView assembles the object view and the objects its rows point at.
func (m *Model) objectView() (screens.ObjectData, []objectRef) {
	ref := m.objectTarget
	d := screens.ObjectData{
		Kind:      ref.Kind,
		Name:      ref.Name,
		Namespace: ref.Namespace,
		Offset:    m.objectOffset,
		Selected:  m.objectCursor,
		ShowYAML:  m.objectYAML,
	}

	switch m.object.State() {
	case async.Idle, async.Loading:
		d.Message = "Loading " + ref.label() + "…"
		return d, nil
	case async.Failed:
		d.Message = "Could not read " + ref.label() + ": " + shortError(m.object.Err())
		d.MessageStatus = theme.StatusCritical
		return d, nil
	}

	obj := m.object.Get()
	if obj == nil {
		d.Message = "No data."
		return d, nil
	}

	now := time.Now()
	d.Kind = orNone(obj.Kind)
	d.Name = obj.Name
	d.Namespace = obj.Namespace
	d.Subtitle = strings.TrimSpace(groupVersionOf(obj) + "   age " + formatAge(obj.CreatedAt, now))
	d.YAML = strings.Split(strings.TrimRight(obj.YAML, "\n"), "\n")

	if pod, ok := m.podFor(obj); ok {
		d.Status = podStatus(pod)
		d.Glyph = m.theme.Glyph(d.Status)
		d.Headline = podHeadline(pod)
	}

	var targets []objectRef
	target := func(r objectRef) int {
		targets = append(targets, r)
		return len(targets) - 1
	}

	d.Sections = []screens.DetailSection{
		m.identitySection(obj),
		m.relationsSection(obj, target),
		m.objectEventsSection(obj, now),
	}
	return d, targets
}

// identitySection is what identifies the object, without repeating the YAML
// underneath it.
func (m *Model) identitySection(obj *resources.Object) screens.DetailSection {
	section := screens.DetailSection{Title: "Identity", Columns: []string{"Field", "Value"}}
	rows := [][2]string{
		{"Name", obj.Name},
		{"Namespace", orNone(obj.Namespace)},
		{"API", groupVersionOf(obj)},
		{"Created", obj.CreatedAt.Format(time.RFC3339)},
		{"Version", orNone(obj.ResourceVersion)},
	}
	for _, r := range rows {
		section.Rows = append(section.Rows, screens.DetailRow{Cells: []string{r[0], r[1]}, Target: -1})
	}
	for _, key := range sortedLabelKeys(obj.Labels) {
		section.Rows = append(section.Rows, screens.DetailRow{
			Cells: []string{"Label " + key, obj.Labels[key]}, Target: -1,
		})
	}
	return section
}

// relationsSection is the point of the whole screen: where to go from here.
// Up to the controller that made this object, down to the objects it made.
func (m *Model) relationsSection(obj *resources.Object, target func(objectRef) int) screens.DetailSection {
	section := screens.DetailSection{
		Title:   "Related",
		Columns: []string{"Direction", "Kind", "Name", "Detail"},
		Empty:   "nothing in this scope points at it",
	}

	for _, owner := range obj.Owners {
		direction := "owner"
		if owner.Controller {
			direction = "controller"
		}
		ref := objectRef{Kind: owner.Kind, Name: owner.Name, Namespace: obj.Namespace}
		section.Rows = append(section.Rows, screens.DetailRow{
			Cells:  []string{direction, owner.Kind, owner.Name, "made this object"},
			Target: target(ref),
		})
	}

	for _, child := range m.childrenOf(obj) {
		section.Rows = append(section.Rows, screens.DetailRow{
			Cells:  []string{"owns", child.ref.Kind, child.ref.Name, child.detail},
			Status: child.status,
			Target: target(child.ref),
		})
	}
	return section
}

// child is one object owned by the object on screen.
type child struct {
	ref    objectRef
	detail string
	status theme.Status
}

// childrenOf finds what this object owns, from the snapshot the dashboard
// already holds. It is deliberately limited to the loaded scope: kubeui does
// not go looking through the cluster to answer a question nobody asked.
func (m *Model) childrenOf(obj *resources.Object) []child {
	if obj.UID == "" {
		return nil
	}
	snapshot := m.apps.Get().Snapshot
	out := make([]child, 0, len(snapshot.Pods))

	for i := range snapshot.Owners {
		if controlledBy(snapshot.Owners[i].Owners, obj.UID) {
			out = append(out, child{
				ref:    objectRef{Kind: snapshot.Owners[i].Kind, Name: snapshot.Owners[i].Name, Namespace: snapshot.Owners[i].Namespace},
				detail: "intermediate controller",
			})
		}
	}
	for i := range snapshot.Workloads {
		w := &snapshot.Workloads[i]
		if !controlledBy(w.Owners, obj.UID) {
			continue
		}
		out = append(out, child{
			ref:    objectRef{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace},
			detail: itoa(int(w.Ready)) + "/" + itoa(int(w.Desired)) + " ready",
		})
	}
	for i := range snapshot.Pods {
		p := &snapshot.Pods[i]
		if !controlledBy(p.Owners, obj.UID) {
			continue
		}
		c := child{
			ref:    objectRef{Kind: "Pod", Name: p.Name, Namespace: p.Namespace},
			detail: p.Phase,
			status: podStatus(p),
		}
		if p.Reason != "" {
			c.detail += ", " + p.Reason
		}
		out = append(out, c)
	}
	return out
}

func controlledBy(owners []application.OwnerRef, uid string) bool {
	for _, o := range owners {
		if o.UID == uid {
			return true
		}
	}
	return false
}

// objectEventsSection shows what the cluster said about this object.
func (m *Model) objectEventsSection(obj *resources.Object, now time.Time) screens.DetailSection {
	section := screens.DetailSection{
		Title:   "Recent events",
		Columns: []string{"Age", "Type", "Reason", "Message"},
	}
	switch m.evidence.State() {
	case async.Idle:
		section.Empty = "not read yet"
		return section
	case async.Loading:
		section.Empty = "loading…"
		return section
	case async.Failed:
		section.Empty = "unavailable — " + shortError(m.evidence.Err())
		return section
	}

	events := m.evidence.Get().EventsAbout(obj.UID, obj.Name)
	if len(events) == 0 {
		section.Empty = "none"
		return section
	}
	if len(events) > maxDetailEvents {
		events = events[:maxDetailEvents]
	}
	for _, e := range events {
		row := screens.DetailRow{
			Cells:  []string{formatAge(e.LastSeen, now), e.Type, e.Reason, e.Message},
			Target: -1,
		}
		if e.Type == "Warning" {
			row.Status = theme.StatusWarning
		}
		section.Rows = append(section.Rows, row)
	}
	return section
}

// podFor finds the pod an object refers to in the loaded snapshot, so the
// object view can lead with its state.
func (m *Model) podFor(obj *resources.Object) (*application.Pod, bool) {
	if obj.Kind != "Pod" {
		return nil, false
	}
	pods := m.apps.Get().Snapshot.Pods
	for i := range pods {
		if pods[i].Name == obj.Name && pods[i].Namespace == obj.Namespace {
			return &pods[i], true
		}
	}
	return nil, false
}

func podHeadline(p *application.Pod) string {
	headline := p.Phase
	if p.Reason != "" {
		headline += ", " + p.Reason
	}
	if p.Ready {
		return headline + ", ready"
	}
	return headline + ", not ready"
}

func podStatus(p *application.Pod) theme.Status {
	switch {
	case p.Terminal():
		return theme.StatusUnknown
	case p.Reason != "":
		return theme.StatusCritical
	case !p.Ready:
		return theme.StatusWarning
	default:
		return theme.StatusHealthy
	}
}

// groupVersionOf renders the API version an object came from.
func groupVersionOf(obj *resources.Object) string {
	gvr := obj.Target.GVR
	if gvr.Group == "" {
		return gvr.Version
	}
	return gvr.Group + "/" + gvr.Version
}

func sortedLabelKeys(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for k := range labels {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

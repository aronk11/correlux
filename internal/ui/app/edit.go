package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/kubeui/internal/domain/diff"
	"github.com/aronk11/kubeui/internal/kube/resources"
	"github.com/aronk11/kubeui/internal/ui/theme"
)

// editFinishedMsg reports that the editor has exited.
type editFinishedMsg struct {
	path string
	err  error
}

// editedMsg reports the outcome of applying an edit.
type editedMsg struct {
	ref    objectRef
	object *resources.Object
	err    error
}

// editObject opens the object's document in the user's editor.
//
// kubeui does not implement an editor. People have one, they have configured
// it, and the worst possible time to learn somebody else's keybindings is while
// changing a production object — so the terminal is handed over to $EDITOR and
// taken back when it exits, exactly as `kubectl edit` does.
func (m *Model) editObject(ref objectRef) tea.Cmd {
	if ref.empty() {
		m.notice("Select an object to edit", theme.StatusWarning)
		return m.expireNotice()
	}
	obj := m.object.Get()
	if m.view != viewObject || obj == nil || m.objectTarget != ref {
		m.notice("Open "+ref.label()+" first, then edit it", theme.StatusWarning)
		return m.expireNotice()
	}
	if _, ok := m.resourceFor(ref.Kind); !ok {
		m.notice("This cluster does not serve "+ref.Kind, theme.StatusWarning)
		return m.expireNotice()
	}

	path, err := writeEditBuffer(obj)
	if err != nil {
		m.notice("Could not prepare the edit: "+err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}
	m.editPath = path
	m.editOriginal = obj.YAML

	editor, args := editorCommand()
	// Background on purpose: an editor session belongs to the person typing in
	// it, and cancelling it from underneath them — on a timeout, or when a
	// request elsewhere is abandoned — would leave both the terminal and their
	// unsaved work in a state nobody asked for.
	cmd := exec.CommandContext(context.Background(), editor, append(args, path)...) //nolint:gosec // the user's own configured editor
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editFinishedMsg{path: path, err: err}
	})
}

// applyEditedBuffer reads back what the editor wrote and asks for consent
// before any of it is sent.
func (m *Model) applyEditedBuffer(msg editFinishedMsg) tea.Cmd {
	defer func() { m.editPath = "" }()

	if msg.err != nil {
		m.discardEditBuffer(msg.path)
		m.notice("The editor exited with an error: "+msg.err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}

	edited, err := os.ReadFile(msg.path) //nolint:gosec // the path kubeui just wrote
	if err != nil {
		m.notice("Could not read the edited file: "+err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}

	before := splitDocument(m.editOriginal)
	after := splitDocument(string(edited))
	changes := diff.Lines(before, after)
	summary := diff.Summarise(changes)
	if !summary.Changed() {
		m.discardEditBuffer(msg.path)
		m.notice("Nothing changed", theme.StatusUnknown)
		return m.expireNotice()
	}

	ref := m.objectTarget
	if err := m.checkIdentity(edited, ref); err != nil {
		m.discardEditBuffer(msg.path)
		m.notice(err.Error(), theme.StatusCritical)
		return m.expireNotice()
	}

	document := append([]byte(nil), edited...)
	m.discardEditBuffer(msg.path)

	return m.confirm(pendingAction{
		Title: "Apply your changes to " + ref.label(),
		Lines: []string{
			changeSummary(summary),
			ref.label() + " in " + orNone(ref.Namespace),
		},
		Diff:      diff.Hunks(changes, 1),
		Challenge: m.productionChallenge(),
		Danger:    summary.Removed > 0,
		Run:       func(m *Model) tea.Cmd { return m.applyEdit(ref, document) },
	})
}

// The edits kubeui refuses to send.
var (
	errChangedKind      = errors.New("the edit changed the object's kind; apply it with kubectl instead")
	errChangedName      = errors.New("the edit renamed the object; that creates a new one rather than changing this")
	errChangedNamespace = errors.New("the edit moved the object to another namespace; that is not an edit")
)

// checkIdentity refuses an edit that renamed the object. Applying it would
// either create a second object or fail confusingly, and neither is what the
// user meant by editing this one.
func (m *Model) checkIdentity(document []byte, ref objectRef) error {
	kind, name, namespace, err := resources.Identity(document)
	if err != nil {
		return err
	}
	switch {
	case kind != "" && !strings.EqualFold(kind, ref.Kind):
		return errChangedKind
	case name != "" && name != ref.Name:
		return errChangedName
	case namespace != "" && ref.Namespace != "" && namespace != ref.Namespace:
		return errChangedNamespace
	}
	return nil
}

// applyEdit sends the edited document.
func (m *Model) applyEdit(ref objectRef, document []byte) tea.Cmd {
	res, ok := m.resourceFor(ref.Kind)
	if !ok {
		return nil
	}
	factory := m.factory
	name := m.contextName
	namespace := ref.Namespace
	if !res.Namespaced {
		namespace = ""
	}

	m.notice("Applying changes to "+ref.label()+"…", theme.StatusUnknown)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), factory.Timeout())
		defer cancel()
		obj, err := factory.UpdateObject(ctx, name, res, namespace, ref.Name, document)
		return editedMsg{ref: ref, object: obj, err: err}
	}
}

// applyEdited reports the outcome and refreshes what is on screen.
func (m *Model) applyEdited(msg editedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice("Could not apply the changes: "+shortError(msg.err), theme.StatusCritical)
		return m.expireNotice()
	}
	m.notice("Applied changes to "+msg.ref.label(), theme.StatusHealthy)
	if m.view == viewObject && m.objectTarget == msg.ref && msg.object != nil {
		m.object.Succeed(m.object.Generation(), msg.object)
	}
	return tea.Batch(m.loadApplications(), m.expireNotice())
}

// writeEditBuffer puts the document somewhere the editor can open it. The name
// carries the object it belongs to, because an editor's tab bar is where people
// look to see what they are changing.
func writeEditBuffer(obj *resources.Object) (string, error) {
	name := strings.ToLower(obj.Kind) + "-" + obj.Name
	file, err := os.CreateTemp("", "kubeui-"+sanitiseFileName(name)+"-*.yaml")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(obj.YAML); err != nil {
		return "", err
	}
	return file.Name(), nil
}

// discardEditBuffer removes the temporary file. A Kubernetes object routinely
// carries secrets, and leaving one in the temp directory is not something a
// tool should do quietly.
func (m *Model) discardEditBuffer(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// editorCommand resolves the editor to hand the terminal to, honouring the same
// variables kubectl does.
func editorCommand() (string, []string) {
	for _, key := range []string{"KUBE_EDITOR", "EDITOR", "VISUAL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		// An editor variable may carry arguments: "code --wait", "vim -f".
		fields := strings.Fields(value)
		return fields[0], fields[1:]
	}
	if runtime.GOOS == "windows" {
		return "notepad", nil
	}
	return "vi", nil
}

// editorName is what the palette entry promises to open.
func editorName() string {
	editor, _ := editorCommand()
	return editor
}

// sanitiseFileName keeps an object's name usable as part of a file name.
func sanitiseFileName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, filepath.Base(name))
}

func splitDocument(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// changeSummary renders what an edit does to the document, in lines.
func changeSummary(s diff.Summary) string {
	switch {
	case s.Added > 0 && s.Removed > 0:
		return "This replaces " + lineCount(s.Removed) + " with " + lineCount(s.Added) + "."
	case s.Added > 0:
		return "This adds " + lineCount(s.Added) + "."
	default:
		return "This removes " + lineCount(s.Removed) + "."
	}
}

func lineCount(n int) string {
	if n == 1 {
		return "1 line"
	}
	return itoa(n) + " lines"
}

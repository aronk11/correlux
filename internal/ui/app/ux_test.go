package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/palette"
)

// revisedApplication is the broken application with a rollout behind it: its
// pods belong to revision 14, and revision 13 is still serving next to them.
func revisedApplication() (application.Application, application.Context) {
	a := brokenApplication()
	a.Workloads[0].UID = "dep"
	for i := range a.Pods {
		a.Pods[i].Owners = []application.OwnerRef{{Kind: "ReplicaSet", Name: "payments-7d8f", UID: "new", Controller: true}}
	}
	own := []application.OwnerRef{{Kind: "Deployment", UID: "dep", Controller: true}}
	evidence := dumpEvidence()
	evidence.Revisions = []application.Revision{
		{Meta: application.Meta{Name: "payments-5c4b", UID: "old", Owners: own, CreatedAt: time.Now().Add(-72 * time.Hour)},
			Number: 13, Desired: 3, Ready: 3, Template: map[string]string{"container payments image": "payments:1.8"}},
		{Meta: application.Meta{Name: "payments-7d8f", UID: "new", Owners: own, CreatedAt: time.Now().Add(-4 * time.Minute)},
			Number: 14, Desired: 3, Template: map[string]string{"container payments image": "payments:1.9"}},
	}
	return a, evidence
}

func TestTheHelpWrapsInsteadOfCuttingSentencesOff(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	press(t, m, "?")
	width, _ := m.overlayInnerSize()
	for _, line := range strings.Split(m.helpTextAt(width), "\n") {
		if w := len([]rune(ansi.ReplaceAllString(line, ""))); w > width {
			t.Errorf("a help line is %d wide in a %d-wide overlay: %q", w, width, line)
		}
	}
	if !strings.Contains(ansi.ReplaceAllString(m.helpTextAt(width), ""), "after showing what") {
		t.Error("the end of a long description must still be there, on the next line")
	}
}

func TestTheHelpSaysWhenChangesAreRefused(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.Config.Safety.ReadOnly = true })
	if !strings.Contains(m.helpText(), "refused here: this session is read-only") {
		t.Error("the help is where somebody looks to learn why a key did nothing")
	}
}

func TestTheDashboardDetailSaysWhatIsWrongRatherThanRepeatingThePodCount(t *testing.T) {
	m := newTestModel(t)
	a := brokenApplication()
	a.Problems = nil // no pod state summary, so the finding has to speak
	loadApplicationsInto(m, a)
	out := plainView(m)
	if !strings.Contains(out, "3 pods restart in a loop") {
		t.Errorf("the detail column must carry the leading finding:\n%s", out)
	}
}

func TestWhyListsEveryFindingFirstAndFoldsIdenticalEvidence(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	a, evidence := revisedApplication()
	loadApplicationsInto(m, a)
	m.openApplication("payments")
	loadEvidenceInto(m, evidence)
	m.explain()

	out := plainView(m)
	findings := strings.Index(out, "FINDINGS")
	revision := strings.Index(out, "revision 14 of Deployment/payments is failing")
	// The first finding's own heading is its second mention: the list names it first.
	first := strings.Index(out, "3 pods restart in a loop")
	detail := first + 1 + strings.Index(out[first+1:], "3 pods restart in a loop")
	if findings < 0 || revision < 0 || revision > detail {
		t.Errorf("the list of findings must lead, naming the rollout before the first finding's detail:\n%s", out)
	}
	if !strings.Contains(out, "3 Pods: payments-7d8f-0, payments-7d8f-1, payments-7d8f-2") {
		t.Errorf("one fact about three pods is one entry:\n%s", out)
	}
	if !strings.Contains(out, "[U] roll Deployment/payments back") {
		t.Errorf("a finding that blames a rollout must offer the counter-move:\n%s", out)
	}

	if cmd := press(t, m, "U"); cmd == nil || m.rollbackGen == 0 {
		t.Error("U on the explanation must read the revisions of the Deployment it blames")
	}
}

func TestWhyDoesNotOfferARollbackWhereChangesAreRefused(t *testing.T) {
	m := newTestModel(t, func(o *Options) { o.Config.Safety.ReadOnly = true })
	a, evidence := revisedApplication()
	loadApplicationsInto(m, a)
	m.openApplication("payments")
	loadEvidenceInto(m, evidence)
	m.explain()
	if strings.Contains(plainView(m), "[U]") {
		t.Error("a read-only session must not advertise a rollback")
	}
}

func TestFleetCommandsDoNotBorrowKeysThatDoSomethingElseHere(t *testing.T) {
	m := newTestModel(t)
	for _, c := range m.registry.Commands() {
		if c.Action == paletteFleetNamespaces && c.Shortcut != "" {
			t.Errorf("outside the fleet %s switches this cluster's namespace; the entry must not claim it", c.Shortcut)
		}
	}
	press(t, m, "F")
	var scoped *palette.Command
	for _, c := range m.registry.Commands() {
		if c.Action == paletteFleetNamespaces {
			scoped = &c
		}
	}
	if scoped == nil || scoped.Shortcut == "" {
		t.Error("in the fleet, the key does scope it, and the entry says so")
	}
}

func TestTheSessionScreenSaysWhetherChangesAreAllowed(t *testing.T) {
	m := newTestModel(t, func(o *Options) {
		o.ContextName = "prod-eu"
		o.Config.Safety.ReadOnlyProduction = true
	})
	m.backToOverview()
	if !strings.Contains(plainView(m), "refused: production contexts are") {
		t.Errorf("the session screen must say changes are refused, and why:\n%s", plainView(m))
	}
}

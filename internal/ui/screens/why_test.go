package screens

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/ui/theme"
)

func oomFinding() WhyFinding {
	return WhyFinding{
		Glyph:    "✖",
		Status:   theme.StatusCritical,
		Severity: "critical",
		Problem:  "3 pods restart in a loop",
		Cause:    "the container exceeded its memory limit and was killed for it",
		Chain:    []string{"Deployment/payments", "Pods", "CrashLoopBackOff", "OOMKilled"},
		Evidence: []WhyEvidence{
			{Source: "Pod/payments-1", Detail: "container payments last terminated with reason OOMKilled, exit code 137"},
		},
		Confidence: "high",
	}
}

func TestAnEmptyUnknownRendersNothing(t *testing.T) {
	f := oomFinding()
	f.Unknown = ""
	d := WhyData{Name: "payments", Findings: []WhyFinding{f}}

	out := RenderWhy(testTheme(), d, 100, 40)
	if strings.Contains(out, "UNKNOWN") {
		t.Errorf("an empty Unknown must render as nothing, not a heading:\n%s", out)
	}
}

func TestAFilledUnknownRendersItsOwnSection(t *testing.T) {
	f := oomFinding()
	f.Unknown = "Kubernetes does not report why memory use grew."
	d := WhyData{Name: "payments", Findings: []WhyFinding{f}}

	out := RenderWhy(testTheme(), d, 100, 40)
	if !strings.Contains(out, "UNKNOWN") {
		t.Errorf("a filled Unknown must render its own section:\n%s", out)
	}
	if !strings.Contains(out, "does not report why memory use grew") {
		t.Errorf("the unknown text itself must appear:\n%s", out)
	}
}

func TestCauseAndEvidenceAreLabelledSeparately(t *testing.T) {
	out := RenderWhy(testTheme(), WhyData{Name: "payments", Findings: []WhyFinding{oomFinding()}}, 100, 40)

	if !strings.Contains(out, "CAUSE") {
		t.Errorf("the reading of the evidence must be under its own heading:\n%s", out)
	}
	if strings.Contains(out, "WHY") {
		t.Errorf("the old unlabelled heading must be gone, got:\n%s", out)
	}
	causeAt := strings.Index(out, "CAUSE")
	evidenceAt := strings.Index(out, "EVIDENCE")
	if causeAt < 0 || evidenceAt < 0 || causeAt > evidenceAt {
		t.Errorf("cause must be read before the evidence that backs it:\n%s", out)
	}
}

func TestRelatedListsTheObjectsAFindingTouchesOnce(t *testing.T) {
	f := oomFinding()
	f.Evidence = append(f.Evidence,
		WhyEvidence{Source: "Pod/payments-1", Detail: "duplicate of the chain's own subject"},
		WhyEvidence{Source: "Event/payments-1", Detail: "BackOff: back-off restarting failed container"},
	)
	out := RenderWhy(testTheme(), WhyData{Name: "payments", Findings: []WhyFinding{f}}, 100, 40)

	at := strings.Index(out, "RELATED")
	if at < 0 {
		t.Fatalf("the objects a finding touches must be listed:\n%s", out)
	}
	related := out[at:]
	if end := strings.Index(related, "confidence:"); end >= 0 {
		related = related[:end]
	}

	if !strings.Contains(related, "Deployment/payments") || !strings.Contains(related, "Pod/payments-1") {
		t.Errorf("the workload and the subject must both be listed:\n%s", related)
	}
	if strings.Contains(related, "Event/payments-1") {
		t.Errorf("an event is a record about an object, not an object of its own:\n%s", related)
	}
	if strings.Count(related, "Pod/payments-1") != 1 {
		t.Errorf("the same object must be listed once even when several facts mention it:\n%s", related)
	}
}

func TestNextRendersOnlyTheActionsGiven(t *testing.T) {
	withNext := WhyData{
		Name:     "payments",
		Findings: []WhyFinding{oomFinding()},
		Next:     []WhyAction{{Key: "l", Text: "read the previous run's logs"}},
	}
	out := RenderWhy(testTheme(), withNext, 100, 40)
	if !strings.Contains(out, "NEXT") || !strings.Contains(out, "[l] read the previous run's logs") {
		t.Errorf("the real key and what it does must both appear:\n%s", out)
	}

	withoutNext := WhyData{Name: "payments", Findings: []WhyFinding{oomFinding()}}
	out = RenderWhy(testTheme(), withoutNext, 100, 40)
	if strings.Contains(out, "NEXT") {
		t.Errorf("no actions to offer must mean no NEXT section, not an empty one:\n%s", out)
	}
}

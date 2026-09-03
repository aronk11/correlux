package app

import (
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/domain/application"
)

func TestRecentActivityIsNewestFirstAndNamesItsLimits(t *testing.T) {
	m := newTestModel(t)
	press(t, m, "E")
	now := time.Now()
	loadEvidenceInto(m, application.Context{Events: []application.Event{
		{Meta: application.Meta{Namespace: "shop"}, Type: "Normal", Reason: "Scheduled", Message: "older", About: application.ObjectRef{Kind: "Pod", Name: "payments-0"}, LastSeen: now.Add(-time.Minute)},
		{Meta: application.Meta{Namespace: "shop"}, Type: "Warning", Reason: "BackOff", Message: "newer", About: application.ObjectRef{Kind: "Pod", Name: "payments-0"}, LastSeen: now},
	}})

	out := plainView(m)
	if !strings.Contains(out, "Recent activity") || !strings.Contains(out, "not an audit log") {
		t.Fatalf("activity must identify itself and its limits:\n%s", out)
	}
	if strings.Index(out, "newer") > strings.Index(out, "older") {
		t.Errorf("newest event must appear first:\n%s", out)
	}
}

func TestActivityEventOpensTheObjectItNames(t *testing.T) {
	m := newTestModel(t)
	loadCatalogInto(m, testCatalog())
	press(t, m, "E")
	loadEvidenceInto(m, application.Context{Events: []application.Event{{
		Meta: application.Meta{Namespace: "default"}, Type: "Warning", Reason: "BackOff",
		About: application.ObjectRef{Kind: "Pod", Name: "payments-0"}, LastSeen: time.Now(),
	}}})
	press(t, m, "enter")
	if m.view != viewObject || m.objectTarget.Kind != "Pod" || m.objectTarget.Name != "payments-0" {
		t.Fatalf("opened %+v in view %v", m.objectTarget, m.view)
	}
	press(t, m, "esc")
	if m.view != viewActivity {
		t.Errorf("Esc must return to the activity timeline, got %v", m.view)
	}
}

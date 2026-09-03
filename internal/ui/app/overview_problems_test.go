package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
	"github.com/aronk11/correlux/internal/ui/theme"
)

func TestOverviewAggregatesApplicationsNodesAndGenericWarningEvents(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, brokenApplication())
	loadEvidenceInto(m, application.Context{
		Nodes: []application.Node{{
			Meta:  application.Meta{Kind: "Node", Name: "worker-1"},
			Ready: false,
		}},
		Events: []application.Event{{
			Type: "Warning", Reason: "ReconcileDelayed", Message: "issuer refused the request",
			About: application.ObjectRef{Kind: "Certificate", Name: "api-tls"},
		}},
	})
	m.view = viewOverview

	out := plainView(m)
	for _, want := range []string{
		"Cluster problems",
		"2 critical, 1 warning",
		"Node/worker-1",
		"not ready",
		"payments",
		"restart in a loop",
		"Certificate/api-tls",
		"issuer refused",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("problem overview must contain %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "default/payments") > strings.Index(out, "Certificate/api-tls") {
		t.Errorf("critical findings must sort before warnings:\n%s", out)
	}
}

func TestExplicitFailureReasonsAreCriticalEvenOnNormalEvents(t *testing.T) {
	status, problem := problemEventStatus(&application.Event{Type: "Normal", Reason: "IssuerNotFound"})
	if !problem || status != theme.StatusCritical {
		t.Errorf("IssuerNotFound = %v/%v, want critical problem", status, problem)
	}
}

func TestOverviewSaysWhenTheBoundedScanFoundNothing(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("api", application.Healthy, 1, 1))
	loadEvidenceInto(m, application.Context{})
	m.view = viewOverview

	if out := plainView(m); !strings.Contains(out, "no known problems") {
		t.Errorf("an empty result must be explicit:\n%s", out)
	}
}

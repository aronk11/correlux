package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
	kubeclient "github.com/aronk11/correlux/internal/kube/client"
)

func roleApp(name string, health application.Health, role, team string) application.Application {
	a := testApplication(name, health, 1, 1)
	a.Traits.Role = application.Role{Name: role, By: "test"}
	a.Traits.Team = team
	return a
}

func TestGroupingKeepsLabelMatesTogetherAndTheWorstGroupFirst(t *testing.T) {
	apps := []application.Application{
		roleApp("ingress-nginx", application.Down, "ingress", ""),
		roleApp("shop", application.Degraded, "", "web"),
		roleApp("traefik", application.Healthy, "ingress", ""),
		roleApp("cart", application.Healthy, "", "web"),
	}
	got := grouped(apps, groupRole)
	order := []string{}
	for i := range got {
		order = append(order, got[i].Name)
	}
	if strings.Join(order, " ") != "ingress-nginx traefik shop cart" {
		t.Errorf("order = %v", order)
	}
	if !groupRole.useful(apps) || !groupTeam.useful(apps) {
		t.Error("role and team both tell these applications apart")
	}
	if groupSystem.useful(apps) || groupNamespace.useful(apps) {
		t.Error("nothing here has a system label or a second namespace: those groupings say nothing")
	}
}

func TestTheGroupKeyCyclesOnlyThroughGroupingsThatSaySomething(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		roleApp("ingress-nginx", application.Down, "ingress", ""),
		roleApp("traefik", application.Healthy, "ingress", ""),
		roleApp("shop", application.Healthy, "", ""),
	)

	m.cycleGrouping()
	if m.appGroup != groupRole {
		t.Fatalf("one namespace and no teams: the first useful grouping is role, got %v", m.appGroup)
	}
	out := view(m)
	for _, want := range []string{"ROLE", "ingress (2)", "applications (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the grouped dashboard must show %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "ingress (2)") != 1 {
		t.Errorf("a group is named once, not on every row:\n%s", out)
	}

	m.cycleGrouping()
	if m.appGroup != groupNone {
		t.Errorf("after the last useful grouping comes none, got %v", m.appGroup)
	}
}

func TestLabelColumnsAppearOnlyWhereSomebodySetTheLabel(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m, testApplication("api", application.Healthy, 1, 1))
	d := m.applicationsData()
	for _, c := range d.Columns {
		if c.Title == "Team" || c.Title == "Role" || c.Title == "System" {
			t.Errorf("no application has a %s, so there is no such column", c.Title)
		}
	}

	loadApplicationsInto(m, roleApp("api", application.Healthy, "", "payments"))
	found := false
	for _, c := range m.applicationsData().Columns {
		found = found || c.Title == "Team"
	}
	if !found {
		t.Error("a team label on one application earns the column")
	}
}

func TestLabelColumnsCanBeFilteredOn(t *testing.T) {
	m := newTestModel(t)
	loadApplicationsInto(m,
		roleApp("ingress-nginx", application.Healthy, "ingress", "platform"),
		roleApp("shop", application.Healthy, "", "web"),
	)
	m.search.SetValue("role=ingress")
	if got := m.visibleApplications(); len(got) != 1 || got[0].Name != "ingress-nginx" {
		t.Errorf("role=ingress matched %d applications", len(got))
	}
	m.search.SetValue("team=web")
	if got := m.visibleApplications(); len(got) != 1 || got[0].Name != "shop" {
		t.Errorf("team=web matched %d applications", len(got))
	}
}

func TestTheSessionScreenSaysWhatTheAccountMayDo(t *testing.T) {
	m := newTestModel(t)
	m.backToOverview()
	if out := view(m); !strings.Contains(out, "Asking the API server") {
		t.Errorf("while the answer is outstanding the panel says so:\n%s", out)
	}
	m.Update(permissionsLoadedMsg{gen: m.permissions.Generation(), permissions: []kubeclient.Permission{
		{Label: "read logs", Allowed: true, Known: true},
		{Label: "open a shell", Known: true},
		{Label: "read secrets"},
	}})
	out := view(m)
	for _, want := range []string{"Allowed", "read logs", "Refused", "open a shell", "No answer", "read secrets"} {
		if !strings.Contains(out, want) {
			t.Errorf("the panel must show %q:\n%s", want, out)
		}
	}
}

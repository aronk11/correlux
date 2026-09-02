package fleet

import (
	"errors"
	"testing"

	"github.com/aronk11/kubeui/internal/domain/application"
)

func app(name string, health application.Health, ready, desired int32) application.Application {
	return application.Application{
		Name: name, Namespace: "shop", Health: health,
		ReadyPods: ready, DesiredPods: desired,
		Summary: "the summary",
	}
}

func member(context string, production bool, apps ...application.Application) Member {
	return Member{Context: context, Production: production, State: Ready, Applications: apps}
}

func names(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestAnApplicationInSeveralClustersIsOneRow(t *testing.T) {
	members := []Member{
		member("prod-eu", true, app("payments", application.Healthy, 3, 3)),
		member("prod-us", true, app("payments", application.Down, 0, 3)),
		member("staging", false, app("payments", application.Degraded, 2, 3)),
	}

	rows := Rows(members)
	if len(rows) != 1 {
		t.Fatalf("the same application in three clusters is one row, got %v", names(rows))
	}
	if len(rows[0].Instances) != 3 {
		t.Fatalf("instances = %d, want three", len(rows[0].Instances))
	}
	if rows[0].Worst != application.Down {
		t.Errorf("the row carries its unhappiest instance, got %v", rows[0].Worst)
	}
	// The broken cluster leads; production comes before the rest at equal health.
	if rows[0].Instances[0].Context != "prod-us" {
		t.Errorf("instances = %v, want the broken cluster first", contexts(rows[0].Instances))
	}
}

func contexts(instances []Instance) []string {
	out := make([]string, 0, len(instances))
	for _, i := range instances {
		out = append(out, i.Context)
	}
	return out
}

func TestProductionComesFirstAmongEquals(t *testing.T) {
	rows := Rows([]Member{
		member("dev", false, app("api", application.Healthy, 1, 1)),
		member("prod-eu", true, app("api", application.Healthy, 1, 1)),
		member("staging", false, app("api", application.Healthy, 1, 1)),
	})

	if got := contexts(rows[0].Instances); got[0] != "prod-eu" {
		t.Errorf("instances = %v, want production first", got)
	}
}

func TestTheWorstApplicationLeadsTheFleet(t *testing.T) {
	rows := Rows([]Member{
		member("prod-eu", true,
			app("api", application.Healthy, 2, 2),
			app("payments", application.Down, 0, 3),
			app("worker", application.Degraded, 7, 8),
		),
	})

	want := []string{"payments", "worker", "api"}
	for i, name := range want {
		if rows[i].Name != name {
			t.Fatalf("rows = %v, want worst first %v", names(rows), want)
		}
	}
}

func TestAClusterThatCouldNotBeReadContributesNothingButIsCounted(t *testing.T) {
	members := []Member{
		member("prod-eu", true, app("payments", application.Healthy, 3, 3)),
		{Context: "prod-us", Production: true, State: Failed, Err: errors.New("i/o timeout")},
		{Context: "staging", State: Loading},
	}

	rows := Rows(members)
	if len(rows) != 1 || len(rows[0].Instances) != 1 {
		t.Fatalf("only the cluster that answered contributes, got %v", names(rows))
	}

	summary := Summarise(members)
	if summary.Clusters != 3 || summary.Answered != 1 || summary.Failed != 1 || summary.Pending != 1 {
		t.Errorf("summary = %+v, want every cluster accounted for", summary)
	}
	if summary.Complete() {
		t.Error("a fleet with an unreachable cluster is not a complete picture, and must not claim to be")
	}
}

func TestACompleteFleetSaysSo(t *testing.T) {
	summary := Summarise([]Member{
		member("prod-eu", true, app("payments", application.Healthy, 3, 3)),
		member("staging", false, app("payments", application.Degraded, 2, 3)),
	})

	if !summary.Complete() {
		t.Error("every cluster answered; the totals cover the fleet")
	}
	if summary.Counts.Total != 2 || summary.Unhealthy != 1 {
		t.Errorf("summary = %+v, want two applications with one unhealthy", summary)
	}
}

func TestWhereAnApplicationIsMissing(t *testing.T) {
	members := []Member{
		member("prod-eu", true, app("payments", application.Healthy, 3, 3)),
		member("prod-us", true, app("api", application.Healthy, 1, 1)),
		{Context: "prod-ap", State: Failed, Err: errors.New("unreachable")},
	}

	rows := Rows(members)
	var payments Row
	for _, r := range rows {
		if r.Name == "payments" {
			payments = r
		}
	}

	missing := payments.Missing(members)
	if len(missing) != 1 || missing[0] != "prod-us" {
		t.Errorf("missing = %v, want the cluster that answered without it", missing)
	}
	// A cluster that failed is not evidence of absence.
	for _, m := range missing {
		if m == "prod-ap" {
			t.Error("an unreachable cluster cannot be said to be missing an application")
		}
	}
}

func TestAMemberIsHealthyOnlyWhenItAnsweredAndNothingIsBroken(t *testing.T) {
	cases := []struct {
		name string
		in   Member
		want bool
	}{
		{"answered, all well", member("dev", false, app("api", application.Healthy, 1, 1)), true},
		{"answered, one down", member("dev", false, app("api", application.Down, 0, 1)), false},
		{"never asked", Member{Context: "dev"}, false},
		{"failed", Member{Context: "dev", State: Failed, Err: errors.New("nope")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Healthy(); got != tc.want {
				t.Errorf("healthy = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnEmptyFleetIsNotAnError(t *testing.T) {
	if rows := Rows(nil); len(rows) != 0 {
		t.Errorf("rows = %v, want none", names(rows))
	}
	summary := Summarise(nil)
	if summary.Clusters != 0 || !summary.Complete() {
		t.Errorf("summary = %+v", summary)
	}
}

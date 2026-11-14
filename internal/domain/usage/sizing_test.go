package usage

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/application"
)

func sized(pods, measured int, used, requests, limits application.Amounts) *AppUsage {
	return &AppUsage{Name: "api", Pods: pods, Measured: measured, Used: used, Requests: requests, Limits: limits,
		MemoryBounded: limits.HasMemory}
}

func TestSizingSaysTheOneThingWorthKnowing(t *testing.T) {
	cases := []struct {
		name  string
		app   *AppUsage
		want  string
		level SizingLevel
	}{
		{"memory near its limit", sized(2, 2, both(100, 950<<20), both(500, 512<<20), both(1000, 1<<30)), "memory at 93% of its limit", SizingRisk},
		{"reserves far more than it uses", sized(2, 2, both(100, 100<<20), both(1000, 1<<30), both(2000, 2<<30)), "reserves 10× CPU, 10× memory of use", SizingNote},
		{"bursting over its request", sized(1, 1, both(900, 300<<20), both(500, 512<<20), application.Amounts{}), "CPU at 180% of its request", SizingNote},
		{"sized about right", sized(1, 1, both(400, 400<<20), both(500, 512<<20), both(1000, 1<<30)), "", SizingFine},
		{"too small to be worth a line", sized(1, 1, both(5, 20<<20), both(100, 128<<20), application.Amounts{}), "", SizingFine},
		{"a pod nobody measured", sized(3, 2, both(10, 10<<20), both(3000, 3<<30), application.Amounts{}), "", SizingFine},
	}
	for _, c := range cases {
		got, level := Sizing(c.app)
		if !strings.Contains(got, c.want) || (c.want == "" && got != "") || level != c.level {
			t.Errorf("%s: %q (%v), want %q (%v)", c.name, got, level, c.want, c.level)
		}
	}

	blind := &AppUsage{Pods: 2, Unsized: 2}
	if got, level := Sizing(blind); !strings.Contains(got, "no requests set") || level != SizingRisk {
		t.Errorf("unsized: %q (%v)", got, level)
	}
}

func TestIdleCountsOnlyWhatWasFullyMeasured(t *testing.T) {
	apps := []AppUsage{
		*sized(1, 1, both(200, 256<<20), both(1000, 1<<30), application.Amounts{}),
		*sized(2, 1, both(0, 0), both(4000, 4<<30), application.Amounts{}),      // half measured: not counted
		*sized(1, 1, both(900, 2<<30), both(500, 1<<30), application.Amounts{}), // over its request: nothing idle
	}
	cpu, memory, covered := Idle(apps)
	if cpu != 800 || memory != (1<<30)-(256<<20) || covered != 2 {
		t.Errorf("idle = %dm, %d bytes over %d applications", cpu, memory, covered)
	}
}

func TestALimitOnOnlyTheSidecarIsNotACeilingForThePod(t *testing.T) {
	app := container("app", both(500, 512<<20), application.Amounts{})
	proxy := container("istio-proxy", both(100, 128<<20), both(200, 256<<20))
	p := pod("api-0", "node-1", app, proxy)
	a := application.Application{Name: "api", Pods: []application.Pod{p}}
	used := map[string]application.Amounts{p.Namespace + "/" + p.Name: both(300, 900<<20)}
	rows := buildApps([]application.Application{a}, used)
	if len(rows) != 1 || rows[0].MemoryBounded {
		t.Fatalf("rows = %+v, want one application whose memory is not bounded", rows)
	}
	if text, _ := Sizing(&rows[0]); strings.Contains(text, "limit") {
		t.Errorf("900Mi against the sidecar's 256Mi is not 352%% of a limit: %q", text)
	}
}

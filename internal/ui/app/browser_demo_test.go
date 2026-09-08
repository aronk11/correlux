//go:build correlux_demo

package app

import (
	"strings"
	"testing"
)

func TestBrowserDemoCoversEveryViewAndOverlay(t *testing.T) {
	for _, screen := range []string{"apps", "application", "why", "pods", "object", "yaml", "logs", "events", "usage", "session", "fleet", "fleet-resources", "fleet-clusters", "fleet-namespaces", "commands", "resources", "clusters", "namespaces", "scale", "restart", "delete", "help"} {
		t.Run(screen, func(t *testing.T) {
			d := NewBrowserDemo()
			f := d.Step(DemoInput{Screen: screen})
			if f.ANSI == "" || !strings.Contains(f.ANSI, "prod-eu") {
				t.Fatal("view lost terminal or context")
			}
			if strings.Contains(f.ANSI, "Loading…") || strings.Contains(f.ANSI, "Looking for applications") {
				t.Fatal("demo left a request unresolved")
			}
			d.Step(DemoInput{Key: "esc"})
		})
	}
}

func TestBrowserDemoScaleRequiresProductionChallenge(t *testing.T) {
	d := NewBrowserDemo()
	d.Step(DemoInput{Screen: "scale"})
	d.Step(DemoInput{Replace: true, Text: "5", Key: "enter"})
	if d.m.overlay != overlayConfirm {
		t.Fatal("scale must preview")
	}
	d.Step(DemoInput{Key: "enter"})
	if d.m.overlay != overlayConfirm {
		t.Fatal("production needs its challenge")
	}
	d.Step(DemoInput{Replace: true, Text: "prod-eu", Key: "enter"})
	apps, _ := d.applications()
	if apps[0].DesiredPods != 5 {
		t.Fatalf("scale did not change local sample data: %d", apps[0].DesiredPods)
	}
}

func TestBrowserDemoCommandsAndFilterUseTheRealModel(t *testing.T) {
	d := NewBrowserDemo()
	d.Step(DemoInput{Key: "/"})
	d.Step(DemoInput{Text: "worker", Key: "enter"})
	d.Step(DemoInput{Key: "ctrl+w"})
	if d.m.selectedApp != "default/worker" {
		t.Fatalf("wrong target %s", d.m.selectedApp)
	}
	d.Step(DemoInput{Screen: "commands"})
	d.Step(DemoInput{Text: "Switch to cluster staging", Key: "enter"})
	if d.m.contextName != "staging" {
		t.Fatal("cluster switch did not run")
	}
	apps, _ := d.applications()
	if apps[0].ReadyPods != apps[0].DesiredPods {
		t.Fatal("staging fixture must be healthy")
	}
}

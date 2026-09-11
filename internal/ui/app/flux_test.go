package app

import (
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/kube/resources"
)

func TestFluxActionsRequireLoadedObjectAndConfirmation(t *testing.T) {
	m := newTestModel(t)
	m.view = viewObject
	m.objectTarget = objectRef{Kind: "HelmRelease", Name: "api", Namespace: "apps", Resource: "helmreleases.helm.toolkit.fluxcd.io"}
	if cmds := m.fluxCommands(); len(cmds) != 0 {
		for _, c := range cmds {
			if c.Action == paletteFlux {
				t.Fatal("action offered before object loaded")
			}
		}
	}
	loadObjectInto(m, &resources.Object{Kind: "HelmRelease", Name: "api", Namespace: "apps", Raw: []byte(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"name":"api","namespace":"apps","resourceVersion":"42"}}`)})
	if m.runCommand("flux.force") != nil {
		t.Fatal("mutation ran before consent")
	}
	if m.pending == nil || m.overlay != overlayConfirm {
		t.Fatal("missing confirmation gate")
	}
	if !strings.Contains(strings.Join(m.pending.Lines, " "), "workloads may be replaced") {
		t.Fatal("missing blast radius")
	}
	m.cancelPending()
	if m.pending != nil {
		t.Fatal("cancellation retained mutation")
	}
}

package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aronk11/correlux/internal/buildinfo"
	"github.com/aronk11/correlux/internal/update"
)

// withVersion stamps a version on the binary for the length of one test, which
// is what a released build has and a `go test` binary does not.
func withVersion(t *testing.T, version string) {
	t.Helper()
	original := buildinfo.Version
	buildinfo.Version = version
	t.Cleanup(func() { buildinfo.Version = original })
}

// learned delivers a check's answer the way the command would.
func learned(m *Model, release update.Release, announce bool) {
	m.Update(updateCheckedMsg{gen: m.updateCheck.Generation(), release: release, announce: announce})
}

// TestANewerVersionIsOnScreen is the feature: the header says there is one, and
// the session view says which and where to get it.
func TestANewerVersionIsOnScreen(t *testing.T) {
	withVersion(t, "v0.10.0")
	m := newTestModel(t)
	m.checkForUpdate(false)
	learned(m, update.Release{
		Version:   "v0.11.0",
		URL:       "https://github.com/aronk11/correlux/releases/tag/v0.11.0",
		CheckedAt: time.Now(),
	}, false)

	if out := plainView(m); !strings.Contains(out, "v0.11.0") {
		t.Errorf("the header must name the newer version:\n%s", out)
	}

	m.backToOverview()
	out := plainView(m)
	for _, want := range []string{
		"v0.10.0 — v0.11.0 is available",
		"github.com/aronk11/correlux/releases/tag/v0.11.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the session view must say what and where, missing %q:\n%s", want, out)
		}
	}
}

// TestBeingUpToDateIsQuietInTheHeaderAndStatedOnce: a header that permanently
// reports "up to date" spends a line on the answer nobody needed.
func TestBeingUpToDateIsQuietInTheHeaderAndStatedOnce(t *testing.T) {
	withVersion(t, "v0.11.0")
	m := newTestModel(t)
	m.checkForUpdate(false)
	learned(m, update.Release{Version: "v0.11.0", CheckedAt: time.Now()}, false)

	if label := m.updateHeaderLabel(); label != "" {
		t.Errorf("header label = %q, want nothing while the build is current", label)
	}
	m.backToOverview()
	if out := plainView(m); !strings.Contains(out, "v0.11.0 — up to date") {
		t.Errorf("the session view must still say it was checked:\n%s", out)
	}
}

// TestACheckThatDidNotHappenNeverReadsAsUpToDate is ADR 5 applied to the one
// value in Correlux that is not a cluster's.
func TestACheckThatDidNotHappenNeverReadsAsUpToDate(t *testing.T) {
	withVersion(t, "v0.10.0")
	m := newTestModel(t)
	m.checkForUpdate(false)
	learned(m, update.Release{Err: errors.New("dial tcp: no route to host")}, false)

	if label := m.updateHeaderLabel(); label != "" {
		t.Errorf("header label = %q, want silence about a check that failed", label)
	}
	value, _ := m.updateSummary()
	if !strings.Contains(value, "could not check") {
		t.Errorf("summary = %q, want the failure named rather than passed off as current", value)
	}
	if strings.Contains(value, "up to date") {
		t.Fatalf("summary = %q — a failed check must never read as up to date", value)
	}
	// And it must not have interrupted anybody.
	if m.message != "" {
		t.Errorf("message = %q, want a background check to stay silent", m.message)
	}
}

// TestTheCheckIsOffWhenTheConfigurationSaysSo, and says so where somebody would
// go looking for it.
func TestTheCheckIsOffWhenTheConfigurationSaysSo(t *testing.T) {
	withVersion(t, "v0.10.0")
	m := newTestModel(t, func(o *Options) { o.Config.Update.Check = false })

	if cmd := m.checkForUpdate(false); cmd != nil {
		t.Fatal("update.check: false must mean Correlux contacts nothing but Kubernetes")
	}
	value, _ := m.updateSummary()
	if !strings.Contains(value, "update check off") {
		t.Errorf("summary = %q, want the switch's state visible", value)
	}
	m.backToOverview()
	if out := plainView(m); !strings.Contains(out, "update.check: true") {
		t.Errorf("the session view must say how to turn it on:\n%s", out)
	}
}

// TestAnUnversionedBuildAsksNothing: a local build cannot be ranked against a
// release, so the request would tell the user nothing and the feed something.
func TestAnUnversionedBuildAsksNothing(t *testing.T) {
	withVersion(t, "dev")
	m := newTestModel(t)

	if cmd := m.checkForUpdate(false); cmd != nil {
		t.Fatal("a build with no version must not reach for the release feed")
	}
	value, _ := m.updateSummary()
	if !strings.Contains(value, "not compared") {
		t.Errorf("summary = %q, want the reason stated", value)
	}
}

// TestAnExplicitCheckAlwaysAnswers: a keystroke that answers silently reads as
// a keystroke that did nothing — and it works even with the setting off,
// because asking is consent.
func TestAnExplicitCheckAlwaysAnswers(t *testing.T) {
	withVersion(t, "v0.10.0")
	m := newTestModel(t, func(o *Options) { o.Config.Update.Check = false })

	if cmd := m.checkForUpdate(true); cmd == nil {
		t.Fatal("an explicitly requested check must run whatever the setting says")
	}
	learned(m, update.Release{Version: "v0.11.0", URL: "https://example.com"}, true)
	if !strings.Contains(m.message, "v0.11.0 is available") {
		t.Errorf("message = %q, want the answer said out loud", m.message)
	}

	learned(m, update.Release{Version: "v0.10.0"}, true)
	if !strings.Contains(m.message, "up to date") {
		t.Errorf("message = %q, want the good answer said too", m.message)
	}
}

func TestAirGappedModeBlocksAutomaticAndForcedUpdates(t *testing.T) {
	withVersion(t, "v0.15.0")
	m := newTestModel(t, func(o *Options) { o.Config.AirGapped = true; o.Config.Update.Check = true })
	for _, force := range []bool{false, true} {
		if cmd := m.checkForUpdate(force); cmd != nil {
			t.Fatalf("air-gapped mode scheduled a check with force=%v", force)
		}
	}
	m.checkUpdateNow()
	if !strings.Contains(m.message, "disabled in air-gapped mode") {
		t.Fatal(m.message)
	}
	for _, command := range m.registry.Commands() {
		if command.Action == paletteCheckUpdate && command.Enabled {
			t.Fatal("manual update command is enabled")
		}
	}
	// A previously learned release must not advertise an online upgrade.
	gen := m.updateCheck.Start()
	m.updateCheck.Succeed(gen, update.Release{Version: "v99.0.0", URL: "https://example.test/release"})
	if m.updateHeaderLabel() != "" {
		t.Fatal("cached update leaked into offline header")
	}
	m.backToOverview()
	out := plainView(m)
	if !strings.Contains(out, "air-gapped") || strings.Contains(out, "v99.0.0") || strings.Contains(out, "update.check: true") {
		t.Fatalf("misleading offline session details:\n%s", out)
	}
}

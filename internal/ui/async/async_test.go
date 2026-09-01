package async

import (
	"errors"
	"testing"
)

func TestZeroValueIsIdle(t *testing.T) {
	var v Value[int]
	if v.State() != Idle {
		t.Errorf("state = %v, want idle", v.State())
	}
	if v.IsLoading() {
		t.Error("an untouched value is not loading")
	}
}

func TestLoadThenSucceed(t *testing.T) {
	var v Value[[]string]
	gen := v.Start()
	if v.State() != Loading {
		t.Fatalf("state = %v, want loading", v.State())
	}
	if !v.Succeed(gen, []string{"a"}) {
		t.Fatal("a current-generation response must be applied")
	}
	if v.State() != Ready {
		t.Errorf("state = %v, want ready", v.State())
	}
	if got := v.Get(); len(got) != 1 || got[0] != "a" {
		t.Errorf("value = %v", got)
	}
	if v.UpdatedAt().IsZero() {
		t.Error("UpdatedAt must be stamped")
	}
}

func TestStaleResponseIsDiscarded(t *testing.T) {
	var v Value[string]
	stale := v.Start()
	fresh := v.Start() // the user switched context; the first request is obsolete

	if v.Succeed(stale, "old cluster") {
		t.Fatal("a response from a superseded request must be dropped")
	}
	if v.Get() == "old cluster" {
		t.Fatal("stale data leaked into the model")
	}
	if !v.Succeed(fresh, "new cluster") {
		t.Fatal("the current response must be applied")
	}
	if v.Get() != "new cluster" {
		t.Errorf("value = %q", v.Get())
	}
}

func TestStaleFailureIsDiscarded(t *testing.T) {
	var v Value[string]
	stale := v.Start()
	fresh := v.Start()
	v.Succeed(fresh, "good")

	if v.Fail(stale, errors.New("timeout")) {
		t.Fatal("a failure from a superseded request must not overwrite fresh data")
	}
	if v.State() != Ready || v.Get() != "good" {
		t.Errorf("state = %v value = %q", v.State(), v.Get())
	}
}

func TestReloadKeepsPreviousValueVisible(t *testing.T) {
	var v Value[int]
	gen := v.Start()
	v.Succeed(gen, 42)

	v.Start() // refresh
	if v.State() != Ready {
		t.Error("a refresh must not flip a loaded value back to loading")
	}
	if v.Get() != 42 {
		t.Errorf("value = %d, want the previous 42 while refreshing", v.Get())
	}
}

func TestFailureRetainsLastKnownValue(t *testing.T) {
	var v Value[int]
	gen := v.Start()
	v.Succeed(gen, 7)

	gen = v.Start()
	v.Fail(gen, errors.New("unreachable"))

	if v.State() != Failed {
		t.Errorf("state = %v, want failed", v.State())
	}
	if v.Get() != 7 {
		t.Errorf("value = %d, want the last known 7", v.Get())
	}
	if v.Err() == nil {
		t.Error("the error must be readable")
	}
}

func TestResetInvalidatesInFlightRequests(t *testing.T) {
	var v Value[string]
	gen := v.Start()
	v.Reset()

	if v.Succeed(gen, "late") {
		t.Fatal("a response that predates a reset must be dropped")
	}
	if v.State() != Idle || v.Get() != "" {
		t.Errorf("state = %v value = %q, want a cleared value", v.State(), v.Get())
	}
}

func TestStateStrings(t *testing.T) {
	for _, s := range []State{Idle, Loading, Ready, Failed} {
		if s.String() == "" {
			t.Errorf("state %d has no label", s)
		}
	}
}

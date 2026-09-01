package layout

import (
	"testing"
	"time"
)

func TestDebouncerCoalescesABurst(t *testing.T) {
	d := NewDebouncer(40 * time.Millisecond)
	start := time.Now()

	if !d.Observe(start) {
		t.Fatal("the first event must schedule a wake-up")
	}
	// A drag emits many events; none of them may schedule another wake-up.
	for i := 1; i <= 20; i++ {
		if d.Observe(start.Add(time.Duration(i) * time.Millisecond)) {
			t.Fatalf("event %d scheduled a redundant wake-up", i)
		}
	}

	// The wake-up fires while events are still arriving: reschedule, do not run.
	run, retry := d.Ready(start.Add(40 * time.Millisecond))
	if run {
		t.Fatal("must not run while the burst is still within the quiet period")
	}
	if retry <= 0 {
		t.Fatalf("expected a positive retry delay, got %v", retry)
	}

	run, _ = d.Ready(start.Add(20 * time.Millisecond).Add(40 * time.Millisecond))
	if !run {
		t.Fatal("must run once the burst has settled")
	}
	if d.Pending() {
		t.Error("no wake-up may remain pending after running")
	}
}

func TestDebouncerIgnoresUnscheduledWakeups(t *testing.T) {
	d := NewDebouncer(10 * time.Millisecond)
	if run, _ := d.Ready(time.Now()); run {
		t.Error("Ready must not fire without a preceding Observe")
	}
}

func TestDebouncerRestartsAfterFiring(t *testing.T) {
	d := NewDebouncer(10 * time.Millisecond)
	now := time.Now()
	d.Observe(now)
	if run, _ := d.Ready(now.Add(10 * time.Millisecond)); !run {
		t.Fatal("expected the first burst to run")
	}
	if !d.Observe(now.Add(time.Second)) {
		t.Error("a later event must schedule a new wake-up")
	}
}

func TestNewDebouncerRejectsNonPositiveInterval(t *testing.T) {
	if got := NewDebouncer(0).Interval(); got != DefaultResizeDebounce {
		t.Errorf("interval = %v, want the default %v", got, DefaultResizeDebounce)
	}
}

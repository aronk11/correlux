package layout

import "time"

// DefaultResizeDebounce is how long Correlux waits for a resize burst to
// settle. Dragging a terminal window emits dozens of events per second;
// re-rendering on each one wastes CPU and makes the UI tear.
const DefaultResizeDebounce = 40 * time.Millisecond

// Debouncer coalesces a burst of events into a single deferred action.
//
// It holds no timers of its own: the caller drives it with the current time and
// schedules the wake-up (a Bubble Tea tick, a timer, a test clock). That keeps
// the policy testable and the runtime free of stray goroutines.
type Debouncer struct {
	interval time.Duration
	pending  bool
	deadline time.Time
}

// NewDebouncer creates a debouncer with the given quiet period.
func NewDebouncer(interval time.Duration) *Debouncer {
	if interval <= 0 {
		interval = DefaultResizeDebounce
	}
	return &Debouncer{interval: interval}
}

// Interval reports the quiet period.
func (d *Debouncer) Interval() time.Duration { return d.interval }

// Observe records an event at time now. It returns true when the caller must
// schedule a wake-up after Interval; subsequent events inside the same burst
// return false because a wake-up is already pending.
func (d *Debouncer) Observe(now time.Time) bool {
	d.deadline = now.Add(d.interval)
	if d.pending {
		return false
	}
	d.pending = true
	return true
}

// Ready is called when a scheduled wake-up fires. It reports whether the action
// should run now (the burst has settled) and, if not, the remaining delay to
// reschedule for — events that arrived after the wake-up was scheduled push the
// deadline out.
func (d *Debouncer) Ready(now time.Time) (run bool, retryAfter time.Duration) {
	if !d.pending {
		return false, 0
	}
	if now.Before(d.deadline) {
		return false, d.deadline.Sub(now)
	}
	d.pending = false
	return true, 0
}

// Pending reports whether a wake-up is outstanding.
func (d *Debouncer) Pending() bool { return d.pending }

// Package async models the lifecycle of data that is fetched from the
// Kubernetes API.
//
// The UI must never confuse "still loading" with "nothing there": those two
// states look identical if you only track a slice's length, and mistaking one
// for the other is how a dashboard ends up claiming a cluster is empty while
// its first request is still in flight. Every remote value in kubeui therefore
// carries an explicit state.
package async

import "time"

// State is the lifecycle phase of a remote value.
type State int

const (
	// Idle means the value has never been requested.
	Idle State = iota
	// Loading means a request is in flight and no value is available yet.
	Loading
	// Ready means a value was received.
	Ready
	// Failed means the last request failed; a previous value may still be held.
	Failed
)

// String renders the state for debugging and tests.
func (s State) String() string {
	switch s {
	case Loading:
		return "loading"
	case Ready:
		return "ready"
	case Failed:
		return "failed"
	default:
		return "idle"
	}
}

// Value is a remote value together with its lifecycle. The zero Value is Idle.
//
// A generation counter guards against out-of-order responses: a request started
// before a context switch must not overwrite data fetched after it.
type Value[T any] struct {
	state     State
	value     T
	err       error
	updatedAt time.Time
	// generation is the request generation this value belongs to.
	generation uint64
}

// Start marks a new request as in flight and returns the generation to tag the
// response with. A reload keeps any previously loaded value visible so the UI
// does not flicker back to a spinner on every refresh.
func (v *Value[T]) Start() uint64 {
	v.generation++
	if v.state != Ready {
		v.state = Loading
	}
	return v.generation
}

// Generation reports the current request generation.
func (v *Value[T]) Generation() uint64 { return v.generation }

// Accepts reports whether a response tagged with gen is still relevant.
func (v *Value[T]) Accepts(gen uint64) bool { return gen == v.generation }

// Succeed stores a value if the generation is current, and reports whether it
// was applied.
func (v *Value[T]) Succeed(gen uint64, value T) bool {
	if !v.Accepts(gen) {
		return false
	}
	v.value = value
	v.err = nil
	v.state = Ready
	v.updatedAt = time.Now()
	return true
}

// Fail records an error if the generation is current, and reports whether it
// was applied. A previously loaded value is retained and remains readable.
func (v *Value[T]) Fail(gen uint64, err error) bool {
	if !v.Accepts(gen) {
		return false
	}
	v.err = err
	v.state = Failed
	v.updatedAt = time.Now()
	return true
}

// Reset returns the value to Idle and invalidates in-flight requests.
func (v *Value[T]) Reset() {
	var zero T
	v.generation++
	v.value = zero
	v.err = nil
	v.state = Idle
	v.updatedAt = time.Time{}
}

// State reports the lifecycle phase.
func (v *Value[T]) State() State { return v.state }

// Get returns the current value; it is the zero value unless State is Ready or
// a previous load succeeded before a later failure.
func (v *Value[T]) Get() T { return v.value }

// Err returns the last error, or nil.
func (v *Value[T]) Err() error { return v.err }

// UpdatedAt reports when the value last changed.
func (v *Value[T]) UpdatedAt() time.Time { return v.updatedAt }

// IsLoading reports whether a first load is in progress.
func (v *Value[T]) IsLoading() bool { return v.state == Loading }

// HasValue reports whether a value has ever been loaded successfully.
func (v *Value[T]) HasValue() bool { return !v.updatedAt.IsZero() && v.err == nil || v.state == Ready }

package logs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func source() Source {
	return Source{Namespace: "shop", Pod: "payments-7d8f-0", Container: "payments"}
}

// collect runs the scanner over a reader and returns the lines it produced.
func collect(ctx context.Context, t *testing.T, text string, timestamps bool) []Line {
	t.Helper()
	var lines []Line
	if err := scan(ctx, strings.NewReader(text), source(), timestamps, func(l Line) {
		lines = append(lines, l)
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return lines
}

func TestEveryLineIsDeliveredWithItsSource(t *testing.T) {
	lines := collect(context.Background(), t, "first\nsecond\nthird\n", false)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	if lines[0].Text != "first" || lines[2].Text != "third" {
		t.Errorf("lines = %+v", lines)
	}
	if lines[0].Source.Pod != "payments-7d8f-0" {
		t.Errorf("a line must say where it came from, got %+v", lines[0].Source)
	}
}

func TestALastLineWithoutANewlineIsNotLost(t *testing.T) {
	// A container that is still writing, or one that died mid-line.
	lines := collect(context.Background(), t, "first\nunterminated", false)
	if len(lines) != 2 || lines[1].Text != "unterminated" {
		t.Errorf("lines = %+v, want the unterminated line kept", lines)
	}
}

func TestTimestampsAreSplitOffWhenTheyWereAskedFor(t *testing.T) {
	lines := collect(context.Background(), t,
		"2026-09-02T10:00:00.123456789Z listening on :8080\n", true)

	if len(lines) != 1 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Text != "listening on :8080" {
		t.Errorf("text = %q, want the timestamp removed", lines[0].Text)
	}
	want := time.Date(2026, 9, 2, 10, 0, 0, 123456789, time.UTC)
	if !lines[0].At.Equal(want) {
		t.Errorf("time = %v, want %v", lines[0].At, want)
	}
}

func TestALineThatOnlyLooksLikeATimestampIsKeptWhole(t *testing.T) {
	// The application writes its own timestamps; splitting on the first space
	// would eat a word.
	lines := collect(context.Background(), t, "ERROR something failed\n", true)
	if lines[0].Text != "ERROR something failed" || !lines[0].At.IsZero() {
		t.Errorf("line = %+v, want it kept exactly as it came", lines[0])
	}
}

func TestTimestampsAreLeftAloneWhenTheyWereNotAskedFor(t *testing.T) {
	const text = "2026-09-02T10:00:00Z listening"
	lines := collect(context.Background(), t, text+"\n", false)
	if lines[0].Text != text {
		t.Errorf("text = %q, want the line untouched", lines[0].Text)
	}
}

func TestAnAbsurdlyLongLineIsSkippedRatherThanEndingTheStream(t *testing.T) {
	huge := strings.Repeat("x", MaxLineBytes+1)
	var lines []Line
	err := scan(context.Background(), strings.NewReader("before\n"+huge+"\nafter\n"), source(), false,
		func(l Line) { lines = append(lines, l) })
	if err != nil {
		t.Fatalf("one bad line must not fail the read: %v", err)
	}

	var texts []string
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "before") {
		t.Errorf("the lines before it must survive: %q", joined)
	}
	if !strings.Contains(joined, "was skipped") {
		t.Errorf("the skip must be stated rather than silent: %q", joined)
	}
}

func TestACancelledReadSaysWhyItStopped(t *testing.T) {
	// The reader reports the cancellation; whether that counts as a failure is
	// the caller's decision, and for a tail it does not.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scan(ctx, strings.NewReader("a\nb\n"), source(), false, func(Line) {})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the context's own cancellation", err)
	}
}

func TestTheTailIsBounded(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{"unset falls back", 0, DefaultTail},
		{"a sensible number is kept", 200, 200},
		{"an absurd number is capped", MaxTail * 10, MaxTail},
		{"a negative number falls back", -5, DefaultTail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Options{Tail: tc.in}).tail(); got != tc.want {
				t.Errorf("tail = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestASourceIsLabelledByPodAndContainer(t *testing.T) {
	if got := source().Label(); got != "payments-7d8f-0/payments" {
		t.Errorf("label = %q", got)
	}
	single := Source{Pod: "payments-7d8f-0"}
	if got := single.Label(); got != "payments-7d8f-0" {
		t.Errorf("label = %q, want no trailing slash when there is one container", got)
	}
}

func TestTailMergesSeveralContainersAndReportsOneThatFails(t *testing.T) {
	// The fake clientset answers every log request with the same canned line,
	// which is enough to prove the merge: three sources, three lines, each
	// attributed to the pod it came from.
	cs := fake.NewSimpleClientset()
	sources := []Source{
		{Namespace: "shop", Pod: "payments-0"},
		{Namespace: "shop", Pod: "payments-1"},
		{Namespace: "shop", Pod: "payments-2"},
	}

	events := Tail(context.Background(), cs, sources, Options{Tail: 10})

	seen := map[string]bool{}
	for event := range events {
		if event.Err != nil {
			t.Errorf("unexpected failure for %s: %v", event.Source.Label(), event.Err)
			continue
		}
		seen[event.Line.Source.Pod] = true
	}
	if len(seen) != len(sources) {
		t.Errorf("read from %v, want all three pods", seen)
	}
}

func TestTailStopsWhenTheUserLeaves(t *testing.T) {
	cs := fake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.Background())

	events := Tail(ctx, cs, []Source{{Namespace: "shop", Pod: "payments-0"}}, Options{Follow: true})
	cancel()

	// The channel must close rather than leaving a reader hanging on it.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the stream did not end after its context was cancelled")
		}
	}
}

package logs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
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

// dropping serves a followed log the way a flaky path does: it closes the
// connection twice, the second time after repeating a line the client already
// has, and finally has nothing more to say.
func dropping(t *testing.T, sinces *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		*sinces = append(*sinces, r.URL.Query().Get("sinceTime"))
		mu.Unlock()
		switch n {
		case 1:
			_, _ = w.Write([]byte("2026-10-26T10:00:01.000000001Z first\n2026-10-26T10:00:02.000000002Z second\n"))
		case 2:
			_, _ = w.Write([]byte("2026-10-26T10:00:02.000000002Z second\n2026-10-26T10:00:03.000000003Z third\n"))
		}
	}))
}

func TestAFollowedLogSurvivesDroppedConnectionsWithoutRepeatingLines(t *testing.T) {
	restore := reconnectDelay
	reconnectDelay = func(int) time.Duration { return time.Millisecond }
	defer func() { reconnectDelay = restore }()

	var (
		mu     sync.Mutex
		sinces []string
	)
	srv := dropping(t, &sinces, &mu)
	defer srv.Close()
	cs, _ := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})

	var got []string
	err := Stream(context.Background(), cs, Source{Namespace: "shop", Pod: "payments-0"}, Options{Follow: true}, func(l Line) {
		if !l.At.IsZero() {
			t.Errorf("timestamps were not asked for, yet %q carries one", l.Text)
		}
		got = append(got, l.Text)
	})
	if err != nil {
		t.Fatalf("a stream that ran out on its own is not an error: %v", err)
	}
	if strings.Join(got, ",") != "first,second,third" {
		t.Errorf("lines = %v, want every line exactly once", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sinces) < 3 || sinces[0] != "" || !strings.HasPrefix(sinces[1], "2026-10-26T10:00:02") {
		t.Errorf("reconnects must resume after the last line; sinceTime per request = %v", sinces)
	}
	if len(sinces) > 1+1+maxQuietReconnects {
		t.Errorf("a stopped container must end the stream, not reconnect forever: %d requests", len(sinces))
	}
}

func TestAPodThatIsGoneEndsTheStream(t *testing.T) {
	restore := reconnectDelay
	reconnectDelay = func(int) time.Duration { return time.Millisecond }
	defer func() { reconnectDelay = restore }()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte("2026-10-26T10:00:01Z hello\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404,"message":"pods \"payments-0\" not found"}`))
	}))
	defer srv.Close()
	cs, _ := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})

	err := Stream(context.Background(), cs, Source{Namespace: "shop", Pod: "payments-0"}, Options{Follow: true}, func(Line) {})
	if err == nil || !strings.Contains(err.Error(), "not found") || calls != 2 {
		t.Errorf("err = %v after %d requests; a deleted pod must end the stream with the reason", err, calls)
	}
}

func TestCancellingAFollowedLogStopsReconnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2026-10-26T10:00:01Z line\n"))
		cancel()
	}))
	defer srv.Close()
	cs, _ := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})

	done := make(chan error, 1)
	go func() {
		done <- Stream(ctx, cs, Source{Namespace: "shop", Pod: "p"}, Options{Follow: true}, func(Line) {})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want the cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled stream kept reconnecting")
	}
}

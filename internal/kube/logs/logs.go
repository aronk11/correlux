// Package logs reads container logs.
//
// Logs are the one thing in Correlux that arrive over time rather than in one
// answer, and that shapes everything here: a read is a stream that is handed
// back line by line, it is bounded on both ends — how much history, how much is
// kept — and it stops the moment its context is cancelled, because a user who
// has moved on must not leave a connection behind.
package logs

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Defaults for a read. The tail is what `kubectl logs` shows by default on a
// crash loop investigation; the line cap is what Correlux keeps in memory.
const (
	DefaultTail  = int64(1000)
	MaxTail      = int64(10000)
	MaxLineBytes = 256 * 1024
)

// Source names one container to read from.
type Source struct {
	Namespace string
	Pod       string
	// Container may be empty, in which case the server picks the pod's only
	// container — and says so when there is more than one.
	Container string
}

// Label renders the source the way a log line is attributed.
func (s Source) Label() string {
	if s.Container == "" {
		return s.Pod
	}
	return s.Pod + "/" + s.Container
}

// Options select what to read.
type Options struct {
	// Follow keeps the stream open and delivers new lines as they are written.
	Follow bool
	// Tail bounds the history read before that; zero means DefaultTail.
	Tail int64
	// Timestamps asks the server to prefix each line with the time it was
	// written, which is the only reliable clock for a log line.
	Timestamps bool
	// Previous reads the log of the *previous* run of a container that has
	// restarted. For a crash loop it is the only log that explains anything.
	Previous bool
}

func (o Options) tail() int64 {
	switch {
	case o.Tail <= 0:
		return DefaultTail
	case o.Tail > MaxTail:
		return MaxTail
	default:
		return o.Tail
	}
}

// Line is one line of output, attributed to where it came from.
type Line struct {
	Source Source
	Text   string
	// At is the server's timestamp when Options.Timestamps was set.
	At time.Time
}

// Stream reads one container's log and calls emit for every line, until the log
// ends or the context is cancelled. A cancelled read returns the context's
// error, which the caller is expected to read as "the user moved on".
//
// A followed log is one HTTP response that is meant to stay open for as long
// as somebody watches, and plenty of things close it without anybody asking: a
// load balancer's idle timeout, the kubelet rotating the log file, a network
// that drops for a second. Stream reconnects when that happens and resumes
// after the last line it delivered, so the screen keeps going instead of
// stopping mid-incident. It ends on its own only when reconnecting keeps
// producing nothing, which is what a container that is no longer running
// looks like.
//
// emit is called from the caller's goroutine, not the UI's, so it must be safe
// to call from anywhere — in Correlux it writes to a channel.
func Stream(
	ctx context.Context,
	cs kubernetes.Interface,
	source Source,
	opts Options,
	emit func(Line),
) error {
	if !opts.Follow {
		tail := opts.tail()
		return streamOnce(ctx, cs, source, &corev1.PodLogOptions{
			Container: source.Container, Previous: opts.Previous, Timestamps: opts.Timestamps, TailLines: &tail,
		}, opts.Timestamps, emit)
	}

	// Server timestamps are how a resumed read knows where it left off; they
	// are taken off again unless the user asked to see them.
	var last time.Time
	deliver := func(line Line) {
		if !last.IsZero() && !line.At.IsZero() && !line.At.After(last) {
			return // already delivered before the connection dropped
		}
		if !line.At.IsZero() {
			last = line.At
		}
		if !opts.Timestamps {
			line.At = time.Time{}
		}
		emit(line)
	}

	tail := opts.tail()
	request := &corev1.PodLogOptions{
		Container: source.Container, Follow: true, Previous: opts.Previous, Timestamps: true, TailLines: &tail,
	}
	err := streamOnce(ctx, cs, source, request, true, deliver)
	quiet := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !resumable(err) {
			return err
		}
		if quiet >= maxQuietReconnects {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay(quiet)):
		}

		request = &corev1.PodLogOptions{Container: source.Container, Follow: true, Previous: opts.Previous, Timestamps: true}
		if last.IsZero() {
			request.TailLines = &tail
		} else {
			since := metav1.NewTime(last)
			request.SinceTime = &since
		}
		before, started := last, time.Now()
		err = streamOnce(ctx, cs, source, request, true, deliver)
		if last.Equal(before) && time.Since(started) < quietWindow {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// Reconnecting is bounded: a stream that closes again at once, with nothing
// new, several times in a row belongs to a container that has stopped.
const (
	maxQuietReconnects = 3
	quietWindow        = 3 * time.Second
)

// reconnectDelay backs off between attempts; a variable so tests need not wait.
var reconnectDelay = func(attempt int) time.Duration {
	return min(time.Duration(1<<attempt)*time.Second, 5*time.Second)
}

// resumable reports whether a failed read is worth trying again. A pod that
// is gone or a request the server refuses will be refused again.
func resumable(err error) bool {
	switch {
	case apierrors.IsNotFound(err), apierrors.IsBadRequest(err), apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return false
	}
	return true
}

func streamOnce(ctx context.Context, cs kubernetes.Interface, source Source, request *corev1.PodLogOptions, timestamps bool, emit func(Line)) error {
	stream, err := cs.CoreV1().Pods(source.Namespace).GetLogs(source.Pod, request).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	return scan(ctx, stream, source, timestamps, emit)
}

// scan turns a byte stream into lines. It is separate from the request so it
// can be tested against a reader, which is where the awkward cases live: a
// final line with no newline, a line longer than any buffer, a stream that ends
// because the context did.
func scan(
	ctx context.Context,
	r io.Reader,
	source Source,
	timestamps bool,
	emit func(Line),
) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		emit(parse(scanner.Text(), source, timestamps))
	}

	// A read cut short because the user moved on ends with the context's own
	// error. Saying why it stopped is the reader's job; deciding that a
	// cancellation is not a failure is the caller's.
	if err := ctx.Err(); err != nil {
		return err
	}

	err := scanner.Err()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bufio.ErrTooLong):
		// One absurd line must not end the stream; the caller keeps what it has.
		emit(Line{Source: source, Text: "[a log line longer than " +
			itoa(MaxLineBytes/1024) + "KiB was skipped]"})
		return nil
	default:
		return err
	}
}

// parse splits the server's timestamp off the front of a line. The server
// writes RFC 3339 with nanoseconds, a space, then the line.
func parse(text string, source Source, timestamps bool) Line {
	line := Line{Source: source, Text: text}
	if !timestamps {
		return line
	}
	stamp, rest, found := strings.Cut(text, " ")
	if !found {
		return line
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		// Not a timestamp after all: keep the line exactly as it came.
		return line
	}
	line.At, line.Text = at, rest
	return line
}

// itoa keeps this package free of strconv for the one number it formats.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Event is one thing that happened while tailing: a line, or a source that
// could not be read.
type Event struct {
	Line Line
	// Err reports that this one source failed. The others keep going: a pod
	// whose container has not started yet must not silence the rest of an
	// application's logs.
	Err    error
	Source Source
}

// bufferedEvents is how many events may wait for the UI before the readers are
// made to wait. It is large enough for a burst and small enough that a screen
// nobody is watching cannot grow without bound.
const bufferedEvents = 1024

// Tail merges several containers into one stream, in the order the lines
// arrive. The channel is closed once every source has ended, which for a
// following tail means the context was cancelled.
func Tail(
	ctx context.Context,
	cs kubernetes.Interface,
	sources []Source,
	opts Options,
) <-chan Event {
	out := make(chan Event, bufferedEvents)

	var wg sync.WaitGroup
	for _, source := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Stream(ctx, cs, source, opts, func(line Line) {
				select {
				case out <- Event{Line: line}:
				case <-ctx.Done():
				}
			})
			if err != nil && ctx.Err() == nil {
				select {
				case out <- Event{Err: err, Source: source}:
				case <-ctx.Done():
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

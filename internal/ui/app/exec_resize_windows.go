//go:build windows

package app

import (
	"github.com/charmbracelet/x/term"
	"k8s.io/client-go/tools/remotecommand"
)

// resizeQueue reports the terminal's size to an exec session once, at the
// start. Windows has no SIGWINCH, so a shell resized mid-session keeps the
// size it started with — the same limitation `kubectl exec` has there.
type resizeQueue struct {
	fd   uintptr
	sent bool
	done chan struct{}
}

func newResizeQueue(fd uintptr) *resizeQueue {
	return &resizeQueue{fd: fd, done: make(chan struct{})}
}

// Next implements remotecommand.TerminalSizeQueue.
func (q *resizeQueue) Next() *remotecommand.TerminalSize {
	if !q.sent {
		q.sent = true
		if w, h, err := term.GetSize(q.fd); err == nil {
			//nolint:gosec // G115: terminal dimensions never approach uint16's range
			return &remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}
		}
	}
	<-q.done
	return nil
}

// stop unblocks Next so the size-reporting goroutine remotecommand runs it on
// can exit once the session ends.
func (q *resizeQueue) stop() {
	close(q.done)
}

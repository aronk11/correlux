//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
	"k8s.io/client-go/tools/remotecommand"
)

// resizeQueue reports the terminal's size to an exec session: once
// immediately, so the remote shell starts at the size it is actually drawn
// at, and again on every SIGWINCH after that.
type resizeQueue struct {
	fd      uintptr
	sig     chan os.Signal
	done    chan struct{}
	initial bool
}

func newResizeQueue(fd uintptr) *resizeQueue {
	q := &resizeQueue{fd: fd, sig: make(chan os.Signal, 1), done: make(chan struct{})}
	signal.Notify(q.sig, syscall.SIGWINCH)
	return q
}

// Next implements remotecommand.TerminalSizeQueue.
func (q *resizeQueue) Next() *remotecommand.TerminalSize {
	if !q.initial {
		q.initial = true
		if size, ok := q.size(); ok {
			return size
		}
	}
	select {
	case <-q.sig:
		size, ok := q.size()
		if !ok {
			return nil
		}
		return size
	case <-q.done:
		return nil
	}
}

func (q *resizeQueue) size() (*remotecommand.TerminalSize, bool) {
	w, h, err := term.GetSize(q.fd)
	if err != nil {
		return nil, false
	}
	//nolint:gosec // G115: terminal dimensions never approach uint16's range
	return &remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}, true
}

// stop releases the signal channel. It must be called once the session ends,
// or every exec session after it leaks a SIGWINCH subscription.
func (q *resizeQueue) stop() {
	signal.Stop(q.sig)
	close(q.done)
}

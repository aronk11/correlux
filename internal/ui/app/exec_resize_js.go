//go:build js && wasm && correlux_demo

package app

import "k8s.io/client-go/tools/remotecommand"

// Browser demos never start a remote shell or subscribe to operating-system signals.
type resizeQueue struct{}

func newResizeQueue(uintptr) *resizeQueue              { return &resizeQueue{} }
func (*resizeQueue) Next() *remotecommand.TerminalSize { return nil }
func (*resizeQueue) stop()                             {}

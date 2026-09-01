package cli

import (
	"flag"
	"io"

	"k8s.io/klog/v2"
)

// silenceClientLogs stops client-go and its dependencies from writing to
// stderr. A stray klog line in the middle of a full-screen TUI corrupts the
// frame; anything worth telling the user is surfaced inside the UI instead.
func silenceClientLogs() {
	var fs flag.FlagSet
	klog.InitFlags(&fs)
	_ = fs.Set("logtostderr", "false")
	_ = fs.Set("alsologtostderr", "false")
	_ = fs.Set("stderrthreshold", "FATAL")
	klog.SetOutput(io.Discard)
}

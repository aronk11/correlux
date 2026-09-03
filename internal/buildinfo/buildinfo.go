// Package buildinfo exposes the version information stamped into the binary at
// build time. Values are overridden with -ldflags -X; when Correlux is built
// with `go install`, they fall back to the module's VCS metadata.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var (
	// Version is the semantic version of this build ("dev" for local builds).
	Version = "dev"
	// Commit is the git revision this build was cut from.
	Commit = ""
	// Date is the RFC3339 build timestamp.
	Date = ""
)

// Info is a resolved, printable description of the running binary.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

// Get resolves build information, filling gaps from the embedded VCS stamps.
func Get() Info {
	i := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return i
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if i.Commit == "" {
				i.Commit = s.Value
			}
		case "vcs.time":
			if i.Date == "" {
				i.Date = s.Value
			}
		}
	}
	return i
}

// String renders a one-line summary, e.g. "correlux v0.1.0 (abc1234)
// darwin/arm64".
func (i Info) String() string {
	s := "correlux " + i.Version
	if i.Commit != "" {
		c := i.Commit
		if len(c) > 7 {
			c = c[:7]
		}
		s += fmt.Sprintf(" (%s)", c)
	}
	return s + " " + i.Platform
}

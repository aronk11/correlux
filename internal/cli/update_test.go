package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/buildinfo"
	"github.com/aronk11/correlux/internal/update"
)

func fakeFeed(t *testing.T, version string) *bool {
	t.Helper()
	installed := false
	original := buildinfo.Version
	buildinfo.Version = "v0.19.0"
	checkRestore, installRestore := checkRelease, installRelease
	checkRelease = func(context.Context, string, string, bool) update.Release {
		return update.Release{Version: version, URL: "https://example.com/release"}
	}
	installRelease = func(_ context.Context, _ update.Plan, tag, _, _ string) (update.Result, error) {
		installed = true
		return update.Result{Version: tag}, nil
	}
	t.Cleanup(func() {
		checkRelease, installRelease = checkRestore, installRestore
		buildinfo.Version = original
	})
	return &installed
}

func TestUpdateAsksAndANoChangesNothing(t *testing.T) {
	installed := fakeFeed(t, "v99.0.0")
	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, strings.NewReader("n\n"), true, false, "/home/me/bin/correlux"); err != nil {
		t.Fatal(err)
	}
	if *installed || !strings.Contains(out.String(), "[y/N]") || !strings.Contains(out.String(), "Nothing changed.") {
		t.Errorf("installed %v, output:\n%s", *installed, out.String())
	}
}

func TestUpdateInstallsOnYes(t *testing.T) {
	installed := fakeFeed(t, "v99.0.0")
	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, strings.NewReader("yes\n"), true, false, "/home/me/bin/correlux"); err != nil {
		t.Fatal(err)
	}
	if !*installed || !strings.Contains(out.String(), "Updated to v99.0.0") {
		t.Errorf("installed %v, output:\n%s", *installed, out.String())
	}
}

func TestUpdateWithoutATerminalNeverInstalls(t *testing.T) {
	installed := fakeFeed(t, "v99.0.0")
	err := runUpdate(context.Background(), &bytes.Buffer{}, strings.NewReader("y\n"), false, false, "/home/me/bin/correlux")
	if err == nil || *installed {
		t.Errorf("err %v, installed %v", err, *installed)
	}
}

func TestUpdatePointsManagedInstallsAtTheirTool(t *testing.T) {
	installed := fakeFeed(t, "v99.0.0")
	var out bytes.Buffer
	if err := runUpdate(context.Background(), &out, strings.NewReader("y\n"), true, false, "/usr/bin/correlux"); err != nil {
		t.Fatal(err)
	}
	if *installed || !strings.Contains(out.String(), "apt, dnf or apk") {
		t.Errorf("installed %v, output:\n%s", *installed, out.String())
	}
}

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarball(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"LICENSE", []byte("Apache")}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(f.body)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func releaseServer(t *testing.T, archive []byte, sumOverride string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if sumOverride != "" {
		digest = sumOverride
	}
	name := ArchiveName("v0.20.0", "linux", "amd64")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.20.0/checksums.txt":
			_, _ = w.Write([]byte(digest + "  " + name + "\n0000000000000000000000000000000000000000000000000000000000000000  other.zip\n"))
		case "/v0.20.0/" + name:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func installedBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "correlux")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestAnUpdateReplacesTheBinaryOnlyWhenTheChecksumMatches(t *testing.T) {
	srv := releaseServer(t, tarball(t, "correlux", []byte("new build")), "")
	defer srv.Close()
	exe := installedBinary(t)

	res, err := installFrom(context.Background(), srv.URL, PlanFor(exe), "v0.20.0", "linux", "amd64", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new build" || res.SignatureVerified || res.Archive != "correlux_0.20.0_linux_amd64.tar.gz" {
		t.Errorf("binary %q, result %+v", got, res)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o100 == 0 {
		t.Error("the new binary must be executable")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".correlux-update-*"))
	if len(leftovers) != 0 {
		t.Errorf("the staging directory must be cleaned up: %v", leftovers)
	}
}

func TestATamperedArchiveIsRefusedAndNothingChanges(t *testing.T) {
	srv := releaseServer(t, tarball(t, "correlux", []byte("evil")), strings.Repeat("a", 64))
	defer srv.Close()
	exe := installedBinary(t)

	_, err := installFrom(context.Background(), srv.URL, PlanFor(exe), "v0.20.0", "linux", "amd64", false)
	if err == nil || !strings.Contains(err.Error(), "does not match the published checksum") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("the installed binary must be untouched, got %q", got)
	}
}

func TestOnlyStableTagsAreInstalled(t *testing.T) {
	exe := installedBinary(t)
	for _, tag := range []string{"v0.20.0-rc.1", "latest", "../../etc", "0.20.0"} {
		if _, err := installFrom(context.Background(), "http://127.0.0.1:1", PlanFor(exe), tag, "linux", "amd64", false); err == nil {
			t.Errorf("tag %q must be refused", tag)
		}
	}
}

func TestBinariesOtherToolsManageAreLeftToThem(t *testing.T) {
	cases := map[string]Method{
		"/opt/homebrew/Caskroom/correlux/0.19.0/correlux":        Homebrew,
		"/home/linuxbrew/.linuxbrew/bin/correlux":                Homebrew,
		"/home/me/.krew/store/correlux/v0.19.0/kubectl-correlux": Krew,
		"/usr/bin/correlux":     SystemPackage,
		"/home/me/bin/correlux": SelfReplace,
	}
	for path, want := range cases {
		plan := PlanFor(path)
		if plan.Method != want {
			t.Errorf("%s: method %v, want %v", path, plan.Method, want)
		}
		if want != SelfReplace && plan.Hint == "" {
			t.Errorf("%s: a managed install must say what to run instead", path)
		}
		if want != SelfReplace {
			if _, err := installFrom(context.Background(), "http://127.0.0.1:1", plan, "v0.20.0", "linux", "amd64", false); err == nil {
				t.Errorf("%s: a managed install must never be replaced", path)
			}
		}
	}
}

func TestWindowsArchivesAreZips(t *testing.T) {
	if got := ArchiveName("v1.2.3", "windows", "arm64"); got != "correlux_1.2.3_windows_arm64.zip" {
		t.Errorf("got %q", got)
	}
}

func TestWindowsArchivesAreUnzippedAndTheRunningBinaryStepsAside(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("correlux.exe")
	_, _ = w.Write([]byte("new windows build"))
	_ = zw.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	name := ArchiveName("v0.20.0", "windows", "amd64")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.20.0/checksums.txt":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
		case "/v0.20.0/" + name:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "correlux.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	if _, err := installFrom(context.Background(), srv.URL, PlanFor(exe), "v0.20.0", "windows", "amd64", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new windows build" {
		t.Errorf("binary = %q", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
		t.Errorf("the previous binary must be kept as .old, got %q", got)
	}
}

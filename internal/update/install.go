package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DownloadBase is where release assets are served from.
const DownloadBase = "https://github.com/aronk11/correlux/releases/download/"

const (
	// downloadTimeout bounds one whole update, archive included.
	downloadTimeout = 5 * time.Minute
	// maxArchive bounds what is read from the network or an archive.
	maxArchive = 200 << 20
	// signer is the only identity allowed to have signed a release: the
	// release workflow of this repository, at a tag.
	signerPrefix = `^https://github\.com/aronk11/correlux/\.github/workflows/release\.yml@refs/tags/`
	signerIssuer = "https://token.actions.githubusercontent.com"
)

// Method is how this copy of Correlux was installed, which decides who may
// replace it. A binary Homebrew, krew or a system package put there belongs
// to that tool; replacing it underneath would leave the tool confused about
// what it installed.
type Method int

const (
	// SelfReplace is a binary somebody put on their PATH themselves.
	SelfReplace Method = iota
	Homebrew
	Krew
	SystemPackage
)

// Plan says what an update of this executable would do.
type Plan struct {
	Method     Method
	Executable string
	// Hint is the command to run instead, for every method but SelfReplace.
	Hint string
}

// PlanFor decides how the executable at exe is updated.
func PlanFor(exe string) Plan {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	slashed := filepath.ToSlash(exe)
	base := strings.TrimSuffix(filepath.Base(exe), ".exe")
	switch {
	case strings.Contains(slashed, "/Cellar/") || strings.Contains(slashed, "/Caskroom/") ||
		strings.Contains(slashed, "/homebrew/") || strings.Contains(slashed, "/linuxbrew/"):
		return Plan{Method: Homebrew, Executable: exe, Hint: "brew upgrade correlux"}
	case strings.HasPrefix(base, "kubectl-correlux") || strings.Contains(slashed, "/.krew/"):
		return Plan{Method: Krew, Executable: exe, Hint: "kubectl krew upgrade correlux"}
	case strings.HasPrefix(slashed, "/usr/bin/") || strings.HasPrefix(slashed, "/bin/") || strings.HasPrefix(slashed, "/usr/sbin/"):
		return Plan{Method: SystemPackage, Executable: exe, Hint: "upgrade the correlux package with apt, dnf or apk"}
	}
	return Plan{Method: SelfReplace, Executable: exe}
}

// Result is what an update did.
type Result struct {
	Version string
	Archive string
	// SignatureVerified is true when cosign confirmed the checksum list was
	// signed by this project's release workflow for exactly this tag. False
	// means the archive matched the published checksum, and nothing more.
	SignatureVerified bool
}

// CosignAvailable reports whether signatures can be verified here.
func CosignAvailable() bool {
	_, err := exec.LookPath("cosign")
	return err == nil
}

// ArchiveName is the release archive for a platform.
func ArchiveName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "correlux_" + strings.TrimPrefix(tag, "v") + "_" + goos + "_" + goarch + ext
}

// Install replaces the executable with release tag. It must only ever be
// called after the user said yes: it is the one thing Correlux does to the
// machine it runs on.
func Install(ctx context.Context, plan Plan, tag, goos, goarch string) (Result, error) {
	return installFrom(ctx, DownloadBase, plan, tag, goos, goarch, CosignAvailable())
}

func installFrom(ctx context.Context, base string, plan Plan, tag, goos, goarch string, verify bool) (Result, error) {
	if plan.Method != SelfReplace {
		return Result{}, errors.New("this copy is managed by another tool: " + plan.Hint)
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(tag) {
		return Result{}, fmt.Errorf("refusing to install %q: only stable release tags are installed", tag)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	dir := filepath.Dir(plan.Executable)
	work, err := os.MkdirTemp(dir, ".correlux-update-")
	if err != nil {
		return Result{}, fmt.Errorf("cannot write next to %s: %w", plan.Executable, err)
	}
	defer os.RemoveAll(work)

	prefix := strings.TrimRight(base, "/") + "/" + tag + "/"
	sums, err := fetch(ctx, prefix+"checksums.txt", 1<<20)
	if err != nil {
		return Result{}, err
	}
	result := Result{Version: tag, Archive: ArchiveName(tag, goos, goarch)}
	if verify {
		if verr := verifySignature(ctx, work, prefix, tag, sums); verr != nil {
			return Result{}, verr
		}
		result.SignatureVerified = true
	}
	want, err := checksumFor(sums, result.Archive)
	if err != nil {
		return Result{}, err
	}

	archive, err := fetch(ctx, prefix+result.Archive, maxArchive)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return Result{}, fmt.Errorf("%s does not match the published checksum (got %s, want %s)", result.Archive, got, want)
	}

	name := "correlux"
	if goos == "windows" {
		name += ".exe"
	}
	binary, err := extract(archive, name, goos == "windows")
	if err != nil {
		return Result{}, err
	}
	return result, replace(plan.Executable, binary, work, goos == "windows")
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "correlux-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download of %s answered %s", filepath.Base(url), resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", filepath.Base(url))
	}
	return body, nil
}

func checksumFor(sums []byte, archive string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == archive && len(fields[0]) == 64 {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", errors.New("the release publishes no checksum for " + archive)
}

func verifySignature(ctx context.Context, work, prefix, tag string, sums []byte) error {
	bundle, err := fetch(ctx, prefix+"checksums.txt.sigstore.json", 1<<20)
	if err != nil {
		return err
	}
	sumsPath, bundlePath := filepath.Join(work, "checksums.txt"), filepath.Join(work, "checksums.txt.sigstore.json")
	if werr := os.WriteFile(sumsPath, sums, 0o600); werr != nil {
		return werr
	}
	if werr := os.WriteFile(bundlePath, bundle, 0o600); werr != nil {
		return werr
	}
	// #nosec G204 -- fixed binary, arguments are paths this function wrote and a quoted tag.
	out, err := exec.CommandContext(ctx, "cosign", "verify-blob", sumsPath,
		"--bundle", bundlePath,
		"--certificate-identity-regexp", signerPrefix+regexp.QuoteMeta(tag)+"$",
		"--certificate-oidc-issuer", signerIssuer).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the release signature did not verify: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func extract(archive []byte, name string, isZip bool) ([]byte, error) {
	if isZip {
		r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range r.File {
			if filepath.Base(f.Name) != name || f.FileInfo().IsDir() {
				continue
			}
			return readZipEntry(f)
		}
		return nil, errors.New("the archive contains no " + name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive contains no " + name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == name {
			return readBounded(tr)
		}
	}
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return readBounded(rc)
}

func readBounded(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxArchive {
		return nil, errors.New("the binary in the archive is larger than expected")
	}
	return b, nil
}

// replace swaps the binary in one rename. Windows will not overwrite a running
// executable but will rename it, so the old one steps aside first and is left
// as .old until the next update removes it.
func replace(exe string, binary []byte, work string, windows bool) error {
	staged := filepath.Join(work, "correlux.new")
	// #nosec G306 -- an executable must be executable.
	if err := os.WriteFile(staged, binary, 0o755); err != nil {
		return err
	}
	if windows {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(staged, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		return nil
	}
	return os.Rename(staged, exe)
}

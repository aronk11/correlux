// Package update answers one question: is there a newer Correlux than this one?
//
// It is the only part of Correlux that talks to anything other than a
// Kubernetes API server, and everything about it is bounded because of that:
// one unauthenticated GET to the public release feed, at most once a day, the
// answer cached on disk, and one line of configuration switches it off for
// good. Nothing about the user, their clusters or their session is sent — the
// request carries the version `correlux version` already prints and nothing
// else, and the answer is a version number and a link.
//
// A failed check is not an error the user has to deal with. A laptop on a
// train, a proxy that refuses, an air-gapped cluster: none of them are
// problems with Correlux, and none of them may interrupt what somebody is
// doing. The failure is remembered so the session view can say the check did
// not happen, which is the one place it belongs (ADR 5: loading, empty and
// denied never look alike).
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Feed is GitHub's own "latest release" endpoint for Correlux. It excludes
	// drafts and pre-releases, so a release candidate never suggests itself to
	// somebody running a stable build.
	Feed = "https://api.github.com/repos/aronk11/correlux/releases/latest"

	// Interval is how long an answer is good for. A release happens a few
	// times a month; asking more often than once a day would be noise on
	// somebody else's server.
	Interval = 24 * time.Hour

	// Timeout bounds the request. It is short on purpose: this is a nicety
	// running behind a screen somebody is trying to use.
	Timeout = 5 * time.Second

	// CacheFile is where the answer is remembered, beside the configuration.
	CacheFile = "update.json"
)

// Release is what the feed said.
type Release struct {
	// Version is the tag, as published ("v0.11.0").
	Version string `json:"version"`
	// URL is the release page, which is where somebody goes next.
	URL string `json:"url"`
	// CheckedAt is when the feed was last asked, successfully or not.
	CheckedAt time.Time `json:"checkedAt"`
	// Err records why the last check did not happen, for the session view.
	// It is never persisted: a proxy that refused this morning says nothing
	// about this afternoon.
	Err error `json:"-"`
}

// Fresh reports whether this answer is recent enough to be reused.
func (r Release) Fresh(now time.Time) bool {
	return !r.CheckedAt.IsZero() && now.Sub(r.CheckedAt) < Interval
}

// CachePath is where the answer lives, given the directory the configuration
// file is in.
func CachePath(configDir string) string {
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, CacheFile)
}

// Check answers from the cache when it can, and asks the feed when it cannot.
//
// force skips the cache, which is what an explicit "check now" means. The
// answer is written back whether or not it is newer, because the point of the
// cache is to bound how often the feed is asked, not to remember good news.
func Check(ctx context.Context, cachePath, current string, force bool) Release {
	return checkAgainst(ctx, Feed, cachePath, current, force)
}

func checkAgainst(ctx context.Context, feed, cachePath, current string, force bool) Release {
	now := time.Now()
	if !force {
		if cached, err := Load(cachePath); err == nil && cached.Fresh(now) {
			return cached
		}
	}

	release, err := fetchFrom(ctx, feed, current)
	if err != nil {
		// A failed check must not poison the cache: the next start should try
		// again rather than repeat a stale answer for a day.
		return Release{CheckedAt: now, Err: err}
	}
	release.CheckedAt = now
	_ = Save(cachePath, release)
	return release
}

// Fetch asks the feed once.
func Fetch(ctx context.Context, current string) (Release, error) {
	return fetchFrom(ctx, Feed, current)
}

func fetchFrom(ctx context.Context, feed, current string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// The version is already in every release archive's name; sending it lets
	// nobody learn anything they could not learn from a download count.
	req.Header.Set("User-Agent", "correlux/"+strings.TrimSpace(current))

	// The default transport honours HTTP_PROXY and HTTPS_PROXY, which is how
	// this works at all inside a corporate network.
	resp, err := (&http.Client{Timeout: Timeout}).Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("the release feed answered %s", resp.Status)
	}

	// A feed that answers with a megabyte of something else is not worth
	// reading into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Release{}, err
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, errors.New("the release feed answered something unreadable")
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return Release{}, errors.New("the release feed named no version")
	}
	return Release{Version: payload.TagName, URL: payload.HTMLURL}, nil
}

// Load reads the remembered answer. A missing or unreadable file is not an
// error worth reporting anywhere: it means the feed has to be asked.
func Load(path string) (Release, error) {
	if path == "" {
		return Release{}, errors.New("no cache path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal(data, &r); err != nil {
		return Release{}, err
	}
	return r, nil
}

// Save remembers the answer, and stays quiet when it cannot: a read-only home
// directory is a reason to ask the feed again tomorrow, not a reason to
// interrupt anybody.
func Save(path string, r Release) error {
	if path == "" {
		return errors.New("no cache path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Newer reports whether latest is a later release than current.
//
// Only the numeric core is compared, and everything after it is ignored. That
// is deliberate: `go install` and a local build stamp versions like
// "v0.10.0-3-gabc1234" — three commits *after* the tag — which strict semantic
// versioning would rank *below* v0.10.0 and announce as an update to a version
// the user is already past. Ignoring the suffix costs a release candidate its
// notification and never tells anybody to install what they are running.
func Newer(current, latest string) bool {
	c, ok := core(current)
	if !ok {
		// "dev", or something nobody can rank. Nothing useful can be said.
		return false
	}
	l, ok := core(latest)
	if !ok {
		return false
	}
	for i := range c {
		switch {
		case l[i] > c[i]:
			return true
		case l[i] < c[i]:
			return false
		}
	}
	return false
}

// Rankable reports whether a version can be compared against a release at all.
// "dev", and anything else without a numeric core, cannot: there is nothing
// useful to say about it, and asking the feed would learn nothing.
func Rankable(version string) bool {
	_, ok := core(version)
	return ok
}

// core parses the major.minor.patch of a version, ignoring a leading "v" and
// anything after the third number.
func core(version string) ([3]int, bool) {
	var out [3]int
	v := strings.TrimSpace(version)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return out, false
	}
	// Cut the suffix a pre-release or a git describe adds.
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > len(out) {
		return out, false
	}
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		numbers = append(numbers, n)
	}
	// A version with fewer than three numbers keeps zeros for the rest: "v1"
	// and "v1.0.0" are the same release.
	copy(out[:], numbers)
	return out, true
}

package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewerRanksReleases(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
		why             string
	}{
		{"v0.10.0", "v0.11.0", true, "a later minor is an update"},
		{"v0.10.0", "v0.10.1", true, "a later patch is an update"},
		{"v0.10.0", "v1.0.0", true, "a later major is an update"},
		{"v0.10.0", "v0.10.0", false, "the version you are running is not an update"},
		{"v0.11.0", "v0.10.0", false, "an older release is never announced"},
		{"0.10.0", "v0.11.0", true, "the leading v is decoration"},
		{"dev", "v0.11.0", false, "a build with no version cannot be ranked"},
		{"v0.10.0", "", false, "a feed that named nothing says nothing"},
		// git describe stamps a local build three commits past the tag. It is
		// not behind v0.10.0, and telling somebody to install what they are
		// already past is worse than saying nothing.
		{"v0.10.0-3-gabc1234", "v0.10.0", false, "a build past the tag is not behind it"},
		{"v0.10.0-3-gabc1234", "v0.11.0", true, "and it is still behind the next release"},
		{"v0.11.0-rc.1", "v0.11.0", false, "a release candidate is not nagged into its own release"},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v — %s", c.current, c.latest, got, c.want, c.why)
		}
	}
}

func TestFetchReadsTheFeed(t *testing.T) {
	var gotAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.11.0",
			"html_url": "https://example.com/releases/v0.11.0",
		})
	}))
	defer srv.Close()

	release, err := fetchFrom(context.Background(), srv.URL, "v0.10.0")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if release.Version != "v0.11.0" {
		t.Errorf("version = %q, want the tag the feed named", release.Version)
	}
	if release.URL == "" {
		t.Error("the release page is where somebody goes next; it must come back")
	}
	if gotAgent != "correlux/v0.10.0" {
		t.Errorf("user agent = %q, want only the version this binary already prints", gotAgent)
	}
}

func TestFetchReportsAFeedThatWillNotAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := fetchFrom(context.Background(), srv.URL, "v0.10.0"); err == nil {
		t.Error("a refused feed must be reported, not read as 'up to date'")
	}
}

// TestAFreshAnswerIsNotAskedForAgain is the whole point of the cache: the feed
// is asked once a day, not once a start.
func TestAFreshAnswerIsNotAskedForAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	if err := Save(path, Release{
		Version: "v0.11.0", URL: "https://example.com", CheckedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9"})
	}))
	defer srv.Close()

	got := checkAgainst(context.Background(), srv.URL, path, "v0.10.0", false)
	if asked != 0 {
		t.Errorf("the feed was asked %d times, want none while the answer is fresh", asked)
	}
	if got.Version != "v0.11.0" {
		t.Errorf("version = %q, want the remembered answer", got.Version)
	}

	// And an explicit check ignores the cache, because somebody asked.
	got = checkAgainst(context.Background(), srv.URL, path, "v0.10.0", true)
	if asked != 1 {
		t.Errorf("the feed was asked %d times, want exactly one when forced", asked)
	}
	if got.Version != "v9.9.9" {
		t.Errorf("version = %q, want the fresh answer", got.Version)
	}
}

// TestAStaleAnswerIsRefreshedAndRemembered.
func TestAStaleAnswerIsRefreshedAndRemembered(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	if err := Save(path, Release{
		Version: "v0.10.0", CheckedAt: time.Now().Add(-2 * Interval),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.12.0"})
	}))
	defer srv.Close()

	if got := checkAgainst(context.Background(), srv.URL, path, "v0.10.0", false); got.Version != "v0.12.0" {
		t.Fatalf("version = %q, want the refreshed answer", got.Version)
	}
	saved, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Version != "v0.12.0" || saved.CheckedAt.IsZero() {
		t.Errorf("saved = %+v, want the new answer with the time it was learned", saved)
	}
}

// TestAFailedCheckDoesNotPoisonTheCache: a laptop on a train must not silence
// the check for the next twenty-four hours.
func TestAFailedCheckDoesNotPoisonTheCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), CacheFile)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.Close() // nothing is listening

	got := checkAgainst(context.Background(), srv.URL, path, "v0.10.0", false)
	if got.Err == nil {
		t.Error("a check that did not happen must say so")
	}
	if got.Version != "" {
		t.Errorf("version = %q, want nothing claimed", got.Version)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a failed check must not be written to the cache")
	}
}

package palette

import (
	"testing"
	"time"
)

func registry(cmds ...Command) *Registry {
	r := NewRegistry()
	r.Set(cmds)
	return r
}

func cmd(id, title string, keywords ...string) Command {
	return Command{ID: id, Title: title, Keywords: keywords, Enabled: true}
}

func titles(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Command.Title
	}
	return out
}

func TestEmptyQueryReturnsEverything(t *testing.T) {
	r := registry(cmd("a", "Switch cluster"), cmd("b", "Switch namespace"))
	if got := r.Search("", 0); len(got) != 2 {
		t.Fatalf("got %d matches, want 2", len(got))
	}
}

func TestWeightOrdersTheDefaultList(t *testing.T) {
	high := cmd("a", "Rarely used")
	high.Weight = 5
	low := cmd("b", "Frequently used")
	low.Weight = 100

	got := titles(registry(high, low).Search("", 0))
	if got[0] != "Frequently used" {
		t.Errorf("order = %v, want the heavier command first", got)
	}
}

func TestPrefixBeatsMidWordMatch(t *testing.T) {
	r := registry(
		cmd("a", "Reload kubeconfig from disk"),
		cmd("b", "Switch namespace"),
	)
	got := titles(r.Search("re", 0))
	if len(got) == 0 || got[0] != "Reload kubeconfig from disk" {
		t.Errorf("order = %v, want the prefix match first", got)
	}
}

func TestKeywordFindsCommandWithUnrelatedTitle(t *testing.T) {
	r := registry(cmd("a", "Switch namespace", "ns", "scope"))
	got := r.Search("ns", 0)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1 — a keyword must find the command", len(got))
	}
	if got[0].Command.ID != "a" {
		t.Errorf("matched %q", got[0].Command.ID)
	}
}

func TestFuzzySubsequenceMatches(t *testing.T) {
	r := registry(cmd("a", "Reload kubeconfig from disk"))
	if got := r.Search("rkfd", 0); len(got) != 1 {
		t.Fatalf("got %d matches, want a fuzzy subsequence hit", len(got))
	}
}

func TestMatchPositionsAreReturnedForHighlighting(t *testing.T) {
	r := registry(cmd("a", "Switch cluster"))
	got := r.Search("clu", 0)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if len(got[0].TitlePositions) != 3 {
		t.Errorf("positions = %v, want three highlighted runes", got[0].TitlePositions)
	}
}

func TestDisabledCommandsRankLast(t *testing.T) {
	enabled := cmd("a", "Restart application")
	disabled := cmd("b", "Restart deployment")
	disabled.Enabled = false

	got := titles(registry(disabled, enabled).Search("restart", 0))
	if len(got) != 2 {
		t.Fatalf("both commands must be listed so the UI can explain the disabled one, got %v", got)
	}
	if got[0] != "Restart application" {
		t.Errorf("order = %v, want the enabled command first", got)
	}
}

func TestUsageBiasesRanking(t *testing.T) {
	now := time.Now()
	r := registry(cmd("a", "Scale workload"), cmd("b", "Scale deployment"))
	r.WithClock(func() time.Time { return now })

	baseline := r.Search("scale", 0)
	if len(baseline) != 2 {
		t.Fatalf("got %d matches, want 2", len(baseline))
	}
	runnerUp := baseline[1].Command

	r.MarkUsed(runnerUp.ID)
	got := titles(r.Search("scale", 0))
	if got[0] != runnerUp.Title {
		t.Errorf("order = %v, want the command this user actually ran (%q) first", got, runnerUp.Title)
	}
}

func TestRecencyDecays(t *testing.T) {
	now := time.Now()
	current := now
	r := registry(cmd("a", "Scale workload"), cmd("b", "Scale deployment"))
	r.WithClock(func() time.Time { return current })

	baseline := r.Search("scale", 0)
	leader, runnerUp := baseline[0].Command, baseline[1].Command

	r.MarkUsed(runnerUp.ID)
	if got := titles(r.Search("scale", 0)); got[0] != runnerUp.Title {
		t.Fatalf("order = %v, want the just-used command first", got)
	}

	// A day later a fresh use of the other command must win: recency decays,
	// where raw frequency alone would keep a stale winner on top forever.
	current = now.Add(24 * time.Hour)
	r.MarkUsed(leader.ID)
	if got := titles(r.Search("scale", 0)); got[0] != leader.Title {
		t.Errorf("order = %v, want the recently used command (%q) first", got, leader.Title)
	}
}

func TestLimitTruncates(t *testing.T) {
	r := registry(cmd("a", "One"), cmd("b", "Two"), cmd("c", "Three"))
	if got := r.Search("", 2); len(got) != 2 {
		t.Errorf("got %d matches, want 2", len(got))
	}
}

func TestNoMatchReturnsNothing(t *testing.T) {
	r := registry(cmd("a", "Switch cluster"))
	if got := r.Search("zzzzz", 0); len(got) != 0 {
		t.Errorf("got %v, want no matches", titles(got))
	}
}

func TestRankingIsStableForEqualScores(t *testing.T) {
	r := registry(cmd("a", "Beta"), cmd("b", "Alpha"))
	first := titles(r.Search("", 0))
	for i := 0; i < 20; i++ {
		if got := titles(r.Search("", 0)); got[0] != first[0] || got[1] != first[1] {
			t.Fatalf("ranking is not deterministic: %v then %v", first, got)
		}
	}
}

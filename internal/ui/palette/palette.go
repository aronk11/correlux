// Package palette implements Correlux's command registry and its fuzzy ranking.
//
// The palette is the discoverability layer of the product: every action must be
// reachable by typing a word a human would think of, not a memorised key. The
// package is deliberately free of any UI dependency so ranking can be tested
// exhaustively and reused by other front ends (search, help, "what can I do
// here?").
package palette

import (
	"sort"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
)

// ActionID identifies what a command does. The palette never executes anything
// itself; the application layer maps IDs to behaviour.
type ActionID string

// Command is one entry in the palette.
type Command struct {
	// ID is the stable identifier of this entry (unique per registry).
	ID string
	// Action is what to perform when the entry is chosen.
	Action ActionID
	// Arg is an optional parameter for the action, e.g. a context name.
	Arg string
	// Title is the primary label, matched against and highlighted.
	Title string
	// Subtitle adds context, e.g. the current value of a setting.
	Subtitle string
	// Category groups entries in the list ("Navigate", "Cluster", "View").
	Category string
	// Keywords are alternative words users may type ("ctx", "namespace", "ns").
	Keywords []string
	// Shortcut is the key that triggers the action directly, if any.
	Shortcut string
	// Weight biases ranking for commands that should surface early. Higher
	// wins; the default is 0.
	Weight int
	// Enabled reports whether the command can run right now. Disabled commands
	// are still listed (so the UI can explain why) but rank last.
	Enabled bool
	// DisabledReason explains why Enabled is false.
	DisabledReason string
}

// Match is a command that matched a query, with the information needed to
// render it.
type Match struct {
	Command Command
	// Score is the ranking score; higher is better.
	Score int
	// TitlePositions are indices of matched runes in Title, for highlighting.
	TitlePositions []int
}

// Clock supplies the current time; injectable so ranking is deterministic in
// tests.
type Clock func() time.Time

// Registry holds the available commands and remembers which ones were used.
type Registry struct {
	commands []Command
	usage    map[string]usage
	now      Clock
}

type usage struct {
	count int
	last  time.Time
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{usage: map[string]usage{}, now: time.Now}
}

// WithClock overrides the clock used for recency scoring.
func (r *Registry) WithClock(c Clock) *Registry {
	r.now = c
	return r
}

// Set replaces the command set. The palette is rebuilt whenever the world
// changes (a different context, a new namespace list), so replacement is the
// normal path and mutation is not offered.
func (r *Registry) Set(commands []Command) {
	r.commands = commands
}

// Commands returns the registered commands in registration order.
func (r *Registry) Commands() []Command { return r.commands }

// MarkUsed records that a command was executed, which biases later ranking
// towards what this user actually does.
func (r *Registry) MarkUsed(id string) {
	u := r.usage[id]
	u.count++
	u.last = r.now()
	r.usage[id] = u
}

// Scoring weights. Fuzzy scores from the matcher are on the order of tens to a
// few hundred; these constants are chosen to nudge, not to dominate.
const (
	titleMatchBonus  = 120
	prefixBonus      = 90
	wordStartBonus   = 45
	keywordMatchBase = 40
	subtitleBase     = 20
	disabledPenalty  = -1000
	recencyMax       = 60
	frequencyPerUse  = 8
	frequencyMax     = 40
	recencyHalfLife  = 30 * time.Minute
)

// Search ranks commands against a query. An empty query returns every command
// in a stable, useful order: enabled first, then weight, then recent usage.
//
// limit <= 0 means "no limit".
func (r *Registry) Search(query string, limit int) []Match {
	query = strings.TrimSpace(query)
	matches := make([]Match, 0, len(r.commands))

	if query == "" {
		for _, c := range r.commands {
			matches = append(matches, Match{Command: c, Score: r.staticScore(c)})
		}
	} else {
		matches = r.searchQuery(query)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Command.Title < matches[j].Command.Title
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func (r *Registry) searchQuery(query string) []Match {
	lowerQuery := strings.ToLower(query)

	titles := make([]string, len(r.commands))
	for i, c := range r.commands {
		titles[i] = c.Title
	}

	matched := make(map[int]Match, len(r.commands))
	for _, res := range fuzzy.Find(query, titles) {
		c := r.commands[res.Index]
		score := titleMatchBonus + res.Score + r.staticScore(c)
		lowerTitle := strings.ToLower(c.Title)
		switch {
		case strings.HasPrefix(lowerTitle, lowerQuery):
			score += prefixBonus
		case containsWordStart(lowerTitle, lowerQuery):
			score += wordStartBonus
		}
		matched[res.Index] = Match{Command: c, Score: score, TitlePositions: res.MatchedIndexes}
	}

	// Commands whose title did not match may still match on a keyword or their
	// subtitle — that is how "ns" finds "Switch namespace".
	for i, c := range r.commands {
		if _, ok := matched[i]; ok {
			continue
		}
		if score, ok := r.secondaryScore(c, lowerQuery); ok {
			matched[i] = Match{Command: c, Score: score + r.staticScore(c)}
		}
	}

	out := make([]Match, 0, len(matched))
	for _, m := range matched {
		out = append(out, m)
	}
	return out
}

func (r *Registry) secondaryScore(c Command, lowerQuery string) (int, bool) {
	best := 0
	found := false
	for _, kw := range c.Keywords {
		kw = strings.ToLower(kw)
		switch {
		case kw == lowerQuery:
			best = maxInt(best, keywordMatchBase+40)
			found = true
		case strings.HasPrefix(kw, lowerQuery):
			best = maxInt(best, keywordMatchBase+20)
			found = true
		case strings.Contains(kw, lowerQuery):
			best = maxInt(best, keywordMatchBase)
			found = true
		}
	}
	if sub := strings.ToLower(c.Subtitle); sub != "" && strings.Contains(sub, lowerQuery) {
		best = maxInt(best, subtitleBase)
		found = true
	}
	if cat := strings.ToLower(c.Category); cat != "" && strings.HasPrefix(cat, lowerQuery) {
		best = maxInt(best, subtitleBase)
		found = true
	}
	return best, found
}

// staticScore is the query-independent part of a command's rank.
func (r *Registry) staticScore(c Command) int {
	score := c.Weight
	if !c.Enabled {
		score += disabledPenalty
	}
	u, ok := r.usage[c.ID]
	if !ok {
		return score
	}
	score += minInt(u.count*frequencyPerUse, frequencyMax)
	if !u.last.IsZero() {
		age := r.now().Sub(u.last)
		if age < 0 {
			age = 0
		}
		// Linear decay over two half-lives keeps the arithmetic obvious and
		// avoids floating point in the hot path.
		decay := int(age / (recencyHalfLife / recencyMax))
		if bonus := recencyMax - decay; bonus > 0 {
			score += bonus
		}
	}
	return score
}

// containsWordStart reports whether query starts a word inside s.
func containsWordStart(s, query string) bool {
	for i := 0; i+len(query) <= len(s); i++ {
		if i > 0 && !isSeparator(s[i-1]) {
			continue
		}
		if s[i:i+len(query)] == query {
			return true
		}
	}
	return false
}

func isSeparator(b byte) bool {
	switch b {
	case ' ', '-', '_', '/', '.', ':':
		return true
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

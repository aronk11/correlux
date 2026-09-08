// Package query parses the filter people type over a list of rows.
//
// The filter used to be fuzzy text over the whole row, which is the right
// default and a poor answer to the questions an incident actually asks:
// "which of these restarted more than five times", "what is younger than an
// hour", "everything that is not healthy". Those are comparisons, and no
// amount of fuzzy matching over "12" finds the rows where that number is
// greater than five.
//
// So a term that names a column is a comparison, and everything else is still
// fuzzy text. `restarts>5 payments` reads as it looks: the restarts column
// above five, and "payments" somewhere in the row.
//
// Nothing here knows what a Pod is. It works on the columns a view already
// draws, which is what lets the same syntax serve the application dashboard,
// any resource table the API server prints, and the fleet's merged table —
// including a custom resource nobody wrote code for.
package query

import (
	"strconv"
	"strings"
	"time"
)

// Operator is how a term compares.
type Operator int

const (
	// OpContains is `field=value`: the forgiving default, because somebody
	// typing status=crash means "has crash in it".
	//
	// `=` is the only spelling. A colon would be the other obvious one, and a
	// colon is also what is in every URL somebody might paste into the filter.
	OpContains Operator = iota
	// OpEquals is `field==value`, for when contains is too generous.
	OpEquals
	// OpNotContains is `field!=value`.
	OpNotContains
	OpGreater
	OpGreaterEqual
	OpLess
	OpLessEqual
)

// Term is one condition. A term with no Field is free text matched fuzzily by
// the caller, which is what keeps the old behaviour exactly as it was.
type Term struct {
	Field string
	Op    Operator
	Value string
	// Negated marks `!text`: free text the row must *not* contain.
	Negated bool
}

// Typed reports whether the term names a column.
func (t Term) Typed() bool { return t.Field != "" }

// Query is a parsed filter: the typed terms, and the free text left over.
type Query struct {
	Terms []Term
	// Text is every word that named no column, joined back together, for the
	// caller's fuzzy pass.
	Text string
	// Excluded is the free text that was negated with `!`.
	Excluded []string
}

// Empty reports whether this query filters nothing.
func (q Query) Empty() bool { return len(q.Terms) == 0 && q.Text == "" && len(q.Excluded) == 0 }

// Typed reports whether anything in the query compares a column, which is what
// the caller needs to know before it bothers looking up column names.
func (q Query) Typed() bool {
	for _, t := range q.Terms {
		if t.Typed() {
			return true
		}
	}
	return false
}

// Parse reads a filter string. It never fails: anything it cannot make sense of
// stays free text, because a filter that rejected what somebody typed
// mid-incident would be worse than one that matched loosely.
func Parse(input string) Query {
	var q Query
	words := strings.Fields(input)
	text := make([]string, 0, len(words))

	for _, word := range words {
		if term, ok := parseTerm(word); ok {
			q.Terms = append(q.Terms, term)
			continue
		}
		if strings.HasPrefix(word, "!") && len(word) > 1 {
			q.Excluded = append(q.Excluded, strings.ToLower(word[1:]))
			continue
		}
		text = append(text, word)
	}
	q.Text = strings.Join(text, " ")
	return q
}

// parseTerm recognises one `field<op>value` word.
func parseTerm(word string) (Term, bool) {
	// The two-character operators are tried first, or `>=` would read as `>`
	// with a value beginning "=".
	for _, candidate := range []struct {
		token string
		op    Operator
	}{
		{">=", OpGreaterEqual},
		{"<=", OpLessEqual},
		{"!=", OpNotContains},
		{"==", OpEquals},
		{">", OpGreater},
		{"<", OpLess},
		{"=", OpContains},
	} {
		field, value, found := strings.Cut(word, candidate.token)
		if !found || field == "" || value == "" {
			continue
		}
		// A word like "https://host" is not a filter term, and neither is
		// anything else whose field half is not a plain name.
		if !plainName(field) {
			return Term{}, false
		}
		return Term{Field: strings.ToLower(field), Op: candidate.op, Value: value}, true
	}
	return Term{}, false
}

// plainName reports whether s could be a column name: letters, digits, dashes
// and dots, nothing else.
func plainName(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return s != ""
}

// aliases are the names people reach for that no API server prints.
var aliases = map[string][]string{
	"ns":     {"namespace"},
	"app":    {"application", "name"},
	"name":   {"name", "application"},
	"health": {"health", "status"},
	"status": {"status", "health"},
	"mem":    {"memory"},
	"node":   {"node"},
}

// Column finds the column a field names: an exact match, then an alias, then a
// unique prefix. It returns the index and whether the name was unambiguous.
//
// A prefix is allowed because "rest" is what somebody types when they mean
// restarts, and refused when it is ambiguous, because guessing between two
// columns is how a filter lies.
func Column(columns []string, field string) (index int, ok bool, ambiguous []string) {
	lower := make([]string, len(columns))
	for i, c := range columns {
		lower[i] = strings.ToLower(strings.TrimSpace(c))
	}

	for i, c := range lower {
		if c == field {
			return i, true, nil
		}
	}
	for _, alias := range aliases[field] {
		for i, c := range lower {
			if c == alias {
				return i, true, nil
			}
		}
	}

	var hits []int
	for i, c := range lower {
		if strings.HasPrefix(c, field) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return -1, false, nil
	case 1:
		return hits[0], true, nil
	default:
		names := make([]string, 0, len(hits))
		for _, i := range hits {
			names = append(names, columns[i])
		}
		return -1, false, names
	}
}

// Match reports whether one row satisfies every typed term. Free text is the
// caller's business: it owns the fuzzy matcher.
func (q Query) Match(columns, cells []string) bool {
	for _, term := range q.Terms {
		if !term.Typed() {
			continue
		}
		index, ok, _ := Column(columns, term.Field)
		if !ok || index >= len(cells) {
			// An unknown column matches nothing, and the caller says so on
			// screen rather than quietly showing an empty list.
			return false
		}
		if !term.match(columns[index], cells[index]) {
			return false
		}
	}
	for _, excluded := range q.Excluded {
		if strings.Contains(strings.ToLower(strings.Join(cells, " ")), excluded) {
			return false
		}
	}
	return true
}

// Problems names the terms this set of columns cannot answer, in the words the
// user typed. A filter that silently matched nothing would look like a cluster
// with nothing in it.
func (q Query) Problems(columns []string) []string {
	var out []string
	for _, term := range q.Terms {
		if !term.Typed() {
			continue
		}
		_, ok, ambiguous := Column(columns, term.Field)
		switch {
		case ok:
		case len(ambiguous) > 0:
			out = append(out, term.Field+" could be "+strings.Join(ambiguous, " or "))
		default:
			out = append(out, "no "+term.Field+" column here")
		}
	}
	return out
}

// match compares one cell.
func (t Term) match(column, cell string) bool {
	cell = strings.TrimSpace(cell)
	switch t.Op {
	case OpContains:
		return strings.Contains(strings.ToLower(cell), strings.ToLower(t.Value))
	case OpEquals:
		return strings.EqualFold(cell, t.Value)
	case OpNotContains:
		return !strings.Contains(strings.ToLower(cell), strings.ToLower(t.Value))
	}

	got, want, ok := numbers(column, cell, t.Value)
	if !ok {
		// Nothing numeric to compare. Saying "no" is the only honest answer:
		// a text cell is neither greater nor smaller than an hour.
		return false
	}
	switch t.Op {
	case OpGreater:
		return got > want
	case OpGreaterEqual:
		return got >= want
	case OpLess:
		return got < want
	case OpLessEqual:
		return got <= want
	}
	return false
}

// numbers renders a cell and a typed value as comparable numbers.
//
// The column decides how: an age column is a duration, and everything else is a
// number, optionally with the suffixes Kubernetes prints — 500m, 1Gi, 2k. That
// rule is worth stating out loud, because "500m" means five hundred minutes in
// one column and half a CPU in another, and a filter that guessed would be
// wrong half the time.
func numbers(column, cell, value string) (got, want float64, ok bool) {
	if isAgeColumn(column) {
		g, gok := parseAge(cell)
		w, wok := parseAge(value)
		return g.Seconds(), w.Seconds(), gok && wok
	}
	g, gok := parseNumber(cell)
	w, wok := parseNumber(value)
	return g, w, gok && wok
}

func isAgeColumn(column string) bool {
	c := strings.ToLower(strings.TrimSpace(column))
	return c == "age" || strings.HasSuffix(c, " age") || c == "last seen" || c == "first seen"
}

// parseNumber reads the number a cell leads with, so "3/5" compares as 3 and
// "12 (2 down)" as 12, and understands the suffixes Kubernetes quantities use.
func parseNumber(cell string) (float64, bool) {
	s := strings.TrimSpace(cell)
	if s == "" || s == "—" || s == "-" {
		return 0, false
	}
	// A ready count is a fraction, and the number people mean is the first
	// one: `pods<1` is "nothing is up".
	if before, _, found := strings.Cut(s, "/"); found {
		s = before
	}

	digits := strings.Builder{}
	rest := ""
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || (r == '-' && i == 0) {
			digits.WriteRune(r)
			continue
		}
		rest = s[i:]
		break
	}
	n, err := strconv.ParseFloat(digits.String(), 64)
	if err != nil {
		return 0, false
	}
	return n * suffixScale(rest), true
}

// suffixScale is the multiplier of a Kubernetes quantity suffix. An unknown
// suffix scales by one rather than failing: "12 pods" is twelve.
func suffixScale(rest string) float64 {
	suffix := ""
	if fields := strings.Fields(rest); len(fields) > 0 {
		suffix = fields[0]
	}
	switch suffix {
	case "n":
		return 1e-9
	case "u", "µ":
		return 1e-6
	case "m":
		return 1e-3
	case "k":
		return 1e3
	case "M":
		return 1e6
	case "G":
		return 1e9
	case "T":
		return 1e12
	case "P":
		return 1e15
	case "Ki":
		return 1 << 10
	case "Mi":
		return 1 << 20
	case "Gi":
		return 1 << 30
	case "Ti":
		return 1 << 40
	case "Pi":
		return 1 << 50
	default:
		return 1
	}
}

// parseAge reads the age format Kubernetes prints — 5d, 2d4h, 13m, 45s — and
// the one people type, which is the same thing.
func parseAge(s string) (time.Duration, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "—" || s == "-" {
		return 0, false
	}

	var total time.Duration
	digits := strings.Builder{}
	any := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		n, err := strconv.Atoi(digits.String())
		if err != nil {
			return 0, false
		}
		digits.Reset()
		unit, ok := ageUnit(r)
		if !ok {
			return 0, false
		}
		total += time.Duration(n) * unit
		any = true
	}
	if digits.Len() > 0 {
		// A bare number is seconds, which is what `age>30` should mean the
		// same way `kubectl` prints one.
		n, err := strconv.Atoi(digits.String())
		if err != nil {
			return 0, false
		}
		total += time.Duration(n) * time.Second
		any = true
	}
	return total, any
}

func ageUnit(r rune) (time.Duration, bool) {
	switch r {
	case 's':
		return time.Second, true
	case 'm':
		return time.Minute, true
	case 'h':
		return time.Hour, true
	case 'd':
		return 24 * time.Hour, true
	case 'y':
		return 365 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

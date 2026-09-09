package query

import (
	"sort"
	"strings"
)

// Sorting a table is the other half of the question the filter answers.
// `restarts>5` is "show me the ones that keep dying"; ordering by restarts is
// "show me the worst one first", and during an incident that is usually the
// faster of the two, because it needs no threshold guessed in advance.
//
// It reuses the filter's own reading of a cell — an age column is a duration,
// every other one is a number with the suffixes Kubernetes prints — so a
// column sorts the way it compares. A table where `age<1h` and ordering by age
// disagreed about what an age is would be worse than one that could not sort
// at all.

// Comparable reports whether a cell carries a value this column can order
// numerically. A cell that does not — a name, a status word, an empty cell —
// is still sortable as text; what this answers is whether the *numeric* rule
// applies, so the caller can keep the cells with no value out of the way.
func Comparable(column, cell string) bool {
	_, ok := value(column, cell)
	return ok
}

// Missing reports a cell with nothing in it to order by: empty, or one of the
// dashes every Kubernetes printer uses for "not set". These sort last whichever
// direction the table is in — an unset value is not a small one, and a column
// sorted worst-first should not open on a screen of blanks.
func Missing(cell string) bool {
	s := strings.TrimSpace(cell)
	return s == "" || s == "—" || s == "-" || s == "<none>" || s == "<unknown>"
}

// Compare orders two cells of the same column: negative when a sorts before b,
// zero when they are equivalent, positive when a sorts after b.
//
// Numeric where the column reads numerically and both cells carry a value,
// case-insensitive text everywhere else. A cell that parses next to one that
// does not falls back to text rather than guessing which of the two is bigger.
func Compare(column, a, b string) int {
	if x, ok := value(column, a); ok {
		if y, ok := value(column, b); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(
		strings.ToLower(strings.TrimSpace(a)),
		strings.ToLower(strings.TrimSpace(b)),
	)
}

// value reads one cell as a number, the way the column's own type says to.
func value(column, cell string) (float64, bool) {
	if isAgeColumn(column) {
		d, ok := parseAge(cell)
		return d.Seconds(), ok
	}
	return parseNumber(cell)
}

// Order returns the row indices of rows sorted by one column.
//
// The sort is stable, so rows the column cannot tell apart keep the order the
// server gave them: a table ordered by status must not reshuffle its equal
// rows every time it reloads. Rows with nothing in that column go last in both
// directions (see Missing), and `column` naming no heading returns the rows
// untouched — a sort the table cannot honour must not silently become a
// different one.
func Order(columns []string, rows [][]string, column string, desc bool) []int {
	out := make([]int, len(rows))
	for i := range out {
		out[i] = i
	}
	index, ok, _ := Column(columns, strings.ToLower(strings.TrimSpace(column)))
	if !ok {
		return out
	}

	cell := func(row int) string {
		if index < len(rows[row]) {
			return rows[row][index]
		}
		return ""
	}
	heading := columns[index]

	// Each cell is read once rather than once per comparison: a resource table
	// holds thousands of rows, and parsing "1h30m" inside the comparator would
	// put that work in the render path n log n times over.
	keys := make([]sortKey, len(rows))
	for i := range rows {
		text := cell(i)
		keys[i] = sortKey{text: strings.ToLower(strings.TrimSpace(text)), missing: Missing(text)}
		keys[i].number, keys[i].numeric = value(heading, text)
	}

	sort.SliceStable(out, func(a, b int) bool { return less(keys[out[a]], keys[out[b]], desc) })
	return out
}

// sortKey is one cell already read: the work every comparison would otherwise
// repeat.
type sortKey struct {
	text    string
	number  float64
	numeric bool
	missing bool
}

// less reports whether a sorts before b, with the direction applied and cells
// with no value held at the end regardless of it.
func less(a, b sortKey, desc bool) bool {
	switch {
	case a.missing && b.missing:
		return false
	case a.missing:
		return false
	case b.missing:
		return true
	}
	if desc {
		return compareKeys(a, b) > 0
	}
	return compareKeys(a, b) < 0
}

func compareKeys(a, b sortKey) int {
	if a.numeric && b.numeric {
		switch {
		case a.number < b.number:
			return -1
		case a.number > b.number:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a.text, b.text)
}

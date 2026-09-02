// Package diff compares two versions of a document, line by line.
//
// It exists so that applying an edit can show what the edit *is*. "Apply your
// changes?" is a question nobody can answer honestly; "these four lines change,
// and one of them is the image tag" is.
//
// The algorithm is the textbook longest-common-subsequence one, bounded so a
// pathological pair of documents costs memory that is known in advance rather
// than whatever the inputs happen to demand.
package diff

// Op is what happened to a line.
type Op int

const (
	// Keep means the line is in both documents.
	Keep Op = iota
	// Remove means the line was in the original only.
	Remove
	// Add means the line is in the edited version only.
	Add
)

// Line is one line of the comparison.
type Line struct {
	Op   Op
	Text string
	// Number is the line's position in the document it belongs to: the
	// original for Keep and Remove, the edited version for Add.
	Number int
}

// maxLines bounds the comparison. A Kubernetes object that runs to ten thousand
// lines is a generated monster, and a diff of it is not what anybody is
// reading; past this, the comparison degrades to "everything changed" rather
// than allocating a hundred million cells.
const maxLines = 5000

// Summary counts what changed.
type Summary struct {
	Added   int
	Removed int
}

// Changed reports whether anything changed at all.
func (s Summary) Changed() bool { return s.Added > 0 || s.Removed > 0 }

// Lines compares two documents.
func Lines(before, after []string) []Line {
	if len(before) > maxLines || len(after) > maxLines {
		return wholesale(before, after)
	}

	// table[i][j] is the length of the longest common subsequence of
	// before[i:] and after[j:].
	table := make([][]int, len(before)+1)
	for i := range table {
		table[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if before[i] == after[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
				continue
			}
			table[i][j] = table[i][j+1]
		}
	}

	out := make([]Line, 0, len(before)+len(after))
	i, j := 0, 0
	for i < len(before) && j < len(after) {
		switch {
		case before[i] == after[j]:
			out = append(out, Line{Op: Keep, Text: before[i], Number: i + 1})
			i, j = i+1, j+1
		case table[i+1][j] >= table[i][j+1]:
			out = append(out, Line{Op: Remove, Text: before[i], Number: i + 1})
			i++
		default:
			out = append(out, Line{Op: Add, Text: after[j], Number: j + 1})
			j++
		}
	}
	for ; i < len(before); i++ {
		out = append(out, Line{Op: Remove, Text: before[i], Number: i + 1})
	}
	for ; j < len(after); j++ {
		out = append(out, Line{Op: Add, Text: after[j], Number: j + 1})
	}
	return out
}

// wholesale is the answer for documents too large to compare properly: every
// line removed and every line added. It is honest — it just says less.
func wholesale(before, after []string) []Line {
	out := make([]Line, 0, len(before)+len(after))
	for i, line := range before {
		out = append(out, Line{Op: Remove, Text: line, Number: i + 1})
	}
	for i, line := range after {
		out = append(out, Line{Op: Add, Text: line, Number: i + 1})
	}
	return out
}

// Summarise counts the changes.
func Summarise(lines []Line) Summary {
	var s Summary
	for _, l := range lines {
		switch l.Op {
		case Add:
			s.Added++
		case Remove:
			s.Removed++
		case Keep:
		}
	}
	return s
}

// Hunks keeps the changed lines and the given number of unchanged lines around
// them, which is what makes a diff readable in a small window.
func Hunks(lines []Line, context int) []Line {
	if context < 0 {
		context = 0
	}
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op == Keep {
			continue
		}
		for j := max(i-context, 0); j <= min(i+context, len(lines)-1); j++ {
			keep[j] = true
		}
	}

	out := make([]Line, 0, len(lines))
	for i, l := range lines {
		if keep[i] {
			out = append(out, l)
		}
	}
	return out
}

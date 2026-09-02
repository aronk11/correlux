package diff

import (
	"strings"
	"testing"
)

// render prints a comparison the way a patch does, so a test failure is
// readable.
func render(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		switch l.Op {
		case Add:
			b.WriteString("+" + l.Text + "\n")
		case Remove:
			b.WriteString("-" + l.Text + "\n")
		case Keep:
			b.WriteString(" " + l.Text + "\n")
		}
	}
	return b.String()
}

func split(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

func TestOneChangedLineIsOneRemovalAndOneAddition(t *testing.T) {
	before := split("kind: Deployment\nspec:\n  replicas: 3\n  paused: false\n")
	after := split("kind: Deployment\nspec:\n  replicas: 5\n  paused: false\n")

	lines := Lines(before, after)
	if got := Summarise(lines); got != (Summary{Added: 1, Removed: 1}) {
		t.Errorf("summary = %+v, want one line changed:\n%s", got, render(lines))
	}
	if !strings.Contains(render(lines), "-  replicas: 3\n+  replicas: 5") {
		t.Errorf("the changed line must be shown as a pair:\n%s", render(lines))
	}
}

func TestAnUnchangedDocumentHasNothingToShow(t *testing.T) {
	doc := split("a\nb\nc\n")
	if got := Summarise(Lines(doc, doc)); got.Changed() {
		t.Errorf("summary = %+v, want no change", got)
	}
}

func TestAddedAndRemovedBlocksAreFoundInTheMiddle(t *testing.T) {
	before := split("a\nb\nc\nd\n")
	after := split("a\nc\nx\nd\ne\n")

	lines := Lines(before, after)
	summary := Summarise(lines)
	if summary.Removed != 1 || summary.Added != 2 {
		t.Errorf("summary = %+v, want b removed and x, e added:\n%s", summary, render(lines))
	}
	// The unchanged lines must survive in order: a diff that reorders a
	// document is worse than no diff at all.
	var kept []string
	for _, l := range lines {
		if l.Op == Keep {
			kept = append(kept, l.Text)
		}
	}
	if strings.Join(kept, "") != "acd" {
		t.Errorf("kept %v, want a, c, d in order", kept)
	}
}

func TestHunksKeepTheContextAroundAChange(t *testing.T) {
	before := split("1\n2\n3\n4\n5\n6\n7\n8\n9\n")
	after := split("1\n2\n3\n4\nX\n6\n7\n8\n9\n")

	hunks := Hunks(Lines(before, after), 1)
	if len(hunks) != 4 {
		t.Fatalf("expected the change plus one line either side, got:\n%s", render(hunks))
	}
	if hunks[0].Text != "4" || hunks[len(hunks)-1].Text != "6" {
		t.Errorf("the context is wrong:\n%s", render(hunks))
	}
}

func TestHunksWithoutContextAreTheChangesAlone(t *testing.T) {
	lines := Lines(split("a\nb\n"), split("a\nc\n"))
	hunks := Hunks(lines, 0)
	for _, l := range hunks {
		if l.Op == Keep {
			t.Errorf("no unchanged line belongs here:\n%s", render(hunks))
		}
	}
}

func TestAnEmptyDocumentOnEitherSide(t *testing.T) {
	added := Summarise(Lines(nil, split("a\nb\n")))
	if added.Added != 2 || added.Removed != 0 {
		t.Errorf("creating a document is all additions, got %+v", added)
	}
	removed := Summarise(Lines(split("a\nb\n"), nil))
	if removed.Removed != 2 || removed.Added != 0 {
		t.Errorf("emptying a document is all removals, got %+v", removed)
	}
}

func TestAnEnormousDocumentDegradesRatherThanAllocating(t *testing.T) {
	// Past the bound the comparison says "everything changed", which is less
	// useful and still true; what it must not do is allocate a table of
	// twenty-five million cells.
	before := make([]string, maxLines+1)
	after := make([]string, maxLines+1)
	for i := range before {
		before[i] = "line"
		after[i] = "line"
	}

	summary := Summarise(Lines(before, after))
	if summary.Added != len(after) || summary.Removed != len(before) {
		t.Errorf("summary = %+v, want the wholesale answer", summary)
	}
}

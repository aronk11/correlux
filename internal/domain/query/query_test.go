package query

import (
	"strings"
	"testing"
)

// dashboard is the application dashboard's own columns, which is the list most
// of these questions are asked of.
var dashboard = []string{
	"Status", "Application", "Namespace", "Pods", "Workloads",
	"Managed by", "Restarts", "Age", "Detail",
}

func row(cells ...string) []string { return cells }

// app builds a dashboard row.
func app(status, name, ns, pods, restarts, age string) []string {
	return row(status, name, ns, pods, "Deployment", "Helm", restarts, age, "")
}

func TestFreeTextStaysFreeText(t *testing.T) {
	q := Parse("payments crash")
	if q.Typed() {
		t.Error("words that name no column must not become comparisons")
	}
	if q.Text != "payments crash" {
		t.Errorf("text = %q, want both words handed back for the fuzzy pass", q.Text)
	}
}

// TestTheQuestionsAnIncidentAsks is the feature, in the words somebody types.
func TestTheQuestionsAnIncidentAsks(t *testing.T) {
	rows := [][]string{
		app("✗ down", "payments", "shop", "0/3", "17", "2d4h"),
		app("! degraded", "checkout", "shop", "2/3", "3", "45m"),
		app("✓ healthy", "billing", "finance", "3/3", "0", "13d"),
	}
	names := func(q Query) []string {
		var out []string
		for _, cells := range rows {
			if q.Match(dashboard, cells) {
				out = append(out, cells[1])
			}
		}
		return out
	}

	cases := []struct {
		filter string
		want   string
		why    string
	}{
		{"restarts>5", "payments", "the one that keeps dying"},
		{"restarts>=3", "payments,checkout", "and the one that dies less"},
		{"age<1h", "checkout", "what is new since this started"},
		{"age>1d", "payments,billing", "and what has been there all along"},
		{"health=down", "payments", "a status is text, and contains is what people mean"},
		{"ns=shop", "payments,checkout", "an alias for the column nobody spells out"},
		{"pods<1", "payments", "nothing is up: the first number of 0/3"},
		{"restarts>0 ns=shop", "payments,checkout", "terms are and-ed"},
		{"health!=healthy", "payments,checkout", "everything that is not fine"},
		{"rest>5", "payments", "an unambiguous prefix is enough"},
	}
	for _, c := range cases {
		got := strings.Join(names(Parse(c.filter)), ",")
		if got != c.want {
			t.Errorf("%q matched %q, want %q — %s", c.filter, got, c.want, c.why)
		}
	}
}

// TestAnUnknownColumnIsSaidOutLoud: a filter that silently matches nothing
// looks exactly like a cluster with nothing in it.
func TestAnUnknownColumnIsSaidOutLoud(t *testing.T) {
	problems := Parse("cpu>500m").Problems(dashboard)
	if len(problems) != 1 || !strings.Contains(problems[0], "no cpu column") {
		t.Fatalf("problems = %v, want the missing column named", problems)
	}
	if Parse("cpu>500m").Match(dashboard, app("✓", "billing", "finance", "3/3", "0", "13d")) {
		t.Error("a term nobody can answer must not quietly match everything")
	}
}

// TestAnAmbiguousPrefixRefusesToGuess: guessing between two columns is how a
// filter lies.
func TestAnAmbiguousPrefixRefusesToGuess(t *testing.T) {
	columns := []string{"Name", "Ready", "Restarts", "Age"}
	problems := Parse("re>2").Problems(columns)
	if len(problems) != 1 || !strings.Contains(problems[0], "could be") {
		t.Fatalf("problems = %v, want both candidates named", problems)
	}
	if !strings.Contains(problems[0], "Ready") || !strings.Contains(problems[0], "Restarts") {
		t.Errorf("problems = %v, want the columns spelled out", problems)
	}
}

// TestAgeIsADurationAndEverythingElseIsANumber is the rule that keeps "500m"
// from meaning two things at once.
func TestAgeIsADurationAndEverythingElseIsANumber(t *testing.T) {
	columns := []string{"Name", "CPU", "Age"}
	cells := row("api", "500m", "30m")

	if !Parse("cpu<1").Match(columns, cells) {
		t.Error("500m in a CPU column is half a core, and less than one")
	}
	if !Parse("age<1h").Match(columns, cells) {
		t.Error("30m in an age column is half an hour, and less than one")
	}
	if Parse("age>1h").Match(columns, cells) {
		t.Error("and it is not more than one")
	}
	if !Parse("cpu>0.4").Match(columns, cells) {
		t.Error("a fractional value must compare")
	}
}

func TestQuantitySuffixes(t *testing.T) {
	columns := []string{"Name", "Memory"}
	if !Parse("memory>1Gi").Match(columns, row("api", "2Gi")) {
		t.Error("2Gi is more than 1Gi")
	}
	if Parse("memory>1Gi").Match(columns, row("api", "512Mi")) {
		t.Error("512Mi is not")
	}
}

// TestNegatedTextExcludesTheRow: "everything but kube-system" is a question
// people ask constantly.
func TestNegatedTextExcludesTheRow(t *testing.T) {
	columns := []string{"Namespace", "Name"}
	q := Parse("!kube-system")
	if q.Match(columns, row("kube-system", "coredns")) {
		t.Error("a negated word must remove the row")
	}
	if !q.Match(columns, row("shop", "payments")) {
		t.Error("and leave every other row alone")
	}
}

// TestSomethingThatIsNotAFilterStaysText: a URL has a colon in it and is not a
// comparison.
func TestSomethingThatIsNotAFilterStaysText(t *testing.T) {
	q := Parse("https://api.example.com")
	if q.Typed() {
		t.Errorf("terms = %+v, want a URL left as text", q.Terms)
	}
	if q.Text != "https://api.example.com" {
		t.Errorf("text = %q, want the URL back", q.Text)
	}
}

func TestAnEmptyFilterFiltersNothing(t *testing.T) {
	if !Parse("   ").Empty() {
		t.Error("whitespace is not a filter")
	}
}

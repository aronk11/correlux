package application

import "testing"

func TestTemplateChangesPutImagesFirstAndHideValuesThatMayBeSecrets(t *testing.T) {
	from := map[string]string{
		"container api image":           "api:1.8",
		"container api limits memory":   "256Mi",
		"container api env DB_PASSWORD": "old",
		"container api args":            "--verbose",
		"annotation vault/secret-path":  "a",
	}
	to := map[string]string{
		"container api image":           "api:1.9",
		"container api limits memory":   "512Mi",
		"container api env DB_PASSWORD": "new",
		"container api env FEATURE_X":   "on",
		"annotation vault/secret-path":  "b",
	}
	var got []string
	for _, c := range TemplateChanges(from, to) {
		got = append(got, c.String())
	}
	want := []string{
		"container api image: api:1.8 → api:1.9",
		"annotation vault/secret-path changed",
		"container api args removed (was --verbose)",
		"container api env DB_PASSWORD changed",
		"container api env FEATURE_X added",
		"container api limits memory: 256Mi → 512Mi",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIdenticalTemplatesHaveNoChanges(t *testing.T) {
	a := map[string]string{"container api image": "api:1.8"}
	if changes := TemplateChanges(a, a); len(changes) != 0 {
		t.Errorf("got %v", changes)
	}
}

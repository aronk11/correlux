package inspection

import (
	"bytes"
	"strings"
	"testing"
)

func TestComparisonExcludesNoiseAndCredentialsButRetainsConfiguration(t *testing.T) {
	before := []byte(`{"metadata":{"name":"a","namespace":"one","uid":"1","annotations":{"kubectl.kubernetes.io/last-applied-configuration":"secret"}},"spec":{"replicas":2,"template":{"env":[{"name":"TOKEN","value":"sensitive"}]},"values":{"password":"hidden"}},"data":{"tls.key":"private"},"status":{"ready":true}}`)
	after := []byte(`{"metadata":{"name":"b","namespace":"two","uid":"2"},"spec":{"replicas":2,"template":{"env":[{"name":"TOKEN","value":"different"}]},"values":{"password":"changed"}},"data":{"tls.key":"other"},"status":{"ready":false}}`)
	normalized, err := Normalized(before)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sensitive", "hidden", "private", "last-applied", "ready"} {
		if bytes.Contains(normalized, []byte(secret)) {
			t.Fatalf("retained %s: %s", secret, normalized)
		}
	}
	section, err := Comparison(before, after)
	if err != nil || len(section.Rows) != 0 {
		t.Fatalf("noise produced differences: %+v %v", section, err)
	}
	after = bytes.Replace(after, []byte(`"replicas":2`), []byte(`"replicas":3`), 1)
	section, err = Comparison(before, after)
	if err != nil {
		t.Fatal(err)
	}
	changes := ""
	var changesSb31 strings.Builder
	for _, row := range section.Rows {
		changesSb31.WriteString(strings.Join(row.Cells, " "))
	}
	changes += changesSb31.String()
	if !strings.Contains(changes, `- source   "`) && !strings.Contains(changes, "- source") {
		t.Fatal(changes)
	}
	if !strings.Contains(changes, `"replicas": 3`) {
		t.Fatal(changes)
	}
}

func TestNormalizeRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{"null", "[]", "bad"} {
		if _, err := Normalized([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

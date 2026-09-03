package decode

import (
	"encoding/base64"
	"strings"
	"testing"
)

// secret builds the document a server holds for a Secret with these values,
// encoded the way the API server stores them.
func secret(values map[string]string) []byte {
	var pairs []string
	for key, plain := range values {
		pairs = append(pairs, `"`+key+`": "`+base64.StdEncoding.EncodeToString([]byte(plain))+`"`)
	}
	return []byte(`{"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
	  "metadata": {"name": "database", "namespace": "shop"},
	  "data": {` + strings.Join(pairs, ", ") + `}}`)
}

// line returns the line of the rendered document whose key this is.
func line(t *testing.T, document, key string) string {
	t.Helper()
	for _, l := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), key+":") {
			return strings.TrimSpace(l)
		}
	}
	t.Fatalf("no %q in the document:\n%s", key, document)
	return ""
}

func TestASecretsValuesAreDecoded(t *testing.T) {
	document, decoded := Document(secret(map[string]string{"password": "hunter2", "user": "payments"}))

	if decoded != 2 {
		t.Errorf("decoded %d values, want both of them", decoded)
	}
	if got := line(t, document, "password"); got != "password: hunter2" {
		t.Errorf("the value is shown as %q, want what was stored in it", got)
	}
}

func TestTheRestOfTheDocumentIsUntouched(t *testing.T) {
	raw := []byte(`{"kind": "Secret", "type": "kubernetes.io/tls",
	  "metadata": {"name": "web", "generation": 1735689600000000000},
	  "data": {"password": "` + base64.StdEncoding.EncodeToString([]byte("hunter2")) + `"},
	  "somethingCorreluxHasNeverHeardOf": {"nested": ["a", "b"]}}`)

	document, _ := Document(raw)

	for _, want := range []string{
		"type: kubernetes.io/tls",
		"generation: 1735689600000000000",
		"somethingCorreluxHasNeverHeardOf",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("the document must still carry %q:\n%s", want, document)
		}
	}
}

func TestAValueThatIsNotBase64StaysExactlyAsItWas(t *testing.T) {
	raw := []byte(`{"kind": "ConfigMap", "data": {"log.level": "debug!", "greeting": "hello there"}}`)

	if document, decoded := Document(raw); decoded != 0 {
		t.Errorf("nothing here is base64, but %d values were rewritten:\n%s", decoded, document)
	}
}

func TestAConfigValueThatIsBase64ByAccidentIsLeftAlone(t *testing.T) {
	// "true" and "8080" decode without complaint, to bytes nobody wrote.
	raw := []byte(`{"kind": "ConfigMap", "data": {"debug": "true", "port": "8080"}}`)

	if document, decoded := Document(raw); decoded != 0 {
		t.Errorf("a ConfigMap's own words must survive the key, %d were decoded:\n%s", decoded, document)
	}
}

func TestABinaryValueIsSummarisedRatherThanDumped(t *testing.T) {
	keystore := make([]byte, 2048)
	keystore[0] = 0x1b // an escape sequence in a Secret must never reach the terminal

	document, decoded := Document(secret(map[string]string{"keystore.jks": string(keystore)}))

	if decoded != 1 {
		t.Fatalf("decoded %d values, want the one", decoded)
	}
	if got := line(t, document, "keystore.jks"); got != "keystore.jks: <binary, 2048 bytes>" {
		t.Errorf("a binary value is shown as %q, want its size instead of its bytes", got)
	}
}

func TestACertificateKeepsItsLines(t *testing.T) {
	cert := "-----BEGIN CERTIFICATE-----\nMIIBkTCB+w==\n-----END CERTIFICATE-----\n"

	document, _ := Document(secret(map[string]string{"tls.crt": cert}))

	if !strings.Contains(document, "\n    -----BEGIN CERTIFICATE-----\n") {
		t.Errorf("a certificate must stay readable across its lines:\n%s", document)
	}
}

func TestTheBinaryHalfOfAConfigMapIsDecodedToo(t *testing.T) {
	raw := []byte(`{"kind": "ConfigMap",
	  "data": {"log.level": "debug!"},
	  "binaryData": {"banner": "` + base64.StdEncoding.EncodeToString([]byte("welcome")) + `"}}`)

	document, decoded := Document(raw)

	if decoded != 1 {
		t.Fatalf("decoded %d values, want the encoded one", decoded)
	}
	if got := line(t, document, "banner"); got != "banner: welcome" {
		t.Errorf("binaryData is shown as %q, want what it holds", got)
	}
}

func TestADocumentWithNothingToDecodeIsNotRewritten(t *testing.T) {
	raw := []byte(`{"kind": "Deployment", "spec": {"replicas": 3}}`)

	document, decoded := Document(raw)

	if decoded != 0 || document != "" {
		t.Errorf("a document with no base64 in it must be left to the caller, got %d values:\n%s", decoded, document)
	}
}

func TestAFieldThatIsNotAMapOfValuesIsNotOneToDecode(t *testing.T) {
	// A custom resource may call anything `data`.
	raw := []byte(`{"kind": "Widget", "data": ["aGVsbG8=", "d29ybGQ="]}`)

	if _, decoded := Document(raw); decoded != 0 {
		t.Errorf("a list is not the field Kubernetes encodes, %d values were decoded", decoded)
	}
}

func TestSomethingThatIsNotAJSONDocumentIsNotDecoded(t *testing.T) {
	if _, decoded := Document([]byte("not a document at all")); decoded != 0 {
		t.Errorf("decoded %d values out of something that is not a document", decoded)
	}
}

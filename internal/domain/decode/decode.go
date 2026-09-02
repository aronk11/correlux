// Package decode turns the base64 a Kubernetes document stores back into what
// was put there.
//
// A Secret is the reason this exists: every value under `data` is base64, so
// the document the server holds is unreadable, and reading it means copying a
// blob out of the terminal into `base64 -d`. That errand is exactly what
// Correlux is for.
//
// The scope is decided by the document rather than by a list of kinds: the
// fields that hold encoded values are `data` and `binaryData`, whatever the
// object calls itself, and a value that is not base64 is left exactly as the
// server sent it. Everything else in the document passes through byte for
// byte, because a document rewritten from a parsed structure quietly loses the
// fields the parser had never heard of — and on a custom resource that is most
// of them.
package decode

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"unicode/utf8"

	"sigs.k8s.io/yaml"
)

// encodedFields are the two places a Kubernetes object keeps base64: a Secret's
// values, and the binary half of a ConfigMap.
var encodedFields = []string{"data", "binaryData"}

// literalGuard is the length below which a value has to decode to text before
// it is believed to be base64 at all.
//
// "true" and "8080" are valid base64 by accident, and a ConfigMap is full of
// them; turning one into three unreadable bytes is a worse answer than leaving
// it alone. Nobody writes twenty-four characters of base64 by accident, so
// above that a value is decoded whatever it turns out to hold.
const literalGuard = 24

// Document renders the document with its base64 values decoded, as YAML, and
// reports how many values it decoded. When that is zero there was nothing to
// decode, and the caller should keep showing what the server sent.
func Document(raw []byte) (string, int) {
	// Only the encoded fields are parsed. Every other field stays the JSON the
	// server sent and is written back out exactly as it came.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", 0
	}

	decoded := 0
	for _, field := range encodedFields {
		values, ok := stringMap(doc[field])
		if !ok {
			continue
		}
		changed := false
		for key, stored := range values {
			plain, ok := value(stored)
			if !ok {
				continue
			}
			values[key] = plain
			changed = true
			decoded++
		}
		if !changed {
			continue
		}
		replacement, err := json.Marshal(values)
		if err != nil {
			return "", 0
		}
		doc[field] = replacement
	}
	if decoded == 0 {
		return "", 0
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return "", 0
	}
	document, err := yaml.JSONToYAML(out)
	if err != nil {
		return "", 0
	}
	return string(document), decoded
}

// stringMap reads a field that maps names to encoded values. Anything else
// shaped — a list, a number, a nested object — is not that field as Kubernetes
// defines it, and is left alone.
func stringMap(field json.RawMessage) (map[string]string, bool) {
	if len(field) == 0 {
		return nil, false
	}
	var values map[string]string
	if err := json.Unmarshal(field, &values); err != nil || len(values) == 0 {
		return nil, false
	}
	return values, true
}

// value decodes one stored value, and reports whether it was base64 at all.
func value(stored string) (string, bool) {
	if stored == "" {
		return "", false
	}
	plain, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", false
	}
	// Kubernetes writes canonical, padded base64. Requiring the encoding to
	// round-trip keeps a value that merely survived a lenient decoder from
	// being presented as something it never was.
	if base64.StdEncoding.EncodeToString(plain) != stored {
		return "", false
	}
	if isText(plain) {
		return string(plain), true
	}
	if len(stored) < literalGuard {
		return "", false
	}
	// Bytes nobody can read must not reach the terminal: an escape sequence
	// that happened to be in a Secret would redraw the screen around it.
	return "<binary, " + strconv.Itoa(len(plain)) + plural(len(plain)) + ">", true
}

func plural(bytes int) string {
	if bytes == 1 {
		return " byte"
	}
	return " bytes"
}

// isText reports whether the decoded bytes are something to read. A certificate
// and a kubeconfig are: they carry newlines and stay text.
func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		switch r {
		case '\n', '\r', '\t':
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

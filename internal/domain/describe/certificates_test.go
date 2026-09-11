package describe

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestCertificateInspectionUsesPublicCertificateOnly(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "api.example.test"}, DNSNames: []string{"api.example.test"}, NotBefore: at.Add(-time.Hour), NotAfter: at.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"data": map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "tls.key": "do-not-decode-this"}}
	for _, test := range []struct {
		at   time.Time
		want string
	}{{at, "within validity period"}, {at.Add(2 * time.Hour), "expired"}, {at.Add(-2 * time.Hour), "not yet valid"}} {
		section := certificateSection(doc, test.at)
		text := ""
		var textSb35 strings.Builder
		for _, row := range section.Rows {
			textSb35.WriteString(strings.Join(row, " "))
		}
		text += textSb35.String()
		if !strings.Contains(text, test.want) || !strings.Contains(text, "api.example.test") || strings.Contains(text, "do-not-decode") {
			t.Fatal(text)
		}
	}
}

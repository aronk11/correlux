package describe

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"strconv"
	"strings"
	"time"
)

// certificateSection reads only public certificates. Private-key fields are
// never decoded or included in the description.
func certificateSection(doc map[string]any, at time.Time) *Section {
	encoded := str(child(doc, "data"), "tls.crt")
	if encoded == "" {
		return nil
	}
	section := &Section{Title: "TLS certificates", Columns: []string{"Field", "Value"}}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		section.add("Error", "tls.crt is not valid base64")
		return section
	}
	count := 0
	for len(data) > 0 && count < 32 {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			section.add("Error", parseErr.Error())
			continue
		}
		count++
		section.add("Certificate", strconv.Itoa(count))
		section.add("Subject", certificate.Subject.String())
		section.add("Issuer", certificate.Issuer.String())
		section.add("DNS names", strings.Join(certificate.DNSNames, ", "))
		section.add("Valid from", certificate.NotBefore.UTC().Format(time.RFC3339))
		section.add("Valid until", certificate.NotAfter.UTC().Format(time.RFC3339))
		validity := "within validity period"
		if at.Before(certificate.NotBefore) {
			validity = "not yet valid"
		}
		if at.After(certificate.NotAfter) {
			validity = "expired"
		}
		section.add("Time validity", validity+"; trust and hostname not verified")
		fingerprint := sha256.Sum256(certificate.Raw)
		section.add("SHA-256", hex.EncodeToString(fingerprint[:]))
	}
	if count == 0 {
		section.add("Error", "no readable PEM certificates in tls.crt")
	}
	if count == 32 && len(data) > 0 {
		section.add("Limit", "only the first 32 certificates are shown")
	}
	return section
}

package cms_test

import (
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/farcloser/limen/internal/verify/cms"
	"github.com/farcloser/limen/internal/verify/cms/cmstest"
)

var content = []byte("the artifact's bytes, as downloaded\n") //nolint:gochecknoglobals // the fixture.

func chain(t *testing.T, opts cmstest.Options) *cmstest.Chain {
	t.Helper()

	made, err := cmstest.NewChain(opts)
	if err != nil {
		t.Fatal(err)
	}

	return made
}

func sign(t *testing.T, made *cmstest.Chain, opts cmstest.SignOptions) []byte {
	t.Helper()

	signature, err := made.Sign(content, opts)
	if err != nil {
		t.Fatal(err)
	}

	return signature
}

// TestVerifyShapes: the shapes vendors publish verify: ECDSA and RSA leaves,
// signed attributes or the content alone, the signer named by issuer and
// serial or by subject key identifier.
func TestVerifyShapes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		chain cmstest.Options
		sign  cmstest.SignOptions
	}{
		"ecdsa, attributes":     {},
		"rsa, attributes":       {chain: cmstest.Options{RSA: true}},
		"content alone":         {sign: cmstest.SignOptions{NoAttributes: true}},
		"by subject key id":     {sign: cmstest.SignOptions{BySubjectKeyID: true}},
		"rsa by subject key id": {chain: cmstest.Options{RSA: true}, sign: cmstest.SignOptions{BySubjectKeyID: true}},
		"intel-like subject":    {chain: cmstest.Options{Subject: pkix.Name{Country: []string{"US"}, Organization: []string{"Intel Corporation"}, CommonName: "Intel Corporation"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			made := chain(t, tc.chain)

			verified, err := cms.Verify(content, sign(t, made, tc.sign), nil, cms.Fingerprint(made.Root), time.Now())
			if err != nil {
				t.Fatalf("verify: %v", err)
			}

			if !verified.Signer.Equal(made.Leaf) || !verified.Root.Equal(made.Root) {
				t.Errorf("signer %s, root %s", verified.Signer.Subject, verified.Root.Subject)
			}
		})
	}
}

// TestVerifyRefuses: every departure is refused by its own error.
func TestVerifyRefuses(t *testing.T) {
	t.Parallel()

	made := chain(t, cmstest.Options{})
	pin := cms.Fingerprint(made.Root)
	now := time.Now()

	good := sign(t, made, cmstest.SignOptions{})

	tampered := append([]byte{}, content...)
	tampered[0] ^= 1

	other := chain(t, cmstest.Options{})
	expired := chain(t, cmstest.Options{NotAfter: now.Add(-time.Hour)})
	plain := chain(t, cmstest.Options{NoCodeSigning: true})

	for name, tc := range map[string]struct {
		content   []byte
		signature []byte
		pin       string
		want      error
	}{
		"tampered content, attributes":    {tampered, good, pin, cms.ErrSignature},
		"tampered content, no attributes": {tampered, sign(t, made, cmstest.SignOptions{NoAttributes: true}), pin, cms.ErrSignature},
		"message digest of other content": {content, sign(t, made, cmstest.SignOptions{WrongDigest: true}), pin, cms.ErrSignature},
		"signature by another chain":      {content, sign(t, other, cmstest.SignOptions{}), pin, cms.ErrChain},
		"intermediate missing":            {content, sign(t, made, cmstest.SignOptions{OmitIntermediate: true}), pin, cms.ErrChain},
		"root neither carried nor given":  {content, sign(t, made, cmstest.SignOptions{OmitRoot: true}), pin, cms.ErrChain},
		"expired signer":                  {content, sign(t, expired, cmstest.SignOptions{}), cms.Fingerprint(expired.Root), cms.ErrChain},
		"not a code-signing certificate":  {content, sign(t, plain, cmstest.SignOptions{}), cms.Fingerprint(plain.Root), cms.ErrChain},
		"not a signature":                 {content, []byte("hello"), pin, cms.ErrShape},
		"fingerprint not a sha256":        {content, good, "abcd", cms.ErrFingerprint},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := cms.Verify(tc.content, tc.signature, nil, tc.pin, now)
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestPinnedIntermediateIsATrustAnchor: the pin is what is trusted; a chain
// that ends at a pinned intermediate ends at the pin.
func TestPinnedIntermediateIsATrustAnchor(t *testing.T) {
	t.Parallel()

	made := chain(t, cmstest.Options{})

	verified, err := cms.Verify(
		content,
		sign(t, made, cmstest.SignOptions{}),
		nil,
		cms.Fingerprint(made.Intermediate),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if !verified.Root.Equal(made.Intermediate) {
		t.Errorf("root = %s, want the pinned intermediate", verified.Root.Subject)
	}
}

// TestFingerprintForms: a pin written with colons or in upper case is the
// same pin.
func TestFingerprintForms(t *testing.T) {
	t.Parallel()

	made := chain(t, cmstest.Options{})
	signature := sign(t, made, cmstest.SignOptions{})
	raw := cms.Fingerprint(made.Root)

	colons := ""
	for i := 0; i < len(raw); i += 2 {
		if colons != "" {
			colons += ":"
		}

		colons += raw[i : i+2]
	}

	for _, pin := range []string{raw, colons, toUpper(raw)} {
		if _, err := cms.Verify(content, signature, nil, pin, time.Now()); err != nil {
			t.Errorf("%s: %v", pin, err)
		}
	}
}

func toUpper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 'a' + 'A'
		}
	}

	return string(out)
}

// TestRootGivenByTheCaller: a signature that carries the chain without its
// root (the vendors' usual) verifies with the root fetched by the caller, in
// each form a CA publishes it; the system's store is never consulted.
func TestRootGivenByTheCaller(t *testing.T) {
	t.Parallel()

	made := chain(t, cmstest.Options{RSA: true})
	signature := sign(t, made, cmstest.SignOptions{OmitRoot: true})

	pemForm := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: made.Root.Raw})

	p7c, err := cmstest.CertsOnly(made.Root)
	if err != nil {
		t.Fatal(err)
	}

	for name, data := range map[string][]byte{"pem": pemForm, "der": made.Root.Raw, "p7c": p7c} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			roots, err := cms.ParseCertificates(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			verified, err := cms.Verify(content, signature, roots, cms.Fingerprint(made.Root), time.Now())
			if err != nil {
				t.Fatalf("verify: %v", err)
			}

			if !verified.Root.Equal(made.Root) {
				t.Errorf("root = %s", verified.Root.Subject)
			}
		})
	}

	if _, err := cms.ParseCertificates([]byte("not a certificate")); !errors.Is(err, cms.ErrShape) {
		t.Errorf("garbage: %v", err)
	}
}

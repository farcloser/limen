// Black-box tests of the clearsign verifier: keys and messages come from
// openpgptest, which writes the packets itself, so every assertion crosses
// two independent readings of the format.

package openpgp_test

import (
	"crypto"
	"errors"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/verify/openpgp"
	"github.com/farcloser/limen/internal/verify/openpgp/openpgptest"
)

const sums = "abc123  linux-6.1.tar.xz\n" +
	"- a line that opens with a dash  \n" +
	"-----BEGIN PGP SIGNATURE-----\n" +
	"\ttabs and trailing spaces   \n" +
	"\n" +
	"last line without newline"

// canonical is sums as the verifier returns it: dash-escapes gone, trailing
// whitespace stripped, a final newline.
const canonical = "abc123  linux-6.1.tar.xz\n" +
	"- a line that opens with a dash\n" +
	"-----BEGIN PGP SIGNATURE-----\n" +
	"\ttabs and trailing spaces\n" +
	"\n" +
	"last line without newline\n"

func TestVerifiesRSAAndEd25519(t *testing.T) {
	t.Parallel()

	for name, generate := range map[string]func() (*openpgptest.Signer, error){
		"rsa": openpgptest.NewRSA, "ed25519": openpgptest.NewEd25519,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signer, err := generate()
			if err != nil {
				t.Fatal(err)
			}

			for _, hash := range []crypto.Hash{crypto.SHA256, crypto.SHA512} {
				message, err := signer.Clearsign(sums, hash)
				if err != nil {
					t.Fatal(err)
				}

				keys, err := openpgp.ParseKeys(signer.PublicKeyBlock())
				if err != nil {
					t.Fatal(err)
				}

				if len(keys) != 1 || string(keys[0].Fingerprint) != signer.Fingerprint() {
					t.Fatalf("keys = %+v, want one with fingerprint %s", keys, signer.Fingerprint())
				}

				verified, err := openpgp.VerifyClearsigned(message, keys)
				if err != nil {
					t.Fatalf("%v: %v", hash, err)
				}

				if string(verified.Text) != canonical {
					t.Errorf("text = %q, want %q", verified.Text, canonical)
				}

				if string(verified.Signer) != signer.Fingerprint() {
					t.Errorf("signer = %s, want %s", verified.Signer, signer.Fingerprint())
				}
			}
		})
	}
}

func TestCRLFInputVerifies(t *testing.T) {
	t.Parallel()

	signer, message, keys := signed(t, sums)

	crlf := strings.ReplaceAll(string(message), "\n", "\r\n")

	verified, err := openpgp.VerifyClearsigned([]byte(crlf), keys)
	if err != nil {
		t.Fatal(err)
	}

	if string(verified.Text) != canonical || string(verified.Signer) != signer.Fingerprint() {
		t.Errorf("verified = %+v", verified)
	}
}

func TestRefuses(t *testing.T) {
	t.Parallel()

	_, message, keys := signed(t, sums)

	other, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	otherKeys, err := openpgp.ParseKeys(other.PublicKeyBlock())
	if err != nil {
		t.Fatal(err)
	}

	weak, err := other.Clearsign(sums, crypto.SHA1)
	if err != nil {
		t.Fatal(err)
	}

	text := string(message)

	sigStart := strings.Index(text, "-----BEGIN PGP SIGNATURE-----")
	if sigStart < 0 {
		t.Fatal("no signature block")
	}

	cases := map[string]struct {
		message string
		keys    []openpgp.Key
		want    error
	}{
		"tampered text":      {strings.Replace(text, "abc123", "abc124", 1), keys, openpgp.ErrSignature},
		"tampered signature": {flipInSignature(t, text), keys, openpgp.ErrSignature},
		"another key":        {text, otherKeys, openpgp.ErrNoKey},
		"no keys":            {text, nil, openpgp.ErrNoKey},
		"sha1":               {string(weak), otherKeys, openpgp.ErrUnsupported},
		"no signature block": {text[:sigStart], keys, openpgp.ErrArmor},
		"not clearsigned":    {"hello\n", keys, openpgp.ErrArmor},
		"checksum mismatch":  {breakChecksum(t, text), keys, openpgp.ErrArmor},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := openpgp.VerifyClearsigned([]byte(testCase.message), testCase.keys)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestParseKeysRefuses(t *testing.T) {
	t.Parallel()

	signer, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	block := string(signer.PublicKeyBlock())

	cases := map[string]struct {
		block string
		want  error
	}{
		"empty":        {"", openpgp.ErrArmor},
		"not a key":    {"-----BEGIN PGP SIGNATURE-----\n\nAAAA\n-----END PGP SIGNATURE-----\n", openpgp.ErrArmor},
		"garbage body": {strings.Replace(block, "\n\n", "\n\n////", 1), openpgp.ErrArmor},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := openpgp.ParseKeys([]byte(testCase.block))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

// signed is a fresh RSA signer, its clearsigned text, and its key.
func signed(t *testing.T, text string) (*openpgptest.Signer, []byte, []openpgp.Key) {
	t.Helper()

	signer, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	message, err := signer.Clearsign(text, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}

	keys, err := openpgp.ParseKeys(signer.PublicKeyBlock())
	if err != nil {
		t.Fatal(err)
	}

	return signer, message, keys
}

// flipInSignature changes one base64 character deep in the signature's
// integer and recomputes nothing, so the armor still decodes but the
// signature no longer matches.
func flipInSignature(t *testing.T, message string) string {
	t.Helper()

	lines := strings.Split(message, "\n")

	crc := -1

	for index := len(lines) - 1; index >= 0; index-- {
		if strings.HasPrefix(lines[index], "=") {
			crc = index

			break
		}
	}

	if crc < 2 {
		t.Fatal("no checksum line")
	}

	// Drop the checksum line, which would catch the flip first, and flip a
	// character in the last data line.
	last := []byte(lines[crc-1])
	if last[0] == 'A' {
		last[0] = 'B'
	} else {
		last[0] = 'A'
	}

	lines[crc-1] = string(last)

	return strings.Join(append(lines[:crc], lines[crc+1:]...), "\n")
}

// breakChecksum changes the checksum line only.
func breakChecksum(t *testing.T, message string) string {
	t.Helper()

	index := strings.LastIndex(message, "\n=")
	if index < 0 {
		t.Fatal("no checksum line")
	}

	tail := []byte(message[index+2:])
	if tail[0] == 'A' {
		tail[0] = 'B'
	} else {
		tail[0] = 'A'
	}

	return message[:index+2] + string(tail)
}

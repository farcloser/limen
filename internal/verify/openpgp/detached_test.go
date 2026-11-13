// Black-box tests of the detached-signature verifier: keys and signatures
// come from openpgptest, which writes the packets itself.

package openpgp_test

import (
	"bytes"
	"crypto"
	"errors"
	"testing"

	"github.com/farcloser/limen/internal/verify/openpgp"
	"github.com/farcloser/limen/internal/verify/openpgp/openpgptest"
)

// artifact stands for the tarball: bytes, not text, so no canonical form
// applies.
const artifact = "\x1f\x8b\x08\x00 the tarball\r\n with every kind of line ending\n\x00"

func TestVerifiesDetached(t *testing.T) {
	t.Parallel()

	tarball := []byte(artifact)

	for name, generate := range map[string]func() (*openpgptest.Signer, error){
		"rsa": openpgptest.NewRSA, "ed25519": openpgptest.NewEd25519,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signer, err := generate()
			if err != nil {
				t.Fatal(err)
			}

			keys, err := openpgp.ParseKeys(signer.PublicKeyBlock())
			if err != nil {
				t.Fatal(err)
			}

			for _, hash := range []crypto.Hash{crypto.SHA256, crypto.SHA512} {
				// The four shapes upstreams publish: .asc and .sig, from a
				// writer of new-format headers and from GnuPG's old-format one.
				for _, shape := range []openpgptest.Detached{
					{Armored: true}, {Armored: false}, {Armored: true, OldHeader: true}, {Armored: false, OldHeader: true},
				} {
					signature, err := signer.SignDetached(tarball, hash, shape)
					if err != nil {
						t.Fatal(err)
					}

					given := bytes.Clone(signature)

					got, err := openpgp.VerifyDetached(tarball, signature, keys)
					if err != nil {
						t.Fatalf("%v %+v: %v", hash, shape, err)
					}

					if string(got) != signer.Fingerprint() {
						t.Errorf("%v %+v: signer = %s, want %s", hash, shape, got, signer.Fingerprint())
					}

					// The signature is the caller's, read and never written:
					// a second check of the same bytes must see them as given.
					if !bytes.Equal(signature, given) {
						t.Errorf("%v %+v: the verifier wrote into the signature it was given", hash, shape)
					}
				}
			}
		})
	}
}

func TestDetachedRefuses(t *testing.T) {
	t.Parallel()

	tarball := []byte(artifact)

	signer, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	other, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	keys, err := openpgp.ParseKeys(signer.PublicKeyBlock())
	if err != nil {
		t.Fatal(err)
	}

	otherKeys, err := openpgp.ParseKeys(other.PublicKeyBlock())
	if err != nil {
		t.Fatal(err)
	}

	signature, err := signer.SignDetached(tarball, crypto.SHA256, openpgptest.Detached{Armored: true})
	if err != nil {
		t.Fatal(err)
	}

	// A clearsigned message's block is a canonical text signature: the wrong
	// kind for a detached check. Over one line with no ending the canonical
	// text is the bytes themselves, so the digest matches and the type alone
	// refuses it.
	oneLine := []byte("one line, no ending")

	clearsigned, err := signer.Clearsign(string(oneLine), crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}

	weak, err := signer.SignDetached(tarball, crypto.SHA1, openpgptest.Detached{})
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte{}, tarball...)
	tampered[len(tampered)/2] ^= 0x01

	for name, tc := range map[string]struct {
		data, signature []byte
		keys            []openpgp.Key
		want            error
	}{
		"tampered data":           {tampered, signature, keys, openpgp.ErrSignature},
		"key not the signer's":    {tarball, signature, otherKeys, openpgp.ErrNoKey},
		"text signature":          {oneLine, clearsigned, keys, openpgp.ErrSignature},
		"weak digest":             {tarball, weak, keys, openpgp.ErrUnsupported},
		"not a packet stream":     {tarball, []byte{0x00, 0x01}, keys, openpgp.ErrPacket},
		"empty":                   {tarball, nil, keys, openpgp.ErrPacket},
		"armor without the frame": {tarball, []byte("-----BEGIN PGP SIGNATURE-----\n"), keys, openpgp.ErrArmor},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := openpgp.VerifyDetached(tc.data, tc.signature, tc.keys)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestDetachedSeveralSigners: a file two keys signed, as two packets in one
// .sig or as two armored blocks in one .asc, verifies against either key
// alone, whichever signature comes first.
func TestDetachedSeveralSigners(t *testing.T) {
	t.Parallel()

	tarball := []byte(artifact)

	first, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	second, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	stranger, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	for name, shape := range map[string]openpgptest.Detached{
		"two packets": {OldHeader: true}, "two armored blocks": {Armored: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var both []byte

			for _, signer := range []*openpgptest.Signer{first, second} {
				signature, err := signer.SignDetached(tarball, crypto.SHA512, shape)
				if err != nil {
					t.Fatal(err)
				}

				both = append(both, signature...)
			}

			for _, signer := range []*openpgptest.Signer{first, second} {
				keys, err := openpgp.ParseKeys(signer.PublicKeyBlock())
				if err != nil {
					t.Fatal(err)
				}

				got, err := openpgp.VerifyDetached(tarball, both, keys)
				if err != nil {
					t.Fatalf("with %s's key alone: %v", signer.Fingerprint(), err)
				}

				if string(got) != signer.Fingerprint() {
					t.Errorf("signer = %s, want %s", got, signer.Fingerprint())
				}
			}

			keys, err := openpgp.ParseKeys(stranger.PublicKeyBlock())
			if err != nil {
				t.Fatal(err)
			}

			if _, err := openpgp.VerifyDetached(tarball, both, keys); !errors.Is(err, openpgp.ErrNoKey) {
				t.Errorf("with a key that signed nothing: err = %v, want ErrNoKey", err)
			}
		})
	}
}

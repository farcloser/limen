// Fuzz targets over the three readers: each takes bytes a URL served, so
// the input space is every byte string. Past the tables' bound the
// properties are the ones that hold for any input: no panic, and an
// acceptance names a key that was given.

package openpgp_test

import (
	"crypto"
	"slices"
	"testing"

	"github.com/farcloser/limen/internal/verify/openpgp"
	"github.com/farcloser/limen/internal/verify/openpgp/openpgptest"
)

// fuzzSigners is one signer of each algorithm and their keys as parsed.
func fuzzSigners(f *testing.F) ([]*openpgptest.Signer, []openpgp.Key) {
	f.Helper()

	rsa, err := openpgptest.NewRSA()
	if err != nil {
		f.Fatal(err)
	}

	ed, err := openpgptest.NewEd25519()
	if err != nil {
		f.Fatal(err)
	}

	keys, err := openpgp.ParseKeys(openpgptest.KeyBlock(rsa, ed))
	if err != nil {
		f.Fatal(err)
	}

	return []*openpgptest.Signer{rsa, ed}, keys
}

// given reports whether signer is one of keys.
func given(signer openpgp.Fingerprint, keys []openpgp.Key) bool {
	return slices.ContainsFunc(keys, func(key openpgp.Key) bool { return key.Fingerprint == signer })
}

func FuzzVerifyDetached(f *testing.F) {
	signers, keys := fuzzSigners(f)

	for _, signer := range signers {
		for _, shape := range []openpgptest.Detached{
			{Armored: true}, {Armored: false}, {Armored: true, OldHeader: true}, {Armored: false, OldHeader: true},
		} {
			signature, err := signer.SignDetached([]byte(artifact), crypto.SHA512, shape)
			if err != nil {
				f.Fatal(err)
			}

			f.Add([]byte(artifact), signature)
		}
	}

	f.Fuzz(func(t *testing.T, data, signature []byte) {
		before := slices.Clone(signature)

		signer, err := openpgp.VerifyDetached(data, signature, keys)
		if err == nil && !given(signer, keys) {
			t.Errorf("accepted for %s, a key that was not given", signer)
		}

		if !slices.Equal(signature, before) {
			t.Error("the verifier wrote into the signature it was given")
		}
	})
}

func FuzzVerifyClearsigned(f *testing.F) {
	signers, keys := fuzzSigners(f)

	for _, signer := range signers {
		message, err := signer.Clearsign(sums, crypto.SHA256)
		if err != nil {
			f.Fatal(err)
		}

		f.Add(message)
	}

	f.Fuzz(func(t *testing.T, message []byte) {
		verified, err := openpgp.VerifyClearsigned(message, keys)
		if err == nil && !given(verified.Signer, keys) {
			t.Errorf("accepted for %s, a key that was not given", verified.Signer)
		}
	})
}

func FuzzParseKeys(f *testing.F) {
	signers, _ := fuzzSigners(f)

	f.Add(openpgptest.KeyBlock(signers...))

	for _, signer := range signers {
		f.Add(signer.PublicKeyBlock())
	}

	f.Fuzz(func(t *testing.T, block []byte) {
		keys, err := openpgp.ParseKeys(block)
		if err == nil && len(keys) == 0 {
			t.Error("a block read without error carries no key")
		}
	})
}

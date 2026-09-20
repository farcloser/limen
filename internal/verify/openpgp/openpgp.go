// Package openpgp verifies OpenPGP clearsigned messages against public keys
// the caller pins by fingerprint, with nothing but the standard library. It
// reads exactly what that takes — an armored public key block, a clearsigned
// document, v4 keys and v4 signatures over RSA or Ed25519 with a SHA-2
// digest — and refuses the rest by name. It is a verifier, not an OpenPGP
// implementation: no encryption, no signing, no web of trust, no key
// expiry or revocation (the caller's pin is the trust decision).
package openpgp

import (
	"crypto"
	"errors"
)

var (
	// ErrArmor is an armored block that does not decode.
	ErrArmor = errors.New("openpgp: malformed armor")
	// ErrPacket is a packet that does not have the shape.
	ErrPacket = errors.New("openpgp: malformed packet")
	// ErrUnsupported names a construct this verifier does not read: another
	// key or signature version, algorithm, or digest.
	ErrUnsupported = errors.New("openpgp: unsupported")
	// ErrSignature is a signature that does not verify against its key.
	ErrSignature = errors.New("openpgp: signature does not verify")
	// ErrNoKey is a signature whose signer is not among the keys given.
	ErrNoKey = errors.New("openpgp: no key for the signer")
)

// Fingerprint is a v4 key fingerprint: 40 uppercase hexadecimal digits.
type Fingerprint string

// Key is one public key or subkey read from a key block.
type Key struct {
	Fingerprint Fingerprint

	algorithm byte
	public    crypto.PublicKey
}

// Verified is a clearsigned message that checked out.
type Verified struct {
	// Text is the signed text as verified: dash-escapes removed, trailing
	// whitespace stripped from every line, LF line endings.
	Text []byte
	// Signer is the key the signature verified against.
	Signer Fingerprint
}

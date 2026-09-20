package openpgp

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- a v4 fingerprint is SHA-1 by definition (RFC 4880 §12.2); it names a key, it secures nothing.
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Public key algorithms (RFC 4880 §9.1; EdDSA from RFC 9580).
const (
	algRSA         = 1
	algRSASignOnly = 3
	algEdDSA       = 22
)

// The v4 key packet (RFC 4880 §5.5.2): version, four octets of creation
// time, the algorithm, then the algorithm's material.
const (
	keyVersion4      = 4
	keyMaterialStart = 6

	// fingerprintPrefix opens the octets a v4 fingerprint hashes: 0x99,
	// a two-octet length, the packet body.
	fingerprintPrefix = 0x99
	fingerprintLen    = 20
	keyIDLen          = 8

	// rsaMinBits is the shortest RSA modulus accepted; anything under is
	// refused as unsupported rather than verified as weak.
	rsaMinBits = 2048
	// rsaMaxExponentBits bounds a public exponent to what rsa.PublicKey
	// carries as an int.
	rsaMaxExponentBits = 31

	ed25519KeyPrefix = 0x40
)

// ed25519OID is the curve OID an EdDSA key packet opens with, length
// octet excluded (RFC 9580 §9.2).
//
//nolint:gochecknoglobals // immutable constant.
var ed25519OID = []byte{0x2B, 0x06, 0x01, 0x04, 0x01, 0xDA, 0x47, 0x0F, 0x01}

// ParseKeys reads every public key and subkey in an armored public key
// block. A key whose algorithm this verifier does not read is kept, with its
// fingerprint, so a signature naming it fails as unsupported rather than as
// unknown.
func ParseKeys(armored []byte) ([]Key, error) {
	blk, err := dearmor(splitLines(armored), kindPublicKey)
	if err != nil {
		return nil, err
	}

	packets, err := readPackets(blk.body)
	if err != nil {
		return nil, err
	}

	var keys []Key

	for _, pkt := range packets {
		if pkt.tag != tagPublicKey && pkt.tag != tagPublicSubkey {
			continue
		}

		key, err := parseKey(pkt.body)
		if err != nil {
			return nil, err
		}

		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no public key packet in the block", ErrPacket)
	}

	return keys, nil
}

// parseKey reads one v4 public key packet body.
func parseKey(body []byte) (Key, error) {
	if len(body) < keyMaterialStart {
		return Key{}, fmt.Errorf("%w: truncated key", ErrPacket)
	}

	if body[0] != keyVersion4 {
		return Key{}, fmt.Errorf("%w: key version %d", ErrUnsupported, body[0])
	}

	key := Key{Fingerprint: fingerprint(body), algorithm: body[keyMaterialStart-1]}
	material := body[keyMaterialStart:]

	var err error

	switch key.algorithm {
	case algRSA, algRSASignOnly:
		key.public, err = parseRSA(material)
	case algEdDSA:
		key.public, err = parseEd25519(material)
	default:
		// Kept without material: named as unsupported if it ever signs.
	}

	if err != nil {
		return Key{}, fmt.Errorf("key %s: %w", key.Fingerprint, err)
	}

	return key, nil
}

// fingerprint is the v4 fingerprint of a key packet body (RFC 4880 §12.2).
func fingerprint(body []byte) Fingerprint {
	hash := sha1.New() // #nosec G401 -- see the import.
	hash.Write([]byte{fingerprintPrefix})
	hash.Write(twoOctetLength(len(body)))
	hash.Write(body)

	return Fingerprint(strings.ToUpper(hex.EncodeToString(hash.Sum(nil))))
}

// parseRSA reads an RSA public key: the modulus, then the exponent.
func parseRSA(material []byte) (*rsa.PublicKey, error) {
	modulus, rest, err := readMPI(material)
	if err != nil {
		return nil, err
	}

	exponent, _, err := readMPI(rest)
	if err != nil {
		return nil, err
	}

	public := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus)}

	if public.N.BitLen() < rsaMinBits {
		return nil, fmt.Errorf("%w: RSA modulus of %d bits", ErrUnsupported, public.N.BitLen())
	}

	exp := new(big.Int).SetBytes(exponent)
	if exp.BitLen() > rsaMaxExponentBits || exp.Sign() <= 0 {
		return nil, fmt.Errorf("%w: RSA public exponent out of range", ErrPacket)
	}

	public.E = int(exp.Int64())

	return public, nil
}

// parseEd25519 reads an EdDSA public key: the curve's OID with a leading
// length octet, then the key as one integer with a 0x40 prefix octet.
func parseEd25519(material []byte) (ed25519.PublicKey, error) {
	if len(material) == 0 || int(material[0]) != len(ed25519OID) ||
		!bytes.Equal(material[1:1+len(ed25519OID)], ed25519OID) {
		return nil, fmt.Errorf("%w: EdDSA curve other than Ed25519", ErrUnsupported)
	}

	point, _, err := readMPI(material[1+len(ed25519OID):])
	if err != nil {
		return nil, err
	}

	if len(point) != 1+ed25519.PublicKeySize || point[0] != ed25519KeyPrefix {
		return nil, fmt.Errorf("%w: malformed Ed25519 point", ErrPacket)
	}

	return ed25519.PublicKey(point[1:]), nil
}

// keyID is the low eight octets of the fingerprint, what an issuer key ID
// subpacket carries.
func (k Key) keyID() string {
	return string(k.Fingerprint[len(k.Fingerprint)-2*keyIDLen:])
}

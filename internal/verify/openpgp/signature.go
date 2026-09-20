package openpgp

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	_ "crypto/sha256" // The SHA-2 digests a signature may name, registered for crypto.Hash.
	_ "crypto/sha512" // Same.
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// Signature types (RFC 4880 §5.2.1): a clearsigned document carries a
// canonical text signature.
const (
	sigTypeText = 0x01
)

// Hash algorithms (RFC 4880 §9.4). The weak ones — MD5, SHA-1, RIPEMD-160 —
// are absent on purpose and fail as unsupported.
const (
	hashSHA256 = 8
	hashSHA384 = 9
	hashSHA512 = 10
	hashSHA224 = 11
)

// Subpackets this verifier reads (RFC 4880 §5.2.3.1; issuer fingerprint
// from RFC 9580).
const (
	subpacketIssuerKeyID       = 16
	subpacketIssuerFingerprint = 33
)

// The v4 signature packet (RFC 4880 §5.2.3): version, type, public key
// algorithm, hash algorithm, a hashed subpacket area, an unhashed one, the
// left two octets of the digest, then the algorithm's integers.
const (
	sigVersion4    = 4
	sigHeaderLen   = 6
	sigTrailerByte = 0xFF
	left16Len      = 2

	errTruncatedSignature = "%w: truncated signature"
)

// signature is one parsed v4 signature packet.
type signature struct {
	sigType byte
	pubAlg  byte
	hashAlg byte
	// hashed is the hashed subpacket area, raw: it is part of what was signed.
	hashed []byte
	// issuerFingerprint or issuerKeyID name the signer, when the signature
	// says; both hex, uppercase.
	issuerFingerprint string
	issuerKeyID       string
	left16            []byte
	integers          [][]byte
}

// parseSignature reads one v4 signature packet body.
func parseSignature(body []byte) (signature, error) {
	if len(body) < sigHeaderLen {
		return signature{}, fmt.Errorf(errTruncatedSignature, ErrPacket)
	}

	if body[0] != sigVersion4 {
		return signature{}, fmt.Errorf("%w: signature version %d", ErrUnsupported, body[0])
	}

	sig := signature{sigType: body[1], pubAlg: body[2], hashAlg: body[3]}

	hashed, rest, err := lengthPrefixed(body[fourOctets:])
	if err != nil {
		return signature{}, err
	}

	unhashed, rest, err := lengthPrefixed(rest)
	if err != nil {
		return signature{}, err
	}

	if len(rest) < left16Len {
		return signature{}, fmt.Errorf(errTruncatedSignature, ErrPacket)
	}

	sig.hashed = hashed
	sig.left16 = rest[:left16Len]
	rest = rest[left16Len:]

	for len(rest) > 0 {
		var integer []byte

		integer, rest, err = readMPI(rest)
		if err != nil {
			return signature{}, err
		}

		sig.integers = append(sig.integers, integer)
	}

	// The issuer is a hint the verification proves or disproves, so the
	// unhashed area's word is as good as the hashed area's here.
	for _, area := range [][]byte{hashed, unhashed} {
		if err := sig.readIssuer(area); err != nil {
			return signature{}, err
		}
	}

	return sig, nil
}

// lengthPrefixed splits a two-octet-length-prefixed area off data.
func lengthPrefixed(data []byte) (area, rest []byte, err error) {
	if len(data) < twoOctets {
		return nil, nil, fmt.Errorf(errTruncatedSignature, ErrPacket)
	}

	length := int(binary.BigEndian.Uint16(data))
	if len(data) < twoOctets+length {
		return nil, nil, fmt.Errorf(errTruncatedSignature, ErrPacket)
	}

	return data[twoOctets : twoOctets+length], data[twoOctets+length:], nil
}

// readIssuer takes the issuer fingerprint and key ID out of a subpacket
// area, keeping the first of each seen.
func (s *signature) readIssuer(area []byte) error {
	subpackets, err := readSubpackets(area)
	if err != nil {
		return err
	}

	for _, sub := range subpackets {
		switch sub.kind {
		case subpacketIssuerFingerprint:
			if s.issuerFingerprint == "" && len(sub.data) == 1+fingerprintLen && sub.data[0] == keyVersion4 {
				s.issuerFingerprint = strings.ToUpper(hex.EncodeToString(sub.data[1:]))
			}
		case subpacketIssuerKeyID:
			if s.issuerKeyID == "" && len(sub.data) == keyIDLen {
				s.issuerKeyID = strings.ToUpper(hex.EncodeToString(sub.data))
			}
		default:
			// Not read.
		}
	}

	return nil
}

// names reports whether the signature says key made it — or says nothing,
// in which case every key is a candidate.
func (s *signature) names(key Key) bool {
	switch {
	case s.issuerFingerprint != "":
		return s.issuerFingerprint == string(key.Fingerprint)
	case s.issuerKeyID != "":
		return s.issuerKeyID == key.keyID()
	default:
		return true
	}
}

// hash is the digest the signature names, or unsupported.
func (s *signature) hash() (crypto.Hash, error) {
	switch s.hashAlg {
	case hashSHA256:
		return crypto.SHA256, nil
	case hashSHA384:
		return crypto.SHA384, nil
	case hashSHA512:
		return crypto.SHA512, nil
	case hashSHA224:
		return crypto.SHA224, nil
	default:
		return 0, fmt.Errorf("%w: hash algorithm %d", ErrUnsupported, s.hashAlg)
	}
}

// trailer is what a v4 signature hashes after the data (RFC 4880 §5.2.4):
// the header up to and including the hashed subpackets, then 0x04 0xFF and
// the length of that header as four octets.
func (s *signature) trailer() []byte {
	hashedLen := len(s.hashed)

	out := append([]byte{sigVersion4, s.sigType, s.pubAlg, s.hashAlg}, twoOctetLength(hashedLen)...)
	out = append(out, s.hashed...)
	out = append(out, sigVersion4, sigTrailerByte)

	return binary.BigEndian.AppendUint32(
		out,
		uint32(sigHeaderLen+hashedLen),
	) // #nosec G115 -- a two-octet length plus six.
}

// verify checks the signature over data with key.
func (s *signature) verify(key Key, data []byte) error {
	if key.public == nil || !sameFamily(key.algorithm, s.pubAlg) {
		return fmt.Errorf("%w: key algorithm %d, signature algorithm %d", ErrUnsupported, key.algorithm, s.pubAlg)
	}

	hashID, err := s.hash()
	if err != nil {
		return err
	}

	hasher := hashID.New()
	_, _ = hasher.Write(data)
	_, _ = hasher.Write(s.trailer())
	digest := hasher.Sum(nil)

	if digest[0] != s.left16[0] || digest[1] != s.left16[1] {
		return fmt.Errorf("%w: digest does not match", ErrSignature)
	}

	switch public := key.public.(type) {
	case *rsa.PublicKey:
		if len(s.integers) != 1 {
			return fmt.Errorf("%w: RSA signature with %d integers", ErrPacket, len(s.integers))
		}

		if err := rsa.VerifyPKCS1v15(public, hashID, digest, leftPad(s.integers[0], public.Size())); err != nil {
			return fmt.Errorf("%w: %w", ErrSignature, err)
		}
	case ed25519.PublicKey:
		if len(s.integers) != twoOctets {
			return fmt.Errorf("%w: EdDSA signature with %d integers", ErrPacket, len(s.integers))
		}

		half := ed25519.SignatureSize / 2

		sig := append(leftPad(s.integers[0], half), leftPad(s.integers[1], half)...)
		if !ed25519.Verify(public, digest, sig) {
			return ErrSignature
		}
	default:
		return fmt.Errorf("%w: key algorithm %d", ErrUnsupported, key.algorithm)
	}

	return nil
}

// sameFamily reports whether a key's algorithm can carry a signature's:
// the two RSA identifiers are one family.
func sameFamily(keyAlg, sigAlg byte) bool {
	isRSA := func(alg byte) bool { return alg == algRSA || alg == algRSASignOnly }

	return keyAlg == sigAlg || (isRSA(keyAlg) && isRSA(sigAlg))
}

// leftPad restores the leading zero octets an integer's encoding dropped,
// to the width the verifier expects.
func leftPad(value []byte, width int) []byte {
	if len(value) >= width {
		return value
	}

	out := make([]byte, width)
	copy(out[width-len(value):], value)

	return out
}

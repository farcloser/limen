// Package openpgptest produces the material the openpgp verifier reads —
// an armored public key block, a clearsigned message — from a key it
// generates, so a test owns both sides of a signature without a PGP tool
// on the machine. It writes the packets by hand and shares no code with the
// verifier: a misreading of the format on one side fails against the other.
package openpgptest

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- a v4 fingerprint is SHA-1 by definition.
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Signer is a generated key that clearsigns text.
type Signer struct {
	rsaKey  *rsa.PrivateKey
	edKey   ed25519.PrivateKey
	created uint32
	// keyBody is the public key packet body, what the fingerprint hashes.
	keyBody []byte
}

// The constants of the packets written here (RFC 4880).
const (
	algRSA   = 1
	algEdDSA = 22

	tagSignature = 2
	tagPublicKey = 6
	tagUserID    = 13

	version4 = 4

	sigTypeText = 0x01

	subpacketCreation          = 2
	subpacketIssuerKeyID       = 16
	subpacketIssuerFingerprint = 33

	rsaBits         = 2048
	ed25519Prefix   = 0x40
	ed25519OIDLen   = 9
	armorLineLength = 64
	crcInit         = 0xB704CE
	crcPoly         = 0x1864CFB
	crcMask         = 0xFFFFFF
	crcTop          = 0x1000000
	crcShiftHigh    = 16
	newHeader       = 0xC0
	lengthTwoBase   = 192
	lengthTwoShift  = 8
	lengthOneMax    = 191
	lengthTwoMax    = 8383
	lengthFive      = 255
	fingerprintTag  = 0x99
	sigTrailerByte  = 0xFF
	keyIDLen        = 8
	userID          = "limen test <test@example.invalid>"
)

// errNoHashNumber is a digest OpenPGP has no number for here.
var errNoHashNumber = errors.New("openpgptest: no OpenPGP number for the hash")

// ed25519OID is the curve OID an EdDSA key packet opens with.
//
//nolint:gochecknoglobals // immutable constant.
var ed25519OID = []byte{0x2B, 0x06, 0x01, 0x04, 0x01, 0xDA, 0x47, 0x0F, 0x01}

// hashIDs maps a crypto.Hash to its OpenPGP algorithm number, the weak
// SHA-1 included so a test can produce what the verifier must refuse.
//
//nolint:gochecknoglobals,mnd // immutable table; the numbers are RFC 4880's hash algorithm ids.
var hashIDs = map[crypto.Hash]byte{
	crypto.SHA1:   2,
	crypto.SHA256: 8,
	crypto.SHA384: 9,
	crypto.SHA512: 10,
	crypto.SHA224: 11,
}

// NewRSA generates an RSA signer.
func NewRSA() (*Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, fmt.Errorf("generating the RSA key: %w", err)
	}

	signer := &Signer{rsaKey: key, created: uint32(time.Now().Unix())} // #nosec G115 -- a Unix time fits until 2106.
	signer.keyBody = signer.publicKeyBody()

	return signer, nil
}

// NewEd25519 generates an Ed25519 signer.
func NewEd25519() (*Signer, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating the Ed25519 key: %w", err)
	}

	signer := &Signer{edKey: key, created: uint32(time.Now().Unix())} // #nosec G115 -- a Unix time fits until 2106.
	signer.keyBody = signer.publicKeyBody()

	return signer, nil
}

// Fingerprint is the key's v4 fingerprint, 40 uppercase hex digits.
func (s *Signer) Fingerprint() string {
	hash := sha1.New() // #nosec G401 -- see the import.
	_, _ = hash.Write([]byte{fingerprintTag})
	_, _ = hash.Write(twoOctets(len(s.keyBody)))
	_, _ = hash.Write(s.keyBody)

	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil)))
}

// PublicKeyBlock is the key as an armored public key block: the key packet
// and a user ID, which is all a verifier that trusts by fingerprint needs.
func (s *Signer) PublicKeyBlock() []byte {
	body := append(packet(tagPublicKey, s.keyBody), packet(tagUserID, []byte(userID))...)

	return armor("PUBLIC KEY BLOCK", body)
}

// Clearsign signs text as a clearsigned message with the given digest.
func (s *Signer) Clearsign(text string, hash crypto.Hash) ([]byte, error) {
	hashID, known := hashIDs[hash]
	if !known {
		return nil, fmt.Errorf("%w: %v", errNoHashNumber, hash)
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	canonical := make([]string, 0, len(lines))
	escaped := make([]string, 0, len(lines))

	for _, line := range lines {
		canonical = append(canonical, strings.TrimRight(line, " \t"))

		if strings.HasPrefix(line, "-") {
			line = "- " + line
		}

		escaped = append(escaped, line)
	}

	sig, err := s.sign([]byte(strings.Join(canonical, "\r\n")), hash, hashID)
	if err != nil {
		return nil, err
	}

	var out strings.Builder

	_, _ = out.WriteString("-----BEGIN PGP SIGNED MESSAGE-----\nHash: " + hashName(hash) + "\n\n")
	_, _ = out.WriteString(strings.Join(escaped, "\n") + "\n")
	_, _ = out.Write(armor("SIGNATURE", packet(tagSignature, sig)))

	return []byte(out.String()), nil
}

// sign builds a v4 canonical text signature packet body over data.
func (s *Signer) sign(data []byte, hash crypto.Hash, hashID byte) ([]byte, error) {
	fingerprint, _ := hex.DecodeString(s.Fingerprint())

	hashed := subpacket(subpacketCreation, binary.BigEndian.AppendUint32(nil, s.created))
	hashed = append(hashed, subpacket(subpacketIssuerFingerprint, append([]byte{version4}, fingerprint...))...)
	unhashed := subpacket(subpacketIssuerKeyID, fingerprint[len(fingerprint)-keyIDLen:])

	header := append([]byte{version4, sigTypeText, s.algorithm(), hashID}, twoOctets(len(hashed))...)
	header = append(header, hashed...)

	trailer := binary.BigEndian.AppendUint32(
		[]byte{version4, sigTrailerByte},
		uint32(len(header)),
	) // #nosec G115 -- a few subpackets.

	hasher := hash.New()
	_, _ = hasher.Write(data)
	_, _ = hasher.Write(header)
	_, _ = hasher.Write(trailer)
	digest := hasher.Sum(nil)

	body := slices.Clone(header)
	body = append(body, twoOctets(len(unhashed))...)
	body = append(body, unhashed...)
	body = append(body, digest[0], digest[1])

	if s.rsaKey != nil {
		raw, err := rsa.SignPKCS1v15(rand.Reader, s.rsaKey, hash, digest)
		if err != nil {
			return nil, fmt.Errorf("signing: %w", err)
		}

		return append(body, mpi(raw)...), nil
	}

	raw := ed25519.Sign(s.edKey, digest)
	half := len(raw) / 2

	body = append(body, mpi(raw[:half])...)

	return append(body, mpi(raw[half:])...), nil
}

// publicKeyBody is the v4 public key packet body.
func (s *Signer) publicKeyBody() []byte {
	body := binary.BigEndian.AppendUint32([]byte{version4}, s.created)
	body = append(body, s.algorithm())

	if s.rsaKey != nil {
		body = append(body, mpi(s.rsaKey.N.Bytes())...)

		return append(body, mpi(big.NewInt(int64(s.rsaKey.E)).Bytes())...)
	}

	body = append(body, ed25519OIDLen)
	body = append(body, ed25519OID...)

	public, isEd25519 := s.edKey.Public().(ed25519.PublicKey)
	if !isEd25519 {
		panic("an Ed25519 private key's public half is an Ed25519 public key")
	}

	return append(body, mpi(append([]byte{ed25519Prefix}, public...))...)
}

func (s *Signer) algorithm() byte {
	if s.rsaKey != nil {
		return algRSA
	}

	return algEdDSA
}

// mpi encodes a big-endian magnitude as a multiprecision integer.
func mpi(value []byte) []byte {
	for len(value) > 0 && value[0] == 0 {
		value = value[1:]
	}

	bits := 0
	if len(value) > 0 {
		bits = (len(value)-1)*8 + new(big.Int).SetBytes(value[:1]).BitLen()
	}

	return append([]byte{byte(bits >> 8), byte(bits)}, value...) // #nosec G115 -- a key is far below 2^16 bits.
}

// packet frames a body as a new-format packet.
func packet(tag byte, body []byte) []byte {
	return append(append([]byte{newHeader | tag}, length(len(body))...), body...)
}

// subpacket frames a subpacket: its length covers the type octet.
func subpacket(kind byte, data []byte) []byte {
	return append(append(length(1+len(data)), kind), data...)
}

// length is a new-format length: one, two, or five octets.
func length(count int) []byte {
	switch {
	case count <= lengthOneMax:
		return []byte{byte(count)} // #nosec G115 -- under 192.
	case count <= lengthTwoMax:
		count -= lengthTwoBase

		return []byte{
			byte(count>>lengthTwoShift + lengthTwoBase),
			byte(count),
		} // #nosec G115 -- the low octet is what the format wants.
	default:
		return binary.BigEndian.AppendUint32([]byte{lengthFive}, uint32(count)) // #nosec G115 -- a test payload.
	}
}

// twoOctets is a length as two big-endian octets.
func twoOctets(count int) []byte {
	return []byte{byte(count >> 8), byte(count)} // #nosec G115 -- bounded at two octets by the format.
}

// armor frames body as an armored block of the given kind, checksum line
// included.
func armor(kind string, body []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(body)

	var out strings.Builder

	_, _ = out.WriteString("-----BEGIN PGP " + kind + "-----\n\n")

	for len(encoded) > armorLineLength {
		_, _ = out.WriteString(encoded[:armorLineLength] + "\n")
		encoded = encoded[armorLineLength:]
	}

	_, _ = out.WriteString(encoded + "\n")

	sum := binary.BigEndian.AppendUint32(nil, crc24(body))[1:]
	_, _ = out.WriteString("=" + base64.StdEncoding.EncodeToString(sum) + "\n")
	_, _ = out.WriteString("-----END PGP " + kind + "-----\n")

	return []byte(out.String())
}

func crc24(data []byte) uint32 {
	crc := uint32(crcInit)

	for _, octet := range data {
		crc ^= uint32(octet) << crcShiftHigh

		for range 8 {
			crc <<= 1
			if crc&crcTop != 0 {
				crc ^= crcPoly
			}
		}
	}

	return crc & crcMask
}

func hashName(hash crypto.Hash) string {
	return strings.ReplaceAll(hash.String(), "-", "")
}

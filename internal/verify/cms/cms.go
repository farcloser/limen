// Package cms verifies a detached CMS signature (RFC 5652 SignedData, the
// PKCS#7 shape vendors publish beside a download) over content, against an
// X.509 chain that ends at a root the caller pins by its SHA-256 fingerprint.
// The standard library has the X.509 half; this package carries the ASN.1
// shape of SignedData and the signed-attributes rules, so limen keeps zero
// dependencies, as internal/verify/openpgp does for clearsigned text.
//
// What a successful verification means: one signer of the signature holds a
// certificate that chains, through the certificates the signature carries,
// to the pinned root, found by its fingerprint among the roots the caller
// supplies and the certificates the signature carries, never in the system's
// store (which differs by platform, and whose code-signing roots Go's macOS
// verifier refuses); that certificate is for code signing and valid at the
// given time; the signed attributes, when present, name the content's digest
// and the signature covers them, or the signature covers the content itself.
package cms

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Verified is what a signature vouched for.
type Verified struct {
	// Signer is the certificate whose key made the signature.
	Signer *x509.Certificate
	// Root is the pinned root the signer's chain ends at.
	Root *x509.Certificate
}

var (
	// ErrShape is a signature that is not a CMS SignedData.
	ErrShape = errors.New("not a CMS SignedData signature")
	// ErrSignature is a signature that does not verify over the content.
	ErrSignature = errors.New("the signature does not verify over the content")
	// ErrChain is a signer whose certificate does not chain to the pinned root.
	ErrChain = errors.New("the signer's certificate does not chain to the pinned root")
	// ErrFingerprint is a root fingerprint that is not a SHA-256.
	ErrFingerprint = errors.New("the root fingerprint must be 64 hex digits")
)

// The object identifiers the shape uses.
//
//nolint:gochecknoglobals // immutable identifiers.
var (
	oidSignedData        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidAttrContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidAttrMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSHA256            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSAEncryption     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSAPSS            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidSHA256WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidECDSAWithSHA256   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidECPublicKey       = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
)

// The ASN.1 shapes of RFC 5652, as encoding/asn1 reads them.
type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulatedContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type encapsulatedContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     []byte `asn1:"explicit,optional,tag:0"`
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type issuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}

// Verify checks that signature, a DER ContentInfo carrying a SignedData
// detached from content, signs content, and that a verified signer's
// certificate chains at the time now to the root whose SHA-256 fingerprint
// is rootFingerprint (hex, case and colons aside), the root being one of
// roots or one of the certificates the signature carries.
func Verify(
	content, signature []byte,
	roots []*x509.Certificate,
	rootFingerprint string,
	now time.Time,
) (Verified, error) {
	pin, err := parseFingerprint(rootFingerprint)
	if err != nil {
		return Verified{}, err
	}

	signed, certs, err := parse(signature)
	if err != nil {
		return Verified{}, err
	}

	if len(signed.EncapContentInfo.Content) != 0 && !bytes.Equal(signed.EncapContentInfo.Content, content) {
		return Verified{}, fmt.Errorf("%w: the signature carries content that is not the content given", ErrSignature)
	}

	var failures []error

	for _, info := range signed.SignerInfos {
		verified, err := verifySigner(info, content, certs, roots, pin, now)
		if err == nil {
			return verified, nil
		}

		failures = append(failures, err)
	}

	if len(failures) == 0 {
		return Verified{}, fmt.Errorf("%w: no signer", ErrShape)
	}

	return Verified{}, errors.Join(failures...)
}

// parseFingerprint reads a SHA-256 fingerprint as a pin writes it.
func parseFingerprint(text string) ([sha256.Size]byte, error) {
	var pin [sha256.Size]byte

	clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), ":", ""))

	raw, err := hex.DecodeString(clean)
	if err != nil || len(raw) != sha256.Size {
		return pin, fmt.Errorf("%w: %q", ErrFingerprint, text)
	}

	copy(pin[:], raw)

	return pin, nil
}

// parse reads the ContentInfo and the certificates it carries.
func parse(signature []byte) (signedData, []*x509.Certificate, error) {
	var info contentInfo

	rest, err := asn1.Unmarshal(signature, &info)
	if err != nil || len(rest) != 0 {
		return signedData{}, nil, fmt.Errorf("%w: %w", ErrShape, orTrailing(err))
	}

	if !info.ContentType.Equal(oidSignedData) {
		return signedData{}, nil, fmt.Errorf("%w: content type %s", ErrShape, info.ContentType)
	}

	var signed signedData

	rest, err = asn1.Unmarshal(info.Content.Bytes, &signed)
	if err != nil || len(rest) != 0 {
		return signedData{}, nil, fmt.Errorf("%w: SignedData: %w", ErrShape, orTrailing(err))
	}

	if !signed.EncapContentInfo.ContentType.Equal(oidData) {
		return signedData{}, nil, fmt.Errorf(
			"%w: encapsulated content type %s",
			ErrShape,
			signed.EncapContentInfo.ContentType,
		)
	}

	var certs []*x509.Certificate

	if len(signed.Certificates.Bytes) != 0 {
		certs, err = x509.ParseCertificates(signed.Certificates.Bytes)
		if err != nil {
			return signedData{}, nil, fmt.Errorf("%w: certificates: %w", ErrShape, err)
		}
	}

	return signed, certs, nil
}

var errTrailing = errors.New("trailing bytes")

func orTrailing(err error) error {
	if err != nil {
		return err
	}

	return errTrailing
}

// verifySigner verifies one SignerInfo: its certificate is found among the
// carried ones, the signature verifies over the signed attributes (which
// then name the content's digest) or over the content, and the certificate
// chains to the pinned root.
func verifySigner(
	info signerInfo,
	content []byte,
	certs, roots []*x509.Certificate,
	pin [sha256.Size]byte,
	now time.Time,
) (Verified, error) {
	signer, err := findSigner(info, certs)
	if err != nil {
		return Verified{}, err
	}

	algorithm, hash, err := signatureAlgorithm(info)
	if err != nil {
		return Verified{}, err
	}

	signed := content

	if len(info.SignedAttrs.Bytes) != 0 {
		if attrErr := checkSignedAttributes(info.SignedAttrs, content, hash); attrErr != nil {
			return Verified{}, attrErr
		}
		// The attributes are signed as a SET (tag 0x31), carried as [0]
		// IMPLICIT: the same bytes under the other tag.
		signed = append([]byte{asn1.TagSet | 0x20}, info.SignedAttrs.FullBytes[1:]...)
	}

	if sigErr := signer.CheckSignature(algorithm, signed, info.Signature); sigErr != nil {
		return Verified{}, fmt.Errorf("%w: %w", ErrSignature, sigErr)
	}

	// Absent extended key usage means "any" to x509; a code-signing
	// certificate says so, and a signature by one that does not is not the
	// vendor's release signature.
	if !slices.Contains(signer.ExtKeyUsage, x509.ExtKeyUsageCodeSigning) &&
		!slices.Contains(signer.ExtKeyUsage, x509.ExtKeyUsageAny) {
		return Verified{}, fmt.Errorf("%w: the signer's certificate is not for code signing", ErrChain)
	}

	root, err := chainTo(signer, certs, roots, pin, now)
	if err != nil {
		return Verified{}, err
	}

	return Verified{Signer: signer, Root: root}, nil
}

// findSigner is the carried certificate the SignerInfo names, by issuer and
// serial (version 1) or by subject key identifier (version 3).
func findSigner(info signerInfo, certs []*x509.Certificate) (*x509.Certificate, error) {
	if info.SID.Class == asn1.ClassContextSpecific && info.SID.Tag == 0 {
		for _, cert := range certs {
			if bytes.Equal(cert.SubjectKeyId, info.SID.Bytes) {
				return cert, nil
			}
		}

		return nil, fmt.Errorf("%w: no carried certificate has the signer's subject key identifier", ErrChain)
	}

	var sid issuerAndSerial
	if _, err := asn1.Unmarshal(info.SID.FullBytes, &sid); err != nil {
		return nil, fmt.Errorf("%w: signer identifier: %w", ErrShape, err)
	}

	for _, cert := range certs {
		if cert.SerialNumber.Cmp(sid.SerialNumber) == 0 && bytes.Equal(cert.RawIssuer, sid.Issuer.FullBytes) {
			return cert, nil
		}
	}

	return nil, fmt.Errorf("%w: no carried certificate has the signer's issuer and serial", ErrChain)
}

// signatureAlgorithm maps the SignerInfo's algorithm identifiers to what
// x509 verifies with, and to the hash the message digest attribute uses.
func signatureAlgorithm(info signerInfo) (x509.SignatureAlgorithm, crypto.Hash, error) {
	hash, err := digestHash(info.DigestAlgorithm.Algorithm)
	if err != nil {
		return x509.UnknownSignatureAlgorithm, 0, err
	}

	sig := info.SignatureAlgorithm.Algorithm

	switch {
	case sig.Equal(oidSHA256WithRSA):
		return x509.SHA256WithRSA, crypto.SHA256, nil
	case sig.Equal(oidSHA384WithRSA):
		return x509.SHA384WithRSA, crypto.SHA384, nil
	case sig.Equal(oidSHA512WithRSA):
		return x509.SHA512WithRSA, crypto.SHA512, nil
	case sig.Equal(oidECDSAWithSHA256):
		return x509.ECDSAWithSHA256, crypto.SHA256, nil
	case sig.Equal(oidECDSAWithSHA384):
		return x509.ECDSAWithSHA384, crypto.SHA384, nil
	case sig.Equal(oidECDSAWithSHA512):
		return x509.ECDSAWithSHA512, crypto.SHA512, nil
	case sig.Equal(oidRSAEncryption), sig.Equal(oidECPublicKey):
		// The signature algorithm names the key alone; the digest algorithm
		// says the hash (what openssl cms writes).
		return pairedAlgorithm(hash, sig.Equal(oidRSAEncryption), false)
	case sig.Equal(oidRSAPSS):
		return pairedAlgorithm(hash, true, true)
	default:
		return x509.UnknownSignatureAlgorithm, 0, fmt.Errorf("%w: signature algorithm %s", ErrShape, sig)
	}
}

// digestHash maps a digest algorithm identifier to its hash.
func digestHash(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(oidSHA256):
		return crypto.SHA256, nil
	case oid.Equal(oidSHA384):
		return crypto.SHA384, nil
	case oid.Equal(oidSHA512):
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("%w: digest algorithm %s", ErrShape, oid)
	}
}

// pairedAlgorithm is the x509 algorithm for a key kind and a hash.
func pairedAlgorithm(hash crypto.Hash, rsa, pss bool) (x509.SignatureAlgorithm, crypto.Hash, error) {
	table := map[crypto.Hash][3]x509.SignatureAlgorithm{
		crypto.SHA256: {x509.ECDSAWithSHA256, x509.SHA256WithRSA, x509.SHA256WithRSAPSS},
		crypto.SHA384: {x509.ECDSAWithSHA384, x509.SHA384WithRSA, x509.SHA384WithRSAPSS},
		crypto.SHA512: {x509.ECDSAWithSHA512, x509.SHA512WithRSA, x509.SHA512WithRSAPSS},
	}

	row, known := table[hash]
	if !known {
		return x509.UnknownSignatureAlgorithm, 0, fmt.Errorf("%w: no signature algorithm for the digest", ErrShape)
	}

	switch {
	case pss:
		return row[2], hash, nil
	case rsa:
		return row[1], hash, nil
	default:
		return row[0], hash, nil
	}
}

// checkSignedAttributes holds the signed attributes to RFC 5652 11.1 and
// 11.2: a content-type naming data and a message-digest equal to the
// content's digest under the signer's digest algorithm.
func checkSignedAttributes(raw asn1.RawValue, content []byte, hash crypto.Hash) error {
	var attrs []attribute

	// The attributes are [0] IMPLICIT SET OF Attribute: read as a SET.
	set := append([]byte{asn1.TagSet | 0x20}, raw.FullBytes[1:]...)

	rest, err := asn1.UnmarshalWithParams(set, &attrs, "set")
	if err != nil || len(rest) != 0 {
		return fmt.Errorf("%w: signed attributes: %w", ErrShape, orTrailing(err))
	}

	var contentType asn1.ObjectIdentifier

	var messageDigest []byte

	for _, attr := range attrs {
		switch {
		case attr.Type.Equal(oidAttrContentType):
			if _, attrErr := asn1.Unmarshal(attr.Values.Bytes, &contentType); attrErr != nil {
				return fmt.Errorf("%w: content-type attribute: %w", ErrShape, attrErr)
			}
		case attr.Type.Equal(oidAttrMessageDigest):
			if _, attrErr := asn1.Unmarshal(attr.Values.Bytes, &messageDigest); attrErr != nil {
				return fmt.Errorf("%w: message-digest attribute: %w", ErrShape, attrErr)
			}
		}
	}

	if !contentType.Equal(oidData) {
		return fmt.Errorf("%w: the signed attributes do not name data as the content type", ErrSignature)
	}

	digest := hash.New()
	digest.Write(content)

	if !bytes.Equal(messageDigest, digest.Sum(nil)) {
		return fmt.Errorf("%w: the signed message digest is not the content's", ErrSignature)
	}

	return nil
}

// chainTo verifies the signer's chain, for code signing at the time now,
// to the pinned root: the certificates with that fingerprint among roots and
// the carried ones are the only trust anchors, the other carried ones the
// intermediates.
func chainTo(
	signer *x509.Certificate,
	certs, roots []*x509.Certificate,
	pin [sha256.Size]byte,
	now time.Time,
) (*x509.Certificate, error) {
	intermediates := x509.NewCertPool()
	anchors := x509.NewCertPool()
	pinned := 0

	for _, cert := range slices.Concat(roots, certs) {
		switch {
		case sha256.Sum256(cert.Raw) == pin:
			anchors.AddCert(cert)

			pinned++
		case !cert.Equal(signer):
			intermediates.AddCert(cert)
		}
	}

	if pinned == 0 {
		return nil, fmt.Errorf("%w: no certificate with the pinned fingerprint among the roots given"+
			" and the certificates the signature carries", ErrChain)
	}

	chains, err := signer.Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		Roots:         anchors,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrChain, err)
	}

	return chains[0][len(chains[0])-1], nil
}

// ParseCertificates reads certificates as a CA publishes them: PEM blocks, a
// DER certificate or concatenation, or a certs-only PKCS#7 (.p7c).
func ParseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	rest := data

	for {
		block, after := pem.Decode(rest)
		if block == nil {
			break
		}

		rest = after

		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: PEM certificate: %w", ErrShape, err)
		}

		certs = append(certs, cert)
	}

	if len(certs) > 0 {
		return certs, nil
	}

	if parsed, err := x509.ParseCertificates(data); err == nil && len(parsed) > 0 {
		return parsed, nil
	}

	_, certs, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: neither PEM, DER certificates nor a certs-only PKCS#7", ErrShape)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: the PKCS#7 carries no certificate", ErrShape)
	}

	return certs, nil
}

// Fingerprint is a certificate's SHA-256 fingerprint as a pin writes it.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)

	return hex.EncodeToString(sum[:])
}

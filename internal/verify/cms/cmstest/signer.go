// Package cmstest makes the material the cms tests need: a chain of fresh
// certificates (a root, an intermediate, a code-signing leaf) and detached
// CMS signatures made with the leaf, in the shapes vendors publish (signed
// attributes, or a signature over the content alone).
package cmstest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// The sizes and names the chain is made with.
const (
	rsaKeyBits         = 2048
	testOrganization   = "Test"
	longFormLengthMask = 0x7f
)

// Chain is a root, an intermediate and a leaf, with the leaf's key.
type Chain struct {
	Root         *x509.Certificate
	Intermediate *x509.Certificate
	Leaf         *x509.Certificate
	key          crypto.Signer
	rsa          bool
}

// Options shape the chain.
type Options struct {
	// RSA keys the leaf with RSA (the vendors' usual) rather than ECDSA P-256.
	RSA bool
	// NoCodeSigning leaves the code-signing purpose off the leaf.
	NoCodeSigning bool
	// Subject is the leaf's subject; "CN=Test Signer,O=Test" when empty.
	Subject pkix.Name
	// NotAfter is the leaf's expiry; a year from now when zero.
	NotAfter time.Time
}

// NewChain makes a fresh chain.
func NewChain(opts Options) (*Chain, error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("root key: %w", err)
	}

	intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("intermediate key: %w", err)
	}

	var leafKey crypto.Signer

	if opts.RSA {
		leafKey, err = rsa.GenerateKey(rand.Reader, rsaKeyBits)
	} else {
		leafKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}

	if err != nil {
		return nil, fmt.Errorf("leaf key: %w", err)
	}

	now := time.Now()

	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root", Organization: []string{testOrganization}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	root, err := selfSigned(rootTemplate, rootKey)
	if err != nil {
		return nil, err
	}

	intermediateTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Intermediate", Organization: []string{testOrganization}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(5 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}

	intermediate, err := issue(intermediateTemplate, root, intermediateKey.Public(), rootKey)
	if err != nil {
		return nil, err
	}

	subject := opts.Subject
	if subject.CommonName == "" {
		subject = pkix.Name{CommonName: "Test Signer", Organization: []string{testOrganization}}
	}

	notAfter := opts.NotAfter
	if notAfter.IsZero() {
		notAfter = now.Add(365 * 24 * time.Hour)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      subject,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		SubjectKeyId: []byte{3, 3, 3, 3},
	}

	if !opts.NoCodeSigning {
		leafTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}
	}

	leaf, err := issue(leafTemplate, intermediate, leafKey.Public(), intermediateKey)
	if err != nil {
		return nil, err
	}

	return &Chain{Root: root, Intermediate: intermediate, Leaf: leaf, key: leafKey, rsa: opts.RSA}, nil
}

func selfSigned(template *x509.Certificate, key *ecdsa.PrivateKey) (*x509.Certificate, error) {
	return issue(template, template, key.Public(), key)
}

func issue(
	template, parent *x509.Certificate,
	public crypto.PublicKey,
	signer crypto.Signer,
) (*x509.Certificate, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, public, signer)
	if err != nil {
		return nil, fmt.Errorf("issuing %s: %w", template.Subject.CommonName, err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", template.Subject.CommonName, err)
	}

	return cert, nil
}

// SignOptions shape a signature.
type SignOptions struct {
	// NoAttributes signs the content itself rather than signed attributes.
	NoAttributes bool
	// OmitRoot leaves the root out of the carried certificates.
	OmitRoot bool
	// OmitIntermediate leaves the intermediate out, breaking the chain.
	OmitIntermediate bool
	// WrongDigest writes a message-digest attribute for other content.
	WrongDigest bool
	// BySubjectKeyID identifies the signer by subject key identifier
	// (SignerInfo version 3) rather than by issuer and serial.
	BySubjectKeyID bool
}

// The identifiers the signer writes.
//
//nolint:gochecknoglobals // immutable identifiers.
var (
	oidSignedData        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidAttrContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidAttrMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSHA256            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidRSAEncryption     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidECDSAWithSHA256   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type issuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
}

type encapsulatedContentInfo struct {
	ContentType asn1.ObjectIdentifier
}

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	EncapContentInfo encapsulatedContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

// Sign makes a detached SignedData over content, SHA-256 throughout, with
// the leaf: ECDSA as ecdsa-with-SHA256, RSA as rsaEncryption beside a
// sha256 digest algorithm, the pair openssl cms writes.
func (c *Chain) Sign(content []byte, opts SignOptions) ([]byte, error) {
	digest := sha256.Sum256(content)
	if opts.WrongDigest {
		digest = sha256.Sum256(append(content, '!'))
	}

	toSign := content

	var attrs asn1.RawValue

	if !opts.NoAttributes {
		set, err := asn1.MarshalWithParams([]attribute{
			{Type: oidAttrContentType, Values: []asn1.RawValue{mustMarshal(oidData)}},
			{Type: oidAttrMessageDigest, Values: []asn1.RawValue{mustMarshal(digest[:])}},
		}, "set")
		if err != nil {
			return nil, fmt.Errorf("attributes: %w", err)
		}

		toSign = set
		attrs = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: set[2:]}

		if set[1]&0x80 != 0 {
			// A long-form length: the header is longer than two bytes.
			attrs.Bytes = set[2+int(set[1]&longFormLengthMask):]
		}
	}

	hashed := sha256.Sum256(toSign)

	signature, err := c.key.Sign(nil, hashed[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}

	sigAlg := pkix.AlgorithmIdentifier{Algorithm: oidECDSAWithSHA256}
	if c.rsa {
		sigAlg = pkix.AlgorithmIdentifier{Algorithm: oidRSAEncryption, Parameters: asn1.NullRawValue}
	}

	info := signerInfo{
		Version: 1,
		SID: mustMarshal(
			issuerAndSerial{Issuer: asn1.RawValue{FullBytes: c.Leaf.RawIssuer}, SerialNumber: c.Leaf.SerialNumber},
		),
		DigestAlgorithm:    pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue},
		SignedAttrs:        attrs,
		SignatureAlgorithm: sigAlg,
		Signature:          signature,
	}

	if opts.BySubjectKeyID {
		info.Version = 3
		info.SID = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, Bytes: c.Leaf.SubjectKeyId}
	}

	var certs []byte

	certs = append(certs, c.Leaf.Raw...)

	if !opts.OmitIntermediate {
		certs = append(certs, c.Intermediate.Raw...)
	}

	if !opts.OmitRoot {
		certs = append(certs, c.Root.Raw...)
	}

	signed := signedData{
		Version:          info.Version,
		DigestAlgorithms: []pkix.AlgorithmIdentifier{{Algorithm: oidSHA256, Parameters: asn1.NullRawValue}},
		EncapContentInfo: encapsulatedContentInfo{ContentType: oidData},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certs},
		SignerInfos:      []signerInfo{info},
	}

	body, err := asn1.Marshal(signed)
	if err != nil {
		return nil, fmt.Errorf("SignedData: %w", err)
	}

	out, err := asn1.Marshal(
		contentInfo{
			ContentType: oidSignedData,
			Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: body},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("ContentInfo: %w", err)
	}

	return out, nil
}

func mustMarshal(v any) asn1.RawValue {
	der, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}

	return asn1.RawValue{FullBytes: der}
}

// CertsOnly is a certs-only SignedData (the .p7c a CA publishes) carrying
// the given certificates.
func CertsOnly(certs ...*x509.Certificate) ([]byte, error) {
	var raw []byte
	for _, cert := range certs {
		raw = append(raw, cert.Raw...)
	}

	body, err := asn1.Marshal(signedData{
		Version:          1,
		DigestAlgorithms: []pkix.AlgorithmIdentifier{},
		EncapContentInfo: encapsulatedContentInfo{ContentType: oidData},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: raw},
		SignerInfos:      []signerInfo{},
	})
	if err != nil {
		return nil, fmt.Errorf("SignedData: %w", err)
	}

	out, err := asn1.Marshal(
		contentInfo{
			ContentType: oidSignedData,
			Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: body},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("ContentInfo: %w", err)
	}

	return out, nil
}

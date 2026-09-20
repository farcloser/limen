package openpgp

import (
	"fmt"
	"slices"
	"strings"
)

// The clearsigned frame (RFC 4880 §7): the signed-message line, `Hash:`
// headers, an empty line, the text with lines opening in a dash escaped by
// `- `, then the signature block.
const (
	clearsignBegin = "-----BEGIN PGP SIGNED MESSAGE-----"
	signatureBegin = armorBegin + kindSignature + armorDashes
	hashHeader     = "Hash:"
	dashEscape     = "- "
	crlf           = "\r\n"
)

// hashNames maps a `Hash:` header's names to the algorithm they announce.
//
//nolint:gochecknoglobals // immutable table.
var hashNames = map[string]byte{
	"SHA256": hashSHA256,
	"SHA384": hashSHA384,
	"SHA512": hashSHA512,
	"SHA224": hashSHA224,
}

// VerifyClearsigned checks a clearsigned message against keys and returns
// the text it vouches for, with the key that signed it. A message carrying
// several signatures verifies when any one of them does against a key
// given; the error otherwise is the last signature's.
func VerifyClearsigned(message []byte, keys []Key) (Verified, error) {
	doc, err := splitClearsigned(splitLines(message))
	if err != nil {
		return Verified{}, err
	}

	blk, err := dearmor(doc.signature, kindSignature)
	if err != nil {
		return Verified{}, err
	}

	packets, err := readPackets(blk.body)
	if err != nil {
		return Verified{}, err
	}

	canonical := []byte(strings.Join(doc.text, crlf))
	err = fmt.Errorf("%w: no signature packet", ErrPacket)

	for _, pkt := range packets {
		if pkt.tag != tagSignature {
			continue
		}

		var signer Fingerprint

		signer, err = verifyOne(pkt.body, doc.hashes, keys, canonical)
		if err == nil {
			return Verified{Text: []byte(strings.Join(doc.text, "\n") + "\n"), Signer: signer}, nil
		}
	}

	return Verified{}, err
}

// verifyOne checks one signature packet against the keys that could have
// made it.
func verifyOne(body, hashes []byte, keys []Key, canonical []byte) (Fingerprint, error) {
	sig, err := parseSignature(body)
	if err != nil {
		return "", err
	}

	if sig.sigType != sigTypeText {
		return "", fmt.Errorf("%w: signature type %#x on a clearsigned message", ErrSignature, sig.sigType)
	}

	if len(hashes) > 0 && !contains(hashes, sig.hashAlg) {
		return "", fmt.Errorf("%w: hash algorithm %d is not among the Hash headers", ErrSignature, sig.hashAlg)
	}

	err = ErrNoKey

	for _, key := range keys {
		if !sig.names(key) {
			continue
		}

		if err = sig.verify(key, canonical); err == nil {
			return key.Fingerprint, nil
		}
	}

	return "", err
}

// clearsigned is a clearsigned message taken apart.
type clearsigned struct {
	// hashes are the algorithms the Hash headers announce; none when there
	// is no header.
	hashes []byte
	// text is the signed text, line by line, dash-escapes removed and
	// trailing whitespace stripped — the canonical form both the digest and
	// the caller read.
	text []string
	// signature is the armored signature block, line by line.
	signature []string
}

// splitClearsigned takes a clearsigned message apart.
func splitClearsigned(lines []string) (clearsigned, error) {
	start := indexOf(lines, clearsignBegin, 0)
	if start < 0 {
		return clearsigned{}, fmt.Errorf("%w: no %q", ErrArmor, clearsignBegin)
	}

	var doc clearsigned

	cursor := start + 1

	for ; cursor < len(lines) && lines[cursor] != ""; cursor++ {
		hashes, err := parseHashHeader(lines[cursor])
		if err != nil {
			return clearsigned{}, err
		}

		doc.hashes = append(doc.hashes, hashes...)
	}

	end := indexOf(lines, signatureBegin, cursor)
	if end < 0 {
		return clearsigned{}, fmt.Errorf("%w: no %q", ErrArmor, signatureBegin)
	}

	for _, line := range lines[cursor+1 : end] {
		doc.text = append(doc.text, strings.TrimRight(strings.TrimPrefix(line, dashEscape), " \t"))
	}

	doc.signature = lines[end:]

	return doc, nil
}

// parseHashHeader reads one `Hash: NAME[,NAME...]` header; a name this
// verifier does not read is unsupported.
func parseHashHeader(line string) ([]byte, error) {
	value, found := strings.CutPrefix(line, hashHeader)
	if !found {
		return nil, fmt.Errorf("%w: header %q on a clearsigned message", ErrArmor, line)
	}

	var hashes []byte

	for name := range strings.SplitSeq(value, ",") {
		name = strings.ToUpper(strings.TrimSpace(name))

		alg, known := hashNames[name]
		if !known {
			return nil, fmt.Errorf("%w: hash %q", ErrUnsupported, name)
		}

		hashes = append(hashes, alg)
	}

	return hashes, nil
}

// contains reports whether alg is among hashes.
func contains(hashes []byte, alg byte) bool {
	return slices.Contains(hashes, alg)
}

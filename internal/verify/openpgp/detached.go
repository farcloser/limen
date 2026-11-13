package openpgp

import (
	"bytes"
	"fmt"
)

// VerifyDetached checks a detached signature over data against keys and
// returns the key that made it. The signature is read as upstreams publish
// it, armored or as the bare packets, and must be a signature of a binary
// document: the bytes as they are, no canonical form. A file several keys
// signed, as several packets or as several armored blocks one after the
// other, verifies when any one signature does against a key given, so the
// caller chooses the signer by the keys it gives; the error otherwise is
// the last signature's.
func VerifyDetached(data, signature []byte, keys []Key) (Fingerprint, error) {
	packets, err := signaturePackets(signature)
	if err != nil {
		return "", err
	}

	err = fmt.Errorf("%w: no signature packet", ErrPacket)

	for _, pkt := range packets {
		if pkt.tag != tagSignature {
			continue
		}

		var signer Fingerprint

		signer, err = verifyBinary(pkt.body, keys, data)
		if err == nil {
			return signer, nil
		}
	}

	return "", err
}

// signaturePackets reads a signature's packets, dearmoring first when it is
// armored: every signature block of the file, since each signer of an .asc
// appends a block of their own. A binary packet stream opens with a header
// octet whose high bit is set, never with a dash, so the frame decides.
func signaturePackets(signature []byte) ([]packet, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(signature, " \t\r\n"), []byte(armorBegin)) {
		return readPackets(signature)
	}

	lines := splitLines(signature)
	begin := armorBegin + kindSignature + armorDashes
	end := armorEnd + kindSignature + armorDashes

	var packets []packet

	// The first block is required: a file with none fails as armor does.
	for start := 0; ; {
		blk, err := dearmor(lines[start:], kindSignature)
		if err != nil {
			return nil, err
		}

		more, err := readPackets(blk.body)
		if err != nil {
			return nil, err
		}

		packets = append(packets, more...)

		// dearmor found the block's two lines at or after start; the next
		// block, when there is one, opens after this one's end.
		start = indexOf(lines, end, indexOf(lines, begin, start)) + 1
		if indexOf(lines, begin, start) < 0 {
			return packets, nil
		}
	}
}

// verifyBinary checks one signature packet, which must sign a binary
// document, against the keys that could have made it.
func verifyBinary(body []byte, keys []Key, data []byte) (Fingerprint, error) {
	sig, err := parseSignature(body)
	if err != nil {
		return "", err
	}

	if sig.sigType != sigTypeBinary {
		return "", fmt.Errorf("%w: signature type %#x on a detached signature", ErrSignature, sig.sigType)
	}

	return sig.signer(keys, data)
}

package openpgp

import (
	"encoding/binary"
	"fmt"
)

// Packet tags this verifier reads (RFC 4880 §4.3); every other tag is
// skipped, not refused, since a key block carries user IDs, self-signatures
// and more that verification does not need.
const (
	tagSignature    = 2
	tagPublicKey    = 6
	tagPublicSubkey = 14
)

// The packet header (RFC 4880 §4.2): bit 7 always set, bit 6 selects the
// new format, where the tag is the low six bits and the length is
// self-describing; in the old format the tag is bits 2-5 and bits 0-1 give
// the length's width.
const (
	headerAlways    = 0x80
	headerNewFormat = 0x40
	newTagMask      = 0x3F
	oldTagShift     = 2
	oldTagMask      = 0x0F
	oldLengthMask   = 0x03

	oldLengthOne           = 0
	oldLengthTwo           = 1
	oldLengthFour          = 2
	oldLengthIndeterminate = 3

	newLengthOneMax     = 191
	newLengthTwoMax     = 223
	newLengthPartialMax = 254
	newLengthTwoBase    = 192
	newLengthTwoShift   = 8
	partialShiftMask    = 0x1F

	twoOctets  = 2
	fourOctets = 4
)

// The malformations reported from more than one place.
const (
	errTruncatedLength = "%w: truncated length"
	errTruncatedBody   = "%w: truncated body"
)

// packet is one packet: its tag and its body.
type packet struct {
	tag  byte
	body []byte
}

// readPackets splits a binary OpenPGP message into its packets.
func readPackets(data []byte) ([]packet, error) {
	var packets []packet

	for len(data) > 0 {
		next, rest, err := readPacket(data)
		if err != nil {
			return nil, err
		}

		packets = append(packets, next)
		data = rest
	}

	return packets, nil
}

// readPacket reads the packet at the head of data and returns what follows.
func readPacket(data []byte) (packet, []byte, error) {
	header := data[0]
	if header&headerAlways == 0 {
		return packet{}, nil, fmt.Errorf("%w: header byte %#x", ErrPacket, header)
	}

	if header&headerNewFormat != 0 {
		return readNewFormat(header&newTagMask, data[1:])
	}

	return readOldFormat(header, data[1:])
}

// readOldFormat reads an old-format packet: the length's width is in the
// header, and length type 3 means the body runs to the end of the input.
func readOldFormat(header byte, data []byte) (packet, []byte, error) {
	tag := (header >> oldTagShift) & oldTagMask

	var width int

	switch header & oldLengthMask {
	case oldLengthOne:
		width = 1
	case oldLengthTwo:
		width = twoOctets
	case oldLengthFour:
		width = fourOctets
	case oldLengthIndeterminate:
		return packet{tag: tag, body: data}, nil, nil
	default:
		return packet{}, nil, fmt.Errorf("%w: length type %d", ErrPacket, header&oldLengthMask)
	}

	if len(data) < width {
		return packet{}, nil, fmt.Errorf(errTruncatedLength, ErrPacket)
	}

	length := 0
	for _, octet := range data[:width] {
		length = length<<8 | int(octet)
	}

	return take(tag, data[width:], length)
}

// readNewFormat reads a new-format packet, partial body lengths included:
// each partial chunk is followed by another length, the last one definite.
func readNewFormat(tag byte, data []byte) (packet, []byte, error) {
	var body []byte

	for {
		length, rest, err := readNewLength(data)
		if err != nil {
			return packet{}, nil, err
		}

		if len(rest) < length.octets {
			return packet{}, nil, fmt.Errorf(errTruncatedBody, ErrPacket)
		}

		body = append(body, rest[:length.octets]...)
		data = rest[length.octets:]

		if !length.partial {
			return packet{tag: tag, body: body}, data, nil
		}
	}
}

// bodyLength is a decoded new-format length: how many octets follow, and
// whether another length follows them.
type bodyLength struct {
	octets  int
	partial bool
}

// readNewLength reads a new-format length: one, two, or five octets, or a
// partial length (a power of two, more to follow).
func readNewLength(data []byte) (bodyLength, []byte, error) {
	if len(data) == 0 {
		return bodyLength{}, nil, fmt.Errorf("%w: missing length", ErrPacket)
	}

	first := int(data[0])

	switch {
	case first <= newLengthOneMax:
		return bodyLength{octets: first}, data[1:], nil
	case first <= newLengthTwoMax:
		if len(data) < twoOctets {
			return bodyLength{}, nil, fmt.Errorf(errTruncatedLength, ErrPacket)
		}

		octets := (first-newLengthTwoBase)<<newLengthTwoShift + int(data[1]) + newLengthTwoBase

		return bodyLength{octets: octets}, data[twoOctets:], nil
	case first <= newLengthPartialMax:
		return bodyLength{octets: 1 << (first & partialShiftMask), partial: true}, data[1:], nil
	default:
		if len(data) < 1+fourOctets {
			return bodyLength{}, nil, fmt.Errorf(errTruncatedLength, ErrPacket)
		}

		return bodyLength{octets: int(binary.BigEndian.Uint32(data[1 : 1+fourOctets]))}, data[1+fourOctets:], nil
	}
}

// take cuts a body of the given length off data.
func take(tag byte, data []byte, length int) (packet, []byte, error) {
	if len(data) < length {
		return packet{}, nil, fmt.Errorf(errTruncatedBody, ErrPacket)
	}

	return packet{tag: tag, body: data[:length]}, data[length:], nil
}

// twoOctetLength encodes a length as two big-endian octets: what a
// fingerprint prefix and a signature's subpacket areas carry.
func twoOctetLength(length int) []byte {
	return []byte{byte(length >> 8), byte(length)} // #nosec G115 -- bounded at two octets by the format.
}

// readMPI reads a multiprecision integer (RFC 4880 §3.2): a two-octet bit
// count, then the magnitude, big-endian.
func readMPI(data []byte) (value, rest []byte, err error) {
	if len(data) < twoOctets {
		return nil, nil, fmt.Errorf("%w: truncated integer", ErrPacket)
	}

	bits := int(binary.BigEndian.Uint16(data))
	octets := (bits + 7) / 8

	if len(data) < twoOctets+octets {
		return nil, nil, fmt.Errorf("%w: truncated integer", ErrPacket)
	}

	return data[twoOctets : twoOctets+octets], data[twoOctets+octets:], nil
}

// A subpacket (RFC 4880 §5.2.3.1): a length, a type octet whose high bit
// marks it critical, and its data.
const (
	subpacketTypeMask = 0x7F
)

type subpacket struct {
	kind byte
	data []byte
}

// readSubpackets splits a subpacket area into its subpackets.
func readSubpackets(data []byte) ([]subpacket, error) {
	var out []subpacket

	for len(data) > 0 {
		length, rest, err := readNewLength(data)
		if err != nil {
			return nil, err
		}

		if length.partial || length.octets == 0 || len(rest) < length.octets {
			return nil, fmt.Errorf("%w: malformed subpacket", ErrPacket)
		}

		out = append(out, subpacket{kind: rest[0] & subpacketTypeMask, data: rest[1:length.octets]})
		data = rest[length.octets:]
	}

	return out, nil
}

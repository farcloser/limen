package openpgp

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
)

// The armor frame (RFC 4880 §6.2): `-----BEGIN PGP <kind>-----`, headers,
// an empty line, base64 lines, an optional `=<crc24>` line, `-----END PGP
// <kind>-----`. RFC 9580 made the checksum line optional.
const (
	armorBegin  = "-----BEGIN PGP "
	armorEnd    = "-----END PGP "
	armorDashes = "-----"
	crcPrefix   = "="

	kindPublicKey = "PUBLIC KEY BLOCK"
	kindSignature = "SIGNATURE"
)

// block is a decoded armored block.
type block struct {
	headers []string
	body    []byte
}

// dearmor decodes the first armored block of the given kind in lines.
func dearmor(lines []string, kind string) (block, error) {
	begin := armorBegin + kind + armorDashes
	end := armorEnd + kind + armorDashes

	start := indexOf(lines, begin, 0)
	if start < 0 {
		return block{}, fmt.Errorf("%w: no %q", ErrArmor, begin)
	}

	var out block

	cursor := start + 1

	for ; cursor < len(lines) && lines[cursor] != ""; cursor++ {
		out.headers = append(out.headers, lines[cursor])
	}

	var (
		encoded strings.Builder
		crc     string
	)

	for cursor++; cursor < len(lines) && lines[cursor] != end; cursor++ {
		line := lines[cursor]

		switch {
		case strings.HasPrefix(line, crcPrefix):
			crc = strings.TrimPrefix(line, crcPrefix)
		case crc != "":
			return block{}, fmt.Errorf("%w: data after the checksum line", ErrArmor)
		default:
			encoded.WriteString(line)
		}
	}

	if cursor >= len(lines) {
		return block{}, fmt.Errorf("%w: no %q", ErrArmor, end)
	}

	body, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		return block{}, fmt.Errorf("%w: %w", ErrArmor, err)
	}

	if crc != "" {
		if err := checkCRC(body, crc); err != nil {
			return block{}, err
		}
	}

	out.body = body

	return out, nil
}

// checkCRC verifies the armor checksum: CRC-24 of the body, base64 (RFC
// 4880 §6.1).
func checkCRC(body []byte, encoded string) error {
	want, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(want) != crcBytes {
		return fmt.Errorf("%w: malformed checksum line", ErrArmor)
	}

	got := binary.BigEndian.AppendUint32(nil, crc24(body))[1:]
	if string(got) != string(want) {
		return fmt.Errorf("%w: checksum mismatch", ErrArmor)
	}

	return nil
}

// CRC-24 as RFC 4880 §6.1 defines it.
const (
	crcInit      = 0xB704CE
	crcPoly      = 0x1864CFB
	crcMask      = 0xFFFFFF
	crcTop       = 0x1000000
	crcBytes     = 3
	crcShiftHigh = 16
)

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

// splitLines cuts data into lines, accepting CRLF and LF endings alike.
func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	return strings.Split(text, "\n")
}

// indexOf is the index of the first line equal to want at or after from,
// ignoring trailing whitespace; -1 when there is none.
func indexOf(lines []string, want string, from int) int {
	for index := from; index < len(lines); index++ {
		if strings.TrimRight(lines[index], " \t") == want {
			return index
		}
	}

	return -1
}

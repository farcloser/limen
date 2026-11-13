// Package pinstest writes into a pins.yaml fixture what only a refresh
// knows, so a test can state "this digest stands for the entry as written"
// without a download.
package pinstest

import (
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/pins"
)

// The lines of a digest block this package reads and writes.
const (
	digestHeader = "    digest:"
	sourcePrefix = "      source: "
	keptPrefixes = "      version: |      commit: "
)

// Source is the source fingerprint of the named entry of text, as a
// refresh would record it.
func Source(t *testing.T, text, name string) string {
	t.Helper()

	manifest, err := pins.Parse([]byte(text))
	if err != nil {
		t.Fatalf("pinstest: %v", err)
	}

	for _, entry := range manifest.Entries {
		if entry.Name == name {
			return entry.Source()
		}
	}

	t.Fatalf("pinstest: no pin %q", name)

	return ""
}

// Recorded returns text with the digest block of each named entry carrying
// the entry's source, after its version and commit lines, where a refresh
// writes it: the digest then stands for the entry as text has it. A source
// line the block already carries is replaced.
func Recorded(t *testing.T, text string, names ...string) string {
	t.Helper()

	for _, name := range names {
		source := Source(t, text, name)
		lines := strings.Split(text, "\n")

		start := indexFrom(lines, 0, func(line string) bool { return line == "  - name: "+name })
		if start < 0 {
			t.Fatalf("pinstest: no entry line for %q", name)
		}

		end := indexFrom(lines, start+1, func(line string) bool { return strings.HasPrefix(line, "  - name: ") })
		if end < 0 {
			end = len(lines)
		}

		header := indexFrom(lines[:end], start, func(line string) bool { return line == digestHeader })
		if header < 0 {
			t.Fatalf("pinstest: %q has no digest block", name)
		}

		count := countBlock(lines[header+1 : end])

		var block []string

		after := 0

		for _, line := range lines[header+1 : header+1+count] {
			if strings.HasPrefix(line, sourcePrefix) {
				continue
			}

			block = append(block, line)

			if keeps(line) {
				after = len(block)
			}
		}

		block = slices.Insert(block, after, sourcePrefix+source)
		text = strings.Join(slices.Concat(lines[:header+1], block, lines[header+1+count:]), "\n")
	}

	return text
}

// keeps reports whether line is one the source line goes after.
func keeps(line string) bool {
	for prefix := range strings.SplitSeq(keptPrefixes, "|") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

// countBlock is how many of lines, from the first, belong to the digest
// block: the ones indented under it.
func countBlock(lines []string) int {
	count := 0
	for count < len(lines) && strings.HasPrefix(lines[count], "      ") {
		count++
	}

	return count
}

// indexFrom is the index of the first line at or after from that match
// accepts; -1 when there is none.
func indexFrom(lines []string, from int, match func(string) bool) int {
	for index := from; index < len(lines); index++ {
		if match(lines[index]) {
			return index
		}
	}

	return -1
}

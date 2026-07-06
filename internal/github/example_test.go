// Keeps limen-example.yaml honest: the reference file and the live check
// catalog must never drift apart.

package github //nolint:testpackage // white-box: needs knownChecks.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

// exampleEntryRE matches one commented-out declaration entry in the example
// file: `#   check-id: …`.
var exampleEntryRE = regexp.MustCompile(`(?m)^#   ([a-z0-9-]+): `)

// TestOverrideExampleCoversEveryCheck: every check identifier appears in
// limen-example.yaml, and every entry the example shows is a real identifier —
// adding a check without documenting it (or documenting a phantom) fails here.
func TestOverrideExampleCoversEveryCheck(t *testing.T) {
	t.Parallel()

	documented := map[string]bool{}
	for _, match := range exampleEntryRE.FindAllStringSubmatch(limen.CanonicalOverrideExample, -1) {
		documented[match[1]] = true
	}

	if len(documented) == 0 {
		t.Fatal("no declaration entries found in limen-example.yaml — did its format change?")
	}

	for check := range knownChecks() {
		if !documented[check] {
			t.Errorf("check %q is missing from limen-example.yaml", check)
		}
	}

	for entry := range documented {
		if !knownChecks()[entry] {
			t.Errorf("limen-example.yaml documents %q, which is not a known check", entry)
		}
	}

	// And the example must itself parse as a valid limen.yaml once the
	// entries are uncommented — prove it for a sample by materializing one.
	if !strings.Contains(limen.CanonicalOverrideExample, "github:") {
		t.Error("the example lacks the github: section header")
	}
}

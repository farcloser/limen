// Keeps limen-example.yaml honest: the reference file and the live check
// catalog must never drift apart.

package github_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/github"
)

// exampleEntryRE matches one commented-out declaration entry in the example
// file: `#   check-id: …`.
var exampleEntryRE = regexp.MustCompile(`(?m)^# {3}([a-z0-9-]+): `)

// documentedChecks returns every check identifier limen-example.yaml documents.
func documentedChecks(t *testing.T) map[string]bool {
	t.Helper()

	documented := map[string]bool{}
	for _, match := range exampleEntryRE.FindAllStringSubmatch(limen.CanonicalOverrideExample, -1) {
		documented[match[1]] = true
	}

	if len(documented) == 0 {
		t.Fatal("no declaration entries found in limen-example.yaml — did its format change?")
	}

	return documented
}

// TestOverrideExampleEntriesAreChecks: every entry the example shows is a
// real identifier — the override loader, which validates identifiers,
// accepts a file declaring all of them. Documenting a phantom fails here.
func TestOverrideExampleEntriesAreChecks(t *testing.T) {
	t.Parallel()

	var file strings.Builder

	file.WriteString("github:\n")

	for entry := range documentedChecks(t) {
		file.WriteString("  " + entry + ": documented\n")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, github.OverridePath), []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := github.LoadOverrides(dir); err != nil {
		t.Errorf("limen-example.yaml documents an entry that is not a known check: %v", err)
	}

	if !strings.Contains(limen.CanonicalOverrideExample, "github:") {
		t.Error("the example lacks the github: section header")
	}
}

// TestOverrideExampleCoversEveryCheck: every check a compliant audit reports,
// repository and organization alike, appears in limen-example.yaml — adding
// a check without documenting it fails here.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestOverrideExampleCoversEveryCheck(t *testing.T) {
	documented := documentedChecks(t)

	responses := compliantResponses()
	maps.Copy(responses, compliantOrgResponses())

	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)
	orgFindings, _ := github.AuditOrg(t.Context(), testOrg, nil)

	for _, finding := range append(findings, orgFindings...) {
		if !documented[finding.Check] {
			t.Errorf("check %q is missing from limen-example.yaml", finding.Check)
		}
	}
}

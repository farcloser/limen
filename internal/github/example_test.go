// Keeps the book's list of declarable identifiers honest: the reference and
// the live check catalog must never drift apart.

package github_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// referenceBlockRE finds the book's reference block: the fenced YAML that
// opens with its own title line.
var referenceBlockRE = regexp.MustCompile(
	"(?s)```yaml\n# \\.lint-github\\.yaml — every declarable identifier\n(.*?)```",
)

// referenceEntryRE matches one commented-out entry in it: `# check-id: …`.
var referenceEntryRE = regexp.MustCompile(`(?m)^# ([a-z0-9-]+): `)

// documentedChecks returns every check identifier the book documents.
func documentedChecks(t *testing.T) map[string]bool {
	t.Helper()

	book, err := os.ReadFile(filepath.Join("..", "..", "book", "github.md"))
	if err != nil {
		t.Fatal(err)
	}

	block := referenceBlockRE.FindSubmatch(book)
	if block == nil {
		t.Fatal("book/github.md carries no reference block — did its title line change?")
	}

	documented := map[string]bool{}
	for _, match := range referenceEntryRE.FindAllSubmatch(block[1], -1) {
		documented[string(match[1])] = true
	}

	if len(documented) == 0 {
		t.Fatal("no entries found in the book's reference block — did its format change?")
	}

	return documented
}

// TestReferenceEntriesAreChecks: every entry the book lists is a real
// identifier — the loader, which validates identifiers, accepts a file
// declaring all of them. Documenting a phantom fails here.
func TestReferenceEntriesAreChecks(t *testing.T) {
	t.Parallel()

	var file strings.Builder

	for entry := range documentedChecks(t) {
		file.WriteString(entry + ": documented\n")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, github.OverridePath), []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := github.LoadOverrides(dir); err != nil {
		t.Errorf("the book documents an entry that is not a known check: %v", err)
	}
}

// TestReferenceCoversEveryCheck: every check a compliant audit reports,
// repository and organization alike, appears in the book's list — adding a
// check without documenting it fails here.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestReferenceCoversEveryCheck(t *testing.T) {
	documented := documentedChecks(t)

	responses := compliantResponses()
	maps.Copy(responses, compliantOrgResponses())

	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)
	orgFindings, _ := github.AuditOrg(t.Context(), testOrg, nil)

	for _, finding := range append(findings, orgFindings...) {
		if !documented[finding.Check] {
			t.Errorf("check %q is missing from the book's list in book/github.md", finding.Check)
		}
	}
}

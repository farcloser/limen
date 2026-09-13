// White-box tests for the -all-repos sweep, through the same gh stub seam as
// the repository and org audits.

package github //nolint:testpackage // white-box (see audit_test.go).

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestOrgReposSkipsArchived: archived repositories are left out — GitHub
// freezes their settings, so every fixable check would fail forever against a
// write the API refuses.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestOrgReposSkipsArchived(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org/repos?per_page=100&type=all": {
			Body: `[{"name": "zeta", "archived": false}, {"name": "old", "archived": true},` +
				`{"name": "alpha", "archived": false}]`,
		},
	})

	repos, err := OrgRepos(testOrg)
	if err != nil {
		t.Fatalf("OrgRepos: %v", err)
	}

	if got := strings.Join(repos, " "); got != "test-org/alpha test-org/zeta" {
		t.Errorf("OrgRepos = %q, want the two live repositories, sorted", got)
	}
}

// TestOrgReposPaginates: the sweep reads every page, not the first. gh
// --paginate merges the array pages, so a stub answering the one request with
// 140 entries stands in for the merged result — what this pins is that the
// request asks for pagination at all, and that nothing truncates after.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestOrgReposPaginates(t *testing.T) {
	const total = 140

	entries := make([]string, 0, total)
	for index := range total {
		entries = append(entries, `{"name": "repo`+strconv.Itoa(index)+`", "archived": false}`)
	}

	logPath := stubGH(t, map[string]stubResponse{
		"GET orgs/test-org/repos?per_page=100&type=all": {Body: "[" + strings.Join(entries, ",") + "]"},
	})

	repos, err := OrgRepos(testOrg)
	if err != nil {
		t.Fatalf("OrgRepos: %v", err)
	}

	if len(repos) != total {
		t.Errorf("OrgRepos returned %d repositories, want all %d — a sweep must not stop at a page",
			len(repos), total)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	if !strings.Contains(string(log), "--paginate") {
		t.Error("the repository listing must ask gh to follow every page")
	}
}

// TestOrgReposUnreadable: a token that cannot list the organization's
// repositories fails the run rather than sweeping an empty set — zero
// repositories audited must never report as zero problems.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestOrgReposUnreadable(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org/repos?per_page=100&type=all": {Fail: true},
	})

	if _, err := OrgRepos(testOrg); err == nil {
		t.Error("an unreadable repository list must be an error, not an empty sweep")
	}
}

// TestAuditManyTagsTargets: every finding and change carries the target it
// came from, the organization first and the repositories in the order given.
// Without the tag a sweep's report says which checks failed but not where.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestAuditManyTagsTargets(t *testing.T) {
	stubGH(t, compliantOrgResponses())

	findings, _ := AuditMany(testOrg, []string{"test-org/alpha", "test-org/beta"}, nil)

	if len(findings) == 0 {
		t.Fatal("no findings")
	}

	seen := []string{}

	for _, finding := range findings {
		if finding.Target == "" {
			t.Fatalf("%s carries no target", finding.Check)
		}

		if len(seen) == 0 || seen[len(seen)-1] != finding.Target {
			seen = append(seen, finding.Target)
		}
	}

	want := "org test-org test-org/alpha test-org/beta"
	if got := strings.Join(seen, " "); got != want {
		t.Errorf("target order = %q, want %q", got, want)
	}
}

package github_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// endpointFamily reduces a logged request to the endpoint it belongs to, as
// GitHub's permission reference names it: names and numbers become
// placeholders, the query goes.
func endpointFamily(line string) string {
	method, path, _ := strings.Cut(line, " ")
	path, _, _ = strings.Cut(path, "?")
	parts := strings.Split(path, "/")

	for i := 0; i < len(parts); i++ {
		switch {
		case i > 0 && parts[i-1] == "repos" && i+1 < len(parts):
			parts[i], parts[i+1] = "{owner}", "{repo}"
			i++
		case i > 0 && parts[i-1] == "orgs":
			parts[i] = "{org}"
		case i > 0 && parts[i-1] == "users":
			parts[i] = "{username}"
		case i > 0 && parts[i-1] == "teams":
			parts[i] = "{team_slug}"
		case i > 0 && parts[i-1] == "environments":
			parts[i] = "{environment_name}"
		case i > 0 && parts[i-1] == "branches":
			parts[i] = "{branch}"
		case i > 0 && parts[i-1] == "memberships":
			parts[i] = "{username}"
		case i > 0 && parts[i-1] == "contents":
			parts = append(parts[:i], "{path}")
		case regexp.MustCompile(`^\d+$`).MatchString(parts[i]):
			parts[i] = "{id}"
		}
	}

	return method + " /" + strings.Join(parts, "/")
}

// TestAuditEndpointFamilies: the endpoints a full audit reads are the ones
// book/github.md's permission table is derived from. A new read lands here
// first, and the table moves with it.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditEndpointFamilies(t *testing.T) {
	log := stubGH(t, compliantOrgResponses())

	if _, err := github.AuditMany(t.Context(), testOrg, []string{"test-org/alpha", "test-org/beta"}, nil); err != nil {
		t.Fatalf("audit: %v", err)
	}

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "GET ") {
			continue
		}

		if family := endpointFamily(line); !slices.Contains(got, family) {
			got = append(got, family)
		}
	}

	slices.Sort(got)

	// Sorted. Each maps to a row of the permission table in book/github.md
	// ("Authentication"); a family added here is a row there, or a permission
	// a row already grants. Two reads run off the compliant path and are not
	// held here: the code scanning default setup, read when the override
	// opts a repository in, and the Actions access level; both are repository
	// Administration read, the row the table gives them.
	want := []string{
		"GET /orgs/{org}",
		"GET /orgs/{org}/actions/permissions",
		"GET /orgs/{org}/actions/permissions/fork-pr-contributor-approval",
		"GET /orgs/{org}/actions/permissions/selected-actions",
		"GET /orgs/{org}/actions/permissions/workflow",
		"GET /orgs/{org}/actions/runners",
		"GET /orgs/{org}/actions/secrets",
		"GET /orgs/{org}/code-security/configurations",
		"GET /orgs/{org}/code-security/configurations/defaults",
		"GET /orgs/{org}/hooks",
		"GET /orgs/{org}/installations",
		"GET /orgs/{org}/members",
		"GET /orgs/{org}/personal-access-tokens",
		"GET /orgs/{org}/repos",
		"GET /orgs/{org}/teams",
		"GET /orgs/{org}/teams/{team_slug}/members",
		"GET /orgs/{org}/teams/{team_slug}/repos",
		"GET /repos/{owner}/{repo}",
		"GET /repos/{owner}/{repo}/actions/permissions",
		"GET /repos/{owner}/{repo}/actions/permissions/fork-pr-contributor-approval",
		"GET /repos/{owner}/{repo}/actions/permissions/workflow",
		"GET /repos/{owner}/{repo}/actions/secrets",
		"GET /repos/{owner}/{repo}/automated-security-fixes",
		"GET /repos/{owner}/{repo}/collaborators",
		"GET /repos/{owner}/{repo}/contents/{path}",
		"GET /repos/{owner}/{repo}/environments",
		"GET /repos/{owner}/{repo}/hooks",
		"GET /repos/{owner}/{repo}/issues",
		"GET /repos/{owner}/{repo}/keys",
		"GET /repos/{owner}/{repo}/pages",
		"GET /repos/{owner}/{repo}/private-vulnerability-reporting",
		"GET /repos/{owner}/{repo}/rulesets",
		"GET /repos/{owner}/{repo}/teams",
		"GET /repos/{owner}/{repo}/vulnerability-alerts",
	}
	if !slices.Equal(got, want) {
		t.Errorf(
			"endpoint families read by a full audit changed; move book/github.md's permission table with them:\n  %s",
			strings.Join(got, "\n  "),
		)
	}
}

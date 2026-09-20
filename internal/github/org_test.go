// Tests for the org-level audit (O1–O7), through the same fake gh as the
// repository tests.

package github_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

const testOrg = "test-org"

// compliantOrgJSON is an organization object (owner-scoped fields included)
// that satisfies every baseline check answered by GET /orgs/{org}.
const compliantOrgJSON = `{
  "description": "an organization",
  "two_factor_requirement_enabled": true,
  "default_repository_permission": "read",
  "members_can_create_public_repositories": false,
  "members_can_fork_private_repositories": false,
  "members_can_change_repo_visibility": false,
  "members_can_delete_repositories": false,
  "members_can_create_public_pages": false,
  "web_commit_signoff_required": true
}`

// compliantOrgResponses answers every endpoint github.AuditOrg reads with a
// baseline-compliant state.
func compliantOrgResponses() map[string]stubResponse {
	return map[string]stubResponse{
		"GET orgs/test-org": {Body: compliantOrgJSON},
		"GET orgs/test-org/members?role=admin&per_page=100": {Body: `[{"login": "alice"}]`},
		"GET orgs/test-org/actions/permissions": {
			Body: `{"enabled_repositories": "all", "allowed_actions": "selected", "sha_pinning_required": true}`,
		},
		"GET orgs/test-org/actions/permissions/workflow": {
			Body: `{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": false}`,
		},
		"GET orgs/test-org/actions/permissions/fork-pr-contributor-approval": {
			Body: `{"approval_policy": "first_time_contributors"}`,
		},
		"GET orgs/test-org/actions/runners?per_page=100": {Body: `{"total_count": 0, "runners": []}`},
		"GET orgs/test-org/code-security/configurations/defaults": {
			Body: `[{"default_for_new_repos": "all", "configuration": {"name": "canonical"}}]`,
		},
		"GET orgs/test-org/code-security/configurations?per_page=100": {
			Body: `[{"id": 1, "name": "canonical", "target_type": "organization", ` +
				`"enforcement": "enforced", "dependabot_security_updates": "disabled"}]`,
		},
		"GET orgs/test-org/installations?per_page=100": {
			Body: `{"total_count": 1, "installations": [{"app_slug": "renovate"}]}`,
		},
		"GET orgs/test-org/hooks?per_page=100":                {Body: `[]`},
		"GET orgs/test-org/actions/secrets?per_page=100":      {Body: `{"total_count": 0, "secrets": []}`},
		"GET orgs/test-org/teams?per_page=100":                {Body: `[{"slug":"agents"}]`},
		"GET orgs/test-org/teams/agents/members?per_page=100": {Body: `[{"login":"bot"}]`},
		"GET orgs/test-org/teams/agents/repos?per_page=100": {
			Body: `[{"name":"alpha","permissions":{"push":true}}]`,
		},
		"GET orgs/test-org/repos?per_page=100&type=all":         {Body: `[{"name":"alpha"}]`},
		"GET orgs/test-org/personal-access-tokens?per_page=100": {Body: `[]`},
		"GET repos/test-org/.github":                            {Body: `{"private": false}`},
		"GET repos/test-org/.github/contents/SECURITY.md":       {Body: `{}`},
		"GET repos/test-org/.github/contents/CONTRIBUTING.md":   {Body: `{}`},
		"GET repos/test-org/.github/contents/profile/README.md": {Body: `{}`},
	}
}

// TestAuditOrgCompliant: a fully compliant organization passes everything
// except the owner roster, which is an advisory BY DESIGN — the roster is
// declared by exempting it, which the second half of the test does.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgCompliant(t *testing.T) {
	stubGH(t, compliantOrgResponses())

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("a compliant organization planned %d change(s)", len(changes))
	}

	for _, finding := range findings {
		if finding.Check == "org-admins" {
			if finding.Status != github.StatusAdvisory {
				t.Errorf("org-admins: %v, want the by-design advisory", finding.Status)
			}

			continue
		}

		if finding.Status != github.StatusOK {
			t.Errorf("%s: %v (%s), want ok", finding.Check, finding.Status, finding.Message)
		}
	}

	// Declaring the roster via the override file is what makes a compliant
	// org fully green.
	findings, _ = github.AuditOrg(context.Background(), testOrg, map[string]string{"org-admins": "alice is the org"})
	if !github.AllOK(findings) {
		t.Error("a compliant organization with a declared roster must pass entirely")
	}

	// The declaration is load-bearing: an owner the declaration does not name
	// brings the advisory back — a blanket exemption would hide a new owner.
	findings, _ = github.AuditOrg(context.Background(), testOrg, map[string]string{"org-admins": "bob is the org"})
	if finding, found := findingByCheck(findings, "org-admins"); !found || finding.Status != github.StatusAdvisory {
		t.Errorf("an undeclared owner must surface as an advisory, got %v", finding.Status)
	}

	// Whole-token matching: a login that appears only as a SUBSTRING of the
	// declaration ("li" inside "alice") is not declared.
	findings, _ = github.AuditOrg(context.Background(), testOrg, map[string]string{"org-admins": "malice is the org"})
	if finding, found := findingByCheck(findings, "org-admins"); !found || finding.Status != github.StatusAdvisory {
		t.Errorf("a substring-only match must not count as declared, got %v", finding.Status)
	}
}

// TestAuditOrgNonCompliant: every floor violation is flagged with the right
// verdict class, and applying the planned changes issues the consolidated
// org PATCH plus the Actions PUTs.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgNonCompliant(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org"] = stubResponse{Body: `{
	  "description": "",
	  "two_factor_requirement_enabled": false,
	  "default_repository_permission": "write",
	  "members_can_create_public_repositories": true,
	  "members_can_fork_private_repositories": true,
	  "members_can_change_repo_visibility": true,
	  "members_can_delete_repositories": true,
	  "members_can_create_public_pages": true,
	  "web_commit_signoff_required": false
	}`}
	responses["GET orgs/test-org/actions/permissions"] = stubResponse{
		Body: `{"enabled_repositories": "all", "allowed_actions": "all", "sha_pinning_required": false}`,
	}
	responses["GET orgs/test-org/actions/permissions/workflow"] = stubResponse{
		Body: `{"default_workflow_permissions": "write", "can_approve_pull_request_reviews": true}`,
	}
	responses["GET orgs/test-org/actions/permissions/fork-pr-contributor-approval"] = stubResponse{
		Body: `{"approval_policy": "first_time_contributors_new_to_github"}`,
	}
	responses["GET repos/test-org/.github"] = stubResponse{Body: `{"private": true}`}
	responses["GET repos/test-org/.github/contents/CONTRIBUTING.md"] = stubResponse{NotFound: true}
	responses["GET repos/test-org/.github/contents/.github/CONTRIBUTING.md"] = stubResponse{NotFound: true}
	responses["GET repos/test-org/.github/contents/docs/CONTRIBUTING.md"] = stubResponse{NotFound: true}
	responses["PATCH orgs/test-org"] = stubResponse{Body: `{}`}
	responses["PUT orgs/test-org/actions/permissions"] = stubResponse{Body: `{}`}
	responses["PUT orgs/test-org/actions/permissions/selected-actions"] = stubResponse{Body: `{}`}
	responses["PUT orgs/test-org/actions/permissions/workflow"] = stubResponse{Body: `{}`}
	responses["PUT orgs/test-org/actions/permissions/fork-pr-contributor-approval"] = stubResponse{Body: `{}`}
	logPath := stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	wantFail := []string{
		"org-default-repository-permission", "org-members-create-public-repositories",
		"org-members-fork-private-repositories",
		"org-members-create-public-pages", "org-web-commit-signoff",
		"org-actions-allowed", "org-actions-sha-pinning",
		"org-actions-workflow-permissions", "org-actions-approve-pull-requests", "org-actions-fork-pr-approval",
	}
	for _, check := range wantFail {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != github.StatusFail {
			t.Errorf("%s: %v, want fail", check, finding.Status)
		}
	}

	wantAdvisory := []string{
		"org-two-factor-requirement", "org-members-change-repository-visibility", "org-members-delete-repositories",
		"org-profile-description", "org-community-health-repo", "org-community-health-content",
	}
	for _, check := range wantAdvisory {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != github.StatusAdvisory {
			t.Errorf("%s: %v, want advisory (never auto-fixed)", check, finding.Status)
		}
	}

	if len(changes) == 0 {
		t.Fatal("a non-compliant organization planned no changes")
	}

	for _, planned := range changes {
		if err := planned.Apply(context.Background()); err != nil {
			t.Errorf("%s: %v", planned.Check, err)
		}
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	calls := string(log)
	for _, want := range []string{
		"PATCH orgs/test-org",
		"PUT orgs/test-org/actions/permissions",
		"PUT orgs/test-org/actions/permissions/selected-actions",
		"PUT orgs/test-org/actions/permissions/workflow",
		"PUT orgs/test-org/actions/permissions/fork-pr-contributor-approval",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("expected an applied %s call", want)
		}
	}
}

// TestAuditOrgCommunityHealthSubdirectory: GitHub resolves community health
// files from the root, .github/, or docs/ of the org .github repository —
// a file under .github/ (a fully standard layout) must count as present.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgCommunityHealthSubdirectory(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET repos/test-org/.github/contents/SECURITY.md"] = stubResponse{NotFound: true}
	responses["GET repos/test-org/.github/contents/.github/SECURITY.md"] = stubResponse{Body: "{}"}
	stubGH(t, responses)

	findings, _ := github.AuditOrg(context.Background(), testOrg, nil)

	finding, found := findingByCheck(findings, "org-community-health-content")
	if !found || finding.Status != github.StatusOK {
		t.Errorf("community-health set with .github/-located files: %v (%s), want ok",
			finding.Status, finding.Message)
	}
}

// TestAuditOrgRenovateInstalled: the Renovate app is a floor — absent, a
// failing verdict with NO planned change (installation is a browser flow with
// no API); present, ok. Neither outcome disturbs the informational app
// inventory, which stays ok with the list either way. An org with no apps at
// all fails the floor too, and the exemption is the self-hosted escape hatch.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgRenovateInstalled(t *testing.T) {
	// Present, among others: ok, and the inventory lists every slug.
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/installations?per_page=100"] = stubResponse{
		Body: `{"total_count": 2, "installations": [{"app_slug": "some-app"}, {"app_slug": "renovate"}]}`,
	}
	stubGH(t, responses)

	findings, _ := github.AuditOrg(context.Background(), testOrg, nil)

	if finding, found := findingByCheck(
		findings,
		"org-renovate-installed",
	); !found ||
		finding.Status != github.StatusOK {
		t.Errorf("renovate installed: %v (%s), want ok", finding.Status, finding.Message)
	}

	if finding, found := findingByCheck(findings, "org-installed-apps"); !found ||
		finding.Status != github.StatusOK || !strings.Contains(finding.Message, "some-app") {
		t.Errorf("app inventory: %v (%s), want ok listing every app", finding.Status, finding.Message)
	}

	// Absent (other apps installed): fail, no fix planned.
	responses["GET orgs/test-org/installations?per_page=100"] = stubResponse{
		Body: `{"total_count": 1, "installations": [{"app_slug": "some-app"}]}`,
	}
	stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	if finding, found := findingByCheck(
		findings,
		"org-renovate-installed",
	); !found ||
		finding.Status != github.StatusFail {
		t.Errorf("renovate missing: %v, want fail", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == "org-renovate-installed" {
			t.Error("renovate installation planned a change; there is no API to install an app")
		}
	}

	if finding, found := findingByCheck(findings, "org-installed-apps"); !found || finding.Status != github.StatusOK {
		t.Errorf("app inventory with renovate missing: %v, want ok (informational)", finding.Status)
	}

	// No apps at all: the inventory is ok ("none"), the floor still fails.
	responses["GET orgs/test-org/installations?per_page=100"] = stubResponse{
		Body: `{"total_count": 0, "installations": []}`,
	}
	stubGH(t, responses)

	findings, _ = github.AuditOrg(context.Background(), testOrg, nil)

	if finding, found := findingByCheck(
		findings,
		"org-renovate-installed",
	); !found ||
		finding.Status != github.StatusFail {
		t.Errorf("no apps: renovate %v, want fail", finding.Status)
	}

	if finding, found := findingByCheck(findings, "org-installed-apps"); !found || finding.Status != github.StatusOK {
		t.Errorf("no apps: inventory %v, want ok", finding.Status)
	}

	// Self-hosted Renovate: the exemption turns the failure into an
	// exempted-ok, as for any other check.
	findings, _ = github.AuditOrg(
		context.Background(),
		testOrg,
		map[string]string{"org-renovate-installed": "self-hosted from ops/renovate"},
	)

	if finding, found := findingByCheck(
		findings,
		"org-renovate-installed",
	); !found ||
		finding.Status != github.StatusOK {
		t.Errorf("exempted renovate: %v, want ok", finding.Status)
	}
}

// TestAuditOrgSecondPage: an owner on the second page of the member listing
// is as much an owner as one on the first, and an App installed past the
// first page of installations is installed. Both listings are read whole,
// so the undeclared owner surfaces and Renovate is found.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgSecondPage(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/members?role=admin&per_page=100"] = stubResponse{
		Pages: []string{`[{"login": "alice"}]`, `[{"login": "mallory"}]`},
	}
	responses["GET orgs/test-org/installations?per_page=100"] = stubResponse{
		Pages: []string{
			`{"total_count": 2, "installations": [{"app_slug": "some-app"}]}`,
			`{"total_count": 2, "installations": [{"app_slug": "renovate"}]}`,
		},
	}
	stubGH(t, responses)

	findings, _ := github.AuditOrg(context.Background(), testOrg, map[string]string{"org-admins": "alice is the org"})

	if finding, found := findingByCheck(findings, "org-admins"); !found || finding.Status != github.StatusAdvisory ||
		!strings.Contains(finding.Message, "mallory") {
		t.Errorf("an owner on the second page must surface: %v (%s)", finding.Status, finding.Message)
	}

	if finding, found := findingByCheck(
		findings,
		"org-renovate-installed",
	); !found ||
		finding.Status != github.StatusOK {
		t.Errorf("renovate on the second page of installations: %v (%s), want ok", finding.Status, finding.Message)
	}

	if finding, found := findingByCheck(findings, "org-installed-apps"); !found ||
		!strings.Contains(finding.Message, "some-app") || !strings.Contains(finding.Message, "renovate") {
		t.Errorf("the app inventory must list both pages: %s", finding.Message)
	}
}

// TestAuditOrgUnverifiable: a token that can read nothing yields only
// unverifiable verdicts — never a pass, never a planned change.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{})

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("an unverifiable org audit planned %d change(s)", len(changes))
	}

	for _, finding := range findings {
		if finding.Status != github.StatusUnverifiable {
			t.Errorf("%s: %v, want unverifiable", finding.Check, finding.Status)
		}
	}

	if github.AllOK(findings) {
		t.Error("an entirely unverifiable org audit must not count as passing")
	}
}

// TestAuditOrgAnonymousObject: GET /orgs/{org} succeeds even anonymously,
// with the owner-scoped fields simply absent — those checks must classify
// as unverifiable, never as compliant zero values.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgAnonymousObject(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org"] = stubResponse{Body: `{"description": "an organization"}`}
	stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("absent fields planned %d change(s) — a zero value leaked through as a verdict", len(changes))
	}

	hidden := []string{
		"org-two-factor-requirement",
		"org-default-repository-permission",
		"org-members-create-public-repositories",
		"org-members-fork-private-repositories",
		"org-members-change-repository-visibility",
		"org-members-delete-repositories",
		"org-members-create-public-pages",
		"org-web-commit-signoff",
	}
	for _, check := range hidden {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != github.StatusUnverifiable {
			t.Errorf("%s: %v, want unverifiable (the field is owner-scoped)", check, finding.Status)
		}
	}

	// The public field still answers.
	if finding, found := findingByCheck(
		findings,
		"org-profile-description",
	); !found ||
		finding.Status != github.StatusOK {
		t.Errorf("org-profile-description: %v, want ok", finding.Status)
	}
}

// TestOrgOverridesValidate: org-level identifiers are valid override keys.
func TestOrgOverridesValidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	file := "github:\n  org-admins: alice is the org\n  org-actions-sha-pinning: pinned by hand\n"
	if err := os.WriteFile(filepath.Join(dir, github.OverridePath), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := github.LoadOverrides(dir); err != nil {
		t.Errorf("org check identifiers must be valid override-file keys: %v", err)
	}
}

// TestOrgActionsFixPreservesCompliantPolicy: the actions-permissions PUT
// replaces the whole object, so a fix that only tightens SHA pinning must
// write the compliant allowed-actions policy back unchanged — and must not
// touch the selected-actions allowlist, which belongs to whoever curated it.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestOrgActionsFixPreservesCompliantPolicy(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/actions/permissions"] = stubResponse{
		Body: `{"enabled_repositories": "selected", "allowed_actions": "local_only", "sha_pinning_required": false}`,
	}
	logPath := stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	if finding, found := findingByCheck(findings, "org-actions-allowed"); !found || finding.Status != github.StatusOK {
		t.Fatalf("local_only policy: %v, want ok", finding.Status)
	}

	applied := false

	for _, planned := range changes {
		if planned.Check == "org-actions-sha-pinning" {
			applied = true

			if err := planned.Apply(context.Background()); err != nil {
				t.Fatalf("apply: %v", err)
			}
		}

		if planned.Check == "org-actions-allowed" || planned.Check == "org-actions-enabled-repositories" {
			t.Errorf("%s: compliant state planned a change", planned.Check)
		}
	}

	if !applied {
		t.Fatal("expected a SHA-pinning change to be planned")
	}

	log, _ := os.ReadFile(logPath)
	calls := string(log)

	for _, want := range []string{
		`"allowed_actions":"local_only"`,
		`"enabled_repositories":"selected"`,
		`"sha_pinning_required":true`,
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("the PUT payload must carry %s", want)
		}
	}

	if strings.Contains(calls, "selected-actions") {
		t.Error("an already-restricted policy must keep its allowlist — no selected-actions PUT")
	}
}

// configurationsResponse is the code-security configuration list an org audit
// reads, with one configuration in the given state.
func configurationsResponse(targetType, securityUpdates string) stubResponse {
	return stubResponse{Body: `[{"id": 264288, "name": "org-config-1", "target_type": "` + targetType +
		`", "enforcement": "enforced", "dependabot_security_updates": "` + securityUpdates + `"}]`}
}

// TestAuditOrgDependabotSecurityUpdatesFixed: an organization-owned
// configuration that enables Dependabot security updates fails and is
// patched back to disabled. This is the setting the REPOSITORY check cannot
// reach — GitHub answers the repo-level DELETE with 422 while the
// configuration says enabled — so the fix must land on the org object.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgDependabotSecurityUpdatesFixed(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/code-security/configurations?per_page=100"] = configurationsResponse(
		"organization", "enabled",
	)

	logPath := stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	finding, found := findingByCheck(findings, "org-dependabot-security-updates")
	if !found || finding.Status != github.StatusFail {
		t.Fatalf("an enabling configuration must fail, got %v", finding.Status)
	}

	var planned *github.Change

	for index, change := range changes {
		if change.Check == "org-dependabot-security-updates" {
			planned = &changes[index]
		}
	}

	if planned == nil {
		t.Fatal("no change planned for the offending configuration")
	}

	if err := planned.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	calls := string(log)
	if !strings.Contains(calls, "PATCH orgs/test-org/code-security/configurations/264288") {
		t.Error("expected the offending configuration to be patched by id")
	}

	if !strings.Contains(calls, `"dependabot_security_updates":"disabled"`) {
		t.Error("expected the patch to disable Dependabot security updates")
	}
}

// TestAuditOrgDependabotSecurityUpdatesNotOurs: a configuration this
// organization does not own is advisory — PATCH would be refused, and
// detaching repositories from it is a human decision.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgDependabotSecurityUpdatesNotOurs(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/code-security/configurations?per_page=100"] = configurationsResponse(
		"global",
		"enabled",
	)

	stubGH(t, responses)

	findings, changes := github.AuditOrg(context.Background(), testOrg, nil)

	finding, found := findingByCheck(findings, "org-dependabot-security-updates")
	if !found || finding.Status != github.StatusAdvisory {
		t.Fatalf("a configuration we do not own must be advisory, got %v", finding.Status)
	}

	for _, change := range changes {
		if change.Check == "org-dependabot-security-updates" {
			t.Error("an unowned configuration must never plan a change")
		}
	}
}

// TestAuditOrgDependabotSecurityUpdatesUnverifiable: a token that cannot read
// the configurations reports unverifiable, never ok — what cannot be verified
// does not pass.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestAuditOrgDependabotSecurityUpdatesUnverifiable(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org/code-security/configurations?per_page=100"] = stubResponse{Fail: true}

	stubGH(t, responses)

	findings, _ := github.AuditOrg(context.Background(), testOrg, nil)

	finding, found := findingByCheck(findings, "org-dependabot-security-updates")
	if !found || finding.Status != github.StatusUnverifiable {
		t.Fatalf("an unreadable configuration list must be unverifiable, got %v", finding.Status)
	}
}

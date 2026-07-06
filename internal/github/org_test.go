// White-box tests for the org-level audit (O1–O7), through the same gh stub
// seam as the repository tests.

package github //nolint:testpackage // white-box (see audit_test.go).

import (
	"os"
	"strings"
	"testing"
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

// compliantOrgResponses answers every endpoint AuditOrg reads with a
// baseline-compliant state.
func compliantOrgResponses() map[string]stubResponse {
	return map[string]stubResponse{
		"GET orgs/test-org":                    {Body: compliantOrgJSON},
		"GET orgs/test-org/members?role=admin": {Body: `[{"login": "alice"}]`},
		"GET orgs/test-org/actions/permissions": {
			Body: `{"enabled_repositories": "all", "allowed_actions": "selected", "sha_pinning_required": true}`,
		},
		"GET orgs/test-org/actions/permissions/workflow": {
			Body: `{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": false}`,
		},
		"GET orgs/test-org/actions/permissions/fork-pr-contributor-approval": {
			Body: `{"approval_policy": "first_time_contributors"}`,
		},
		"GET orgs/test-org/actions/runners": {Body: `{"total_count": 0, "runners": []}`},
		"GET orgs/test-org/code-security/configurations/defaults": {
			Body: `[{"default_for_new_repos": "all", "configuration": {"name": "canonical"}}]`,
		},
		"GET orgs/test-org/installations":                       {Body: `{"total_count": 0, "installations": []}`},
		"GET orgs/test-org/hooks":                               {Body: `[]`},
		"GET orgs/test-org/actions/secrets":                     {Body: `{"total_count": 0, "secrets": []}`},
		"GET orgs/test-org/teams":                               {Body: `[]`},
		"GET orgs/test-org/personal-access-tokens":              {Body: `[]`},
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
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestAuditOrgCompliant(t *testing.T) {
	stubGH(t, compliantOrgResponses())

	findings, changes := AuditOrg(testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("a compliant organization planned %d change(s)", len(changes))
	}

	for _, finding := range findings {
		if finding.Check == checkOrgAdmins {
			if finding.Status != StatusAdvisory {
				t.Errorf("org-admins: %v, want the by-design advisory", finding.Status)
			}

			continue
		}

		if finding.Status != StatusOK {
			t.Errorf("%s: %v (%s), want ok", finding.Check, finding.Status, finding.Message)
		}
	}

	// Declaring the roster via the override file is what makes a compliant
	// org fully green.
	findings, _ = AuditOrg(testOrg, map[string]string{checkOrgAdmins: "alice is the org"})
	if !AllOK(findings) {
		t.Error("a compliant organization with a declared roster must pass entirely")
	}

	// The declaration is load-bearing: an owner the declaration does not name
	// brings the advisory back — a blanket exemption would hide a new owner.
	findings, _ = AuditOrg(testOrg, map[string]string{checkOrgAdmins: "bob is the org"})
	if finding, found := findingByCheck(findings, checkOrgAdmins); !found || finding.Status != StatusAdvisory {
		t.Errorf("an undeclared owner must surface as an advisory, got %v", finding.Status)
	}

	// Whole-token matching: a login that appears only as a SUBSTRING of the
	// declaration ("li" inside "alice") is not declared.
	findings, _ = AuditOrg(testOrg, map[string]string{checkOrgAdmins: "malice is the org"})
	if finding, found := findingByCheck(findings, checkOrgAdmins); !found || finding.Status != StatusAdvisory {
		t.Errorf("a substring-only match must not count as declared, got %v", finding.Status)
	}
}

// TestAuditOrgNonCompliant: every floor violation is flagged with the right
// verdict class, and applying the planned changes issues the consolidated
// org PATCH plus the Actions PUTs.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
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

	findings, changes := AuditOrg(testOrg, nil)

	wantFail := []string{
		checkOrgDefaultRepoPerm, checkOrgCreatePublicRepos, checkOrgForkPrivateRepos,
		checkOrgCreatePublicPages, checkOrgWebCommitSignoff,
		checkOrgActionsAllowed, checkOrgActionsShaPinning,
		checkOrgActionsWorkflow, checkOrgActionsApprovePRs, checkOrgForkPRApproval,
	}
	for _, check := range wantFail {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != StatusFail {
			t.Errorf("%s: %v, want fail", check, finding.Status)
		}
	}

	wantAdvisory := []string{
		checkOrgTwoFactor, checkOrgChangeVisibility, checkOrgDeleteRepos,
		checkOrgProfileDescription, checkOrgCommunityHealthRepo, checkOrgCommunityHealthSet,
	}
	for _, check := range wantAdvisory {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != StatusAdvisory {
			t.Errorf("%s: %v, want advisory (never auto-fixed)", check, finding.Status)
		}
	}

	if len(changes) == 0 {
		t.Fatal("a non-compliant organization planned no changes")
	}

	for _, planned := range changes {
		if err := planned.Apply(); err != nil {
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
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestAuditOrgCommunityHealthSubdirectory(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET repos/test-org/.github/contents/SECURITY.md"] = stubResponse{NotFound: true}
	responses["GET repos/test-org/.github/contents/.github/SECURITY.md"] = stubResponse{Body: "{}"}
	stubGH(t, responses)

	findings, _ := AuditOrg(testOrg, nil)

	if finding, found := findingByCheck(findings, checkOrgCommunityHealthSet); !found || finding.Status != StatusOK {
		t.Errorf("community-health set with .github/-located files: %v (%s), want ok",
			finding.Status, finding.Message)
	}
}

// TestAuditOrgUnverifiable: a token that can read nothing yields only
// unverifiable verdicts — never a pass, never a planned change.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestAuditOrgUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{})

	findings, changes := AuditOrg(testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("an unverifiable org audit planned %d change(s)", len(changes))
	}

	for _, finding := range findings {
		if finding.Status != StatusUnverifiable {
			t.Errorf("%s: %v, want unverifiable", finding.Check, finding.Status)
		}
	}

	if AllOK(findings) {
		t.Error("an entirely unverifiable org audit must not count as passing")
	}
}

// TestAuditOrgAnonymousObject: GET /orgs/{org} succeeds even anonymously,
// with the owner-scoped fields simply absent — those checks must classify
// as unverifiable, never as compliant zero values.
//
//nolint:paralleltest // serial by design: mutates the package-level ghBin.
func TestAuditOrgAnonymousObject(t *testing.T) {
	responses := compliantOrgResponses()
	responses["GET orgs/test-org"] = stubResponse{Body: `{"description": "an organization"}`}
	stubGH(t, responses)

	findings, changes := AuditOrg(testOrg, nil)

	if len(changes) != 0 {
		t.Errorf("absent fields planned %d change(s) — a zero value leaked through as a verdict", len(changes))
	}

	hidden := []string{
		checkOrgTwoFactor, checkOrgDefaultRepoPerm, checkOrgCreatePublicRepos,
		checkOrgForkPrivateRepos, checkOrgChangeVisibility, checkOrgDeleteRepos,
		checkOrgCreatePublicPages, checkOrgWebCommitSignoff,
	}
	for _, check := range hidden {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != StatusUnverifiable {
			t.Errorf("%s: %v, want unverifiable (the field is owner-scoped)", check, finding.Status)
		}
	}

	// The public field still answers.
	if finding, found := findingByCheck(findings, checkOrgProfileDescription); !found || finding.Status != StatusOK {
		t.Errorf("org-profile-description: %v, want ok", finding.Status)
	}
}

// TestOrgOverridesValidate: org-level identifiers are valid override keys.
func TestOrgOverridesValidate(t *testing.T) {
	t.Parallel()

	if !knownChecks()[checkOrgAdmins] || !knownChecks()[checkOrgActionsShaPinning] {
		t.Error("org check identifiers must be valid override-file keys")
	}
}

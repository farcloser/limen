package github_test

import (
	"os"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

const (
	repoTeamsKey    = "GET repos/test/repo/teams?per_page=100"
	orgTeamsKey     = "GET orgs/test/teams?per_page=100"
	grantKey        = "PUT orgs/test/teams/agents/repos/test/repo"
	orgTeamReposKey = "GET orgs/test-org/teams/agents/repos?per_page=100"
	orgReposKey     = "GET orgs/test-org/repos?per_page=100&type=all"
	orgMembersKey   = "GET orgs/test-org/teams/agents/members?per_page=100"
)

func TestAgentsTeamGranted(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	// maintain nests write: still compliant.
	responses[repoTeamsKey] = stubResponse{Body: `[{"slug":"agents","permission":"maintain"}]`}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, found := findingByCheck(findings, "agents-team"); !found || finding.Status != github.StatusOK {
		t.Errorf("agents team with maintain: %v (%s), want ok", finding.Status, finding.Message)
	}

	for _, planned := range changes {
		if planned.Check == "agents-team" {
			t.Error("a granted team planned a change")
		}
	}
}

func TestAgentsTeamMissingGrant(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses[repoTeamsKey] = stubResponse{
		Body: `[{"slug":"agents","permission":"pull"},{"slug":"ops","permission":"admin"}]`,
	}
	responses[grantKey] = stubResponse{Body: `{}`}
	logPath := stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "agents-team")
	if !found || finding.Status != github.StatusFail || finding.Current != "pull" {
		t.Fatalf("agents team with pull: %v current=%q, want fail with current pull", finding.Status, finding.Current)
	}

	if !strings.Contains(finding.Message, "gh api -X PUT orgs/test/teams/agents/repos/test/repo") {
		t.Errorf("the finding must print the grant command, got %q", finding.Message)
	}

	applied := 0

	for _, planned := range changes {
		if planned.Check != "agents-team" {
			continue
		}

		applied++

		if err := planned.Apply(t.Context()); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}

	if applied != 1 {
		t.Fatalf("planned %d change(s) for the grant, want 1", applied)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	if !strings.Contains(string(log), grantKey+"\n{\"permission\":\"push\"}") {
		t.Errorf("expected a PUT granting push, log:\n%s", log)
	}
}

func TestAgentsTeamAbsentFromRepo(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses[repoTeamsKey] = stubResponse{Body: `[]`}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "agents-team")
	if !found || finding.Status != github.StatusFail || finding.Current != "(no access)" {
		t.Errorf("no grant at all: %v current=%q, want fail with no access", finding.Status, finding.Current)
	}

	planned := 0

	for _, change := range changes {
		if change.Check == "agents-team" {
			planned++
		}
	}

	if planned != 1 {
		t.Errorf("planned %d change(s), want 1", planned)
	}
}

func TestAgentsTeamNoSuchTeam(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses[orgTeamsKey] = stubResponse{Body: `[{"slug":"ops"}]`}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "agents-team")
	if !found || finding.Status != github.StatusFail || !strings.Contains(finding.Message, "no "+"agents"+" team") {
		t.Errorf("missing team: %v (%s), want fail naming the missing team", finding.Status, finding.Message)
	}

	for _, planned := range changes {
		if planned.Check == "agents-team" {
			t.Error("a missing team planned a change; teams are never auto-fixed")
		}
	}
}

func TestAgentsTeamUserOwner(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET orgs/test"] = stubResponse{NotFound: true}
	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)

	if finding, found := findingByCheck(findings, "agents-team"); !found || finding.Status != github.StatusOK ||
		!strings.Contains(finding.Message, "not applicable") {
		t.Errorf("user-owned repository: %v (%s), want ok, not applicable", finding.Status, finding.Message)
	}
}

func TestAgentsTeamUnreadable(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	// The repository's team grants are admin-only: a 404 there is a scope
	// problem, never a pass and never a finding.
	responses := compliantResponses()
	responses[repoTeamsKey] = stubResponse{NotFound: true}
	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "agents-team")
	if !found || finding.Status != github.StatusUnverifiable {
		t.Errorf("unreadable grants: %v, want unverifiable", finding.Status)
	}

	// Same for the organization's team list.
	responses = compliantResponses()
	responses[orgTeamsKey] = stubResponse{NotFound: true}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, found = findingByCheck(findings, "agents-team")
	if !found || finding.Status != github.StatusUnverifiable {
		t.Errorf("unreadable team list: %v, want unverifiable", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == "agents-team" {
			t.Error("an unverifiable check planned a change")
		}
	}
}

func TestAuditOrgAgentsTeam(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	// Compliant: every live repository granted, one member.
	stubGH(t, compliantOrgResponses())

	findings, _ := github.AuditOrg(t.Context(), testOrg, nil)

	if finding, found := findingByCheck(findings, "org-agents-team"); !found || finding.Status != github.StatusOK {
		t.Errorf("all granted: %v (%s), want ok", finding.Status, finding.Message)
	}

	// Two repositories without the grant (one of them read-only), one
	// archived and ungranted, which does not count: fail, and one change that
	// grants exactly the two.
	responses := compliantOrgResponses()
	responses[orgReposKey] = stubResponse{
		Body: `[{"name":"alpha"},{"name":"beta"},{"name":"gamma"},{"name":"old","archived":true}]`,
	}
	responses[orgTeamReposKey] = stubResponse{
		Body: `[{"name":"alpha","permissions":{"push":true}},{"name":"gamma","permissions":{"push":false}}]`,
	}
	responses["PUT orgs/test-org/teams/agents/repos/test-org/beta"] = stubResponse{Body: `{}`}
	responses["PUT orgs/test-org/teams/agents/repos/test-org/gamma"] = stubResponse{Body: `{}`}
	logPath := stubGH(t, responses)

	findings, changes := github.AuditOrg(t.Context(), testOrg, nil)

	finding, found := findingByCheck(findings, "org-agents-team")
	if !found || finding.Status != github.StatusFail || finding.Current != "beta, gamma" {
		t.Fatalf("missing grants: %v current=%q, want fail listing beta, gamma", finding.Status, finding.Current)
	}

	for _, planned := range changes {
		if planned.Check == "org-agents-team" {
			if err := planned.Apply(t.Context()); err != nil {
				t.Fatalf("apply: %v", err)
			}
		}
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	calls := string(log)

	for _, want := range []string{
		"PUT orgs/test-org/teams/agents/repos/test-org/beta\n{\"permission\":\"push\"}",
		"PUT orgs/test-org/teams/agents/repos/test-org/gamma\n{\"permission\":\"push\"}",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("expected %q in the call log:\n%s", want, calls)
		}
	}

	if strings.Contains(calls, "repos/test-org/old") {
		t.Error("an archived repository must not be granted")
	}

	// No members: advisory, nothing planned — people are never auto-fixed.
	responses = compliantOrgResponses()
	responses[orgMembersKey] = stubResponse{Body: `[]`}
	stubGH(t, responses)

	findings, changes = github.AuditOrg(t.Context(), testOrg, nil)

	if finding, found := findingByCheck(
		findings,
		"org-agents-team",
	); !found ||
		finding.Status != github.StatusAdvisory {
		t.Errorf("empty team: %v, want advisory", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == "org-agents-team" {
			t.Error("an empty team planned a change")
		}
	}

	// No team: fail, nothing planned.
	responses = compliantOrgResponses()
	responses["GET orgs/test-org/teams?per_page=100"] = stubResponse{Body: `[]`}
	stubGH(t, responses)

	findings, changes = github.AuditOrg(t.Context(), testOrg, nil)

	if finding, found := findingByCheck(findings, "org-agents-team"); !found || finding.Status != github.StatusFail {
		t.Errorf("no team: %v, want fail", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == "org-agents-team" {
			t.Error("a missing team planned a change")
		}
	}
}

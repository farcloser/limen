package github_test

import (
	"os"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

const (
	rulesetsWithoutRenovate = `[{"id":1,"name":"limen:main","target":"branch","enforcement":"active"},` +
		`{"id":2,"name":"limen:tags","target":"tag","enforcement":"active"}]`
	renovateEnvironmentPath = "repos/test/repo/environments/renovate"
	renovatePoliciesList    = "GET " + renovateEnvironmentPath + "/deployment-branch-policies?per_page=100"
	renovateSecretGet       = "GET " + renovateEnvironmentPath + "/secrets/UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY"
	noPolicies              = `{"total_count":0,"branch_policies":[]}`
)

// applyChecks applies every planned change of the named checks.
func applyChecks(t *testing.T, changes []github.Change, checks ...string) {
	t.Helper()

	for _, planned := range changes {
		for _, check := range checks {
			if planned.Check == check {
				if err := planned.Apply(t.Context()); err != nil {
					t.Fatalf("apply %s: %v", check, err)
				}
			}
		}
	}
}

// TestRenovateGuardsCreated: a repository running the checksum workflow with
// neither the limen:renovate ruleset nor the renovate environment gets both,
// the ruleset letting Renovate and the update App (by its registry id)
// through, the environment admitting renovate/* alone.
func TestRenovateGuardsCreated(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/rulesets?per_page=100"] = stubResponse{Body: rulesetsWithoutRenovate}
	responses["GET "+renovateEnvironmentPath] = stubResponse{NotFound: true}
	responses[renovatePoliciesList] = stubResponse{Body: noPolicies}
	responses["POST repos/test/repo/rulesets"] = stubResponse{}
	responses["PUT "+renovateEnvironmentPath] = stubResponse{}
	responses["POST "+renovateEnvironmentPath+"/deployment-branch-policies"] = stubResponse{}
	logPath := stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	for _, check := range []string{"ruleset-renovate-branches", "renovate-environment"} {
		if finding, found := findingByCheck(findings, check); !found || finding.Status != github.StatusFail {
			t.Errorf("%s absent: %v, want fail", check, finding.Status)
		}
	}

	applyChecks(t, changes, "ruleset-renovate-branches", "renovate-environment")

	log, _ := os.ReadFile(logPath)
	for _, want := range []string{
		`"name":"limen:renovate"`, `"include":["refs/heads/renovate/**"]`,
		`{"actor_type":"Integration","bypass_mode":"always","actor_id":2740}`,
		`{"actor_type":"Integration","bypass_mode":"always","actor_id":7}`,
		`"custom_branch_policies":true`, `{"name":"renovate/*","type":"branch"}`,
	} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the fixes did not write %s:\n%s", want, log)
		}
	}
}

// TestRenovateEnvironmentKeyMissing: the environment is right and holds no
// key. The value is the human's, so the check fails with the command and
// plans nothing.
func TestRenovateEnvironmentKeyMissing(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses[renovateSecretGet] = stubResponse{NotFound: true}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "renovate-environment")
	if finding.Status != github.StatusFail {
		t.Fatalf("missing key: %v, want fail", finding.Status)
	}

	if !strings.Contains(
		finding.Message,
		"gh secret set UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY --env renovate --repo test/repo",
	) {
		t.Errorf("the finding does not carry the command: %s", finding.Message)
	}

	for _, planned := range changes {
		if planned.Check == "renovate-environment" {
			t.Error("the key's value is the human's: no change may be planned")
		}
	}
}

// TestRenovateEnvironmentDrift: an environment open to every branch, with an
// extra policy once restricted, is reconciled: the PUT restores custom
// policies and the stray policy is deleted; renovate/* already present is
// not written again.
func TestRenovateEnvironmentDrift(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET "+renovateEnvironmentPath] = stubResponse{
		Body: `{"deployment_branch_policy":null,"protection_rules":[]}`,
	}
	responses[renovatePoliciesList] = stubResponse{
		Body: `{"total_count":2,"branch_policies":[{"id":11,"name":"renovate/*","type":"branch"},{"id":12,"name":"main","type":"branch"}]}`,
	}
	responses["PUT "+renovateEnvironmentPath] = stubResponse{}
	responses["DELETE "+renovateEnvironmentPath+"/deployment-branch-policies/12"] = stubResponse{}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "renovate-environment")
	if finding.Status != github.StatusFail || !strings.Contains(finding.Message, "every branch") {
		t.Fatalf("open environment: %v (%s), want fail naming it", finding.Status, finding.Message)
	}

	applyChecks(t, changes, "renovate-environment")
}

// TestRenovateGuardsWithoutChecksumWorkflow: a repository without the
// checksum workflow has no key to guard: the environment check passes saying
// so, the ruleset is not asked for, and the App is never looked up.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRenovateGuardsWithoutChecksumWorkflow(t *testing.T) {
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows/update-aqua-checksum.yaml"] = stubResponse{NotFound: true}
	responses["GET repos/test/repo/rulesets?per_page=100"] = stubResponse{Body: rulesetsWithoutRenovate}
	delete(responses, "GET apps/limen-ci-test")
	logPath := stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "renovate-environment"); finding.Status != github.StatusOK {
		t.Errorf("no checksum workflow: %v (%s), want ok", finding.Status, finding.Message)
	}

	if _, found := findingByCheck(findings, "ruleset-renovate-branches"); found {
		t.Error("no checksum workflow: the renovate ruleset must not be asked for")
	}

	if len(changes) != 0 {
		t.Errorf("no checksum workflow: %d change(s) planned, want none", len(changes))
	}

	log, _ := os.ReadFile(logPath)
	if strings.Contains(string(log), "GET apps/") {
		t.Errorf("the App was looked up with nothing to guard:\n%s", log)
	}
}

// TestRenovateRulesetWithoutApp: the ruleset's bypass list names the update
// App by id; an organisation that never registered it gets a failure naming
// the registration, and no ruleset written without the App in it.
func TestRenovateRulesetWithoutApp(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/rulesets?per_page=100"] = stubResponse{Body: rulesetsWithoutRenovate}
	responses["GET apps/limen-ci-test"] = stubResponse{NotFound: true}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "ruleset-renovate-branches")
	if finding.Status != github.StatusFail || !strings.Contains(finding.Message, "limen bootstrap") {
		t.Fatalf("no App: %v (%s), want fail naming the registration", finding.Status, finding.Message)
	}

	for _, planned := range changes {
		if planned.Check == "ruleset-renovate-branches" {
			t.Error("no App: no ruleset may be written without it in the bypass list")
		}
	}
}

// TestRenovateEnvironmentDetection: each drift the environment check names is
// detected on its own. A custom-policy environment admitting `main` beside
// `renovate/*`, and one carrying a reviewer, both fail naming it.
func TestRenovateEnvironmentDetection(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	cases := []struct {
		name     string
		detail   string
		policies string
		want     string
	}{
		{
			name:     "an extra policy",
			detail:   `{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[]}`,
			policies: `{"total_count":2,"branch_policies":[{"id":11,"name":"renovate/*","type":"branch"},{"id":12,"name":"main","type":"branch"}]}`,
			want:     "instead of renovate/* alone",
		},
		{
			name:     "a reviewer",
			detail:   `{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[{"id":1,"type":"required_reviewers"}]}`,
			policies: `{"total_count":1,"branch_policies":[{"id":11,"name":"renovate/*","type":"branch"}]}`,
			want:     "carries protection rules",
		},
	}

	for _, testCase := range cases {
		responses := compliantResponses()
		responses["GET "+renovateEnvironmentPath] = stubResponse{Body: testCase.detail}
		responses[renovatePoliciesList] = stubResponse{Body: testCase.policies}
		stubGH(t, responses)

		findings, _ := github.Audit(t.Context(), testRepo, nil)

		finding, _ := findingByCheck(findings, "renovate-environment")
		if finding.Status != github.StatusFail || !strings.Contains(finding.Message, testCase.want) {
			t.Errorf("%s: %v (%s), want fail saying %q", testCase.name, finding.Status, finding.Message, testCase.want)
		}
	}
}

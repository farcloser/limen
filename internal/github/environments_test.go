package github_test

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// workflowListing is the contents-API listing of a workflows directory
// carrying the given files.
func workflowListing(names ...string) stubResponse {
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, `{"name":"`+name+`","path":".github/workflows/`+name+`","type":"file"}`)
	}

	return stubResponse{Body: "[" + strings.Join(entries, ",") + "]"}
}

// fileResponse is the contents-API answer for a file with the given text.
func fileResponse(text string) stubResponse {
	return stubResponse{Body: `{"content":"` + base64.StdEncoding.EncodeToString([]byte(text)) + `"}`}
}

// signingWorkflow is a release workflow whose sign job reads the signing
// credentials from the release environment.
const signingWorkflow = "on:\n  push:\n    tags: ['v*']\njobs:\n  sign:\n    environment: release\n" +
	"    env:\n      P12: ${{ secrets.APPLE_DEVELOPER_ID_P12 }}\n      KEY: ${{ secrets.APPLE_NOTARY_KEY }}\n"

// releaseEnvironmentJSON is a release environment of the canonical shape,
// reviewed by user 7.
const releaseEnvironmentJSON = `{"total_count":1,"environments":[{"name":"release",` +
	`"protection_rules":[{"type":"required_reviewers","prevent_self_review":false,` +
	`"reviewers":[{"type":"User","reviewer":{"login":"alice","id":7}}]},{"type":"branch_policy"}],` +
	`"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}]}`

const releasePoliciesPath = "repos/test/repo/environments/release/deployment-branch-policies"

func signingResponses() map[string]stubResponse {
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows"] = workflowListing("release.yaml")
	responses["GET repos/test/repo/contents/.github/workflows/release.yaml"] = fileResponse(signingWorkflow)
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{Body: releaseEnvironmentJSON}
	responses["GET "+releasePoliciesPath+"?per_page=100"] = stubResponse{
		Body: `{"total_count":1,"branch_policies":[{"id":2,"name":"v*","type":"tag"}]}`,
	}
	responses["GET repos/test/repo/environments/release/secrets?per_page=100"] = stubResponse{
		Body: `{"total_count":2,"secrets":[{"name":"APPLE_DEVELOPER_ID_P12"},{"name":"APPLE_NOTARY_KEY"}]}`,
	}

	return responses
}

func callLog(t *testing.T, logPath string) string {
	t.Helper()

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	return string(log)
}

// TestRepositorySecretIsAdvisory: a repository secret is readable from any
// branch a write account pushes, so one present is an advisory naming the
// environment move, never a fix.
func TestRepositorySecretIsAdvisory(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/actions/secrets?per_page=100"] = stubResponse{
		Body: `{"total_count":1,"secrets":[{"name":"DEPLOY_TOKEN"}]}`,
	}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "actions-secrets")
	if !found || finding.Status != github.StatusAdvisory {
		t.Fatalf("actions-secrets: %+v, want an advisory", finding)
	}

	if !strings.Contains(finding.Message, "gh secret set DEPLOY_TOKEN --env <environment> --repo test/repo") {
		t.Errorf("the advisory does not name the move: %s", finding.Message)
	}

	for _, change := range changes {
		if change.Check == "actions-secrets" {
			t.Error("a secret is never moved by a fix")
		}
	}
}

// TestReleaseEnvCompliant: the canonical release environment holding
// what the sign job reads passes.
func TestReleaseEnvCompliant(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	stubGH(t, signingResponses())

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusOK {
		t.Errorf("environments: %+v, want ok", finding)
	}

	if len(changes) != 0 {
		t.Errorf("planned %d change(s) on a compliant release environment", len(changes))
	}
}

// TestReleaseEnvCreated: a sign job reading the signing credentials
// with no release environment fails, and the fix creates the environment
// reviewed by the owners, admitting the v* tags alone.
func TestReleaseEnvCreated(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: `{"total_count":0,"environments":[]}`,
	}
	responses["GET orgs/test/members?role=admin&per_page=100"] = stubResponse{Body: `[{"login":"alice","id":7}]`}
	responses["PUT repos/test/repo/environments/release"] = stubResponse{}
	responses["POST "+releasePoliciesPath] = stubResponse{}
	logPath := stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "environments")
	if finding.Status != github.StatusFail || finding.Current != "does not exist" {
		t.Fatalf("environments: %+v, want a failure on the missing environment", finding)
	}

	if len(changes) != 1 || changes[0].Check != "environments" {
		t.Fatalf("changes: %+v, want the one environment change", changes)
	}

	if err := changes[0].Apply(t.Context()); err != nil {
		t.Fatal(err)
	}

	calls := callLog(t, logPath)

	for _, want := range []string{
		`PUT repos/test/repo/environments/release`,
		`"deployment_branch_policy":{"custom_branch_policies":true,"protected_branches":false}`,
		`"reviewers":[{"id":7,"type":"User"}]`,
		`POST ` + releasePoliciesPath,
		`{"name":"v*","type":"tag"}`,
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("the fix did not %s:\n%s", want, calls)
		}
	}
}

// TestReleaseEnvStrayPolicyRemoved: a release environment admitting
// a branch beside the v* tags is reconciled, its reviewers kept and the stray
// policy deleted.
func TestReleaseEnvStrayPolicyRemoved(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET "+releasePoliciesPath+"?per_page=100"] = stubResponse{
		Body: `{"total_count":2,"branch_policies":[{"id":1,"name":"main","type":"branch"},` +
			`{"id":2,"name":"v*","type":"tag"}]}`,
	}
	responses["PUT repos/test/repo/environments/release"] = stubResponse{}
	responses["DELETE "+releasePoliciesPath+"/1"] = stubResponse{}
	logPath := stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusFail ||
		!strings.Contains(finding.Current, "branch main") {
		t.Fatalf("environments: %+v, want a failure naming the stray branch", finding)
	}

	if len(changes) != 1 {
		t.Fatalf("changes: %+v", changes)
	}

	if err := changes[0].Apply(t.Context()); err != nil {
		t.Fatal(err)
	}

	calls := callLog(t, logPath)
	if !strings.Contains(calls, `"reviewers":[{"id":7,"type":"User"}]`) {
		t.Errorf("the existing reviewer was not kept:\n%s", calls)
	}

	if !strings.Contains(calls, "DELETE "+releasePoliciesPath+"/1") {
		t.Errorf("the stray branch policy was not deleted:\n%s", calls)
	}

	if strings.Contains(calls, "POST "+releasePoliciesPath) {
		t.Errorf("the tag policy already existed and was added again:\n%s", calls)
	}
}

// TestReleaseEnvWithoutReviewerFails: a release environment nobody
// reviews fails; without a readable owner roster there is no fix.
func TestReleaseEnvWithoutReviewerFails(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: strings.Replace(
			releaseEnvironmentJSON,
			`"reviewers":[{"type":"User","reviewer":{"login":"alice","id":7}}]`,
			`"reviewers":[]`,
			1,
		),
	}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusFail ||
		finding.Current != "requires no reviewer" {
		t.Fatalf("environments: %+v", finding)
	}

	if len(changes) != 0 {
		t.Errorf("no owner roster readable, yet a change was planned: %+v", changes)
	}
}

// TestReleaseEnvMissingSecretNamed: a secret the sign job
// reads and the environment does not hold fails with the command that sets
// it; the value is the human's.
func TestReleaseEnvMissingSecretNamed(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/environments/release/secrets?per_page=100"] = stubResponse{
		Body: `{"total_count":1,"secrets":[{"name":"APPLE_DEVELOPER_ID_P12"}]}`,
	}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "environments")
	if finding.Status != github.StatusFail ||
		!strings.Contains(finding.Message, "gh secret set APPLE_NOTARY_KEY --env release --repo test/repo") {
		t.Fatalf("environments: %+v, want the gh secret set command", finding)
	}

	if len(changes) != 0 {
		t.Errorf("a secret's value is never a fix's: %+v", changes)
	}
}

// TestSigningSecretNeedsReleaseEnv: a job reading a
// signing credential with no environment declared still needs release.
func TestSigningSecretNeedsReleaseEnv(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/contents/.github/workflows/release.yaml"] = fileResponse(
		strings.Replace(signingWorkflow, "    environment: release\n", "", 1),
	)
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: `{"total_count":0,"environments":[]}`,
	}
	responses["GET orgs/test/members?role=admin&per_page=100"] = stubResponse{Body: `[{"login":"alice","id":7}]`}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Current != "does not exist" {
		t.Fatalf("environments: %+v", finding)
	}

	if len(changes) != 1 {
		t.Errorf("changes: %+v, want the environment created", changes)
	}
}

// TestEnvAdmittingEveryBranchFails: an environment a job deploys to
// with no deployment branch policy gates nothing; which refs it should admit
// is the project's, so no fix.
func TestEnvAdmittingEveryBranchFails(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows"] = workflowListing("deploy.yaml")
	responses["GET repos/test/repo/contents/.github/workflows/deploy.yaml"] = fileResponse(
		"on:\n  push:\n    branches: [main]\njobs:\n  deploy:\n    environment: staging\n" +
			"    env:\n      T: ${{ secrets.DEPLOY_TOKEN }}\n",
	)
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: `{"total_count":1,"environments":[{"name":"staging","protection_rules":[],` +
			`"deployment_branch_policy":null}]}`,
	}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusFail ||
		finding.Current != "every branch" {
		t.Fatalf("environments: %+v", finding)
	}

	if len(changes) != 0 {
		t.Errorf("the refs an environment admits are the project's, yet a change was planned: %+v", changes)
	}
}

// TestEnvNamedButMissingFails: a job deploying to an environment that
// does not exist fails; GitHub would create it ungated on the first run.
func TestEnvNamedButMissingFails(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows"] = workflowListing("deploy.yaml")
	responses["GET repos/test/repo/contents/.github/workflows/deploy.yaml"] = fileResponse(
		"on: push\njobs:\n  deploy:\n    environment: staging\n    secrets: inherit\n" +
			"    uses: o/r/.github/workflows/d.yaml@a\n",
	)
	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusFail ||
		finding.Current != "missing" {
		t.Fatalf("environments: %+v", finding)
	}
}

// TestUnreadableWorkflowIsUnverifiable: an environment check that could not
// read a workflow says so rather than passing what it did not see.
func TestUnreadableWorkflowIsUnverifiable(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows"] = workflowListing("deploy.yaml")
	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusUnverifiable {
		t.Fatalf("environments: %+v, want unverifiable", finding)
	}
}

// TestReleaseEnvForUserOwnedRepo: with no organization to
// read owners from, the user the repository belongs to reviews.
func TestReleaseEnvForUserOwnedRepo(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: `{"total_count":0,"environments":[]}`,
	}
	responses["GET orgs/test/members?role=admin&per_page=100"] = stubResponse{NotFound: true}
	responses["GET users/test"] = stubResponse{Body: `{"login":"test","id":9}`}
	responses["PUT repos/test/repo/environments/release"] = stubResponse{}
	responses["POST "+releasePoliciesPath] = stubResponse{}
	logPath := stubGH(t, responses)

	_, changes := github.Audit(t.Context(), testRepo, nil)
	if len(changes) != 1 {
		t.Fatalf("changes: %+v, want the environment created", changes)
	}

	if err := changes[0].Apply(t.Context()); err != nil {
		t.Fatal(err)
	}

	if calls := callLog(t, logPath); !strings.Contains(calls, `"reviewers":[{"id":9,"type":"User"}]`) {
		t.Errorf("the owning user does not review:\n%s", calls)
	}
}

// appleCallerWorkflow is a project's release.yaml on the Apple release lane:
// the lane's sign job declares the release environment, in limen's file.
const appleCallerWorkflow = "on:\n  push:\n    tags: ['v*']\njobs:\n  release:\n" +
	"    uses: farcloser/limen/.github/workflows/limen-release-apple.yaml@" +
	"0123456789abcdef0123456789abcdef01234567 # v9.9.9\n    secrets: inherit\n"

// TestAppleLaneCallerNeedsRelease: a release.yaml calling the Apple lane
// deploys to release through the called sign job, which reads the identity
// there: the environment is required, created by the fix, and judged for
// the identity's two secrets.
func TestAppleLaneCallerNeedsRelease(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := signingResponses()
	responses["GET repos/test/repo/contents/.github/workflows/release.yaml"] = fileResponse(appleCallerWorkflow)
	responses["GET repos/test/repo/environments?per_page=100"] = stubResponse{
		Body: `{"total_count":0,"environments":[]}`,
	}
	responses["GET orgs/test/members?role=admin&per_page=100"] = stubResponse{Body: `[{"login":"alice","id":7}]`}
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Current != "does not exist" {
		t.Fatalf("environments: %+v, want the missing release environment", finding)
	}

	if len(changes) != 1 {
		t.Fatalf("changes: %+v, want the environment created", changes)
	}

	// With the environment there, the identity's secrets are what it must hold.
	responses = signingResponses()
	responses["GET repos/test/repo/contents/.github/workflows/release.yaml"] = fileResponse(appleCallerWorkflow)
	responses["GET repos/test/repo/environments/release/secrets?per_page=100"] = stubResponse{
		Body: `{"total_count":1,"secrets":[{"name":"APPLE_DEVELOPER_ID_P12"}]}`,
	}
	stubGH(t, responses)

	findings, _ = github.Audit(t.Context(), testRepo, nil)

	finding, _ := findingByCheck(findings, "environments")
	if finding.Status != github.StatusFail ||
		!strings.Contains(finding.Message, "gh secret set APPLE_DEVELOPER_ID_P12_PASSWORD --env release") {
		t.Fatalf("environments: %+v, want the missing password named", finding)
	}
}

// TestCalledOnlyWorkflowDeploysNowhere: a workflow that runs only when called
// (limen's own copy of the lane) deploys in its callers' repositories, so
// the environment its job declares is not this repository's to hold.
func TestCalledOnlyWorkflowDeploysNowhere(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo/contents/.github/workflows"] = workflowListing("limen-release-apple.yaml")
	responses["GET repos/test/repo/contents/.github/workflows/limen-release-apple.yaml"] = fileResponse(
		"on:\n  workflow_call:\n    secrets:\n      APPLE_DEVELOPER_ID_P12:\n        required: true\n" +
			"jobs:\n  sign:\n    environment: release\n    env:\n      P: ${{ secrets.APPLE_DEVELOPER_ID_P12 }}\n",
	)
	stubGH(t, responses)

	findings, changes := github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "environments"); finding.Status != github.StatusOK {
		t.Errorf("environments: %+v, want ok", finding)
	}

	if len(changes) != 0 {
		t.Errorf("changes: %+v, want none", changes)
	}
}

// White-box tests: the gh transport seam (ghBin) and the auditor's internals
// are package-private by design — testing through them is the point.

package github //nolint:testpackage // white-box (see above).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testRepo = "test/repo"

// compliantRepoJSON is a repository object that satisfies every baseline
// check answered by GET /repos/{owner}/{repo}.
const compliantRepoJSON = `{
  "private": false,
  "default_branch": "main",
  "description": "a description",
  "topics": ["tooling"],
  "has_wiki": false,
  "has_projects": false,
  "has_discussions": false,
  "allow_merge_commit": false,
  "allow_squash_merge": true,
  "allow_rebase_merge": true,
  "allow_auto_merge": true,
  "allow_update_branch": true,
  "allow_forking": true,
  "delete_branch_on_merge": true,
  "web_commit_signoff_required": true,
  "squash_merge_commit_title": "PR_TITLE",
  "squash_merge_commit_message": "PR_BODY",
  "security_and_analysis": {
    "secret_scanning": {"status": "enabled"},
    "secret_scanning_push_protection": {"status": "enabled"}
  }
}`

// stubResponse is one canned gh api answer: a body (exit 0), a 404, or a
// generic error. Fields are exported for the JSON round trip to the stub
// process; the type stays test-internal.
type stubResponse struct {
	Body     string `json:"body"`
	NotFound bool   `json:"notFound"`
	Fail     bool   `json:"fail"`
}

// ghStubEnv carries the stub directory to the re-executed test binary. When
// set, TestMain acts as the fake gh instead of running the suite.
const ghStubEnv = "LIMEN_TEST_GH_STUB_DIR"

// stubGH points ghBin at THIS test binary in stub mode (the stdlib
// helper-process pattern): re-executed by the code under test, TestMain sees
// ghStubEnv and answers "METHOD path" keys from responses; anything unlisted
// errors generically (the unverifiable path). Every invocation is logged to
// the returned file, write payloads included. No shell is involved anywhere —
// a generated script cannot be exec'd on Windows (no shebangs, PATHEXT), and
// a .bat shim re-parses metacharacters like the '&' in query strings.
func stubGH(t *testing.T, responses map[string]stubResponse) string {
	t.Helper()

	dir := t.TempDir()

	data, err := json.Marshal(responses)
	if err != nil {
		t.Fatalf("marshaling stub responses: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "responses.json"), data, 0o600); err != nil {
		t.Fatalf("writing stub responses: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}

	// Inherited by the children the code under test spawns (it does not set
	// cmd.Env). Setenv also restores on cleanup and forbids t.Parallel —
	// these tests are serial by design already (they mutate ghBin).
	t.Setenv(ghStubEnv, dir)

	previous := ghBin
	ghBin = self

	t.Cleanup(func() { ghBin = previous })

	return filepath.Join(dir, "calls.log")
}

// TestMain lets the binary play both roles: the test suite, and — when
// re-executed by the code under test with ghStubEnv set — the fake gh.
func TestMain(m *testing.M) {
	if dir := os.Getenv(ghStubEnv); dir != "" {
		// The stub's exit status IS its contract (gh exits 1 on API errors);
		// this branch never reaches the test runner's own exit handling.
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runGHStub(dir))
	}

	m.Run()
}

// runGHStub is the fake gh. argv mirrors the production invocation:
// api --method METHOD PATH [--input -]. Mirroring the real gh: bodies go to
// stdout with exit 0, errors to stderr with exit 1.
func runGHStub(dir string) int {
	args := os.Args[1:]
	if len(args) < 4 {
		fmt.Fprintf(os.Stderr, "gh stub: unexpected argv %q\n", args)

		return 1
	}

	method, path := args[2], args[3]
	key := method + " " + path

	logFile, err := os.OpenFile(
		filepath.Join(dir, "calls.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}
	defer logFile.Close()

	// Log first, respond second — unlisted writes are logged too.
	fmt.Fprintln(logFile, key)

	if len(args) >= 6 && args[4] == "--input" {
		if _, err := io.Copy(logFile, os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

			return 1
		}

		fmt.Fprintln(logFile)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "responses.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	var responses map[string]stubResponse
	if err := json.Unmarshal(raw, &responses); err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	response, listed := responses[key]

	switch {
	case !listed && method == "GET":
		// Unknown reads error (the unverifiable path)...
		fmt.Fprintln(os.Stderr, "gh: boom (HTTP 500)")

		return 1
	case !listed:
		// ...while unlisted writes succeed silently.
		return 0
	case response.NotFound:
		fmt.Fprintln(os.Stderr, "gh: Not Found (HTTP 404)")

		return 1
	case response.Fail:
		fmt.Fprintln(os.Stderr, "gh: boom (HTTP 500)")

		return 1
	default:
		fmt.Print(response.Body)

		return 0
	}
}

// compliantResponses answers every audited endpoint with baseline-satisfying
// values.
func compliantResponses() map[string]stubResponse {
	return map[string]stubResponse{
		"GET repos/test/repo":                                 {Body: compliantRepoJSON},
		"GET repos/test/repo/vulnerability-alerts":            {Body: ""},
		"GET repos/test/repo/automated-security-fixes":        {Body: `{"enabled": true}`},
		"GET repos/test/repo/private-vulnerability-reporting": {Body: `{"enabled": true}`},
		"GET repos/test/repo/actions/permissions/workflow": {
			Body: `{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`,
		},
		"GET repos/test/repo/actions/permissions": {Body: `{"enabled":true,"allowed_actions":"selected"}`},
		"GET repos/test/repo/rulesets": {
			Body: `[{"id":1,"name":"limen:main","target":"branch","enforcement":"active"},{"id":2,"name":"limen:tags","target":"tag","enforcement":"active"}]`,
		},
		"GET repos/test/repo/rulesets/1": {
			Body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},{"type":"required_linear_history"},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":false,"required_status_checks":[{"context":"verify (ubuntu-24.04)"}]}}]}`,
		},
		"GET repos/test/repo/rulesets/2": {
			Body: `{"rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"}]}`,
		},
		"GET repos/test/repo/actions/permissions/fork-pr-contributor-approval": {
			Body: `{"approval_policy":"first_time_contributors"}`,
		},
		"GET repos/test/repo/collaborators?affiliation=outside&per_page=100": {Body: `[]`},
		"GET repos/test/repo/hooks": {Body: `[]`},
		"GET repos/test/repo/keys":  {Body: `[]`},
		"GET repos/test/repo/pages": {NotFound: true},
	}
}

func findingByCheck(findings []Finding, check string) (Finding, bool) {
	for _, finding := range findings {
		if finding.Check == check {
			return finding, true
		}
	}

	return Finding{}, false
}

func TestAuditCompliant(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	stubGH(t, compliantResponses())

	findings, changes := Audit(testRepo, nil)

	if !AllOK(findings) {
		for _, finding := range findings {
			if !finding.OK() {
				t.Errorf("%s: %s (%s)", finding.Check, finding.Status, finding.Message)
			}
		}
	}

	if len(changes) != 0 {
		t.Errorf("compliant repository planned %d change(s)", len(changes))
	}
}

func TestAuditNonCompliant(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo"] = stubResponse{Body: `{
	  "private": true,
	  "default_branch": "master",
	  "description": "",
	  "topics": [],
	  "has_wiki": true,
	  "has_projects": false,
	  "has_discussions": false,
	  "allow_merge_commit": true,
	  "allow_squash_merge": true,
	  "allow_rebase_merge": true,
	  "allow_auto_merge": false,
	  "allow_update_branch": true,
	  "allow_forking": true,
	  "delete_branch_on_merge": false,
	  "web_commit_signoff_required": false,
	  "squash_merge_commit_title": "COMMIT_OR_PR_TITLE",
	  "squash_merge_commit_message": "COMMIT_MESSAGES",
	  "security_and_analysis": {
	    "secret_scanning": {"status": "disabled"},
	    "secret_scanning_push_protection": {"status": "disabled"}
	  }
	}`}
	responses["GET repos/test/repo/vulnerability-alerts"] = stubResponse{NotFound: true}
	responses["GET repos/test/repo/rulesets"] = stubResponse{Body: `[]`}
	stubGH(t, responses)

	findings, changes := Audit(testRepo, nil)

	wantFail := []string{
		checkMergeMethods, checkSquashDefaults, checkDeleteBranchOnMerge, checkAutoMerge,
		checkWebCommitSignoff, checkWiki, checkForking, checkSecretScanning,
		checkPushProtection, checkDependabotAlerts, checkRulesetDefaultBranch, checkRulesetVersionTags,
	}
	for _, check := range wantFail {
		finding, found := findingByCheck(findings, check)
		if !found || finding.Status != StatusFail {
			t.Errorf("%s: status %v, want fail", check, finding.Status)
		}
	}

	wantAdvisory := []string{checkDefaultBranch, checkDescription}
	for _, check := range wantAdvisory {
		finding, found := findingByCheck(findings, check)
		if !found || finding.Status != StatusAdvisory {
			t.Errorf("%s: status %v, want advisory", check, finding.Status)
		}
	}

	// Private repository: topics do not apply.
	if finding, _ := findingByCheck(findings, checkTopics); finding.Status != StatusOK {
		t.Errorf("topics on a private repository: %v, want ok", finding.Status)
	}

	if len(changes) == 0 {
		t.Fatal("non-compliant repository planned no changes")
	}
}

func TestAuditUnverifiable(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	// Everything errors (a token with no access at all).
	stubGH(t, map[string]stubResponse{})

	findings, changes := Audit(testRepo, nil)

	if len(changes) != 0 {
		t.Errorf("unverifiable audit planned %d change(s)", len(changes))
	}

	for _, finding := range findings {
		if finding.Check == checkCodeScanning {
			// Opt-in and not opted in: reported ok without any API call.
			continue
		}

		if finding.Status != StatusUnverifiable {
			t.Errorf("%s: status %v, want unverifiable", finding.Check, finding.Status)
		}
	}

	if AllOK(findings) {
		t.Error("an entirely unverifiable audit must not count as passing")
	}
}

func TestApplyChanges(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo"] = stubResponse{Body: strings.Replace(
		compliantRepoJSON, `"has_wiki": false`, `"has_wiki": true`, 1)}
	responses["GET repos/test/repo/vulnerability-alerts"] = stubResponse{NotFound: true}
	logPath := stubGH(t, responses)

	_, changes := Audit(testRepo, nil)
	if len(changes) == 0 {
		t.Fatal("expected planned changes")
	}

	for _, planned := range changes {
		if err := planned.Apply(testRepo); err != nil {
			t.Errorf("%s: %v", planned.Check, err)
		}
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading call log: %v", err)
	}

	calls := string(log)
	if !strings.Contains(calls, "PATCH repos/test/repo") {
		t.Error("expected a consolidated PATCH of the repository object")
	}

	if !strings.Contains(calls, `"has_wiki":false`) {
		t.Error("expected the PATCH payload to turn the wiki off")
	}

	if !strings.Contains(calls, "PUT repos/test/repo/vulnerability-alerts") {
		t.Error("expected Dependabot alerts to be enabled")
	}
}

func TestOverrideExempts(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo"] = stubResponse{Body: strings.Replace(
		compliantRepoJSON, `"has_wiki": false`, `"has_wiki": true`, 1)}
	stubGH(t, responses)

	findings, changes := Audit(testRepo, map[string]string{checkWiki: "hosts the operations runbook"})

	finding, found := findingByCheck(findings, checkWiki)
	if !found || finding.Status != StatusOK {
		t.Errorf("exempted wiki check: %v, want ok", finding.Status)
	}

	if !strings.Contains(finding.Message, "hosts the operations runbook") {
		t.Errorf("exemption message should carry the reason, got %q", finding.Message)
	}

	for _, planned := range changes {
		if planned.Check == checkWiki {
			t.Error("an exempted check must not plan a change")
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, filepath.FromSlash(OverridePath))

	write := func(content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Missing file: no exceptions (an empty, non-nil map), no error.
	missing, err := LoadOverrides(t.TempDir())
	if err != nil || len(missing) != 0 {
		t.Errorf("missing file: overrides %v, err %v", missing, err)
	}

	write("# comment\n\nwiki: hosts the runbook\npages: marketing site\n")

	overrides, err := LoadOverrides(dir)
	if err != nil {
		t.Fatalf("valid file: %v", err)
	}

	if overrides[checkWiki] != "hosts the runbook" || overrides[checkPages] != "marketing site" {
		t.Errorf("parsed overrides: %v", overrides)
	}

	write("nonsense-check: because\n")

	if _, err := LoadOverrides(dir); err == nil {
		t.Error("unknown check identifier must fail the file")
	}

	write("wiki:\n")

	if _, err := LoadOverrides(dir); err == nil {
		t.Error("an exception without a reason must fail the file")
	}
}

func TestInferRepo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		remote string
		want   string
		wantOK bool
	}{
		{name: "ssh", remote: "git@github.com:farcloser/limen.git", want: "farcloser/limen", wantOK: true},
		{name: "https", remote: "https://github.com/farcloser/limen", want: "farcloser/limen", wantOK: true},
		{
			name:   "https with .git",
			remote: "https://github.com/farcloser/limen.git",
			want:   "farcloser/limen",
			wantOK: true,
		},
		{name: "not github", remote: "https://gitlab.com/x/y.git", wantOK: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, args := range [][]string{
				{"init", "--quiet"},
				{"remote", "add", "origin", testCase.remote},
			} {
				gitCmd := exec.Command(
					"git",
					append([]string{"-C", dir}, args...)...)
				if out, gitErr := gitCmd.CombinedOutput(); gitErr != nil {
					t.Fatalf("git %v: %v: %s", args, gitErr, out)
				}
			}

			slug, err := InferRepo(dir)

			if testCase.wantOK && (err != nil || slug != testCase.want) {
				t.Errorf("InferRepo = %q, %v; want %q", slug, err, testCase.want)
			}

			if !testCase.wantOK && err == nil {
				t.Errorf("InferRepo = %q, want error", slug)
			}
		})
	}
}

func TestForkPRApproval(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo/actions/permissions/fork-pr-contributor-approval"] = stubResponse{
		Body: `{"approval_policy":"first_time_contributors_new_to_github"}`,
	}
	logPath := stubGH(t, responses)

	findings, changes := Audit(testRepo, nil)

	finding, found := findingByCheck(findings, checkForkPRApproval)
	if !found || finding.Status != StatusFail {
		t.Errorf("weakest approval policy: %v, want fail", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == checkForkPRApproval {
			if err := planned.Apply(testRepo); err != nil {
				t.Fatalf("apply: %v", err)
			}
		}
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(log), "PUT repos/test/repo/actions/permissions/fork-pr-contributor-approval") {
		t.Error("expected the approval policy to be raised via PUT")
	}

	if !strings.Contains(string(log), `"approval_policy":"first_time_contributors"`) {
		t.Error("expected the baseline approval policy in the payload")
	}
}

func TestActionsAccessLevel(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	// Public repository: not applicable, and the endpoint is never queried.
	stubGH(t, compliantResponses())

	findings, _ := Audit(testRepo, nil)
	if finding, _ := findingByCheck(findings, checkActionsAccessLevel); finding.Status != StatusOK {
		t.Errorf("public repository access level: %v, want ok (not applicable)", finding.Status)
	}

	// Private repository with outside access: fail with a fix down to none.
	responses := compliantResponses()
	responses["GET repos/test/repo"] = stubResponse{
		Body: strings.Replace(compliantRepoJSON, `"private": false`, `"private": true`, 1),
	}
	responses["GET repos/test/repo/actions/permissions/access"] = stubResponse{Body: `{"access_level":"organization"}`}
	stubGH(t, responses)

	findings, changes := Audit(testRepo, nil)
	if finding, _ := findingByCheck(findings, checkActionsAccessLevel); finding.Status != StatusFail {
		t.Errorf("private repository with organization access: %v, want fail", finding.Status)
	}

	planned := false

	for _, change := range changes {
		if change.Check == checkActionsAccessLevel {
			planned = true
		}
	}

	if !planned {
		t.Error("expected a planned change down to access level none")
	}
}

func TestCodeScanningOptIn(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level ghBin.
	// Not opted in: ok, and the endpoint is never queried (the stub would
	// error, which would surface as unverifiable).
	stubGH(t, compliantResponses())

	findings, _ := Audit(testRepo, nil)
	if finding, _ := findingByCheck(findings, checkCodeScanning); finding.Status != StatusOK {
		t.Errorf("not opted in: %v, want ok", finding.Status)
	}

	// Opted in via the override file, not configured: fail with a fix.
	responses := compliantResponses()
	responses["GET repos/test/repo/code-scanning/default-setup"] = stubResponse{Body: `{"state":"not-configured"}`}
	logPath := stubGH(t, responses)

	optIn := map[string]string{checkCodeScanning: "this repo parses untrusted input"}

	findings, changes := Audit(testRepo, optIn)
	if finding, _ := findingByCheck(findings, checkCodeScanning); finding.Status != StatusFail {
		t.Errorf("opted in and not configured: %v, want fail (opt-in must not read as exemption)", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == checkCodeScanning {
			if err := planned.Apply(testRepo); err != nil {
				t.Fatalf("apply: %v", err)
			}
		}
	}

	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), "PATCH repos/test/repo/code-scanning/default-setup") {
		t.Error("expected default setup to be configured via PATCH")
	}

	// Opted in and configured: ok.
	responses["GET repos/test/repo/code-scanning/default-setup"] = stubResponse{Body: `{"state":"configured"}`}
	stubGH(t, responses)

	findings, _ = Audit(testRepo, optIn)
	if finding, _ := findingByCheck(findings, checkCodeScanning); finding.Status != StatusOK {
		t.Errorf("opted in and configured: %v, want ok", finding.Status)
	}
}

func TestOutsideCollaborators(t *testing.T) { //nolint:paralleltest // serial: mutates ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo/collaborators?affiliation=outside&per_page=100"] = stubResponse{
		Body: `[{"login":"drifter","role_name":"admin","permissions":{"push":true,"maintain":true,"admin":true}},` +
			`{"login":"reader","role_name":"read","permissions":{"push":false,"maintain":false,"admin":false}}]`,
	}
	stubGH(t, responses)

	findings, changes := Audit(testRepo, nil)

	finding, found := findingByCheck(findings, checkOutsideCollaborators)
	if !found || finding.Status != StatusAdvisory {
		t.Errorf("outside collaborator with admin: %v, want advisory", finding.Status)
	}

	if !strings.Contains(finding.Current, "drifter") || strings.Contains(finding.Current, "reader") {
		t.Errorf("advisory should name only the elevated collaborator, got %q", finding.Current)
	}

	for _, planned := range changes {
		if planned.Check == checkOutsideCollaborators {
			t.Error("people are never auto-fixed: no change may be planned")
		}
	}
}

func TestRulesetContextPreservation(t *testing.T) { //nolint:paralleltest // serial: mutates ghBin.
	// limen:main is missing required_linear_history but carries the project's
	// own status-check context: the reconcile payload must preserve it and
	// must not inject the canonical defaults.
	responses := compliantResponses()
	responses["GET repos/test/repo/rulesets/1"] = stubResponse{
		Body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},` +
			`{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"my-ci"}]}}]}`,
	}
	logPath := stubGH(t, responses)

	findings, changes := Audit(testRepo, nil)

	finding, _ := findingByCheck(findings, checkRulesetDefaultBranch)
	if finding.Status != StatusFail {
		t.Fatalf("ruleset missing a rule: %v, want fail", finding.Status)
	}

	for _, planned := range changes {
		if planned.Check == checkRulesetDefaultBranch {
			if err := planned.Apply(testRepo); err != nil {
				t.Fatalf("apply: %v", err)
			}
		}
	}

	log, _ := os.ReadFile(logPath)
	payload := string(log)

	if !strings.Contains(payload, `"context":"my-ci"`) {
		t.Error("reconcile must preserve the project's own status-check contexts")
	}

	if strings.Contains(payload, "verify (ubuntu-24.04)") {
		t.Error("reconcile must not replace project contexts with the canonical defaults")
	}
}

func TestRulesetEmptyContextsFail(t *testing.T) { //nolint:paralleltest // serial: mutates ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo/rulesets/1"] = stubResponse{
		Body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},` +
			`{"type":"required_linear_history"},{"type":"required_status_checks","parameters":{"required_status_checks":[]}}]}`,
	}
	stubGH(t, responses)

	findings, _ := Audit(testRepo, nil)

	finding, _ := findingByCheck(findings, checkRulesetDefaultBranch)
	if finding.Status != StatusFail {
		t.Errorf("required status checks with no contexts: %v, want fail", finding.Status)
	}
}

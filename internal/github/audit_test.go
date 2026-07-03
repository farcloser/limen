// White-box tests: the gh transport seam (ghBin) and the auditor's internals
// are package-private by design — testing through them is the point.

package github //nolint:testpackage // white-box (see above).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// generic error.
type stubResponse struct {
	body     string
	notFound bool
	fail     bool
}

// stubGH points ghBin at a generated script answering "METHOD path" keys from
// responses; anything unlisted errors generically (the unverifiable path).
// Every invocation is logged to the returned file, write payloads included.
func stubGH(t *testing.T, responses map[string]stubResponse) string {
	t.Helper()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")

	var script strings.Builder

	script.WriteString("#!/bin/sh\n")
	// args: api --method METHOD PATH [--input -]
	fmt.Fprintf(&script, "echo \"$3 $4\" >> %q\n", logPath)
	script.WriteString(
		"if [ \"${5:-}\" = \"--input\" ]; then cat >> " + fmt.Sprintf(
			"%q",
			logPath,
		) + "; echo >> " + fmt.Sprintf(
			"%q",
			logPath,
		) + "; fi\n",
	)
	script.WriteString("case \"$3 $4\" in\n")

	// Stable arm order keeps the script diffable when tests fail.
	keys := make([]string, 0, len(responses))
	for key := range responses {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for index, key := range keys {
		response := responses[key]
		fmt.Fprintf(&script, "%q)\n", key)

		switch {
		case response.notFound:
			script.WriteString("  echo 'gh: Not Found (HTTP 404)' >&2; exit 1;;\n")
		case response.fail:
			script.WriteString("  echo 'gh: boom (HTTP 500)' >&2; exit 1;;\n")
		default:
			// Bodies go through fixture files: quoting JSON into the script
			// would mangle newlines.
			fixture := filepath.Join(dir, fmt.Sprintf("fixture-%d.json", index))
			if err := os.WriteFile(fixture, []byte(response.body), 0o600); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}

			fmt.Fprintf(&script, "  cat %q; exit 0;;\n", fixture)
		}
	}

	// Writes not explicitly listed succeed silently; unknown reads error.
	script.WriteString(
		"*)\n  case \"$3\" in\n  GET) echo 'gh: boom (HTTP 500)' >&2; exit 1;;\n  *) exit 0;;\n  esac;;\n",
	)
	script.WriteString("esac\n")

	scriptPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o700); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}

	previous := ghBin
	ghBin = scriptPath

	t.Cleanup(func() { ghBin = previous })

	return logPath
}

// compliantResponses answers every audited endpoint with baseline-satisfying
// values.
func compliantResponses() map[string]stubResponse {
	return map[string]stubResponse{
		"GET repos/test/repo":                                 {body: compliantRepoJSON},
		"GET repos/test/repo/vulnerability-alerts":            {body: ""},
		"GET repos/test/repo/automated-security-fixes":        {body: `{"enabled": true}`},
		"GET repos/test/repo/private-vulnerability-reporting": {body: `{"enabled": true}`},
		"GET repos/test/repo/actions/permissions/workflow": {
			body: `{"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}`,
		},
		"GET repos/test/repo/actions/permissions": {body: `{"enabled":true,"allowed_actions":"selected"}`},
		"GET repos/test/repo/rulesets": {
			body: `[{"id":1,"name":"limen:main","target":"branch","enforcement":"active"},{"id":2,"name":"limen:tags","target":"tag","enforcement":"active"}]`,
		},
		"GET repos/test/repo/rulesets/1": {
			body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},{"type":"required_linear_history"},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":false,"required_status_checks":[{"context":"verify (ubuntu-24.04)"}]}}]}`,
		},
		"GET repos/test/repo/rulesets/2": {
			body: `{"rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"}]}`,
		},
		"GET repos/test/repo/actions/permissions/fork-pr-contributor-approval": {
			body: `{"approval_policy":"first_time_contributors"}`,
		},
		"GET repos/test/repo/collaborators?affiliation=outside&per_page=100": {body: `[]`},
		"GET repos/test/repo/hooks": {body: `[]`},
		"GET repos/test/repo/keys":  {body: `[]`},
		"GET repos/test/repo/pages": {notFound: true},
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
	responses["GET repos/test/repo"] = stubResponse{body: `{
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
	responses["GET repos/test/repo/vulnerability-alerts"] = stubResponse{notFound: true}
	responses["GET repos/test/repo/rulesets"] = stubResponse{body: `[]`}
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
	responses["GET repos/test/repo"] = stubResponse{body: strings.Replace(
		compliantRepoJSON, `"has_wiki": false`, `"has_wiki": true`, 1)}
	responses["GET repos/test/repo/vulnerability-alerts"] = stubResponse{notFound: true}
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
	responses["GET repos/test/repo"] = stubResponse{body: strings.Replace(
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
		body: `{"approval_policy":"first_time_contributors_new_to_github"}`,
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
		body: strings.Replace(compliantRepoJSON, `"private": false`, `"private": true`, 1),
	}
	responses["GET repos/test/repo/actions/permissions/access"] = stubResponse{body: `{"access_level":"organization"}`}
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
	responses["GET repos/test/repo/code-scanning/default-setup"] = stubResponse{body: `{"state":"not-configured"}`}
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
	responses["GET repos/test/repo/code-scanning/default-setup"] = stubResponse{body: `{"state":"configured"}`}
	stubGH(t, responses)

	findings, _ = Audit(testRepo, optIn)
	if finding, _ := findingByCheck(findings, checkCodeScanning); finding.Status != StatusOK {
		t.Errorf("opted in and configured: %v, want ok", finding.Status)
	}
}

func TestOutsideCollaborators(t *testing.T) { //nolint:paralleltest // serial: mutates ghBin.
	responses := compliantResponses()
	responses["GET repos/test/repo/collaborators?affiliation=outside&per_page=100"] = stubResponse{
		body: `[{"login":"drifter","role_name":"admin","permissions":{"push":true,"maintain":true,"admin":true}},` +
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
		body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},` +
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
		body: `{"rules":[{"type":"pull_request"},{"type":"deletion"},{"type":"non_fast_forward"},` +
			`{"type":"required_linear_history"},{"type":"required_status_checks","parameters":{"required_status_checks":[]}}]}`,
	}
	stubGH(t, responses)

	findings, _ := Audit(testRepo, nil)

	finding, _ := findingByCheck(findings, checkRulesetDefaultBranch)
	if finding.Status != StatusFail {
		t.Errorf("required status checks with no contexts: %v, want fail", finding.Status)
	}
}

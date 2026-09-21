package github_test

import (
	"os"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

func TestMarkIneffective(t *testing.T) {
	t.Parallel()

	findings := []github.Finding{
		{Check: "auto-merge", Status: github.StatusFail, Message: "auto-merge must be allowed"},
		{Check: "wiki", Status: github.StatusFail, Message: "wiki must be off"},
		{Check: "projects", Status: github.StatusOK, Message: "projects are off"},
	}

	// auto-merge and projects were applied; wiki was not planned at all.
	github.MarkIneffective(findings, []string{"auto-merge", "projects"})

	const prefix = "the fix applied without error, but GitHub left the setting unchanged: "

	if got := findings[0].Message; !strings.HasPrefix(got, prefix) || !strings.Contains(got, "limen.yaml") {
		t.Errorf("applied and still failing: %q, want the ineffective verdict with the ways out", got)
	}

	if findings[0].Status != github.StatusFail {
		t.Errorf("an ignored write is still a failure, got %v", findings[0].Status)
	}

	if got := findings[1].Message; got != "wiki must be off" {
		t.Errorf("a check that was not applied must keep its message, got %q", got)
	}

	if got := findings[2].Message; got != "projects are off" {
		t.Errorf("a passing check must keep its message, got %q", got)
	}
}

func TestAutoMergePrivateWarnsUpFront(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	responses := compliantResponses()
	responses["GET repos/test/repo"] = stubResponse{
		Body: strings.NewReplacer(
			`"private": false`, `"private": true`,
			`"allow_auto_merge": true`, `"allow_auto_merge": false`,
		).Replace(compliantRepoJSON),
	}
	stubGH(t, responses)

	findings, _ := github.Audit(t.Context(), testRepo, nil)

	finding, found := findingByCheck(findings, "auto-merge")
	if !found || finding.Status != github.StatusFail {
		t.Fatalf("auto-merge off: %v, want fail", finding.Status)
	}

	if !strings.Contains(finding.Message, "plan") {
		t.Errorf("a private repository must be told auto-merge is plan-gated, got %q", finding.Message)
	}

	// Public: the plain message, nothing about plans.
	responses["GET repos/test/repo"] = stubResponse{
		Body: strings.Replace(compliantRepoJSON, `"allow_auto_merge": true`, `"allow_auto_merge": false`, 1),
	}
	stubGH(t, responses)

	findings, _ = github.Audit(t.Context(), testRepo, nil)

	if finding, _ := findingByCheck(findings, "auto-merge"); strings.Contains(finding.Message, "plan") {
		t.Errorf("a public repository is not plan-gated, got %q", finding.Message)
	}
}

// A private repository whose owner is known to be on the Free plan: the
// write would be accepted and ignored, so the audit fails the check and
// plans nothing — the plan must not report "→ compliant" for a fix that
// cannot take. Any other plan, or one the token cannot see, is planned as
// before and judged by the re-audit.
func TestAutoMergeNotPlannedOnFreePrivate(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	private := strings.NewReplacer(
		`"private": false`, `"private": true`,
		`"allow_auto_merge": true`, `"allow_auto_merge": false`,
	).Replace(compliantRepoJSON)

	plans := map[string]struct {
		org     stubResponse
		planned bool
	}{
		"free":    {org: stubResponse{Body: `{"plan":{"name":"free"}}`}, planned: false},
		"team":    {org: stubResponse{Body: `{"plan":{"name":"team"}}`}, planned: true},
		"unknown": {org: stubResponse{Body: `{"login":"test"}`}, planned: true},
		"a user":  {org: stubResponse{NotFound: true}, planned: true},
	}

	for name, plan := range plans {
		responses := compliantResponses()
		responses["GET repos/test/repo"] = stubResponse{Body: private}
		responses["GET orgs/test"] = plan.org
		logPath := stubGH(t, responses)

		findings, changes := github.Audit(t.Context(), testRepo, nil)

		finding, _ := findingByCheck(findings, "auto-merge")
		if finding.Status != github.StatusFail {
			t.Fatalf("%s: auto-merge off: %v, want fail", name, finding.Status)
		}

		planned := false

		for _, change := range changes {
			if change.Check == "auto-merge" {
				planned = true
			}

			if err := change.Apply(t.Context()); err != nil {
				t.Fatalf("%s: apply %s: %v", name, change.Check, err)
			}
		}

		if planned != plan.planned {
			t.Errorf("%s: auto-merge planned %v, want %v (%s)", name, planned, plan.planned, finding.Message)
		}

		log, _ := os.ReadFile(logPath)
		if wrote := strings.Contains(string(log), "allow_auto_merge"); wrote != plan.planned {
			t.Errorf("%s: allow_auto_merge written %v, want %v", name, wrote, plan.planned)
		}

		if !plan.planned && !strings.Contains(finding.Message, "none is planned") {
			t.Errorf("%s: the finding must say no fix is planned, got %q", name, finding.Message)
		}
	}
}

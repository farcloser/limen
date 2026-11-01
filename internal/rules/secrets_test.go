package rules_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

const (
	pullRequestWithSecret = "on: [push, pull_request]\njobs:\n  plan:\n    runs-on: ubuntu-24.04\n" +
		"    env:\n      CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}\n    steps:\n      - run: tofu plan\n"
	signingOnTags = "on:\n  push:\n    tags: ['v*']\njobs:\n  sign:\n    runs-on: macos-15\n" +
		"    env:\n      P12: ${{ secrets.APPLE_DEVELOPER_ID_P12 }}\n    steps:\n      - run: just sign\n"
)

func secretsFinding(t *testing.T, files map[string]string) rules.Finding {
	t.Helper()

	return findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "secrets")
}

// TestSecretsCompliant: the canonical workflows read the token alone, and a
// job on a tag push with a plain secret and no environment is not open.
func TestSecretsCompliant(t *testing.T) {
	t.Parallel()

	if f := secretsFinding(t, compliantFiles()); !f.OK() {
		t.Errorf("the compliant repository fails the secrets rule: %s", f.Message)
	}

	tagged := compliantFiles()
	tagged[".github/workflows/publish.yaml"] = strings.ReplaceAll(signingOnTags, "APPLE_DEVELOPER_ID_P12", "NPM_TOKEN")

	if f := secretsFinding(t, tagged); !f.OK() {
		t.Errorf("a secret on a push confined to tags is a tag ruleset's to gate: %s", f.Message)
	}
}

// TestSecretOnPullRequestFails: a secret in a job a pull request starts,
// with no environment, is read from any branch a write account pushes.
func TestSecretOnPullRequestFails(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".github/workflows/infra.yaml"] = pullRequestWithSecret

	f := secretsFinding(t, files)
	if f.OK() || f.Path != ".github/workflows/infra.yaml" {
		t.Fatalf("secrets: %+v, want a failure on infra.yaml", f)
	}

	for _, want := range []string{"job plan", "CLOUDFLARE_API_TOKEN", "runs on push", "environment:"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q: %s", want, f.Message)
		}
	}

	// An environment on the job answers it; GitHub's side is the environments check's.
	gated := compliantFiles()
	gated[".github/workflows/infra.yaml"] = strings.Replace(
		pullRequestWithSecret,
		"    env:",
		"    environment: infra\n    env:",
		1,
	)

	if f := secretsFinding(t, gated); !f.OK() {
		t.Errorf("a job behind an environment fails: %s", f.Message)
	}
}

// TestInheritedSecretsOnDispatchFail: `secrets: inherit` hands every secret
// to the called workflow, and a dispatch runs from any branch.
func TestInheritedSecretsOnDispatchFail(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".github/workflows/call.yaml"] = "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 0 * * *'\njobs:\n" +
		"  call:\n    uses: org/repo/.github/workflows/x.yaml@0000000000000000000000000000000000000000\n    secrets: inherit\n"

	f := secretsFinding(t, files)
	if f.OK() || !strings.Contains(f.Message, "every secret") || !strings.Contains(f.Message, "workflow_dispatch") {
		t.Fatalf("secrets: %+v", f)
	}
}

// TestSigningSecretNeedsReleaseEnvironment: a signing credential is the
// release environment's wherever it is read, a tag push included; in that
// environment it passes.
func TestSigningSecretNeedsReleaseEnvironment(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".github/workflows/release.yaml"] = signingOnTags

	f := secretsFinding(t, files)
	if f.OK() || !strings.Contains(f.Message, "APPLE_DEVELOPER_ID_P12") || !strings.Contains(f.Message, "release") {
		t.Fatalf("secrets: %+v, want the release environment named", f)
	}

	for _, name := range []string{"APPLE_NOTARY_KEY", "APPLE_PROVISIONING_PROFILE"} {
		alone := compliantFiles()
		alone[".github/workflows/release.yaml"] = strings.ReplaceAll(signingOnTags, "APPLE_DEVELOPER_ID_P12", name)

		if f := secretsFinding(t, alone); f.OK() || !strings.Contains(f.Message, name) {
			t.Errorf("%s alone outside release: %+v, want the failure naming it", name, f)
		}
	}

	wrongEnvironment := compliantFiles()
	wrongEnvironment[".github/workflows/release.yaml"] = strings.Replace(
		signingOnTags,
		"    env:",
		"    environment: signing\n    env:",
		1,
	)

	if f := secretsFinding(t, wrongEnvironment); f.OK() {
		t.Error("a signing credential in another environment passed")
	}

	gated := compliantFiles()
	gated[".github/workflows/release.yaml"] = strings.Replace(
		signingOnTags,
		"    env:",
		"    environment: release\n    env:",
		1,
	)

	if f := secretsFinding(t, gated); !f.OK() {
		t.Errorf("the sign job in the release environment fails: %s", f.Message)
	}
}

// TestSecretsFixIsAdvisory: the environment a job needs is the project's to
// name, so fix reports the finding and writes nothing.
func TestSecretsFixIsAdvisory(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".github/workflows/infra.yaml"] = pullRequestWithSecret
	root := writeRepo(t, files)

	outcomes := rules.Fix(t.Context(), root, rules.FixOptions{Policy: rules.DefaultPolicy()})
	for _, o := range outcomesFor(outcomes, "secrets") {
		if o.Action != rules.ActionAdvisory || o.Path != ".github/workflows/infra.yaml" {
			t.Errorf("secrets fix: %+v, want an advisory on infra.yaml", o)
		}
	}

	if len(outcomesFor(outcomes, "secrets")) != 1 {
		t.Errorf("secrets outcomes: %+v, want one", outcomesFor(outcomes, "secrets"))
	}
}

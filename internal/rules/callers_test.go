package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

const (
	otherRevision  = "fedcba9876543210fedcba9876543210fedcba98"
	callerSecurity = ".github/workflows/security.yaml"
	verifyCopy     = ".github/workflows/limen-verify.yaml"
)

// consumer is the policy of a repository that is not limen.
func consumer() rules.Policy {
	policy := rules.DefaultPolicy()
	policy.Repository = testRepository

	return policy
}

// limenItself is the policy of limen's own repository.
func limenItself() rules.Policy {
	policy := rules.DefaultPolicy()
	policy.Repository = "farcloser/limen"

	return policy
}

// TestCallerReferenceIsRenovates: a caller whose uses: line Renovate moved to
// another commit and version is canonical; the line is not limen's to judge.
func TestCallerReferenceIsRenovates(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[callerSecurity] = strings.Replace(
		files[callerSecurity],
		testRevision+" # "+testVersion,
		otherRevision+" # v10.0.0",
		1,
	)
	root := writeRepo(t, files)

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); !f.OK() {
		t.Errorf("a moved reference must pass: %s", f.Message)
	}

	outcomes := rules.Fix(
		t.Context(),
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)
	for _, o := range outcomes {
		if o.Path == callerSecurity && o.Action != rules.ActionNone {
			t.Errorf("fix touched a canonical caller: %s (%s)", o.Action, o.Message)
		}
	}
}

// TestCallerLocalOutsideLimen: the local form calls a file only limen
// carries; a consumer with it fails, and fix moves the line to the release's
// commit with the rest of the file as it was. In limen the local form is the
// canonical one.
func TestCallerLocalOutsideLimen(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[callerSecurity] = rules.CanonicalCallersFor("farcloser/limen", "", "")[callerSecurity]
	root := writeRepo(t, files)

	if f := findingByRule(
		rules.Check(root, consumer()),
		"workflows",
	); f.OK() ||
		!strings.Contains(f.Message, "only farcloser/limen carries") {
		t.Errorf("a local call outside limen must fail naming it: %v %s", f.OK(), f.Message)
	}

	if f := findingByRule(rules.Check(root, limenItself()), "workflows"); !f.OK() {
		t.Errorf("the local call is limen's own: %s", f.Message)
	}

	rules.Fix(
		t.Context(),
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(callerSecurity)))
	if string(data) != compliantFiles()[callerSecurity] {
		t.Errorf("fix did not move the line to the release's commit:\n%s", data)
	}
}

// TestCallerBodyDriftKeepsReference: an edited caller is reset to the
// canonical text, its own reference kept, since that line is Renovate's; a
// development build, with no commit to write, seeds nothing where the file
// is missing.
func TestCallerBodyDriftKeepsReference(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	moved := strings.Replace(files[callerSecurity], testRevision+" # "+testVersion, otherRevision+" # v10.0.0", 1)
	files[callerSecurity] = moved + "  stray:\n    runs-on: ubuntu-24.04\n"
	root := writeRepo(t, files)

	if f := findingByRule(
		rules.Check(root, consumer()),
		"workflows",
	); f.OK() ||
		!strings.Contains(f.Message, "canonical caller") {
		t.Errorf("an edited caller must fail: %v %s", f.OK(), f.Message)
	}

	rules.Fix(
		t.Context(),
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(callerSecurity)))
	if string(data) != moved {
		t.Errorf("fix must reset the body and keep the reference:\n%s", data)
	}

	missing := compliantFiles()
	delete(missing, callerSecurity)
	root = writeRepo(t, missing)

	for _, o := range rules.Fix(t.Context(), root, rules.FixOptions{Policy: consumer()}) {
		if o.Path == callerSecurity &&
			(o.Action != rules.ActionAdvisory || !strings.Contains(o.Message, "development build")) {
			t.Errorf("a dev build must not make up a commit: %s (%s)", o.Action, o.Message)
		}
	}

	rules.Fix(
		t.Context(),
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)

	data, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(callerSecurity)))
	if string(data) != compliantFiles()[callerSecurity] {
		t.Errorf("a release build seeds the caller at its commit:\n%s", data)
	}
}

// TestCIVerifyCall: a consumer's ci.yaml calling the local limen-verify.yaml
// fails, and fix rewrites that one line, the rest of the project's file
// untouched; a copy of limen-verify.yaml itself is a stray fix removes.
func TestCIVerifyCall(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	own := strings.Replace(rules.CanonicalCIFor("farcloser/limen", "", ""), "name: ci\n", "name: ci # mine\n", 1)
	files[".github/workflows/ci.yaml"] = own
	files[verifyCopy] = "name: limen-verify\non:\n  workflow_call:\n"
	root := writeRepo(t, files)

	f := findingByRule(rules.Check(root, consumer()), "workflows")
	if f.OK() || !strings.Contains(f.Message, "limen-verify.yaml") {
		t.Errorf("a stray copy or a local call must fail naming limen-verify.yaml: %v %s", f.OK(), f.Message)
	}

	outcomes := rules.Fix(
		t.Context(),
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)

	deleted := false

	for _, o := range outcomes {
		if o.Path == verifyCopy && o.Action == rules.ActionDeleted {
			deleted = true
		}
	}

	if !deleted {
		t.Error("fix did not remove the stray limen-verify.yaml")
	}

	want := strings.Replace(
		rules.CanonicalCIFor(testRepository, testRevision, testVersion),
		"name: ci\n",
		"name: ci # mine\n",
		1,
	)

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(".github/workflows/ci.yaml")))
	if string(data) != want {
		t.Errorf("fix must rewrite the call and nothing else:\n%s", data)
	}

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); !f.OK() {
		t.Errorf("after fix: %s", f.Message)
	}
}

// TestCallerReferenceMustBeACommit: a reference to a branch or a tag is not
// a pin; it fails, and fix rewrites the line to the release's commit.
func TestCallerReferenceMustBeACommit(t *testing.T) {
	t.Parallel()

	for name, ref := range map[string]string{"branch": "main # v10.0.0", "tag": "v0.17.0"} {
		files := compliantFiles()
		files[callerSecurity] = strings.Replace(files[callerSecurity], testRevision+" # "+testVersion, ref, 1)
		root := writeRepo(t, files)

		if f := findingByRule(
			rules.Check(root, consumer()),
			"workflows",
		); f.OK() ||
			!strings.Contains(f.Message, "does not call") {
			t.Errorf("%s: a reference that is not a commit must fail: %v %s", name, f.OK(), f.Message)
		}

		rules.Fix(
			t.Context(),
			root,
			rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
		)

		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(callerSecurity)))
		if string(data) != compliantFiles()[callerSecurity] {
			t.Errorf("%s: fix must pin the line to the release's commit:\n%s", name, data)
		}
	}
}

// TestCallerCommentsAside: a caller is held to what it does, not what it
// says: an edited or added comment is canonical, so a comment changed in
// limen never asks a repository for a workflow write.
func TestCallerCommentsAside(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[callerSecurity] = "# the project's note\n" + strings.Replace(
		files[callerSecurity],
		"name: security\n",
		"name: security # ours\n",
		1,
	)
	root := writeRepo(t, files)

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); !f.OK() {
		t.Errorf("comments must not count: %s", f.Message)
	}
}

// TestCallerOnRenovateBranch: on a Renovate branch the fix-up may not write a
// workflow file, so a drifted caller is an advisory, left as it is.
func TestCallerOnRenovateBranch(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	drifted := files[callerSecurity] + "  stray:\n    runs-on: ubuntu-24.04\n"
	files[callerSecurity] = drifted
	root := writeRepo(t, files)

	advised := false

	for _, o := range rules.Fix(t.Context(), root, rules.FixOptions{
		Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion, RenovateBranch: true,
	}) {
		if o.Path == callerSecurity && o.Action == rules.ActionAdvisory && strings.Contains(o.Message, "hand push") {
			advised = true
		}
	}

	if !advised {
		t.Error("a Renovate branch must leave the caller with an advisory")
	}

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(callerSecurity)))
	if string(data) != drifted {
		t.Errorf("a Renovate branch's fix must not write the caller:\n%s", data)
	}
}

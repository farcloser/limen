package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

const releaseWorkflow = ".github/workflows/release.yaml"

// appleFiles is a compliant repository whose release.yaml calls the Apple
// release lane at the test release's commit.
func appleFiles() map[string]string {
	files := compliantFiles()
	files[releaseWorkflow] = rules.CanonicalAppleCallerFor(testRepository, testRevision, testVersion)

	return files
}

func releaseOutcomes(t *testing.T, root string, opts rules.FixOptions) []rules.Outcome {
	t.Helper()

	var found []rules.Outcome

	for _, o := range rules.Fix(t.Context(), root, opts) {
		if o.Path == releaseWorkflow {
			found = append(found, o)
		}
	}

	return found
}

// TestAppleCallerCanonicalPasses: a release.yaml calling the lane is held as
// a thin caller; canonical, it passes and fix reports it once, untouched.
func TestAppleCallerCanonicalPasses(t *testing.T) {
	t.Parallel()

	root := writeRepo(t, appleFiles())

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); !f.OK() {
		t.Errorf("the canonical Apple caller must pass: %s", f.Message)
	}

	outcomes := releaseOutcomes(
		t,
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionNone {
		t.Errorf("release.yaml outcomes: %+v, want one, untouched", outcomes)
	}
}

// TestAppleCallerDriftResetAroundReference: a body edited beside a reference
// Renovate moved fails, and fix resets the body keeping the reference.
func TestAppleCallerDriftResetAroundReference(t *testing.T) {
	t.Parallel()

	files := appleFiles()
	moved := strings.Replace(files[releaseWorkflow], testRevision+" # "+testVersion, otherRevision+" # v10.0.0", 1)
	files[releaseWorkflow] = moved + "  stray:\n    runs-on: ubuntu-24.04\n"
	root := writeRepo(t, files)

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); f.OK() ||
		!strings.Contains(f.Message, "canonical caller") {
		t.Errorf("an edited Apple caller must fail: %v %s", f.OK(), f.Message)
	}

	outcomes := releaseOutcomes(
		t,
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionOverwrote {
		t.Errorf("release.yaml outcomes: %+v, want one reset", outcomes)
	}

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(releaseWorkflow)))
	if string(data) != moved {
		t.Errorf("fix must reset the body and keep the reference:\n%s", data)
	}
}

// TestAppleCallerLocalOutsideLimen: a project that points its release.yaml at
// the lane by its local path has declared the lane; fix sets the release's
// commit, since the lane lives in limen alone.
func TestAppleCallerLocalOutsideLimen(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[releaseWorkflow] = "name: release\njobs:\n  release:\n    uses: ./.github/workflows/limen-release-apple.yaml\n"
	root := writeRepo(t, files)

	if f := findingByRule(
		rules.Check(root, consumer()),
		"workflows",
	); f.OK() ||
		!strings.Contains(f.Message, "farcloser/limen") {
		t.Errorf("a local reference outside limen must fail naming the commit form: %v %s", f.OK(), f.Message)
	}

	outcomes := releaseOutcomes(
		t,
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionOverwrote {
		t.Errorf("release.yaml outcomes: %+v, want one reset", outcomes)
	}

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(releaseWorkflow)))
	if string(data) != rules.CanonicalAppleCallerFor(testRepository, testRevision, testVersion) {
		t.Errorf("fix must write the canonical caller at the release's commit:\n%s", data)
	}

	// A development build has no commit to write.
	root = writeRepo(t, files)

	outcomes = releaseOutcomes(t, root, rules.FixOptions{Policy: consumer()})
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionAdvisory {
		t.Errorf("a development build must leave an advisory: %+v", outcomes)
	}
}

// TestAppleCallerOnRenovateBranch: a drifted Apple caller on a Renovate
// branch is left for a hand push, like the other callers.
func TestAppleCallerOnRenovateBranch(t *testing.T) {
	t.Parallel()

	files := appleFiles()
	files[releaseWorkflow] += "  stray:\n    runs-on: ubuntu-24.04\n"
	root := writeRepo(t, files)

	outcomes := releaseOutcomes(
		t,
		root,
		rules.FixOptions{
			Policy:         consumer(),
			RenovateBranch: true,
			SelfRevision:   testRevision,
			SelfVersion:    testVersion,
		},
	)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionAdvisory {
		t.Errorf("release.yaml outcomes: %+v, want one advisory", outcomes)
	}

	data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(releaseWorkflow)))
	if string(data) != files[releaseWorkflow] {
		t.Error("a Renovate branch's fix-up must not write a workflow file")
	}
}

// TestPlainReleaseStaysTheProjects: a release.yaml that does not call the
// lane is the seeded-once workflow, the project's own once seeded.
func TestPlainReleaseStaysTheProjects(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[releaseWorkflow] = strings.Replace(limen.CanonicalWorkflowRelease, "name: release", "name: mine", 1)
	root := writeRepo(t, files)

	if f := findingByRule(rules.Check(root, consumer()), "workflows"); !f.OK() {
		t.Errorf("an edited seeded-once release workflow is the project's: %s", f.Message)
	}

	outcomes := releaseOutcomes(
		t,
		root,
		rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
	)
	for _, o := range outcomes {
		if o.Action != rules.ActionNone {
			t.Errorf("fix touched the project's release workflow: %+v", o)
		}
	}
}

// TestAppleCallerReferenceMustBeACommit: a release.yaml naming the lane at a
// branch or a tag has declared the lane, so the caller rule fails it and fix
// pins the line to the release's commit, as for the three callers.
func TestAppleCallerReferenceMustBeACommit(t *testing.T) {
	t.Parallel()

	for name, ref := range map[string]string{"branch": "main # v10.0.0", "tag": "v0.17.0"} {
		files := appleFiles()
		files[releaseWorkflow] = strings.Replace(files[releaseWorkflow], testRevision+" # "+testVersion, ref, 1)
		root := writeRepo(t, files)

		if f := findingByRule(rules.Check(root, consumer()), "workflows"); f.OK() ||
			!strings.Contains(f.Message, "does not call") {
			t.Errorf("%s: a reference that is not a commit must fail: %v %s", name, f.OK(), f.Message)
		}

		rules.Fix(
			t.Context(),
			root,
			rules.FixOptions{Policy: consumer(), SelfRevision: testRevision, SelfVersion: testVersion},
		)

		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(releaseWorkflow)))
		if string(data) != appleFiles()[releaseWorkflow] {
			t.Errorf("%s: fix must pin the line to the release's commit:\n%s", name, data)
		}
	}
}

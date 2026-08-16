package rules //nolint:testpackage // white-box: exercises the unexported editing helper directly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

const testIdentity = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"

// TestEnsureIgnoredAuthor covers the editing helper on the shapes it meets:
// the seed's one-line array, an already multi-line array, an empty array,
// and a file with no such key.
func TestEnsureIgnoredAuthor(t *testing.T) {
	t.Parallel()

	// The canonical seed: one-line array. Grows into the multi-line shape
	// with the new address FIRST and the seed's entry preserved.
	updated, ok := ensureIgnoredAuthor(limen.CanonicalRenovate, testIdentity)
	if !ok {
		t.Fatal("the canonical seed's gitIgnoredAuthors was not found")
	}

	want := "  gitIgnoredAuthors: [\n" +
		"    \"" + testIdentity + "\",\n" +
		"    \"41898282+github-actions[bot]@users.noreply.github.com\",\n" +
		"  ],\n"
	if !strings.Contains(updated, want) {
		t.Errorf("seed after insertion lacks the canonical multi-line block:\n%s", updated)
	}

	// Everything outside the array is byte-identical: the file minus the
	// array must be unchanged.
	head, _, found := strings.Cut(limen.CanonicalRenovate, "gitIgnoredAuthors")
	if !found || !strings.HasPrefix(updated, head) {
		t.Error("content before the array changed")
	}

	if !strings.HasSuffix(updated, "],\n}\n") {
		t.Errorf("content after the array changed:\n%s", updated)
	}

	// Idempotent through the rule's presence test, and a second insertion of
	// a DIFFERENT address goes first while keeping order.
	again, ok := ensureIgnoredAuthor(updated, "1+other[bot]@users.noreply.github.com")
	if !ok || !strings.Contains(again,
		"    \"1+other[bot]@users.noreply.github.com\",\n    \""+testIdentity+"\",\n") {
		t.Errorf("second insertion misordered:\n%s", again)
	}

	// Empty array.
	empty, ok := ensureIgnoredAuthor("{\n  gitIgnoredAuthors: [],\n}\n", testIdentity)
	if !ok || empty != "{\n  gitIgnoredAuthors: [\n    \""+testIdentity+"\",\n  ],\n}\n" {
		t.Errorf("empty array:\n%s", empty)
	}

	// Quoted key (plain JSON) works too.
	quoted, ok := ensureIgnoredAuthor("{\n  \"gitIgnoredAuthors\": [\"a@b\"]\n}\n", testIdentity)
	if !ok || !strings.Contains(quoted, "\""+testIdentity+"\",\n    \"a@b\",\n  ]\n}") {
		t.Errorf("quoted key:\n%s", quoted)
	}

	// No key: not editable.
	if _, ok := ensureIgnoredAuthor("{\n  extends: [\"config:recommended\"],\n}\n", testIdentity); ok {
		t.Error("a file without gitIgnoredAuthors must not be edited")
	}
}

// TestRenovateRule: check and fix agree, the identity is enforced only when
// known, and fix's edit is exactly what check wants.
func TestRenovateRule(t *testing.T) {
	t.Parallel()

	known := DefaultPolicy()
	known.UpdateAppIdentity = testIdentity

	// Unknown identity: pass / none, file untouched.
	root := writeRepo(t, compliantFiles())
	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); !f.OK() {
		t.Errorf("unknown identity must not fail: %s", f.Message)
	}

	if o := outcomeByRule(Fix(root, FixOptions{Policy: DefaultPolicy()}), ruleRenovate); o.Action != ActionNone {
		t.Errorf("unknown identity: %s, want none", o.Action)
	}

	// Known and missing: fail, then fix merges it and check passes.
	if f := findingByRule(Check(root, known), ruleRenovate); f.OK() {
		t.Error("a missing update-App identity must fail when known")
	}

	if o := outcomeByRule(Fix(root, FixOptions{Policy: known}), ruleRenovate); o.Action != ActionMerged {
		t.Errorf("fix: %s (%s), want merged", o.Action, o.Message)
	}

	data, err := os.ReadFile(filepath.Join(root, pathRenovate))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "\""+testIdentity+"\",\n") {
		t.Errorf("fix did not write the identity:\n%s", data)
	}

	if f := findingByRule(Check(root, known), ruleRenovate); !f.OK() {
		t.Errorf("after fix: %s", f.Message)
	}

	// Idempotent.
	if o := outcomeByRule(Fix(root, FixOptions{Policy: known}), ruleRenovate); o.Action != ActionNone {
		t.Errorf("second fix: %s, want none", o.Action)
	}

	// A project that rewrote renovate.json5 without the key: advisory, and the
	// message names the address to add.
	custom := compliantFiles()
	custom[pathRenovate] = "{ extends: [\"config:recommended\"] }\n"
	root = writeRepo(t, custom)

	if o := outcomeByRule(Fix(root, FixOptions{Policy: known}), ruleRenovate); o.Action != ActionAdvisory ||
		!strings.Contains(o.Message, testIdentity) {
		t.Errorf("no key: %s (%s), want advisory naming the address", o.Action, o.Message)
	}

	// No renovate.json5 at all: the workflows rule owns that verdict; this
	// rule stays quiet on both sides.
	missing := compliantFiles()
	delete(missing, pathRenovate)
	root = writeRepo(t, missing)

	if f := findingByRule(Check(root, known), ruleRenovate); !f.OK() {
		t.Errorf("missing file must not double-report: %s", f.Message)
	}
}

func outcomeByRule(outcomes []Outcome, rule string) Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return Outcome{Rule: rule, Action: ActionFailed, Message: "rule not remediated"}
}

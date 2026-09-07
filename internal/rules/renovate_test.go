package rules //nolint:testpackage // white-box: exercises the unexported editing helper directly.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

const testIdentity = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"

// TestEnsureIgnoredAuthor covers the editing helper on the shapes it meets:
// the canonical seed (whatever its array currently holds — the seed is
// limen's own renovate.json5, and limen fix keeps that array current, so the
// test derives its expectation from the seed rather than hard-coding it), a
// one-line array, an already multi-line array, an empty array, and a file
// with no such key.
func TestEnsureIgnoredAuthor(t *testing.T) {
	t.Parallel()

	// The canonical seed: the new address goes FIRST, every existing entry is
	// preserved in order, and the block takes the canonical multi-line shape.
	updated, ok := ensureIgnoredAuthor(limen.CanonicalRenovate, testIdentity)
	if !ok {
		t.Fatal("the canonical seed's gitIgnoredAuthors was not found")
	}

	existing := ignoredAuthorsOf(t, limen.CanonicalRenovate)
	if !slices.Contains(existing, "41898282+github-actions[bot]@users.noreply.github.com") {
		t.Fatalf("the seed must carry at least the default-token identity, got %v", existing)
	}

	want := "  gitIgnoredAuthors: [\n    \"" + testIdentity + "\",\n"
	for _, entry := range existing {
		want += "    \"" + entry + "\",\n"
	}

	want += "  ],\n"
	if !strings.Contains(updated, want) {
		t.Errorf("seed after insertion lacks the expected block:\n%s\n--- got:\n%s", want, updated)
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

	// The seed's one-line shape (what a fresh seed looked like before any fix
	// touched it) grows into the multi-line block.
	oneLine := "{\n  gitIgnoredAuthors: [\"41898282+github-actions[bot]@users.noreply.github.com\"],\n}\n"

	grown, ok := ensureIgnoredAuthor(oneLine, testIdentity)
	if !ok || grown != "{\n  gitIgnoredAuthors: [\n    \""+testIdentity+"\",\n"+
		"    \"41898282+github-actions[bot]@users.noreply.github.com\",\n  ],\n}\n" {
		t.Errorf("one-line array:\n%s", grown)
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

// TestEnsurePresetRef covers the editing helper on every shape it meets: a
// tagged reference to move, the seed's local reference to pin, an extends
// array without the preset (one-line, multi-line, empty), a file with no
// extends at all, and one with nothing to anchor on.
func TestEnsurePresetRef(t *testing.T) {
	t.Parallel()

	const ref = "github>farcloser/limen#v9.9.9"

	cases := []struct {
		name, in, want string
	}{
		{
			"tagged, moved",
			"{\n  extends: [\"github>farcloser/limen#v0.0.1\"],\n}\n",
			"{\n  extends: [\"" + ref + "\"],\n}\n",
		},
		{"local, pinned", "{\n  extends: [\"local>farcloser/limen\"],\n}\n", "{\n  extends: [\"" + ref + "\"],\n}\n"},
		{
			"one-line, absent",
			"{\n  extends: [\"config:recommended\"],\n}\n",
			"{\n  extends: [\"" + ref + "\", \"config:recommended\"],\n}\n",
		},
		{
			"multi-line, absent",
			"{\n  extends: [\n    \"config:recommended\",\n  ],\n}\n",
			"{\n  extends: [\n    \"" + ref + "\",\n    \"config:recommended\",\n  ],\n}\n",
		},
		{"empty", "{\n  extends: [],\n}\n", "{\n  extends: [\"" + ref + "\"],\n}\n"},
		{
			"no extends",
			"{\n  minimumReleaseAge: \"3 days\",\n}\n",
			"{\n  extends: [\"" + ref + "\"],\n  minimumReleaseAge: \"3 days\",\n}\n",
		},
	}

	for _, c := range cases {
		got, ok := ensurePresetRef(c.in, ref)
		if !ok || got != c.want {
			t.Errorf("%s: ok=%v\n--- got:\n%s--- want:\n%s", c.name, ok, got, c.want)
		}

		if !hasPresetRef(got, ref) {
			t.Errorf("%s: the result does not satisfy the check", c.name)
		}
	}

	if _, ok := ensurePresetRef("not json at all\n", ref); ok {
		t.Error("a file without an opening brace must not be edited")
	}

	// Two references, one stale: not satisfied; the edit moves both.
	two := "{\n  extends: [\"" + ref + "\", \"local>farcloser/limen\"],\n}\n"
	if hasPresetRef(two, ref) {
		t.Error("a stale second reference must not pass")
	}

	if got, _ := ensurePresetRef(two, ref); !hasPresetRef(got, ref) {
		t.Errorf("both references must be moved:\n%s", got)
	}
}

// TestCanonicalPresetRef: limen extends its own default branch; every other
// repository extends the preset at its pinned limen version; a repository
// without a limen pin has nothing to pin to.
func TestCanonicalPresetRef(t *testing.T) {
	t.Parallel()

	self := writeRepo(t, map[string]string{"go.mod": "module github.com/farcloser/limen\n\ngo 1.26\n"})
	if got := canonicalPresetRef(self); got != presetLocalRef {
		t.Errorf("limen itself: %q, want %q", got, presetLocalRef)
	}

	pinned := writeRepo(t, map[string]string{
		"go.mod":    "module example.com/thing\n\ngo 1.26\n",
		"aqua.yaml": "packages:\n  - name: farcloser/limen@v1.2.3 # renovate: depName=farcloser/limen\n    registry: local\n",
	})
	if got := canonicalPresetRef(pinned); got != "github>farcloser/limen#v1.2.3" {
		t.Errorf("pinned repository: %q", got)
	}

	if got := canonicalPresetRef(writeRepo(t, map[string]string{"aqua.yaml": "packages: []\n"})); got != "" {
		t.Errorf("no limen pin: %q, want nothing enforced", got)
	}

	if want := "github>farcloser/limen#" + canonicalLimenVersion(t); presetRefFor(limen.CanonicalAquaYAML) != want {
		t.Errorf("canonical manifest: %q, want %q", presetRefFor(limen.CanonicalAquaYAML), want)
	}
}

// canonicalLimenVersion reads the farcloser/limen pin off the canonical
// manifest, the way the fixtures derive every version they need.
func canonicalLimenVersion(t *testing.T) string {
	t.Helper()

	m := regexp.MustCompile(`(?m)^  - name: farcloser/limen@(\S+)`).FindStringSubmatch(limen.CanonicalAquaYAML)
	if m == nil {
		t.Fatal("the canonical aqua.yaml carries no farcloser/limen pin")
	}

	return m[1]
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

	// The seed as seeded — extending limen's own default branch — is not what
	// a repository must carry: check names the pinned reference, fix sets it
	// (identity known or not), and the file is otherwise untouched.
	seeded := compliantFiles()
	seeded[pathRenovate] = limen.CanonicalRenovate
	root = writeRepo(t, seeded)

	want := "github>farcloser/limen#" + canonicalLimenVersion(t)
	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); f.OK() || !strings.Contains(f.Message, want) {
		t.Errorf("the raw seed must fail naming %s, got: %+v", want, f)
	}

	if o := outcomeByRule(Fix(root, FixOptions{Policy: DefaultPolicy()}), ruleRenovate); o.Action != ActionMerged {
		t.Errorf("fix on the raw seed: %s (%s), want merged", o.Action, o.Message)
	}

	data, err = os.ReadFile(filepath.Join(root, pathRenovate))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != CanonicalRenovateFor(limen.CanonicalAquaYAML) {
		t.Errorf("fix must pin the reference and change nothing else:\n%s", data)
	}

	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); !f.OK() {
		t.Errorf("after pinning: %s", f.Message)
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

// ignoredAuthorsOf extracts the quoted entries of the file's gitIgnoredAuthors
// array, in order.
func ignoredAuthorsOf(t *testing.T, content string) []string {
	t.Helper()

	loc := ignoredAuthorsKeyPattern.FindStringIndex(content)
	if loc == nil {
		t.Fatal("no gitIgnoredAuthors array")
	}

	open := loc[1] - 1

	closeIdx := closingBracket(content, open)
	if closeIdx < 0 {
		t.Fatal("unterminated gitIgnoredAuthors array")
	}

	var entries []string

	for _, quoted := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(content[open+1:closeIdx], -1) {
		entries = append(entries, quoted[1])
	}

	return entries
}

func outcomeByRule(outcomes []Outcome, rule string) Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return Outcome{Rule: rule, Action: ActionFailed, Message: "rule not remediated"}
}

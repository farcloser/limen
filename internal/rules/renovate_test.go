package rules //nolint:testpackage // white-box: exercises the unexported config helpers directly.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

const testIdentity = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"

// TestConfigRoundTrip: the two things rendering must not do to a config it
// only meant to edit one key of — escape the `>` in a preset reference, and
// turn an integer into a float.
func TestConfigRoundTrip(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]byte(`{"extends":["github>farcloser/limen#v1.2.3"],"prConcurrentLimit":10}`))
	if err != nil {
		t.Fatal(err)
	}

	out, err := render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "github>farcloser/limen#v1.2.3") {
		t.Errorf("the preset reference was escaped:\n%s", out)
	}

	if !strings.Contains(out, "10") || strings.Contains(out, "1e+01") {
		t.Errorf("an integer did not survive:\n%s", out)
	}

	// Rendering is deterministic: a fix that changes nothing writes nothing.
	again, err := render(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if again != out {
		t.Error("render is not deterministic")
	}
}

// TestSetPresetRef covers the shapes extends arrives in: absent, carrying an
// outdated reference, carrying only unrelated presets, and already correct.
func TestSetPresetRef(t *testing.T) {
	t.Parallel()

	const want = "github>farcloser/limen#v9.9.9"

	for name, input := range map[string]string{
		"absent":           `{}`,
		"no extends array": `{"extends":"github>farcloser/limen#v1.0.0"}`,
		"outdated ref":     `{"extends":["github>farcloser/limen#v1.0.0","config:recommended"]}`,
		"local ref":        `{"extends":["local>farcloser/limen"]}`,
		"only unrelated":   `{"extends":["config:recommended"]}`,
		"already correct":  `{"extends":["` + want + `","config:recommended"]}`,
		"duplicated stale": `{"extends":["github>farcloser/limen#v1.0.0","github>farcloser/limen#v2.0.0"]}`,
	} {
		cfg, err := parseConfig([]byte(input))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		cfg.setPresetRef(want)

		if !cfg.hasPresetRef(want) {
			t.Errorf("%s: not set", name)
		}

		refs, _ := cfg.strings(extendsKey)
		if got := slices.Contains(refs, want); !got {
			t.Errorf("%s: extends = %v", name, refs)
		}

		// Unrelated presets survive, and the stale reference does not.
		for _, ref := range refs {
			if ref != want && presetRefPattern.MatchString(ref) {
				t.Errorf("%s: stale reference kept: %q", name, ref)
			}
		}
	}

	// An unrelated preset is preserved alongside.
	cfg, _ := parseConfig([]byte(`{"extends":["config:recommended"]}`))
	cfg.setPresetRef(want)

	if refs, _ := cfg.strings(extendsKey); !slices.Contains(refs, "config:recommended") {
		t.Errorf("an unrelated preset was dropped: %v", refs)
	}
}

// TestAddIgnoredAuthor: the new address goes first, existing entries keep
// their order, and adding twice does not duplicate.
func TestAddIgnoredAuthor(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]byte(`{"gitIgnoredAuthors":["a@example.com","b@example.com"]}`))
	if err != nil {
		t.Fatal(err)
	}

	cfg.addIgnoredAuthor(testIdentity)

	got, _ := cfg.strings(ignoredAuthorsKey)
	if want := []string{testIdentity, "a@example.com", "b@example.com"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	cfg.addIgnoredAuthor(testIdentity)

	if got, _ := cfg.strings(ignoredAuthorsKey); len(got) != 3 {
		t.Errorf("adding twice duplicated: %v", got)
	}

	// Absent key: the array is created.
	empty, _ := parseConfig([]byte(`{}`))
	empty.addIgnoredAuthor(testIdentity)

	if got, _ := empty.strings(ignoredAuthorsKey); !slices.Equal(got, []string{testIdentity}) {
		t.Errorf("absent key: %v", got)
	}
}

// TestCanonicalSeed: the seed limen ships must itself satisfy the rule's
// non-negotiable key — a seed that skipped forkProcessing would silently
// disable Renovate on every fork it lands in.
func TestCanonicalSeed(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]byte(limen.CanonicalRenovate))
	if err != nil {
		t.Fatalf("the canonical seed is not valid JSON: %v", err)
	}

	if !cfg.hasForkProcessing() {
		t.Errorf("the seed must set %s to %q", forkProcessingKey, forkProcessingValue)
	}

	if !cfg.hasPresetRef(presetLocalRef) {
		t.Errorf("the seed must extend %q", presetLocalRef)
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

	if !slices.Contains(ignoredAuthorsOf(t, root), testIdentity) {
		t.Error("fix did not write the identity")
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

	data, err := os.ReadFile(filepath.Join(root, pathRenovate))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != CanonicalRenovateFor(limen.CanonicalAquaYAML) {
		t.Errorf("fix must pin the reference and change nothing else:\n%s", data)
	}

	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); !f.OK() {
		t.Errorf("after pinning: %s", f.Message)
	}

	// forkProcessing is not inheritable: a config that drops it fails, and
	// fix puts it back. This is the whole reason the file is renovate.json.
	noForks := compliantFiles()
	noForks[pathRenovate] = `{"extends":["` + want + `"]}` + "\n"
	root = writeRepo(t, noForks)

	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); f.OK() ||
		!strings.Contains(f.Message, forkProcessingKey) {
		t.Errorf("a config without %s must fail naming it, got: %+v", forkProcessingKey, f)
	}

	if o := outcomeByRule(Fix(root, FixOptions{Policy: DefaultPolicy()}), ruleRenovate); o.Action != ActionMerged {
		t.Errorf("fix must set %s: %s (%s)", forkProcessingKey, o.Action, o.Message)
	}

	if f := findingByRule(Check(root, DefaultPolicy()), ruleRenovate); !f.OK() {
		t.Errorf("after setting %s: %s", forkProcessingKey, f.Message)
	}

	// A project that rewrote the file without the identity's key: fix creates
	// the array rather than giving up, which the regex editor could not do.
	custom := compliantFiles()
	custom[pathRenovate] = `{"extends":["config:recommended"]}` + "\n"
	root = writeRepo(t, custom)

	if o := outcomeByRule(Fix(root, FixOptions{Policy: known}), ruleRenovate); o.Action != ActionMerged {
		t.Errorf("no key: %s (%s), want merged", o.Action, o.Message)
	}

	if !slices.Contains(ignoredAuthorsOf(t, root), testIdentity) {
		t.Error("fix did not create gitIgnoredAuthors")
	}

	// Invalid JSON is the one thing fix will not guess at.
	broken := compliantFiles()
	broken[pathRenovate] = "{ extends: [\"config:recommended\"] }\n" // JSON5, not JSON
	root = writeRepo(t, broken)

	if f := findingByRule(Check(root, known), ruleRenovate); f.OK() {
		t.Error("invalid JSON must fail check")
	}

	if o := outcomeByRule(Fix(root, FixOptions{Policy: known}), ruleRenovate); o.Action != ActionAdvisory {
		t.Errorf("invalid JSON: %s (%s), want advisory", o.Action, o.Message)
	}

	// No renovate.json at all: the workflows rule owns that verdict; this
	// rule stays quiet on both sides.
	missing := compliantFiles()
	delete(missing, pathRenovate)
	root = writeRepo(t, missing)

	if f := findingByRule(Check(root, known), ruleRenovate); !f.OK() {
		t.Errorf("missing file must not double-report: %s", f.Message)
	}
}

// ignoredAuthorsOf reads the repository's gitIgnoredAuthors array, in order.
func ignoredAuthorsOf(t *testing.T, root string) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, pathRenovate))
	if err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		GitIgnoredAuthors []string `json:"gitIgnoredAuthors"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("renovate.json is not valid JSON: %v", err)
	}

	return cfg.GitIgnoredAuthors
}

func outcomeByRule(outcomes []Outcome, rule string) Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return Outcome{Rule: rule, Action: ActionFailed, Message: "rule not remediated"}
}

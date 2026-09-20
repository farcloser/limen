package rules_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

const testIdentity = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"

// presetRefRE matches a reference to limen's preset, at any ref.
var presetRefRE = regexp.MustCompile(`^(?:github|local)>farcloser/limen(?:#.*)?$`)

// pinnedPresetRef is the reference every repository must carry: the preset at
// the canonical manifest's limen version.
func pinnedPresetRef(t *testing.T) string {
	t.Helper()

	return "github>farcloser/limen#" + canonicalLimenVersion(t)
}

// renovateConfig reads the repository's renovate.json back as generic JSON.
func renovateConfig(t *testing.T, root string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("renovate.json is not valid JSON: %v", err)
	}

	return cfg
}

// stringsAt returns the array of strings at key, or nil.
func stringsAt(cfg map[string]any, key string) []string {
	raw, _ := cfg[key].([]any)

	var out []string

	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

// TestConfigRoundTrip: the two things a fix must not do to a config it only
// meant to edit one key of — escape the `>` in a preset reference, and turn
// an integer into a float.
func TestConfigRoundTrip(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	// No forkProcessing, so fix has one key to set and must leave the rest as it found it.
	files["renovate.json"] = `{"extends":["` + pinnedPresetRef(t) + `"],"prConcurrentLimit":10}` + "\n"
	root := writeRepo(t, files)

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"renovate",
	); o.Action != rules.ActionMerged {
		t.Fatalf("fix: %s (%s), want merged", o.Action, o.Message)
	}

	data, _ := os.ReadFile(filepath.Join(root, "renovate.json"))

	out := string(data)
	if !strings.Contains(out, pinnedPresetRef(t)) {
		t.Errorf("the preset reference was escaped:\n%s", out)
	}

	if !strings.Contains(out, "10") || strings.Contains(out, "1e+01") {
		t.Errorf("an integer did not survive:\n%s", out)
	}
}

// TestSetPresetRef covers the shapes extends arrives in: absent, carrying an
// outdated reference, carrying only unrelated presets, and already correct.
func TestSetPresetRef(t *testing.T) {
	t.Parallel()

	want := pinnedPresetRef(t)

	for name, input := range map[string]string{
		"absent":           `{"forkProcessing":"enabled"}`,
		"no extends array": `{"forkProcessing":"enabled","extends":"github>farcloser/limen#v1.0.0"}`,
		"outdated ref":     `{"forkProcessing":"enabled","extends":["github>farcloser/limen#v1.0.0","config:recommended"]}`,
		"local ref":        `{"forkProcessing":"enabled","extends":["local>farcloser/limen"]}`,
		"only unrelated":   `{"forkProcessing":"enabled","extends":["config:recommended"]}`,
		"already correct":  `{"forkProcessing":"enabled","extends":["` + want + `","config:recommended"]}`,
		"duplicated stale": `{"forkProcessing":"enabled","extends":["github>farcloser/limen#v1.0.0","github>farcloser/limen#v2.0.0"]}`,
	} {
		files := compliantFiles()
		files["renovate.json"] = input + "\n"
		root := writeRepo(t, files)

		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()})

		refs := stringsAt(renovateConfig(t, root), "extends")
		if !slices.Contains(refs, want) {
			t.Errorf("%s: extends = %v, want %s", name, refs, want)
		}

		// Unrelated presets survive, and the stale reference does not.
		for _, ref := range refs {
			if ref != want && presetRefRE.MatchString(ref) {
				t.Errorf("%s: stale reference kept: %q", name, ref)
			}
		}

		if strings.Contains(input, "config:recommended") && !slices.Contains(refs, "config:recommended") {
			t.Errorf("%s: an unrelated preset was dropped: %v", name, refs)
		}

		if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); !f.OK() {
			t.Errorf("%s: after fix: %s", name, f.Message)
		}
	}
}

// TestAddIgnoredAuthor: the new address goes first, existing entries keep
// their order, and adding twice does not duplicate.
func TestAddIgnoredAuthor(t *testing.T) {
	t.Parallel()

	known := rules.DefaultPolicy()
	known.UpdateAppIdentity = testIdentity

	files := compliantFiles()
	files["renovate.json"] = `{"forkProcessing":"enabled","extends":["` + pinnedPresetRef(t) + `"],` +
		`"gitIgnoredAuthors":["a@example.com","b@example.com"]}` + "\n"
	root := writeRepo(t, files)

	rules.Fix(context.Background(), root, rules.FixOptions{Policy: known})

	if got, want := ignoredAuthorsOf(
		t,
		root,
	), []string{
		testIdentity,
		"a@example.com",
		"b@example.com",
	}; !slices.Equal(
		got,
		want,
	) {
		t.Errorf("got %v, want %v", got, want)
	}

	rules.Fix(context.Background(), root, rules.FixOptions{Policy: known})

	if got := ignoredAuthorsOf(t, root); len(got) != 3 {
		t.Errorf("fixing twice duplicated: %v", got)
	}

	// Absent key: the array is created.
	files["renovate.json"] = `{"forkProcessing":"enabled","extends":["` + pinnedPresetRef(t) + `"]}` + "\n"
	root = writeRepo(t, files)

	rules.Fix(context.Background(), root, rules.FixOptions{Policy: known})

	if got := ignoredAuthorsOf(t, root); !slices.Equal(got, []string{testIdentity}) {
		t.Errorf("absent key: %v", got)
	}
}

// TestCanonicalSeed: the seed limen ships must itself satisfy the rule's
// non-negotiable key — a seed that skipped forkProcessing would silently
// disable Renovate on every fork it lands in.
func TestCanonicalSeed(t *testing.T) {
	t.Parallel()

	var cfg map[string]any
	if err := json.Unmarshal([]byte(limen.CanonicalRenovate), &cfg); err != nil {
		t.Fatalf("the canonical seed is not valid JSON: %v", err)
	}

	if cfg["forkProcessing"] != "enabled" {
		t.Errorf("the seed must set forkProcessing to %q", "enabled")
	}

	if !slices.Contains(stringsAt(cfg, "extends"), "local>farcloser/limen") {
		t.Errorf("the seed must extend %q", "local>farcloser/limen")
	}

	// The seed carries what every repository needs and nothing of limen's
	// own: no manager, and no App identity but GitHub's — the org's is the
	// rule's to add, since the farcloser App never pushes to another org.
	if _, has := cfg["customManagers"]; has {
		t.Error("the seed must carry no customManagers: a manager is a project's own")
	}

	if got := stringsAt(cfg, "gitIgnoredAuthors"); !slices.Equal(got,
		[]string{"41898282+github-actions[bot]@users.noreply.github.com"}) {
		t.Errorf("the seed's gitIgnoredAuthors = %v, want the github-actions identity alone", got)
	}
}

// TestSupersededConfig: a renovate.json5 (or any other config file Renovate
// would read only in renovate.json's absence) beside renovate.json is dead
// config that still looks authoritative. check fails naming it; fix edits
// renovate.json as usual but ends advisory, naming it, and never removes it.
func TestSupersededConfig(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["renovate.json5"] = "{ extends: ['config:recommended'] }\n"
	root := writeRepo(t, files)

	finding := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate")
	if finding.OK() || finding.Path != "renovate.json5" || !strings.Contains(finding.Message, "dead config") {
		t.Errorf("check must fail naming renovate.json5, got: %+v", finding)
	}

	outcome := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"renovate",
	)
	if outcome.Action != rules.ActionAdvisory || !strings.Contains(outcome.Message, "renovate.json5") {
		t.Errorf("fix must end advisory naming renovate.json5, got: %s (%s)", outcome.Action, outcome.Message)
	}

	if _, err := os.Stat(filepath.Join(root, "renovate.json5")); err != nil {
		t.Errorf("fix must leave renovate.json5 in place: %v", err)
	}
}

// TestCanonicalPresetRef: limen extends its own default branch; every other
// repository extends the preset at its pinned limen version; a repository
// without a limen pin has nothing to pin to.
func TestCanonicalPresetRef(t *testing.T) {
	t.Parallel()

	const local = `{"forkProcessing":"enabled","extends":["local>farcloser/limen"]}` + "\n"

	self := writeRepo(t, map[string]string{
		"go.mod":        "module github.com/farcloser/limen\n\ngo 1.26\n",
		"renovate.json": local,
	})
	if f := findingByRule(rules.Check(self, rules.DefaultPolicy()), "renovate"); !f.OK() {
		t.Errorf("limen itself extends its own default branch: %s", f.Message)
	}

	pinned := writeRepo(t, map[string]string{
		"go.mod":        "module example.com/thing\n\ngo 1.26\n",
		"aqua.yaml":     "packages:\n  - name: farcloser/limen@v1.2.3 # renovate: depName=farcloser/limen\n    registry: local\n",
		"renovate.json": local,
	})
	if f := findingByRule(rules.Check(pinned, rules.DefaultPolicy()), "renovate"); f.OK() ||
		!strings.Contains(f.Message, "github>farcloser/limen#v1.2.3") {
		t.Errorf("a pinned repository must be asked for the preset at its pin, got: %+v", f)
	}

	unpinned := writeRepo(t, map[string]string{
		"aqua.yaml":     "packages: []\n",
		"renovate.json": `{"forkProcessing":"enabled","extends":["config:recommended"]}` + "\n",
	})
	if f := findingByRule(rules.Check(unpinned, rules.DefaultPolicy()), "renovate"); !f.OK() {
		t.Errorf("no limen pin: nothing to enforce, got: %s", f.Message)
	}

	if !strings.Contains(rules.CanonicalRenovateFor(limen.CanonicalAquaYAML), pinnedPresetRef(t)) {
		t.Errorf("the canonical manifest's renovate.json does not carry %s", pinnedPresetRef(t))
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

	known := rules.DefaultPolicy()
	known.UpdateAppIdentity = testIdentity

	// Unknown identity: pass / none, file untouched.
	root := writeRepo(t, compliantFiles())
	if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); !f.OK() {
		t.Errorf("unknown identity must not fail: %s", f.Message)
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"renovate",
	); o.Action != rules.ActionNone {
		t.Errorf("unknown identity: %s, want none", o.Action)
	}

	// Known and missing: fail, then fix merges it and check passes.
	if f := findingByRule(rules.Check(root, known), "renovate"); f.OK() {
		t.Error("a missing update-App identity must fail when known")
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: known}),
		"renovate",
	); o.Action != rules.ActionMerged {
		t.Errorf("fix: %s (%s), want merged", o.Action, o.Message)
	}

	if !slices.Contains(ignoredAuthorsOf(t, root), testIdentity) {
		t.Error("fix did not write the identity")
	}

	if f := findingByRule(rules.Check(root, known), "renovate"); !f.OK() {
		t.Errorf("after fix: %s", f.Message)
	}

	// Idempotent.
	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: known}),
		"renovate",
	); o.Action != rules.ActionNone {
		t.Errorf("second fix: %s, want none", o.Action)
	}

	// A compliant file in the project's own serialization — keys in its
	// order, an escaped non-ASCII character — is left byte for byte: fix
	// decides on values, and a rewrite into limen's form on every run put a
	// formatting diff into every branch the checksum workflow ran on.
	handEdited := compliantFiles()
	handEdited["renovate.json"] = `{"gitIgnoredAuthors":["` + testIdentity + `"],"forkProcessing":"enabled",` +
		`"extends":["github>farcloser/limen#` + canonicalLimenVersion(t) + `"],"description":["\u2014 by hand"]}` + "\n"
	root = writeRepo(t, handEdited)

	if f := findingByRule(rules.Check(root, known), "renovate"); !f.OK() {
		t.Errorf("a compliant hand-edited file must pass: %s", f.Message)
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: known}),
		"renovate",
	); o.Action != rules.ActionNone {
		t.Errorf("fix on a compliant hand-edited file: %s (%s), want none", o.Action, o.Message)
	}

	got, readErr := os.ReadFile(filepath.Join(root, "renovate.json"))
	if readErr != nil || string(got) != handEdited["renovate.json"] {
		t.Errorf("fix must leave a value-identical file untouched, got:\n%s", got)
	}

	// The seed as seeded — extending limen's own default branch — is not what
	// a repository must carry: check names the pinned reference, fix sets it
	// (identity known or not), and the file is otherwise untouched.
	seeded := compliantFiles()
	seeded["renovate.json"] = limen.CanonicalRenovate
	root = writeRepo(t, seeded)

	want := "github>farcloser/limen#" + canonicalLimenVersion(t)
	if f := findingByRule(
		rules.Check(root, rules.DefaultPolicy()),
		"renovate",
	); f.OK() ||
		!strings.Contains(f.Message, want) {
		t.Errorf("the raw seed must fail naming %s, got: %+v", want, f)
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"renovate",
	); o.Action != rules.ActionMerged {
		t.Errorf("fix on the raw seed: %s (%s), want merged", o.Action, o.Message)
	}

	data, err := os.ReadFile(filepath.Join(root, "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != rules.CanonicalRenovateFor(limen.CanonicalAquaYAML) {
		t.Errorf("fix must pin the reference and change nothing else:\n%s", data)
	}

	if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); !f.OK() {
		t.Errorf("after pinning: %s", f.Message)
	}

	// forkProcessing is not inheritable: a config that drops it fails, and
	// fix puts it back. This is the whole reason the file is renovate.json.
	noForks := compliantFiles()
	noForks["renovate.json"] = `{"extends":["` + want + `"]}` + "\n"
	root = writeRepo(t, noForks)

	if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); f.OK() ||
		!strings.Contains(f.Message, "forkProcessing") {
		t.Errorf("a config without %s must fail naming it, got: %+v", "forkProcessing", f)
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"renovate",
	); o.Action != rules.ActionMerged {
		t.Errorf("fix must set %s: %s (%s)", "forkProcessing", o.Action, o.Message)
	}

	if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); !f.OK() {
		t.Errorf("after setting %s: %s", "forkProcessing", f.Message)
	}

	// A project that rewrote the file without the identity's key: fix creates
	// the array rather than giving up, which the regex editor could not do.
	custom := compliantFiles()
	custom["renovate.json"] = `{"extends":["config:recommended"]}` + "\n"
	root = writeRepo(t, custom)

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: known}),
		"renovate",
	); o.Action != rules.ActionMerged {
		t.Errorf("no key: %s (%s), want merged", o.Action, o.Message)
	}

	if !slices.Contains(ignoredAuthorsOf(t, root), testIdentity) {
		t.Error("fix did not create gitIgnoredAuthors")
	}

	// Invalid JSON is the one thing fix will not guess at.
	broken := compliantFiles()
	broken["renovate.json"] = "{ extends: [\"config:recommended\"] }\n" // JSON5, not JSON
	root = writeRepo(t, broken)

	if f := findingByRule(rules.Check(root, known), "renovate"); f.OK() {
		t.Error("invalid JSON must fail check")
	}

	if o := outcomeByRule(
		rules.Fix(context.Background(), root, rules.FixOptions{Policy: known}),
		"renovate",
	); o.Action != rules.ActionAdvisory {
		t.Errorf("invalid JSON: %s (%s), want advisory", o.Action, o.Message)
	}

	// No renovate.json at all: the workflows rule owns that verdict; this
	// rule stays quiet on both sides.
	missing := compliantFiles()
	delete(missing, "renovate.json")
	root = writeRepo(t, missing)

	if f := findingByRule(rules.Check(root, known), "renovate"); !f.OK() {
		t.Errorf("missing file must not double-report: %s", f.Message)
	}
}

// ignoredAuthorsOf reads the repository's gitIgnoredAuthors array, in order.
func ignoredAuthorsOf(t *testing.T, root string) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "renovate.json"))
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

func outcomeByRule(outcomes []rules.Outcome, rule string) rules.Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return rules.Outcome{Rule: rule, Action: rules.ActionFailed, Message: "rule not remediated"}
}

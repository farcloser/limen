package rules_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

const (
	testIdentity = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"
	// testRepository is the "owner/name" the fixtures stand for, as the
	// caller would resolve it from the origin remote.
	testRepository = "example-org/thing"
	// testPresetRef is the reference a repository named testRepository must
	// extend: the shared configuration, read from itself by name.
	testPresetRef = "local>example-org/thing//.limen/renovate"
)

// presetRefRE matches any reference to the shared configuration: the
// in-repository form under any name, and the retired farcloser/limen preset.
var presetRefRE = regexp.MustCompile(
	`^(?:(?:github|local)>farcloser/limen|local>[^/]+/[^/]+//\.limen/renovate)(?:#.*)?$`,
)

// withRepository is the default policy with the repository known.
func withRepository() rules.Policy {
	policy := rules.DefaultPolicy()
	policy.Repository = testRepository

	return policy
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
	raw, isList := cfg[key].([]any)
	if !isList {
		return nil
	}

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
	files["renovate.json"] = `{"extends":["` + testPresetRef + `"],"prConcurrentLimit":10}` + "\n"
	root := writeRepo(t, files)

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()}),
	); o.Action != rules.ActionMerged {
		t.Fatalf("fix: %s (%s), want merged", o.Action, o.Message)
	}

	data, _ := os.ReadFile(filepath.Join(root, "renovate.json"))

	out := string(data)
	if !strings.Contains(out, testPresetRef) {
		t.Errorf("the preset reference was escaped:\n%s", out)
	}

	if !strings.Contains(out, "10") || strings.Contains(out, "1e+01") {
		t.Errorf("an integer did not survive:\n%s", out)
	}
}

// TestSetPresetRef covers the shapes extends arrives in: absent, carrying the
// retired preset, carrying another name's reference (a rename, a fork), only
// unrelated presets, and already correct.
func TestSetPresetRef(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"absent":           `{"forkProcessing":"enabled"}`,
		"no extends array": `{"forkProcessing":"enabled","extends":"github>farcloser/limen#v1.0.0"}`,
		"retired tagged":   `{"forkProcessing":"enabled","extends":["github>farcloser/limen#v1.0.0","config:recommended"]}`,
		"retired local":    `{"forkProcessing":"enabled","extends":["local>farcloser/limen"]}`,
		"renamed":          `{"forkProcessing":"enabled","extends":["local>example-org/old-name//.limen/renovate"]}`,
		"only unrelated":   `{"forkProcessing":"enabled","extends":["config:recommended"]}`,
		"already correct":  `{"forkProcessing":"enabled","extends":["` + testPresetRef + `","config:recommended"]}`,
		"duplicated stale": `{"forkProcessing":"enabled","extends":["github>farcloser/limen#v1.0.0","github>farcloser/limen#v2.0.0"]}`,
	} {
		files := compliantFiles()
		files["renovate.json"] = input + "\n"
		root := writeRepo(t, files)

		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()})

		refs := stringsAt(renovateConfig(t, root), "extends")
		if !slices.Contains(refs, testPresetRef) {
			t.Errorf("%s: extends = %v, want %s", name, refs, testPresetRef)
		}

		// Unrelated presets survive, and the stale reference does not.
		for _, ref := range refs {
			if ref != testPresetRef && presetRefRE.MatchString(ref) {
				t.Errorf("%s: stale reference kept: %q", name, ref)
			}
		}

		if strings.Contains(input, "config:recommended") && !slices.Contains(refs, "config:recommended") {
			t.Errorf("%s: an unrelated preset was dropped: %v", name, refs)
		}

		if f := findingByRule(rules.Check(root, withRepository()), "renovate"); !f.OK() {
			t.Errorf("%s: after fix: %s", name, f.Message)
		}
	}
}

// TestPresetRefRepositoryUnknown: without an origin remote the name cannot be
// known, so the reference is not enforced — except that the retired preset,
// gone from every later limen release, fails check and leaves fix advisory.
func TestPresetRefRepositoryUnknown(t *testing.T) {
	t.Parallel()

	for name, extends := range map[string]string{
		"another name": "local>other-org/other//.limen/renovate",
		"none":         "config:recommended",
	} {
		files := compliantFiles()
		files["renovate.json"] = `{"forkProcessing":"enabled","extends":["` + extends + `"]}` + "\n"
		root := writeRepo(t, files)

		if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); !f.OK() {
			t.Errorf("%s: an unknown repository must not fail: %s", name, f.Message)
		}

		if o := renovateOutcome(
			rules.Fix(t.Context(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		); o.Action != rules.ActionNone {
			t.Errorf("%s: fix: %s (%s), want none", name, o.Action, o.Message)
		}
	}

	files := compliantFiles()
	files["renovate.json"] = `{"forkProcessing":"enabled","extends":["github>farcloser/limen#v1.0.0"]}` + "\n"
	root := writeRepo(t, files)

	if f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "renovate"); f.OK() ||
		!strings.Contains(f.Message, "retired") {
		t.Errorf("the retired preset must fail naming it, got: %+v", f)
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: rules.DefaultPolicy()}),
	); o.Action != rules.ActionAdvisory {
		t.Errorf("fix on the retired preset, repository unknown: %s (%s), want advisory", o.Action, o.Message)
	}
}

// TestAddIgnoredAuthor: the new address goes first, existing entries keep
// their order, and adding twice does not duplicate.
func TestAddIgnoredAuthor(t *testing.T) {
	t.Parallel()

	known := withRepository()
	known.UpdateAppIdentity = testIdentity

	files := compliantFiles()
	files["renovate.json"] = `{"forkProcessing":"enabled","extends":["` + testPresetRef + `"],` +
		`"gitIgnoredAuthors":["a@example.com","b@example.com"]}` + "\n"
	root := writeRepo(t, files)

	rules.Fix(t.Context(), root, rules.FixOptions{Policy: known})

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

	rules.Fix(t.Context(), root, rules.FixOptions{Policy: known})

	if got := ignoredAuthorsOf(t, root); len(got) != 3 {
		t.Errorf("fixing twice duplicated: %v", got)
	}

	// Absent key: the array is created.
	files["renovate.json"] = `{"forkProcessing":"enabled","extends":["` + testPresetRef + `"]}` + "\n"
	root = writeRepo(t, files)

	rules.Fix(t.Context(), root, rules.FixOptions{Policy: known})

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
	if err := json.Unmarshal([]byte(rules.CanonicalRenovateFor("")), &cfg); err != nil {
		t.Fatalf("the canonical seed is not valid JSON: %v", err)
	}

	if cfg["forkProcessing"] != "enabled" {
		t.Errorf("the seed must set forkProcessing to %q", "enabled")
	}

	// The seed cannot know the name of the repository it lands in: the
	// reference is the rule's to write.
	if _, has := cfg["extends"]; has {
		t.Error("the seed must carry no extends: the reference names the repository")
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

	if !strings.Contains(rules.CanonicalRenovateFor(testRepository), testPresetRef) {
		t.Errorf("the seed for %s does not carry %s", testRepository, testPresetRef)
	}
}

// TestOwnConfigIsTheSeed: limen's own renovate.json is the seed as `limen
// fix` leaves it in limen — its reference and the org's App identity added —
// plus the one manager only the preset's author needs, and its description
// entry. Prose fixed in one and not the other fails here.
func TestOwnConfigIsTheSeed(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["renovate.json"] = rules.CanonicalRenovateFor("")
	root := writeRepo(t, files)

	policy := rules.DefaultPolicy()
	policy.Repository = "farcloser/limen"
	policy.UpdateAppIdentity = "300983632+limen-ci-farcloser[bot]@users.noreply.github.com"

	rules.Fix(t.Context(), root, rules.FixOptions{Policy: policy})

	want := renovateConfig(t, root)

	own, err := os.ReadFile(filepath.Join("..", "..", "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(own, &got); err != nil {
		t.Fatalf("limen's renovate.json is not valid JSON: %v", err)
	}

	delete(got, "customManagers")

	description, isList := got["description"].([]any)
	if !isList {
		t.Fatal("limen's renovate.json carries no description array")
	}

	got["description"] = slices.DeleteFunc(slices.Clone(description), func(entry any) bool {
		text, isString := entry.(string)

		return isString && strings.HasPrefix(text, "customManagers:")
	})

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"limen's renovate.json, its own manager aside, is not the seed as fix leaves it:\ngot  %v\nwant %v",
			got,
			want,
		)
	}
}

// TestSharedConfigPinned: .limen/renovate.json is content-pinned — missing or
// drifted fails check, and fix writes the canonical file back.
func TestSharedConfigPinned(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]*string{
		"missing": nil,
		"drifted": new(`{"minimumReleaseAge":"0 days"}` + "\n"),
	} {
		files := compliantFiles()
		delete(files, ".limen/renovate.json")

		if content != nil {
			files[".limen/renovate.json"] = *content
		}

		root := writeRepo(t, files)

		if f := findingByRule(rules.Check(root, withRepository()), "renovate"); f.OK() ||
			!strings.Contains(f.Message, ".limen/renovate.json") {
			t.Errorf("%s: check must fail naming .limen/renovate.json, got: %+v", name, f)
		}

		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()})

		data, err := os.ReadFile(filepath.Join(root, ".limen", "renovate.json"))
		if err != nil || string(data) != limen.CanonicalRenovatePreset {
			t.Errorf("%s: fix must write the canonical file (err %v)", name, err)
		}

		if f := findingByRule(rules.Check(root, withRepository()), "renovate"); !f.OK() {
			t.Errorf("%s: after fix: %s", name, f.Message)
		}
	}
}

// TestSupersededConfig: a renovate.json5 (or any other config file Renovate
// would read only in renovate.json's absence, .jsonc included) beside
// renovate.json is dead config that still looks authoritative. check fails
// naming it; fix edits renovate.json as usual but ends advisory, naming it, and
// never removes it.
func TestSupersededConfig(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"renovate.json5", ".github/renovate.jsonc"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := compliantFiles()
			files[name] = "{ extends: ['config:recommended'] }\n"
			root := writeRepo(t, files)

			finding := findingByRule(rules.Check(root, withRepository()), "renovate")
			if finding.OK() || finding.Path != name || !strings.Contains(finding.Message, "dead config") {
				t.Errorf("check must fail naming %s, got: %+v", name, finding)
			}

			outcome := renovateOutcome(rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()}))
			if outcome.Action != rules.ActionAdvisory || !strings.Contains(outcome.Message, name) {
				t.Errorf("fix must end advisory naming %s, got: %s (%s)", name, outcome.Action, outcome.Message)
			}

			if _, err := os.Stat(filepath.Join(root, name)); err != nil {
				t.Errorf("fix must leave %s in place: %v", name, err)
			}
		})
	}
}

// TestRenovateRule: check and fix agree, the identity is enforced only when
// known, and fix's edit is exactly what check wants.
func TestRenovateRule(t *testing.T) {
	t.Parallel()

	known := withRepository()
	known.UpdateAppIdentity = testIdentity

	// Unknown identity: pass / none, file untouched.
	root := writeRepo(t, compliantFiles())
	if f := findingByRule(rules.Check(root, withRepository()), "renovate"); !f.OK() {
		t.Errorf("unknown identity must not fail: %s", f.Message)
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()}),
	); o.Action != rules.ActionNone {
		t.Errorf("unknown identity: %s, want none", o.Action)
	}

	// Known and missing: fail, then fix merges it and check passes.
	if f := findingByRule(rules.Check(root, known), "renovate"); f.OK() {
		t.Error("a missing update-App identity must fail when known")
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: known}),
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
	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: known}),
	); o.Action != rules.ActionNone {
		t.Errorf("second fix: %s, want none", o.Action)
	}

	// A compliant file in the project's own serialization — keys in its
	// order, an escaped non-ASCII character — is left byte for byte: fix
	// decides on values, and a rewrite into limen's form on every run put a
	// formatting diff into every branch the checksum workflow ran on.
	handEdited := compliantFiles()
	handEdited["renovate.json"] = `{"gitIgnoredAuthors":["` + testIdentity + `"],"forkProcessing":"enabled",` +
		`"extends":["` + testPresetRef + `"],"description":["— by hand"]}` + "\n"
	root = writeRepo(t, handEdited)

	if f := findingByRule(rules.Check(root, known), "renovate"); !f.OK() {
		t.Errorf("a compliant hand-edited file must pass: %s", f.Message)
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: known}),
	); o.Action != rules.ActionNone {
		t.Errorf("fix on a compliant hand-edited file: %s (%s), want none", o.Action, o.Message)
	}

	got, readErr := os.ReadFile(filepath.Join(root, "renovate.json"))
	if readErr != nil || string(got) != handEdited["renovate.json"] {
		t.Errorf("fix must leave a value-identical file untouched, got:\n%s", got)
	}

	// The seed as seeded carries no reference: with the repository known,
	// check names it, fix sets it (identity known or not), and the file is
	// otherwise untouched.
	seeded := compliantFiles()
	seeded["renovate.json"] = rules.CanonicalRenovateFor("")
	root = writeRepo(t, seeded)

	if f := findingByRule(rules.Check(root, withRepository()), "renovate"); f.OK() ||
		!strings.Contains(f.Message, testPresetRef) {
		t.Errorf("the raw seed must fail naming %s, got: %+v", testPresetRef, f)
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()}),
	); o.Action != rules.ActionMerged {
		t.Errorf("fix on the raw seed: %s (%s), want merged", o.Action, o.Message)
	}

	data, err := os.ReadFile(filepath.Join(root, "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != rules.CanonicalRenovateFor(testRepository) {
		t.Errorf("fix must set the reference and change nothing else:\n%s", data)
	}

	if f := findingByRule(rules.Check(root, withRepository()), "renovate"); !f.OK() {
		t.Errorf("after setting the reference: %s", f.Message)
	}

	// forkProcessing is not inheritable: a config that drops it fails, and
	// fix puts it back. This is the whole reason the file is renovate.json.
	noForks := compliantFiles()
	noForks["renovate.json"] = `{"extends":["` + testPresetRef + `"]}` + "\n"
	root = writeRepo(t, noForks)

	if f := findingByRule(rules.Check(root, withRepository()), "renovate"); f.OK() ||
		!strings.Contains(f.Message, "forkProcessing") {
		t.Errorf("a config without %s must fail naming it, got: %+v", "forkProcessing", f)
	}

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: withRepository()}),
	); o.Action != rules.ActionMerged {
		t.Errorf("fix must set %s: %s (%s)", "forkProcessing", o.Action, o.Message)
	}

	if f := findingByRule(rules.Check(root, withRepository()), "renovate"); !f.OK() {
		t.Errorf("after setting %s: %s", "forkProcessing", f.Message)
	}

	// A project that rewrote the file without the identity's key: fix creates
	// the array rather than giving up, which the regex editor could not do.
	custom := compliantFiles()
	custom["renovate.json"] = `{"extends":["config:recommended"]}` + "\n"
	root = writeRepo(t, custom)

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: known}),
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

	if o := renovateOutcome(
		rules.Fix(t.Context(), root, rules.FixOptions{Policy: known}),
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

// renovateOutcome is the renovate rule's outcome for renovate.json among
// outcomes, or a failure when the file was not remediated at all.
func renovateOutcome(outcomes []rules.Outcome) rules.Outcome {
	const rule = "renovate"

	for _, o := range outcomes {
		if o.Rule == rule && o.Path == "renovate.json" {
			return o
		}
	}

	return rules.Outcome{Rule: rule, Action: rules.ActionFailed, Message: "rule not remediated"}
}

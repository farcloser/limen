package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

// TestCanonicalAquaHasNoTwoLinePins: the canonical manifest is itself the
// shape the rule enforces — every pin one-line, nothing for the fix to do.
func TestCanonicalAquaHasNoTwoLinePins(t *testing.T) {
	t.Parallel()

	if strings.Contains(limen.CanonicalAquaYAML, "\n    version:") {
		t.Error("the canonical aqua.yaml carries a version: line")
	}

	if o := outcomesFor(
		rules.Fix(context.Background(), writeRepo(t, compliantFiles()), bootstrapOpts()),
		"aqua",
	); !allNone(
		o,
	) {
		t.Errorf("fix on the canonical aqua.yaml has something to do: %v", o)
	}
}

// allNone reports whether every outcome is a no-op.
func allNone(outcomes []rules.Outcome) bool {
	for _, o := range outcomes {
		if o.Action != rules.ActionNone {
			return false
		}
	}

	return true
}

// canonicalPin returns a canonical package's one-line pin exactly as the
// embedded aqua.yaml spells it, and its version. The fixtures below are
// built from these rather than from copied literals: a copied version is a
// second pin of the same tool that Renovate does not know about, and every
// bump of the canonical manifest then broke these tests for no reason.
func canonicalPin(t *testing.T, name string) (line, version string) {
	t.Helper()

	re := regexp.MustCompile(`(?m)^  - name: ` + regexp.QuoteMeta(name) + `@(\S+)\n`)

	m := re.FindStringSubmatch(limen.CanonicalAquaYAML)
	if m == nil {
		t.Fatalf("the canonical aqua.yaml carries no one-line pin for %s", name)
	}

	return m[0], m[1]
}

// TestAquaTwoLinePinsCollapsed: a project pinning packages on a separate
// version: line fails check, and fix collapses exactly those entries — the
// project's version kept, quotes kept, continuation lines untouched, the
// two-line renovate hook dropped, a project's own comment kept — after which
// check passes and fix is idempotent.
func TestAquaTwoLinePinsCollapsed(t *testing.T) {
	t.Parallel()

	// Canonical, except four two-line pins: one with the renovate hook and a
	// project's own (older) version, one quoted, one with a registry:
	// continuation line AFTER the version, one with a project's own comment
	// on the version line.
	goLine, _ := canonicalPin(t, "golang/go")
	jqLine, jqVersion := canonicalPin(t, "jqlang/jq")
	cuLine, cuVersion := canonicalPin(t, "uutils/coreutils")
	cliLine, cliVersion := canonicalPin(t, "cli/cli")

	const heldBackGo = "go1.0.0" // any version but the canonical: the project's, kept as is

	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest, goLine,
		"  - name: golang/go\n    version: "+heldBackGo+" # renovate: depName=golang/go\n", 1)
	manifest = strings.Replace(manifest, jqLine,
		"  - name: \"jqlang/jq\"\n    version: \""+jqVersion+"\"\n", 1)
	manifest = strings.Replace(manifest, cuLine+"    registry: local\n",
		"  - name: uutils/coreutils\n"+
			"    version: "+cuVersion+" # renovate: depName=uutils/coreutils\n"+
			"    registry: local\n", 1)
	manifest = strings.Replace(manifest, cliLine,
		"  - name: cli/cli\n    version: "+cliVersion+" # held back on purpose\n", 1)

	if strings.Count(manifest, "\n    version:") != 4 {
		t.Fatal("the fixture did not diverge from the canonical as intended — update the replacements")
	}

	files := compliantFiles()
	files["aqua.yaml"] = manifest
	dir := writeRepo(t, files)

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); f.OK() ||
		!strings.Contains(f.Message, "version: line") || !strings.Contains(f.Message, "golang/go") ||
		!strings.Contains(f.Message, "jqlang/jq") || !strings.Contains(f.Message, "uutils/coreutils") ||
		!strings.Contains(f.Message, "cli/cli") {
		t.Errorf("check must name every two-line pin, got: %+v", f)
	}

	if !allResolvedOutcomes(outcomesFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "aqua")) {
		t.Fatal("fix did not resolve the two-line pins")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	out := string(data)

	for _, want := range []string{
		"  - name: golang/go@" + heldBackGo + "\n",
		"  - name: \"jqlang/jq@" + jqVersion + "\"\n",
		cuLine + "    registry: local\n",
		"  - name: cli/cli@" + cliVersion + " # held back on purpose\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged manifest lacks:\n%s\n--- got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "\n    version:") || strings.Contains(out, "depName=golang/go") ||
		strings.Contains(out, "depName=uutils/coreutils") {
		t.Errorf("a version: line or renovate hook survived the collapse:\n%s", out)
	}

	// Nothing else moved: apart from the four collapses the file is the same.
	if strings.Count(out, "- name:") != strings.Count(manifest, "- name:") {
		t.Error("the collapse changed the number of package entries")
	}

	if want := len(strings.Split(manifest, "\n")) - 4; len(strings.Split(out, "\n")) != want {
		t.Errorf("expected exactly four lines fewer, got %d vs %d", len(strings.Split(out, "\n")), want)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("merged manifest must pass check: %s", f.Message)
	}

	if o := outcomesFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "aqua"); !allNone(o) {
		t.Errorf("fix is not idempotent: %v", o)
	}
}

// allResolvedOutcomes reports whether every outcome resolved its rule.
func allResolvedOutcomes(outcomes []rules.Outcome) bool {
	for _, o := range outcomes {
		if !resolved(o.Action) {
			return false
		}
	}

	return true
}

// TestAquaTwoLinePinsFoldIntoWholesaleReplacement: when the packages section
// is rebuilt anyway (a canonical package is missing), the collapse folds into
// that single replacement rather than adding an overlapping one.
func TestAquaTwoLinePinsFoldIntoWholesaleReplacement(t *testing.T) {
	t.Parallel()

	goLine, goVersion := canonicalPin(t, "golang/go")
	cliLine, _ := canonicalPin(t, "cli/cli")

	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest, goLine,
		"  - name: golang/go\n    version: "+goVersion+" # renovate: depName=golang/go\n", 1)
	manifest = strings.Replace(manifest, cliLine, "", 1) // now missing

	files := compliantFiles()
	files["aqua.yaml"] = manifest
	dir := writeRepo(t, files)

	if !allResolvedOutcomes(outcomesFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "aqua")) {
		t.Fatal("fix did not resolve the manifest")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))

	out := string(data)
	if !strings.Contains(out, goLine) ||
		!strings.Contains(out, cliLine) || strings.Contains(out, "\n    version:") {
		t.Errorf("merged manifest:\n%s", out)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("merged manifest must pass: %s", f.Message)
	}
}

package rules //nolint:testpackage // white-box: exercises the manifest merge directly.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

// TestCanonicalAquaHasNoTwoLinePins: the canonical manifest is itself the
// shape the rule enforces — every pin one-line, nothing for the fix to do.
func TestCanonicalAquaHasNoTwoLinePins(t *testing.T) {
	t.Parallel()

	if pins := canonicalAqua.twoLinePins(); len(pins) > 0 {
		t.Errorf("the canonical aqua.yaml carries two-line pins: %v", pins)
	}

	if strings.Contains(limen.CanonicalAquaYAML, "\n    version:") {
		t.Error("the canonical aqua.yaml carries a version: line")
	}
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
	gvLine, gvVersion := canonicalPin(t, "github.com/vbatts/git-validation")
	cliLine, cliVersion := canonicalPin(t, "cli/cli")

	const heldBackGo = "go1.0.0" // any version but the canonical: the project's, kept as is

	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest, goLine,
		"  - name: golang/go\n    version: "+heldBackGo+" # renovate: depName=golang/go\n", 1)
	manifest = strings.Replace(manifest, jqLine,
		"  - name: \"jqlang/jq\"\n    version: \""+jqVersion+"\"\n", 1)
	manifest = strings.Replace(manifest, gvLine+"    registry: local\n",
		"  - name: github.com/vbatts/git-validation\n"+
			"    version: "+gvVersion+" # renovate: depName=_go/github.com/vbatts/git-validation\n"+
			"    registry: local\n", 1)
	manifest = strings.Replace(manifest, cliLine,
		"  - name: cli/cli\n    version: "+cliVersion+" # held back on purpose\n", 1)

	if strings.Count(manifest, "\n    version:") != 4 {
		t.Fatal("the fixture did not diverge from the canonical as intended — update the replacements")
	}

	parsed, ok := parseAquaManifest(manifest)
	if !ok {
		t.Fatal("fixture does not parse")
	}

	if f := checkAquaManifest("aqua.yaml", parsed); f == nil ||
		!strings.Contains(f.Message, "version: line") || !strings.Contains(f.Message, "golang/go") ||
		!strings.Contains(f.Message, "jqlang/jq") || !strings.Contains(f.Message, "git-validation") ||
		!strings.Contains(f.Message, "cli/cli") {
		t.Errorf("check must name every two-line pin, got: %+v", f)
	}

	out, summary := mergeAquaManifest(parsed, "")
	if len(summary) == 0 {
		t.Fatal("merge reported no edits")
	}

	for _, want := range []string{
		"  - name: golang/go@" + heldBackGo + "\n",
		"  - name: \"jqlang/jq@" + jqVersion + "\"\n",
		gvLine + "    registry: local\n",
		"  - name: cli/cli@" + cliVersion + " # held back on purpose\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged manifest lacks:\n%s\n--- got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "\n    version:") || strings.Contains(out, "depName=golang/go") ||
		strings.Contains(out, "depName=_go/") {
		t.Errorf("a version: line or renovate hook survived the collapse:\n%s", out)
	}

	// Nothing else moved: apart from the four collapses the file is the same.
	if strings.Count(out, "- name:") != strings.Count(manifest, "- name:") {
		t.Error("the collapse changed the number of package entries")
	}

	if want := len(strings.Split(manifest, "\n")) - 4; len(strings.Split(out, "\n")) != want {
		t.Errorf("expected exactly four lines fewer, got %d vs %d", len(strings.Split(out, "\n")), want)
	}

	reparsed, ok := parseAquaManifest(out)
	if !ok {
		t.Fatalf("merged manifest does not parse:\n%s", out)
	}

	if f := checkAquaManifest("aqua.yaml", reparsed); f != nil {
		t.Errorf("merged manifest must pass check: %s", f.Message)
	}

	if again, summary := mergeAquaManifest(reparsed, ""); len(summary) != 0 || again != out {
		t.Errorf("merge is not idempotent: %v", summary)
	}
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

	parsed, ok := parseAquaManifest(manifest)
	if !ok {
		t.Fatal("fixture does not parse")
	}

	out, summary := mergeAquaManifest(parsed, "")
	if len(summary) < 2 { //nolint:mnd // one summary line for the missing package, one for the collapse.
		t.Fatalf("expected both a missing-package add and a collapse, got %v", summary)
	}

	if !strings.Contains(out, goLine) ||
		!strings.Contains(out, cliLine) || strings.Contains(out, "\n    version:") {
		t.Errorf("merged manifest:\n%s", out)
	}

	if reparsed, ok := parseAquaManifest(out); !ok || checkAquaManifest("aqua.yaml", reparsed) != nil {
		t.Errorf("merged manifest must parse and pass:\n%s", out)
	}
}

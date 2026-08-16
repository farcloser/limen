package rules //nolint:testpackage // white-box: exercises the manifest merge directly.

import (
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

// TestAquaTwoLinePinsCollapsed: a project pinning packages on a separate
// version: line fails check, and fix collapses exactly those entries — the
// project's version kept, quotes kept, continuation lines untouched, the
// two-line renovate hook dropped, a project's own comment kept — after which
// check passes and fix is idempotent.
func TestAquaTwoLinePinsCollapsed(t *testing.T) {
	t.Parallel()

	// Canonical, except four two-line pins: one with the renovate hook, one
	// quoted, one with a registry: continuation line AFTER the version, one
	// with a project's own comment on the version line.
	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest,
		"  - name: golang/go@go1.26.6\n",
		"  - name: golang/go\n    version: go1.25.9 # renovate: depName=golang/go\n", 1)
	manifest = strings.Replace(manifest,
		"  - name: jqlang/jq@jq-1.8.2\n",
		"  - name: \"jqlang/jq\"\n    version: \"jq-1.8.2\"\n", 1)
	manifest = strings.Replace(manifest,
		"  - name: github.com/vbatts/git-validation@v1.2.2\n    registry: local\n",
		"  - name: github.com/vbatts/git-validation\n"+
			"    version: v1.2.2 # renovate: depName=_go/github.com/vbatts/git-validation\n"+
			"    registry: local\n", 1)
	manifest = strings.Replace(manifest,
		"  - name: cli/cli@v2.96.0\n",
		"  - name: cli/cli\n    version: v2.96.0 # held back on purpose\n", 1)

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
		"  - name: golang/go@go1.25.9\n",
		"  - name: \"jqlang/jq@jq-1.8.2\"\n",
		"  - name: github.com/vbatts/git-validation@v1.2.2\n    registry: local\n",
		"  - name: cli/cli@v2.96.0 # held back on purpose\n",
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

	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest,
		"  - name: golang/go@go1.26.6\n",
		"  - name: golang/go\n    version: go1.26.6 # renovate: depName=golang/go\n", 1)
	manifest = strings.Replace(manifest, "  - name: cli/cli@v2.96.0\n", "", 1) // now missing

	parsed, ok := parseAquaManifest(manifest)
	if !ok {
		t.Fatal("fixture does not parse")
	}

	out, summary := mergeAquaManifest(parsed, "")
	if len(summary) < 2 { //nolint:mnd // one summary line for the missing package, one for the collapse.
		t.Fatalf("expected both a missing-package add and a collapse, got %v", summary)
	}

	if !strings.Contains(out, "  - name: golang/go@go1.26.6\n") ||
		!strings.Contains(out, "  - name: cli/cli@v2.96.0\n") || strings.Contains(out, "\n    version:") {
		t.Errorf("merged manifest:\n%s", out)
	}

	if reparsed, ok := parseAquaManifest(out); !ok || checkAquaManifest("aqua.yaml", reparsed) != nil {
		t.Errorf("merged manifest must parse and pass:\n%s", out)
	}
}

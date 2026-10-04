package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

// TestCanonicalAquaHasNoTwoLinePins: the canonical manifest and tool set are
// themselves the shape the rule enforces — every pin one-line, nothing for the
// fix to do.
func TestCanonicalAquaHasNoTwoLinePins(t *testing.T) {
	t.Parallel()

	if strings.Contains(limen.CanonicalAquaYAML, "\n    version:") {
		t.Error("the canonical aqua.yaml carries a version: line")
	}

	if strings.Contains(limen.CanonicalAquaPackages, "\n    version:") {
		t.Error("the canonical .limen/aqua.yaml carries a version: line")
	}

	if o := outcomesFor(
		rules.Fix(t.Context(), writeRepo(t, compliantFiles()), bootstrapOpts()),
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

// TestAquaTwoLinePinsCollapsed: a project pinning its own packages on a
// separate version: line fails check, and fix collapses exactly those entries
// — the project's version kept, quotes kept, continuation lines untouched, the
// two-line renovate hook dropped, a project's own comment kept — after which
// check passes and fix is idempotent.
func TestAquaTwoLinePinsCollapsed(t *testing.T) {
	t.Parallel()

	// Canonical, plus four project packages pinned on two lines: one with the
	// renovate hook, one quoted, one with a registry: continuation line AFTER
	// the version, one with a project's own comment on the version line.
	manifest := withProjectEntries(t,
		"  - name: junegunn/fzf\n    version: v0.60.0 # renovate: depName=junegunn/fzf\n"+
			"  - name: \"mikefarah/yq\"\n    version: \"v4.44.0\"\n"+
			"  - name: example/local-tool\n    version: v1.2.3 # renovate: depName=example/local-tool\n"+
			"    registry: local\n"+
			"  - name: sharkdp/fd\n    version: v10.1.0 # held back on purpose\n")

	files := compliantFiles()
	files[".aqua/aqua.yaml"] = manifest
	dir := writeRepo(t, files)

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); f.OK() ||
		!strings.Contains(f.Message, "version: line") || !strings.Contains(f.Message, "junegunn/fzf") ||
		!strings.Contains(f.Message, "mikefarah/yq") || !strings.Contains(f.Message, "example/local-tool") ||
		!strings.Contains(f.Message, "sharkdp/fd") {
		t.Errorf("check must name every two-line pin, got: %+v", f)
	}

	if !allResolvedOutcomes(outcomesFor(rules.Fix(t.Context(), dir, bootstrapOpts()), "aqua")) {
		t.Fatal("fix did not resolve the two-line pins")
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".aqua", "aqua.yaml"))
	out := string(data)

	for _, want := range []string{
		"  - name: junegunn/fzf@v0.60.0\n",
		"  - name: \"mikefarah/yq@v4.44.0\"\n",
		"  - name: example/local-tool@v1.2.3\n    registry: local\n",
		"  - name: sharkdp/fd@v10.1.0 # held back on purpose\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged manifest lacks:\n%s\n--- got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "\n    version:") || strings.Contains(out, "depName=junegunn/fzf") ||
		strings.Contains(out, "depName=example/local-tool") {
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

	if o := outcomesFor(rules.Fix(t.Context(), dir, bootstrapOpts()), "aqua"); !allNone(o) {
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
// is rebuilt anyway (the canonical import is missing), the collapse folds into
// that single replacement rather than adding an overlapping one.
func TestAquaTwoLinePinsFoldIntoWholesaleReplacement(t *testing.T) {
	t.Parallel()

	importLine := canonicalImportLine(t) + "\n"

	manifest := limen.CanonicalAquaYAML + "  - name: junegunn/fzf\n    version: v0.60.0 # renovate: depName=junegunn/fzf\n"
	manifest = strings.Replace(manifest, importLine, "", 1) // now missing

	files := compliantFiles()
	files[".aqua/aqua.yaml"] = manifest
	dir := writeRepo(t, files)

	if !allResolvedOutcomes(outcomesFor(rules.Fix(t.Context(), dir, bootstrapOpts()), "aqua")) {
		t.Fatal("fix did not resolve the manifest")
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".aqua", "aqua.yaml"))

	out := string(data)
	if !strings.Contains(out, "  - name: junegunn/fzf@v0.60.0\n") ||
		!strings.Contains(out, importLine) || strings.Contains(out, "\n    version:") {
		t.Errorf("merged manifest:\n%s", out)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("merged manifest must pass: %s", f.Message)
	}
}

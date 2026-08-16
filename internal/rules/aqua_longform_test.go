package rules //nolint:testpackage // white-box: exercises the manifest merge directly.

import (
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

// TestTwoLineCanonicalPkgs: the set is derived from the canonical manifest,
// and carries the depName each version line declares — the package's own
// name for the preset's dedicated managers, "_go/<module>" for go_install
// modules routed to the go datasource.
func TestTwoLineCanonicalPkgs(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"golang/go":                                  "golang/go",
		"jqlang/jq":                                  "jqlang/jq",
		"github.com/google/go-licenses/v2":           "_go/github.com/google/go-licenses/v2",
		"github.com/vbatts/git-validation":           "_go/github.com/vbatts/git-validation",
		"github.com/farcloser/godolint/cmd/godolint": "_go/github.com/farcloser/godolint/cmd/godolint",
	}
	for name, depName := range want {
		if got := twoLineCanonicalPkgs[name]; got != depName {
			t.Errorf("%s: depName %q, want %q", name, got, depName)
		}
	}

	// One-line canonical pins are not in the set: their versions are plain
	// semver the preset reads as-is.
	for _, name := range []string{"casey/just", "cli/cli", "sigstore/cosign", "lycheeverse/lychee"} {
		if _, found := twoLineCanonicalPkgs[name]; found {
			t.Errorf("%s must not be a two-line-canonical package", name)
		}
	}
}

// TestAquaShortFormPinsRewritten: a project pinning a two-line-canonical
// package in the one-line form fails check, and fix rewrites exactly that
// line — the project's version kept, continuation lines untouched, quotes
// preserved — after which check passes and fix is idempotent.
func TestAquaShortFormPinsRewritten(t *testing.T) {
	t.Parallel()

	// A manifest that is canonical except for four one-line pins, one of them
	// quoted, one with a registry: continuation line, one at a project-owned
	// version.
	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest,
		"  - name: golang/go\n    version: go1.26.6 # renovate: depName=golang/go\n",
		"  - name: golang/go@go1.25.9\n", 1)
	manifest = strings.Replace(manifest,
		"  - name: jqlang/jq\n    version: jq-1.8.2 # renovate: depName=jqlang/jq\n",
		"  - name: \"jqlang/jq@jq-1.8.2\"\n", 1)
	manifest = strings.Replace(manifest,
		"  - name: github.com/vbatts/git-validation\n"+
			"    version: v1.2.2 # renovate: depName=_go/github.com/vbatts/git-validation\n"+
			"    registry: local\n",
		"  - name: github.com/vbatts/git-validation@v1.2.2\n    registry: local\n", 1)

	if manifest == limen.CanonicalAquaYAML {
		t.Fatal("the fixture did not diverge from the canonical — update the replacements")
	}

	parsed, ok := parseAquaManifest(manifest)
	if !ok {
		t.Fatal("fixture does not parse")
	}

	if f := checkAquaManifest("aqua.yaml", parsed); f == nil ||
		!strings.Contains(f.Message, "one-line") || !strings.Contains(f.Message, "golang/go") ||
		!strings.Contains(f.Message, "jqlang/jq") || !strings.Contains(f.Message, "git-validation") {
		t.Errorf("check must name every one-line pin, got: %+v", f)
	}

	out, summary := mergeAquaManifest(parsed, "")
	if len(summary) == 0 {
		t.Fatal("merge reported no edits")
	}

	for _, want := range []string{
		"  - name: golang/go\n    version: go1.25.9 # renovate: depName=golang/go\n",
		"  - name: \"jqlang/jq\"\n    version: jq-1.8.2 # renovate: depName=jqlang/jq\n",
		"  - name: github.com/vbatts/git-validation\n" +
			"    version: v1.2.2 # renovate: depName=_go/github.com/vbatts/git-validation\n" +
			"    registry: local\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("merged manifest lacks:\n%s\n--- got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "golang/go@") || strings.Contains(out, "jqlang/jq@") ||
		strings.Contains(out, "git-validation@") {
		t.Errorf("a one-line pin survived the rewrite:\n%s", out)
	}

	// Nothing else moved: apart from the three rewrites the file is the same.
	if strings.Count(out, "- name:") != strings.Count(manifest, "- name:") {
		t.Error("the rewrite changed the number of package entries")
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

// TestAquaShortFormPinsFoldIntoWholesaleReplacement: when the packages
// section is rebuilt anyway (a canonical package is missing), the rewrite
// folds into that single replacement rather than adding an overlapping one.
func TestAquaShortFormPinsFoldIntoWholesaleReplacement(t *testing.T) {
	t.Parallel()

	manifest := limen.CanonicalAquaYAML
	manifest = strings.Replace(manifest,
		"  - name: golang/go\n    version: go1.26.6 # renovate: depName=golang/go\n",
		"  - name: golang/go@go1.26.6\n", 1)
	manifest = strings.Replace(manifest, "  - name: cli/cli@v2.96.0\n", "", 1) // now missing

	parsed, ok := parseAquaManifest(manifest)
	if !ok {
		t.Fatal("fixture does not parse")
	}

	out, summary := mergeAquaManifest(parsed, "")
	if len(summary) < 2 { //nolint:mnd // one summary line for the missing package, one for the rewrite.
		t.Fatalf("expected both a missing-package add and a rewrite, got %v", summary)
	}

	if !strings.Contains(out, "  - name: golang/go\n    version: go1.26.6 # renovate: depName=golang/go\n") ||
		!strings.Contains(out, "  - name: cli/cli@v2.96.0\n") {
		t.Errorf("merged manifest:\n%s", out)
	}

	if reparsed, ok := parseAquaManifest(out); !ok || checkAquaManifest("aqua.yaml", reparsed) != nil {
		t.Errorf("merged manifest must parse and pass:\n%s", out)
	}
}

package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/license"
	"github.com/farcloser/limen/internal/rules"
)

// bootstrapOpts remediates like `limen bootstrap`: a Closed-source LICENSE is
// generated for a missing one.
func bootstrapOpts() rules.FixOptions {
	return rules.FixOptions{Policy: rules.DefaultPolicy(), License: license.Closed, Holder: "Farcloser", Year: 2026}
}

// outcomesFor returns every outcome a rule produced, in order, for rules that
// remediate more than one file.
func outcomesFor(outcomes []rules.Outcome, rule string) []rules.Outcome {
	var got []rules.Outcome

	for _, o := range outcomes {
		if o.Rule == rule {
			got = append(got, o)
		}
	}

	return got
}

func outcomeFor(outcomes []rules.Outcome, rule string) rules.Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return rules.Outcome{Rule: rule, Action: rules.ActionFailed, Message: "rule not remediated"}
}

// TestFixCreatesEverything is the bootstrap path minus `git init`: a repo that is
// only a .git directory is remediated into a fully compliant one, and rules.Check then
// passes every rule. (The .git is pre-seeded so the test does not shell out.)
func TestFixCreatesEverything(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, nil) // just .git

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())
	if !rules.AllResolved(outcomes) {
		for _, o := range outcomes {
			if !resolved(o.Action) {
				t.Errorf("unresolved: %s -> %s (%s)", o.Rule, o.Action, o.Message)
			}
		}
	}

	if l := outcomeFor(outcomes, "license"); l.Action != rules.ActionCreated {
		t.Errorf("license action = %s, want created", l.Action)
	}
	// The remediated tree must satisfy rules.Check.
	if findings := rules.Check(dir, rules.DefaultPolicy()); !rules.AllOK(findings) {
		for _, f := range findings {
			if !f.OK() {
				t.Errorf("post-fix check failure: %s -> %s", f.Rule, f.Message)
			}
		}
	}
}

// TestFixIsIdempotent runs fix twice; the second run must change nothing.
func TestFixIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, nil)
	rules.Fix(context.Background(), dir, bootstrapOpts())

	second := rules.Fix(context.Background(), dir, bootstrapOpts())
	for _, o := range second {
		if o.Action != rules.ActionNone {
			t.Errorf("second fix touched %s: %s (%s)", o.Rule, o.Action, o.Message)
		}
	}
}

func TestFixLeavesExistingGitignore(t *testing.T) {
	t.Parallel()

	const own = "# the project's own\nbin/\n"

	dir := writeRepo(t, map[string]string{".gitignore": own})

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "gitignore")
	if o.Action != rules.ActionNone {
		t.Fatalf("gitignore action = %s, want none (an existing file is left as-is)", o.Action)
	}
	// The existing file is untouched — not overwritten with the canonical.
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(data) != own {
		t.Errorf("fix modified an existing .gitignore: %q", string(data))
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gitignore"); !f.OK() {
		t.Errorf("an existing .gitignore should pass: %s", f.Message)
	}
}

// TestFixJustfileRegimes: the root Justfile is the project's own — a missing
// one is seeded, one lacking the shared-baseline import gets it APPENDED
// (never overwritten), and one carrying it is untouched.
func TestFixJustfileRegimes(t *testing.T) {
	t.Parallel()

	// Missing -> seeded; the seed carries the import and ends in a newline
	// (just --fmt rejects a file without one — a fresh repo must not be born
	// lint-red).
	seeded := writeRepo(t, nil)
	if o := justfileOutcome(rules.Fix(context.Background(), seeded, bootstrapOpts())); o.Action != rules.ActionCreated {
		t.Fatalf("missing Justfile: %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(seeded, "Justfile"))
	if !strings.Contains(string(data), rules.CanonicalJustfileImport) || !strings.HasSuffix(string(data), "\n") {
		t.Errorf("seed must carry the import and end in a newline, got: %q", data)
	}

	// Present without the import -> merged: import appended, content kept.
	ownRecipes := "greet:\n\t@echo hand-rolled\n"

	merged := writeRepo(t, map[string]string{"Justfile": ownRecipes})
	if o := justfileOutcome(rules.Fix(context.Background(), merged, bootstrapOpts())); o.Action != rules.ActionMerged {
		t.Fatalf("Justfile without the import: %s, want merged", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(merged, "Justfile"))
	if !strings.Contains(string(data), "hand-rolled") ||
		!strings.Contains(string(data), rules.CanonicalJustfileImport) {
		t.Errorf("merge must keep the project's recipes and add the import, got: %q", data)
	}

	// Present with the import -> the project's own, untouched.
	own := rules.CanonicalJustfileImport + "\n\ngreet:\n\t@echo mine\n"

	untouched := writeRepo(t, map[string]string{"Justfile": own})
	if o := justfileOutcome(rules.Fix(context.Background(), untouched, bootstrapOpts())); o.Action != rules.ActionNone {
		t.Fatalf("compliant Justfile: %s, want none", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(untouched, "Justfile"))
	if string(data) != own {
		t.Error("a compliant Justfile must never be modified")
	}
}

func justfileOutcome(outcomes []rules.Outcome) rules.Outcome {
	for _, o := range outcomes {
		if o.Rule == "justfile" && (o.Path == "Justfile" || o.Path == "justfile" || o.Path == ".justfile") {
			return o
		}
	}

	return rules.Outcome{Rule: "justfile", Action: rules.ActionFailed, Message: "no root Justfile outcome"}
}

func TestFixOverwritesDriftedEditorconfig(t *testing.T) {
	t.Parallel()

	// A drifted .editorconfig (extra section) is overwritten to the canonical
	// exactly — content-pinned, no merge, no advisory.
	drifted := rules.CanonicalEditorconfig + "\n[*.lua]\nindent_size = 2\n"
	dir := writeRepo(t, map[string]string{".editorconfig": drifted})

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "editorconfig")
	if o.Action != rules.ActionOverwrote {
		t.Fatalf("editorconfig action = %s, want overwrote", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".editorconfig"))
	if string(data) != rules.CanonicalEditorconfig {
		t.Error("editorconfig was not reset to the canonical exactly")
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "editorconfig"); !f.OK() {
		t.Errorf("editorconfig should pass after overwrite: %s", f.Message)
	}
}

// TestFixLicensePolicy: fix (no chosen license) never invents a LICENSE, while
// bootstrap writes the chosen one; a present-but-disallowed LICENSE is advisory
// either way.
func TestFixLicensePolicy(t *testing.T) {
	t.Parallel()

	// Missing + fix (no License) -> advisory, no file written.
	dir := writeRepo(t, nil)
	if o := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"license",
	); o.Action != rules.ActionAdvisory {
		t.Errorf("fix on missing LICENSE = %s, want advisory", o.Action)
	}

	if _, err := os.Stat(filepath.Join(dir, "LICENSE")); err == nil {
		t.Error("fix must not create a LICENSE")
	}

	// Missing + bootstrap (License=Closed) -> created and recognized.
	dir2 := writeRepo(t, nil)
	if o := outcomeFor(
		rules.Fix(context.Background(), dir2, bootstrapOpts()),
		"license",
	); o.Action != rules.ActionCreated {
		t.Fatalf("bootstrap on missing LICENSE = %s, want created", o.Action)
	}

	if f := findingByRule(
		rules.Check(dir2, rules.DefaultPolicy()),
		"license",
	); !f.OK() ||
		f.Message != "license Closed-source" {
		t.Errorf("generated LICENSE not recognized as Closed-source: %s", f.Message)
	}

	// Present-but-disallowed -> advisory, untouched.
	dir3 := writeRepo(t, map[string]string{"LICENSE": "GNU GENERAL PUBLIC LICENSE Version 3\n"})
	if o := outcomeFor(
		rules.Fix(context.Background(), dir3, bootstrapOpts()),
		"license",
	); o.Action != rules.ActionAdvisory {
		t.Errorf("disallowed LICENSE = %s, want advisory", o.Action)
	}
}

func TestFixOverwritesDriftedShellcheck(t *testing.T) {
	t.Parallel()

	// Shell present, a drifted .limen/.shellcheckrc -> overwritten to the canonical
	// exactly (content-pinned: the repo's own directive is not preserved).
	dir := writeRepo(t, map[string]string{
		"build.sh":             "#!/bin/sh\necho hi\n",
		".limen/.shellcheckrc": rules.CanonicalShellcheckrc + "\ndisable=SC2034\n",
	})

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "shellcheck")
	if o.Action != rules.ActionOverwrote {
		t.Fatalf("shellcheck action = %s, want overwrote", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", ".shellcheckrc"))
	if string(data) != rules.CanonicalShellcheckrc {
		t.Errorf("shellcheck was not reset to the canonical exactly:\n%s", data)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "shellcheck"); !f.OK() {
		t.Errorf("shellcheck should pass after overwrite: %s", f.Message)
	}
}

// TestFixKeepsLowercaseReadme: a readme.md is the README; the fixer leaves it
// alone and never writes README.md beside it. (On a case-insensitive
// filesystem the two names are one file, so the check is on the directory
// listing, not on a Stat of README.md.)
func TestFixKeepsLowercaseReadme(t *testing.T) {
	t.Parallel()

	const body = "# the real readme\n"

	dir := writeRepo(t, map[string]string{"readme.md": body})

	if o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "readme"); o.Action != rules.ActionNone {
		t.Fatalf("readme action = %s (%s), want none", o.Action, o.Message)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Name() == "README.md" {
			t.Fatalf("fix planted %s beside readme.md", "README.md")
		}
	}

	if data, _ := os.ReadFile(filepath.Join(dir, "readme.md")); string(data) != body {
		t.Errorf("readme.md was rewritten:\n%s", data)
	}
}

func TestShellcheckrcIsUnconditional(t *testing.T) {
	t.Parallel()

	// A repository with no shell at all still gets .limen/.shellcheckrc: the
	// linter passes --rcfile unconditionally, so a repo without the file gets
	// "unable to read --rcfile" from `lint shell` — a warning that reads like
	// breakage rather than an inapplicable rule.
	dir := writeRepo(t, map[string]string{"README.md": "# no shell here\n"})

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "shellcheck"); f.OK() {
		t.Error("a repository missing .limen/.shellcheckrc must fail, shell or not")
	}

	if o := outcomeFor(
		rules.Fix(context.Background(), dir, bootstrapOpts()),
		"shellcheck",
	); o.Action != rules.ActionCreated {
		t.Fatalf("shellcheck action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", ".shellcheckrc"))
	if string(data) != rules.CanonicalShellcheckrc {
		t.Errorf("seeded .shellcheckrc is not the canonical:\n%s", data)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "shellcheck"); !f.OK() {
		t.Errorf("shellcheck should pass after seeding: %s", f.Message)
	}
}

func TestFixAgents(t *testing.T) {
	t.Parallel()

	// Missing -> AGENTS.md created from the canonical, CLAUDE.md seeded.
	dir := writeRepo(t, nil)

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())
	if got := outcomesFor(
		outcomes,
		"agents",
	); len(got) != 2 || got[0].Action != rules.ActionCreated ||
		got[1].Action != rules.ActionCreated {
		t.Fatalf("agents outcomes = %v, want two creations", got)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(data) != rules.CanonicalAgents {
		t.Error("created AGENTS.md does not equal the canonical")
	}

	data, _ = os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if string(data) != limen.CanonicalClaudeSeed {
		t.Errorf("seeded CLAUDE.md = %q, want the import line", data)
	}

	// Drifted AGENTS.md -> overwritten exactly; a project-owned CLAUDE.md -> untouched.
	drifted := writeRepo(t, map[string]string{
		"AGENTS.md": rules.CanonicalAgents + "\n- my own rule\n",
		"CLAUDE.md": "@AGENTS.md\n\n## Mine\n",
	})

	got := outcomesFor(rules.Fix(context.Background(), drifted, bootstrapOpts()), "agents")
	if len(got) != 2 || got[0].Action != rules.ActionOverwrote || got[1].Action != rules.ActionNone {
		t.Fatalf("agents outcomes = %v, want overwrote + none", got)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, "AGENTS.md"))
	if string(data) != rules.CanonicalAgents {
		t.Errorf("AGENTS.md was not reset to the canonical exactly:\n%s", data)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, "CLAUDE.md"))
	if string(data) != "@AGENTS.md\n\n## Mine\n" {
		t.Errorf("a project-owned CLAUDE.md was touched:\n%s", data)
	}

	if f := findingByRule(rules.Check(drifted, rules.DefaultPolicy()), "agents"); !f.OK() {
		t.Errorf("agents should pass after fix: %s", f.Message)
	}
}

func TestFixGitattributes(t *testing.T) {
	t.Parallel()

	// Missing .gitattributes -> created from the canonical (unconditional rule).
	dir := writeRepo(t, nil)

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "gitattributes")
	if o.Action != rules.ActionCreated {
		t.Fatalf("gitattributes action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if string(data) != rules.CanonicalGitattributes {
		t.Error("created .gitattributes does not equal the canonical")
	}

	// Drifted -> overwritten to the canonical exactly (content-pinned: a local
	// addition would reintroduce git's line-ending conversion).
	drifted := writeRepo(t, map[string]string{
		".gitattributes": rules.CanonicalGitattributes + "\n*.md text\n",
	})
	if o := outcomeFor(
		rules.Fix(context.Background(), drifted, bootstrapOpts()),
		"gitattributes",
	); o.Action != rules.ActionOverwrote {
		t.Fatalf("gitattributes action = %s, want overwrote", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, ".gitattributes"))
	if string(data) != rules.CanonicalGitattributes {
		t.Errorf(".gitattributes was not reset to the canonical exactly:\n%s", data)
	}

	if f := findingByRule(rules.Check(drifted, rules.DefaultPolicy()), "gitattributes"); !f.OK() {
		t.Errorf("gitattributes should pass after overwrite: %s", f.Message)
	}
}

func TestFixLychee(t *testing.T) {
	t.Parallel()

	// Missing .limen/lychee.toml -> created from the canonical (unconditional rule).
	dir := writeRepo(t, nil)

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "lychee")
	if o.Action != rules.ActionCreated {
		t.Fatalf("lychee action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", "lychee.toml"))
	if string(data) != rules.CanonicalLychee {
		t.Error("created .limen/lychee.toml does not equal the canonical")
	}

	// Drifted -> overwritten to the canonical exactly (content-pinned: a local
	// addition is not preserved; project exclusions belong in a root lychee.toml).
	drifted := writeRepo(t, map[string]string{
		".limen/lychee.toml": rules.CanonicalLychee + "\ncache = true\n",
	})
	if o := outcomeFor(
		rules.Fix(context.Background(), drifted, bootstrapOpts()),
		"lychee",
	); o.Action != rules.ActionOverwrote {
		t.Fatalf("lychee action = %s, want overwrote", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, ".limen", "lychee.toml"))
	if string(data) != rules.CanonicalLychee {
		t.Errorf("lychee config was not reset to the canonical exactly:\n%s", data)
	}

	if f := findingByRule(rules.Check(drifted, rules.DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("lychee should pass after overwrite: %s", f.Message)
	}

	// A project's own root lychee.toml is never touched.
	own := "exclude = ['https://example\\.internal/']\n"
	withOwn := writeRepo(t, map[string]string{".lychee.toml": own})
	rules.Fix(context.Background(), withOwn, bootstrapOpts())

	data, _ = os.ReadFile(filepath.Join(withOwn, ".lychee.toml"))
	if string(data) != own {
		t.Error("fix modified the project's own root .lychee.toml")
	}
}

func TestFixCreatesMissingShellcheck(t *testing.T) {
	t.Parallel()

	// Shell present, no .limen/.shellcheckrc -> created from the canonical.
	dir := writeRepo(t, map[string]string{"build.sh": "#!/bin/sh\necho hi\n"})

	o := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "shellcheck")
	if o.Action != rules.ActionCreated {
		t.Fatalf("shellcheck action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", ".shellcheckrc"))
	if string(data) != rules.CanonicalShellcheckrc {
		t.Error("created .limen/.shellcheckrc does not equal the canonical")
	}
}

// TestFixMergesAquaManifest: an existing manifest keeps its own packages and
// versions, gains the canonical sections and missing canonical packages without
// duplicates, and has its checksums regenerated — not copied from limen.
func TestFixMergesAquaManifest(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "checksum:\n  enabled: false\npackages:\n  - name: junegunn/fzf@v0.60.0\n  - name: casey/just@v99.99.99\n",
	})

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())
	if !rules.AllResolved(outcomes) {
		for _, o := range outcomes {
			if !resolved(o.Action) {
				t.Errorf("unresolved: %s -> %s (%s)", o.Rule, o.Action, o.Message)
			}
		}
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))

	manifest := string(data)
	if !strings.Contains(manifest, "require_checksum: true") {
		t.Error("checksum section was not reset to the canonical")
	}

	if !strings.Contains(manifest, "type: standard") || !strings.Contains(manifest, "path: .limen/aqua-registry.yaml") {
		t.Error("registries section was not added")
	}

	if !strings.Contains(manifest, "junegunn/fzf@v0.60.0") {
		t.Error("the project's own package was dropped")
	}

	if !strings.Contains(manifest, "casey/just@v99.99.99") {
		t.Error("the project's own version of a canonical package was not kept")
	}

	if n := strings.Count(manifest, "- name: casey/just@"); n != 1 {
		t.Errorf("casey/just appears %d times, want exactly 1 (no duplicates)", n)
	}

	sums, err := os.ReadFile(filepath.Join(dir, "aqua-checksums.json"))
	if err != nil {
		t.Fatalf("aqua-checksums.json not written: %v", err)
	}

	if string(sums) != stubChecksums {
		t.Errorf("checksums were not regenerated by aqua, got:\n%s", sums)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("aqua rule should pass after merge: %s", f.Message)
	}
	// A second fix must find nothing left to do.
	for _, o := range rules.Fix(context.Background(), dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action != rules.ActionNone {
			t.Errorf("second fix touched aqua again: %s (%s)", o.Action, o.Message)
		}
	}
}

// TestBootstrapSelfPinRelease: a release build seeds aqua.yaml with the
// farcloser/limen pin rewritten to the running version — the embedded pin
// necessarily lags one release, and seeding it verbatim would hand the repo to
// an older limen than the one that wrote its files. The rewrite forfeits the
// pristine shortcut, so checksums are regenerated, never copied.
func TestBootstrapSelfPinRelease(t *testing.T) {
	t.Parallel()

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := t.TempDir()

	outcomes := rules.Fix(context.Background(), dir, opts)
	for _, o := range outcomes {
		if !resolved(o.Action) {
			t.Errorf("unresolved: %s -> %s (%s)", o.Rule, o.Action, o.Message)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	manifest := string(data)
	if !strings.Contains(manifest, "farcloser/limen@v9.9.9") {
		t.Error("the limen pin was not rewritten to the running version")
	}

	if !strings.Contains(manifest, "farcloser/limen@v9.9.9 # renovate: depName=farcloser/limen") {
		t.Error("the renovate comment did not survive the pin rewrite")
	}

	if n := strings.Count(manifest, "- name: farcloser/limen@"); n != 1 {
		t.Errorf("farcloser/limen appears %d times, want exactly 1", n)
	}

	sums, err := os.ReadFile(filepath.Join(dir, "aqua-checksums.json"))
	if err != nil {
		t.Fatalf("aqua-checksums.json not generated: %v", err)
	}

	if string(sums) != stubChecksums {
		t.Errorf("checksums must be regenerated after the pin rewrite, not seeded:\n%s", sums)
	}
}

// TestBootstrapSelfPinDev: a dev build (no SelfVersion) seeds the embedded
// canonical pair verbatim — the provably matching aqua.yaml/aqua-checksums.json
// combination that keeps bootstrap compliant offline.
func TestBootstrapSelfPinDev(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	rules.Fix(context.Background(), dir, bootstrapOpts())

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != limen.CanonicalAquaYAML {
		t.Error("a dev build must seed the embedded aqua.yaml verbatim")
	}

	sums, err := os.ReadFile(filepath.Join(dir, "aqua-checksums.json"))
	if err != nil {
		t.Fatal(err)
	}

	if string(sums) != limen.CanonicalAquaChecksums {
		t.Error("a dev build must seed the embedded aqua-checksums.json verbatim")
	}
}

// TestFixInsertsSelfPinAtRunningVersion: when a merge adds the missing
// farcloser/limen pin to an existing manifest, a release build inserts it at
// the running version — the same skew argument as seeding — while packages the
// project already pins keep their own versions.
func TestFixInsertsSelfPinAtRunningVersion(t *testing.T) {
	t.Parallel()

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "packages:\n  - name: casey/just@v99.99.99\n",
	})

	rules.Fix(context.Background(), dir, opts)

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	manifest := string(data)
	if !strings.Contains(manifest, "farcloser/limen@v9.9.9") {
		t.Error("the inserted limen pin does not carry the running version")
	}

	if !strings.Contains(manifest, "casey/just@v99.99.99") {
		t.Error("a project-owned version was rewritten")
	}
}

// TestFixMovesExistingSelfPin: an existing farcloser/limen pin is the one
// version in the manifest fix does rewrite — a release build moves it to the
// running version, so the limen that wrote the repo's canonical files is the
// limen the repo pins (leaving it would hand the tree to an older limen that
// flags the fresh files as drift and "repairs" them backwards). The renovate
// comment survives the move, and checksums are regenerated in the same fix.
func TestFixMovesExistingSelfPin(t *testing.T) {
	t.Parallel()

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	// The canonical manifest with the limen pin at an older release — the only
	// drift is the pin itself, exercising the standalone one-line rewrite.
	dir := writeRepo(t, map[string]string{
		"aqua.yaml": withSelfPin(t, "v0.0.1"),
	})

	outcomes := rules.Fix(context.Background(), dir, opts)
	if !rules.AllResolved(outcomes) {
		t.Error("fix should resolve a manifest whose only drift is the limen pin")
	}

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	manifest := string(data)
	if !strings.Contains(manifest, "farcloser/limen@v9.9.9 # renovate: depName=farcloser/limen") {
		t.Error("the existing limen pin was not moved to the running version with the renovate comment intact")
	}

	if strings.Contains(manifest, "farcloser/limen@v0.0.1") {
		t.Error("the old limen pin survived the move")
	}

	if n := strings.Count(manifest, "- name: farcloser/limen@"); n != 1 {
		t.Errorf("farcloser/limen appears %d times, want exactly 1", n)
	}

	sums, err := os.ReadFile(filepath.Join(dir, "aqua-checksums.json"))
	if err != nil {
		t.Fatalf("aqua-checksums.json not generated: %v", err)
	}

	if string(sums) != stubChecksums {
		t.Errorf("checksums must be regenerated after the pin move:\n%s", sums)
	}
	// A second fix must find nothing left to move.
	for _, o := range rules.Fix(context.Background(), dir, opts) {
		if o.Rule == "aqua" && o.Action != rules.ActionNone {
			t.Errorf("second fix touched aqua again: %s (%s)", o.Action, o.Message)
		}
	}
}

// TestFixMovesExistingSelfPinInReplacedPackages: the pin move folds into the
// wholesale packages-section replacement when canonical packages are also
// missing — the two edits must not produce overlapping replacements.
func TestFixMovesExistingSelfPinInReplacedPackages(t *testing.T) {
	t.Parallel()

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "packages:\n" +
			"  - name: farcloser/limen@v0.0.1 # renovate: depName=farcloser/limen\n" +
			"  - name: casey/just@v99.99.99\n",
	})

	rules.Fix(context.Background(), dir, opts)

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	manifest := string(data)
	if !strings.Contains(manifest, "farcloser/limen@v9.9.9 # renovate: depName=farcloser/limen") {
		t.Error("the existing limen pin was not moved during the packages-section replacement")
	}

	if n := strings.Count(manifest, "- name: farcloser/limen@"); n != 1 {
		t.Errorf("farcloser/limen appears %d times, want exactly 1", n)
	}

	if !strings.Contains(manifest, "casey/just@v99.99.99") {
		t.Error("a project-owned version was rewritten")
	}
}

// TestFixKeepsExistingSelfPinDev: a dev build has no version to stamp — an
// existing limen pin, however stale, stays exactly where the project put it.
func TestFixKeepsExistingSelfPinDev(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": withSelfPin(t, "v0.0.1"),
	})

	rules.Fix(context.Background(), dir, bootstrapOpts())

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "farcloser/limen@v0.0.1") {
		t.Error("a dev build must never move an existing limen pin")
	}
}

// TestFixGeneratesChecksumsForExistingManifest: a pre-existing manifest with no
// checksums file gets a generated one — limen's canonical checksums describe a
// different package set and must never be copied in (audit finding A1).
func TestFixGeneratesChecksumsForExistingManifest(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{"aqua.yaml": limen.CanonicalAquaYAML})

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())
	if !rules.AllResolved(outcomes) {
		t.Error("fix should resolve a canonical manifest with missing checksums")
	}

	sums, err := os.ReadFile(filepath.Join(dir, "aqua-checksums.json"))
	if err != nil {
		t.Fatalf("aqua-checksums.json not generated: %v", err)
	}

	if string(sums) != stubChecksums {
		t.Errorf("checksums were copied, not generated:\n%s", sums)
	}
}

// TestFixAquaUnavailableIsAdvisory: when aqua cannot run, remediation must not
// pretend — no checksums appear and the outcome is an advisory.
func TestFixAquaUnavailableIsAdvisory(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	t.Setenv("PATH", t.TempDir()) // nothing on it: no aqua

	dir := writeRepo(t, map[string]string{"aqua.yaml": limen.CanonicalAquaYAML})

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())
	if rules.AllResolved(outcomes) {
		t.Error("fix without a working aqua should leave the rule unresolved")
	}

	var advisory bool

	for _, o := range outcomes {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory {
			advisory = true
		}
	}

	if !advisory {
		t.Error("expected an advisory telling the user to run aqua themselves")
	}

	if fileExists(filepath.Join(dir, "aqua-checksums.json")) {
		t.Error("no checksums file should have been written")
	}
}

// TestFixLeavesUnparseableAquaAlone: a manifest fix cannot confidently parse is
// never rewritten — advisory only, file byte-identical.
func TestFixLeavesUnparseableAquaAlone(t *testing.T) {
	t.Parallel()

	const flow = "checksum: {enabled: true, require_checksum: true}\npackages: []\n"

	dir := writeRepo(t, map[string]string{"aqua.yaml": flow, "aqua-checksums.json": "{}\n"})
	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())

	var advisory bool

	for _, o := range outcomes {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory {
			advisory = true
		}
	}

	if !advisory {
		t.Error("an unparseable manifest should yield an advisory")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if string(data) != flow {
		t.Error("an unparseable manifest must not be modified")
	}
}

// TestFixLeavesQuotedKeyAquaAlone: a YAML-legal top-level key the line parser
// cannot bound (quoted here), sitting after packages: in a manifest missing a
// canonical pin — the exact shape that once had fix append the missing pin
// inside the user's block (aqua never saw it; check stayed green). The shape
// now refuses to parse: advisory only, file byte-identical.
func TestFixLeavesQuotedKeyAquaAlone(t *testing.T) {
	t.Parallel()

	manifest := strings.Replace(limen.CanonicalAquaYAML, canonicalAquaLine(t, "koalaman/shellcheck@")+"\n", "", 1) +
		"\"my-user-key\":\n  - my precious user data\n"
	dir := writeRepo(t, map[string]string{"aqua.yaml": manifest, "aqua-checksums.json": "{}\n"})

	var advisory bool

	for _, o := range rules.Fix(context.Background(), dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory {
			advisory = true
		}
	}

	if !advisory {
		t.Error("a manifest with an unbounded top-level key should yield an advisory")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if string(data) != manifest {
		t.Error("a manifest with an unbounded top-level key must not be modified")
	}
}

// TestAquaMergeMovesQuotedSelfPin: a quoted farcloser/limen pin satisfies the
// package-presence check (quotes are stripped for matching), so the
// baseline-owned version move must find it too — and keep the quotes it came
// with.
func TestAquaMergeMovesQuotedSelfPin(t *testing.T) {
	t.Parallel()

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := writeRepo(t, map[string]string{"aqua.yaml": "packages:\n  - name: \"farcloser/limen@v0.0.1\"\n"})

	rules.Fix(context.Background(), dir, opts)

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if !strings.Contains(string(data), `- name: "farcloser/limen@v9.9.9"`) {
		t.Errorf("the quoted self-pin was not moved:\n%s", data)
	}
}

// TestFixAquaDuplicatesAdvisory: duplicate package entries cannot be resolved
// automatically (which version would win?) — fix reports them instead.
func TestFixAquaDuplicatesAdvisory(t *testing.T) {
	t.Parallel()

	line := canonicalAquaLine(t, "casey/just@")
	manifest := strings.Replace(limen.CanonicalAquaYAML, line+"\n", line+"\n  - name: casey/just@v0.0.1\n", 1)
	dir := writeRepo(t, map[string]string{"aqua.yaml": manifest, "aqua-checksums.json": "{}\n"})

	var advisory bool

	for _, o := range rules.Fix(context.Background(), dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory {
			advisory = true

			if !strings.Contains(o.Message, "duplicate") {
				t.Errorf("advisory does not name the duplicate: %s", o.Message)
			}
		}
	}

	if !advisory {
		t.Error("duplicate package entries should yield an advisory")
	}
}

// TestFixWorkflows: the two regimes of the .github surface — pinned pieces
// are overwritten on drift, seeded pieces are created once and never touched,
// and the release workflow follows the goreleaser opt-in.
func TestFixWorkflows(t *testing.T) {
	t.Parallel()

	// Drifted pinned piece -> overwritten to the canonical exactly; existing
	// customized seeded pieces -> untouched.
	dir := writeRepo(t, map[string]string{
		".github/workflows/update-aqua-checksum.yaml": "name: tampered\n",
		".github/workflows/ci.yaml":                   "name: my-own-ci\n",
	})

	outcomes := rules.Fix(context.Background(), dir, bootstrapOpts())

	for _, o := range outcomes {
		if o.Rule != "workflows" {
			continue
		}

		switch o.Path {
		case ".github/workflows/update-aqua-checksum.yaml":
			if o.Action != rules.ActionOverwrote {
				t.Errorf("tampered checksum workflow: %s, want overwrote", o.Action)
			}
		case ".github/workflows/ci.yaml":
			if o.Action != rules.ActionNone {
				t.Errorf("existing ci workflow: %s, want none (left untouched)", o.Action)
			}
		case ".github/actions/setup-aqua/action.yaml", "renovate.json":
			if o.Action != rules.ActionCreated {
				t.Errorf("%s: %s, want created", o.Path, o.Action)
			}
		case ".github/workflows/release.yaml":
			if o.Action != rules.ActionNone {
				t.Errorf("release workflow without goreleaser: %s, want none", o.Action)
			}
		default:
			// Other rule-adjacent paths (project.just seeding): not under test.
		}
	}

	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(".github/workflows/update-aqua-checksum.yaml")))
	if string(data) != limen.CanonicalWorkflowUpdateAquaChecksum {
		t.Error("checksum workflow was not reset to the canonical")
	}

	custom, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(".github/workflows/ci.yaml")))
	if string(custom) != "name: my-own-ci\n" {
		t.Error("the project's own ci workflow was modified")
	}

	// goreleaser present -> the release workflow is seeded.
	releasing := writeRepo(t, map[string]string{".goreleaser.yaml": "version: 2\n"})
	for _, o := range rules.Fix(context.Background(), releasing, bootstrapOpts()) {
		if o.Rule == "workflows" && o.Path == ".github/workflows/release.yaml" && o.Action != rules.ActionCreated {
			t.Errorf("release workflow with goreleaser: %s, want created", o.Action)
		}
	}
}

// TestBootstrapSeedsOverrideExample: the reference declarations file is
// bootstrap-only documentation — bootstrap writes it once (and never
// overwrites an existing one), while fix (no License) must not seed it, and
// no check requires it (a project that deleted it meant it).
func TestBootstrapSeedsOverrideExample(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	rules.Fix(context.Background(), dir, bootstrapOpts())

	data, err := os.ReadFile(filepath.Join(dir, "limen-example.yaml"))
	if err != nil {
		t.Fatalf("bootstrap did not seed limen-example.yaml: %v", err)
	}

	if string(data) != limen.CanonicalOverrideExample {
		t.Error("the seeded example does not match the embedded canonical")
	}

	// Re-running with the file replaced must leave it alone: seeded once.
	if err := os.WriteFile(filepath.Join(dir, "limen-example.yaml"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rules.Fix(context.Background(), dir, bootstrapOpts())

	data, _ = os.ReadFile(filepath.Join(dir, "limen-example.yaml"))
	if string(data) != "mine\n" {
		t.Error("bootstrap overwrote an existing limen-example.yaml")
	}
}

// TestFixDoesNotSeedOverrideExample: fix is not bootstrap — no example file.
func TestFixDoesNotSeedOverrideExample(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

	if _, err := os.Stat(filepath.Join(dir, "limen-example.yaml")); err == nil {
		t.Error("fix must not seed limen-example.yaml")
	}
}

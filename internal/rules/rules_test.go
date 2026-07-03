// White-box by necessity: these tests exercise unexported check*/remediate*
// helpers and the aquaBin test seam, none of which are part of the package API.

package rules //nolint:testpackage // white-box (see above)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
)

const mitText = `Permission is hereby granted, free of charge, to any person.
THE SOFTWARE IS PROVIDED "AS IS".`

// canonicalAquaWith returns the canonical aqua.yaml with one substring swapped,
// for tests that bend a single aspect of the manifest. It fails the test when
// the substring is not there, so a canonical-file change cannot silently turn
// the test into a no-op.
func canonicalAquaWith(t *testing.T, old, replacement string) string {
	t.Helper()

	if !strings.Contains(limen.CanonicalAquaYAML, old) {
		t.Fatalf("canonical aqua.yaml no longer contains %q — update this test", old)
	}

	return strings.Replace(limen.CanonicalAquaYAML, old, replacement, 1)
}

// writeRepo creates a temp directory seeded with the given files (name ->
// content) and a .git directory, so it satisfies the git rule. Tests that want
// a non-repo use t.TempDir() directly.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatalf("seeding .git: %v", err)
	}

	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}

	return dir
}

// compliantFiles returns the file set of a fully compliant repository, which
// individual tests can mutate to exercise a single failing rule.
func compliantFiles() map[string]string {
	// Every pattern DefaultPolicy requires, plus an extra of the repo's own.
	compliantGitignore := strings.Join(DefaultRequiredGitignore, "\n") + "\n*.log\n"

	files := map[string]string{
		"README.md":           "# Thing",
		"LICENSE":             mitText,
		".editorconfig":       CanonicalEditorconfig,
		".gitignore":          compliantGitignore,
		"Justfile":            CanonicalJustfileImport + "\n",
		"aqua.yaml":           limen.CanonicalAquaYAML,
		"aqua-checksums.json": "{}\n",
		// The aqua policy, local registry, and lychee config are content-pinned exactly.
		"aqua-policy.yaml":          CanonicalAquaPolicy,
		".limen/aqua-registry.yaml": CanonicalAquaRegistry,
		".limen/lychee.toml":        CanonicalLychee,
		// aqua.yaml is YAML, so the conditional yamlfmt rule fires; satisfy it
		// with the canonical baseline.
		".limen/.yamlfmt": CanonicalYamlfmt,
		// The .github surface: two content-pinned pieces, two seeded ones
		// (any content satisfies the seeded pair — canonical used here).
		pathWorkflowChecksum: limen.CanonicalWorkflowUpdateAquaChecksum,
		pathActionSetupAqua:  limen.CanonicalActionSetupAqua,
		pathWorkflowCI:       limen.CanonicalWorkflowCI,
		pathRenovate:         limen.CanonicalRenovate,
	}
	// Every shared just module (.limen/*.just) must be present.
	for _, m := range limen.JustModules() {
		files[m.Path] = m.Content
	}

	return files
}

func findingByRule(findings []Finding, rule string) Finding {
	for _, f := range findings {
		if f.Rule == rule {
			return f
		}
	}

	return Finding{Rule: rule, Status: StatusFail, Message: "rule not evaluated"}
}

func TestCheckCompliantRepo(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, compliantFiles())

	findings := Check(dir, DefaultPolicy())
	if !AllOK(findings) {
		for _, f := range findings {
			if !f.OK() {
				t.Errorf("unexpected failure: %s -> %s", f.Rule, f.Message)
			}
		}
	}

	if got := findingByRule(findings, "license").Message; got != "license MIT" {
		t.Errorf("license message = %q, want %q", got, "license MIT")
	}
}

func TestCheckMissingEverything(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	findings := Check(dir, DefaultPolicy())
	if AllOK(findings) {
		t.Fatal("expected failures for an empty repo")
	}

	for _, rule := range []string{"git", "readme", "license", "editorconfig", "gitignore", "justfile", "aqua", "lychee"} {
		if findingByRule(findings, rule).OK() {
			t.Errorf("rule %s unexpectedly passed", rule)
		}
	}
}

func TestCheckDisallowedLicense(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"README.md":     "# Thing",
		"LICENSE":       "GNU GENERAL PUBLIC LICENSE Version 3",
		".editorconfig": CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	f := findingByRule(Check(dir, DefaultPolicy()), "license")
	if f.OK() {
		t.Fatal("expected GPL LICENSE to fail")
	}
}

func TestCheckReadmeVariantAccepted(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"README":        "plain readme",
		"LICENSE":       mitText,
		".editorconfig": CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	f := findingByRule(Check(dir, DefaultPolicy()), "readme")
	if !f.OK() {
		t.Errorf("plain README should be accepted, got: %s", f.Message)
	}

	if f.Path != "README" {
		t.Errorf("path = %q, want README", f.Path)
	}
}

func TestEditorconfigMustMatchExactly(t *testing.T) {
	t.Parallel()

	// The exact canonical passes.
	if f := findingByRule(Check(writeRepo(t, compliantFiles()), DefaultPolicy()), "editorconfig"); !f.OK() {
		t.Errorf("the exact canonical .editorconfig should pass: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra section fails.
	extra := compliantFiles()

	extra[".editorconfig"] = CanonicalEditorconfig + "\n[*.lua]\nindent_size = 2\n"
	if f := findingByRule(Check(writeRepo(t, extra), DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("canonical + an extra section should fail (content-pinned, no extras)")
	}

	// A changed value fails.
	changed := compliantFiles()

	changed[".editorconfig"] = strings.Replace(CanonicalEditorconfig, "indent_size = 2", "indent_size = 4", 1)
	if f := findingByRule(Check(writeRepo(t, changed), DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("a changed indent_size should fail")
	}

	// A missing section (truncated canonical) fails.
	partial := compliantFiles()

	cut := strings.Index(CanonicalEditorconfig, "[*.md]")
	if cut < 0 {
		t.Fatal("canonical .editorconfig no longer contains a [*.md] section — update this test")
	}

	partial[".editorconfig"] = CanonicalEditorconfig[:cut]
	if f := findingByRule(Check(writeRepo(t, partial), DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("a truncated .editorconfig should fail")
	}
}

func TestLycheeMustMatchExactly(t *testing.T) {
	t.Parallel()

	// The exact canonical passes.
	if f := findingByRule(Check(writeRepo(t, compliantFiles()), DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("the exact canonical .limen/lychee.toml should pass: %s", f.Message)
	}

	// The rule is unconditional: a repo without the file fails.
	missing := compliantFiles()
	delete(missing, ".limen/lychee.toml")

	if f := findingByRule(Check(writeRepo(t, missing), DefaultPolicy()), "lychee"); f.OK() {
		t.Error("a missing .limen/lychee.toml should fail")
	}

	// It is content-pinned: even the canonical plus an extra setting fails — a
	// project's own exclusions belong in a root lychee.toml.
	extra := compliantFiles()

	extra[".limen/lychee.toml"] = CanonicalLychee + "\ncache = true\n"
	if f := findingByRule(Check(writeRepo(t, extra), DefaultPolicy()), "lychee"); f.OK() {
		t.Error("canonical + an extra setting should fail (content-pinned, no extras)")
	}

	// A root lychee.toml is the project's own: its presence changes nothing.
	own := compliantFiles()

	own["lychee.toml"] = "exclude = ['https://example\\.internal/']\n"
	if f := findingByRule(Check(writeRepo(t, own), DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("a project's own root lychee.toml should not affect the rule: %s", f.Message)
	}
}

func TestCheckGitRepoRequired(t *testing.T) {
	t.Parallel()

	// A directory with all files but no .git fails the git rule.
	dir := t.TempDir()
	for name, content := range compliantFiles() {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if f := findingByRule(Check(dir, DefaultPolicy()), "git"); f.OK() {
		t.Error("a non-git directory should fail the git rule")
	}

	// writeRepo seeds .git, so the same files now pass.
	repo := writeRepo(t, compliantFiles())
	if f := findingByRule(Check(repo, DefaultPolicy()), "git"); !f.OK() {
		t.Errorf("a directory with .git should pass: %s", f.Message)
	}
}

func TestGitRepoAcceptsGitFile(t *testing.T) {
	t.Parallel()

	// A worktree/submodule has .git as a file, not a directory.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../.git/worktrees/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if f := findingByRule(Check(dir, DefaultPolicy()), "git"); !f.OK() {
		t.Errorf("a .git file (worktree) should satisfy the git rule: %s", f.Message)
	}
}

func TestGitignoreRequiresPatterns(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	// Drop one required pattern; the rule must fail and name it.
	missing := DefaultRequiredGitignore[len(DefaultRequiredGitignore)-1]
	kept := DefaultRequiredGitignore[:len(DefaultRequiredGitignore)-1]
	files[".gitignore"] = strings.Join(kept, "\n") + "\n"

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "gitignore")
	if f.OK() {
		t.Fatalf("a .gitignore missing %q should fail", missing)
	}

	if !strings.Contains(f.Message, missing) {
		t.Errorf("message did not name the missing pattern %q: %s", missing, f.Message)
	}
}

func TestGitignorePatternSpellingsAccepted(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	// Every required pattern is present, but a couple are written in anchored,
	// directory-suffixed, and **/-prefixed spellings that must normalize equal.
	var lines []string

	for _, p := range DefaultRequiredGitignore {
		switch p {
		case ".DS_Store":
			p = "/.DS_Store"
		case ".idea/":
			p = "**/.idea/"
		default:
			// Every other pattern keeps its canonical spelling.
		}

		lines = append(lines, p)
	}

	files[".gitignore"] = "# junk\n" + strings.Join(lines, "\n") + "\n"

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "gitignore")
	if !f.OK() {
		t.Errorf("equivalent pattern spellings should pass, got: %s", f.Message)
	}
}

func TestJustfileRequiresImport(t *testing.T) {
	t.Parallel()

	// A Justfile without the shared-baseline import fails.
	files := compliantFiles()
	files["Justfile"] = "info:\n\t@echo hand-rolled\n"

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "justfile")
	if f.OK() {
		t.Fatal("a Justfile without the shared-baseline import should fail")
	}

	// The import plus any amount of the project's own content passes: the
	// root Justfile is the project's own.
	files = compliantFiles()

	files["Justfile"] = "# mine\n" + CanonicalJustfileImport + "\n\nstray:\n\t@echo x\n"
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "justfile"); !f.OK() {
		t.Errorf("a Justfile with the import and its own recipes should pass: %s", f.Message)
	}
}

func TestJustfileRequiresSharedModules(t *testing.T) {
	t.Parallel()

	mods := limen.JustModules()
	if len(mods) == 0 {
		t.Skip("no shared just modules to exercise")
	}

	first := mods[0]

	// A missing shared module fails, naming it.
	files := compliantFiles()
	delete(files, first.Path)

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "justfile")
	if f.OK() {
		t.Fatalf("a missing %s should fail", first.Path)
	}

	if !strings.Contains(f.Message, first.Path) {
		t.Errorf("message did not name the missing module: %s", f.Message)
	}

	// A shared module that drifts from the baseline fails.
	files = compliantFiles()

	files[first.Path] = first.Content + "\nextra:\n\t@echo x\n"
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "justfile"); f.OK() {
		t.Errorf("a drifted %s should fail: %s", first.Path, f.Message)
	}
}

func TestJustfileOwnRecipesNotJudged(t *testing.T) {
	t.Parallel()

	// Whatever the project puts around the import line is its own business.
	files := compliantFiles()
	files["Justfile"] = CanonicalJustfileImport + "\n\nwhatever:\n\t@echo project-specific\n"

	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "justfile"); !f.OK() {
		t.Errorf("project recipes in the root Justfile must not be judged: %s", f.Message)
	}
}

func TestAquaRequiresManifest(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "aqua.yaml")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a repo without aqua.yaml should fail the aqua rule")
	}
}

func TestAquaRequiresChecksumsFile(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "aqua-checksums.json")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("aqua.yaml without a committed aqua-checksums.json should fail")
	}

	if !strings.Contains(f.Message, "aqua-checksums.json") {
		t.Errorf("message did not name the missing file: %s", f.Message)
	}
}

func TestAquaRequiresCanonicalChecksumSection(t *testing.T) {
	t.Parallel()

	// Dropping require_checksum from the section is drift from the canonical:
	// a missing/mismatched checksum would then not fail the install.
	files := compliantFiles()
	files["aqua.yaml"] = canonicalAquaWith(t, "  require_checksum: true\n", "")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("aqua.yaml without require_checksum: true should fail")
	}

	if !strings.Contains(f.Message, "checksum") {
		t.Errorf("message did not name the checksum section: %s", f.Message)
	}
}

func TestAquaAcceptsYmlVariant(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "aqua.yaml")
	files["aqua.yml"] = limen.CanonicalAquaYAML

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if !f.OK() {
		t.Errorf("a canonical aqua.yml should pass: %s", f.Message)
	}

	if f.Path != "aqua.yml" {
		t.Errorf("path = %q, want aqua.yml", f.Path)
	}
}

// canonicalAquaLine returns the full line of the canonical aqua.yaml containing
// substr, so tests can manipulate entries without hardcoding versions (which
// Renovate bumps).
func canonicalAquaLine(t *testing.T, substr string) string {
	t.Helper()

	for _, line := range strings.Split(limen.CanonicalAquaYAML, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}

	t.Fatalf("canonical aqua.yaml has no line containing %q — update this test", substr)

	return ""
}

// TestAquaProjectOwnedParts: package versions, extra packages, and the standard
// registry ref are the project's — none of them may fail the rule.
func TestAquaProjectOwnedParts(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	justLine := canonicalAquaLine(t, "casey/just@")
	manifest := canonicalAquaWith(t, justLine, "  - name: casey/just@v99.99.99") // own version of a canonical package
	manifest = strings.Replace(manifest, "packages:", "packages:\n  - name: junegunn/fzf@v0.60.0", 1)
	manifest = replaceRef(t, manifest, "v9.9.9") // Renovate-bumped registry ref

	files["aqua.yaml"] = manifest
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("project-owned versions/packages/ref should pass: %s", f.Message)
	}
}

// replaceRef swaps the standard registry ref value in a manifest for another.
func replaceRef(t *testing.T, manifest, ref string) string {
	t.Helper()

	const anchor = "ref: "

	i := strings.Index(manifest, anchor)
	if i < 0 {
		t.Fatal("no ref: line in manifest")
	}

	end := i + len(anchor)
	for end < len(manifest) && manifest[end] != ' ' && manifest[end] != '\n' {
		end++
	}

	return manifest[:i+len(anchor)] + ref + manifest[end:]
}

func TestAquaRejectsMovingRegistryRef(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["aqua.yaml"] = replaceRef(t, limen.CanonicalAquaYAML, "main")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a branch registry ref should fail (must be an exact pin)")
	}

	if !strings.Contains(f.Message, "ref") {
		t.Errorf("message did not name the ref: %s", f.Message)
	}
}

func TestAquaRejectsExtraRegistry(t *testing.T) {
	t.Parallel()

	files := compliantFiles()

	files["aqua.yaml"] = canonicalAquaWith(
		t,
		"registries:",
		"registries:\n  - name: rogue\n    type: github_content\n    repo_owner: evil\n    repo_name: registry\n    ref: v1.0.0\n    path: registry.yaml",
	)
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua"); f.OK() {
		t.Error("an extra registry should fail (registries section is canonical)")
	}
}

func TestAquaRequiresCanonicalPackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	line := canonicalAquaLine(t, "koalaman/shellcheck@")
	files["aqua.yaml"] = canonicalAquaWith(t, line+"\n", "")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a missing canonical package should fail")
	}

	if !strings.Contains(f.Message, "koalaman/shellcheck") {
		t.Errorf("message did not name the missing package: %s", f.Message)
	}
}

func TestAquaRejectsDuplicatePackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	line := canonicalAquaLine(t, "casey/just@")
	files["aqua.yaml"] = canonicalAquaWith(t, line+"\n", line+"\n  - name: casey/just@v0.0.1\n")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("duplicate package entries should fail")
	}

	if !strings.Contains(f.Message, "duplicate") || !strings.Contains(f.Message, "casey/just") {
		t.Errorf("message did not name the duplicate: %s", f.Message)
	}
}

func TestAquaRejectsUnparseableManifest(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	// Flow-style sections are outside the shape the rule prescribes.
	files["aqua.yaml"] = "checksum: {enabled: true, require_checksum: true}\nregistries: [{type: standard, ref: v4.530.0}]\npackages: []\n"

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a flow-style manifest should fail (cannot be verified)")
	}

	if !strings.Contains(f.Message, "parsed") {
		t.Errorf("message did not say the manifest is unparseable: %s", f.Message)
	}
}

func TestAquaPinsPolicyAndRegistry(t *testing.T) {
	t.Parallel()

	// Missing aqua-policy.yaml fails.
	noPolicy := compliantFiles()
	delete(noPolicy, "aqua-policy.yaml")

	if f := findingByRule(Check(writeRepo(t, noPolicy), DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing aqua-policy.yaml should fail the aqua rule")
	}

	// A drifted aqua-policy.yaml fails (content-pinned).
	badPolicy := compliantFiles()

	badPolicy["aqua-policy.yaml"] = CanonicalAquaPolicy + "\n# local edit\n"
	if f := findingByRule(Check(writeRepo(t, badPolicy), DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a drifted aqua-policy.yaml should fail (content-pinned)")
	}

	// Missing .limen/aqua-registry.yaml fails.
	noReg := compliantFiles()
	delete(noReg, ".limen/aqua-registry.yaml")

	if f := findingByRule(Check(writeRepo(t, noReg), DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing .limen/aqua-registry.yaml should fail the aqua rule")
	}

	// A drifted registry fails.
	badReg := compliantFiles()

	badReg[".limen/aqua-registry.yaml"] = CanonicalAquaRegistry + "\n# local edit\n"
	if f := findingByRule(Check(writeRepo(t, badReg), DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a drifted .limen/aqua-registry.yaml should fail (content-pinned)")
	}
}

func TestYamlfmtConditional(t *testing.T) {
	t.Parallel()

	// No YAML anywhere: the yamlfmt rule produces no finding.
	noYAML := compliantFiles()
	for _, y := range []string{
		"aqua.yaml", "aqua-policy.yaml", ".limen/aqua-registry.yaml",
		pathWorkflowChecksum, pathActionSetupAqua, pathWorkflowCI,
	} {
		delete(noYAML, y) // remove every *.yaml/*.yml in the set
	}

	if findingByRule(Check(writeRepo(t, noYAML), DefaultPolicy()), "yamlfmt").Message != "rule not evaluated" {
		t.Error("yamlfmt rule should not appear when there is no YAML")
	}

	// A YAML file without .limen/.yamlfmt fails.
	noConfig := compliantFiles()
	delete(noConfig, ".limen/.yamlfmt")

	if f := findingByRule(Check(writeRepo(t, noConfig), DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Errorf("YAML present without .limen/.yamlfmt should fail, got: %s", f.Message)
	}

	// A .limen/.yamlfmt that differs from the canonical fails.
	wrong := compliantFiles()

	wrong[".limen/.yamlfmt"] = "formatter:\n  type: basic\n"
	if f := findingByRule(Check(writeRepo(t, wrong), DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Errorf("a .limen/.yamlfmt that differs from the canonical should fail, got: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra directive fails.
	extra := compliantFiles()

	extra[".limen/.yamlfmt"] = CanonicalYamlfmt + "\nline_ending: lf\n"
	if f := findingByRule(Check(writeRepo(t, extra), DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Error("a .limen/.yamlfmt with an extra directive should fail (content-pinned, no extras)")
	}

	// The exact canonical passes.
	exact := compliantFiles() // compliantFiles seeds the canonical .limen/.yamlfmt
	if f := findingByRule(Check(writeRepo(t, exact), DefaultPolicy()), "yamlfmt"); !f.OK() {
		t.Errorf("the exact canonical .limen/.yamlfmt should pass: %s", f.Message)
	}
}

func TestShellcheckConditional(t *testing.T) {
	t.Parallel()

	// No shell anywhere: the shellcheck rule produces no finding.
	noShell := writeRepo(t, compliantFiles())
	if findingByRule(Check(noShell, DefaultPolicy()), "shellcheck").Message != "rule not evaluated" {
		t.Error("shellcheck rule should not appear when there is no shell")
	}

	// A shell source without .limen/.shellcheckrc fails.
	files := compliantFiles()
	files["build.sh"] = "#!/bin/sh\necho hi\n"

	withShell := writeRepo(t, files)
	if f := findingByRule(Check(withShell, DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("shell present without .limen/.shellcheckrc should fail, got: %s", f.Message)
	}

	// A .limen/.shellcheckrc that differs from the canonical fails.
	files[".limen/.shellcheckrc"] = "disable=SC2034\n"
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("a .limen/.shellcheckrc that differs from the canonical should fail, got: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra directive fails.
	files[".limen/.shellcheckrc"] = CanonicalShellcheckrc + "\ndisable=SC2034\n"
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "shellcheck"); f.OK() {
		t.Error("a .limen/.shellcheckrc with an extra directive should fail (content-pinned, no extras)")
	}

	// The exact canonical passes.
	files[".limen/.shellcheckrc"] = CanonicalShellcheckrc
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "shellcheck"); !f.OK() {
		t.Errorf("the exact canonical .limen/.shellcheckrc should pass: %s", f.Message)
	}
}

func TestShellcheckDetectsShebangAndIgnoresGit(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["scripts-hook"] = "#!/usr/bin/env bash\necho hi\n" // extensionless, shebang
	dir := writeRepo(t, files)
	// A shell file inside .git must not trigger the rule on its own; here the
	// real trigger is scripts-hook, so the rule should appear and fail.
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks-sample.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if f := findingByRule(Check(dir, DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("extensionless shell shebang should be detected: %s", f.Message)
	}
}

func TestShellcheckIgnoresGitOnly(t *testing.T) {
	t.Parallel()

	// The only shell lives under .git: the rule must not fire.
	dir := writeRepo(t, compliantFiles())
	if err := os.WriteFile(filepath.Join(dir, ".git", "x.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if findingByRule(Check(dir, DefaultPolicy()), "shellcheck").Message != "rule not evaluated" {
		t.Error("shell under .git must not trigger the shellcheck rule")
	}
}

func TestDirectoryNamedLikeFileIsNotAccepted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "LICENSE"), 0o700); err != nil {
		t.Fatal(err)
	}

	f := findingByRule(Check(dir, DefaultPolicy()), "license")
	if f.OK() {
		t.Error("a directory named LICENSE should not satisfy the license rule")
	}
}

// "Counts as shell" equals "ShellCheck can lint it": sh/bash/dash/ksh shebangs
// trigger the shellcheck rule; zsh and fish must not — ShellCheck has no dialect
// for them, so the config would be dead weight — and neither may sneak in via
// their "sh" substring.
func TestShellShebangDialects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		shebang string
		want    bool
	}{
		{"#!/bin/sh", true},
		{"#!/usr/bin/env bash", true},
		{"#!/bin/dash", true},
		{"#!/usr/bin/ksh", true},
		{"#!/usr/bin/env -S bash -eu", true},
		{"#!/bin/zsh", false},
		{"#!/usr/bin/env zsh", false},
		{"#!/usr/bin/fish", false},
		{"#!/usr/bin/python3", false},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "script")
		if err := os.WriteFile(path, []byte(tc.shebang+"\necho hi\n"), 0o700); err != nil {
			t.Fatal(err)
		}

		if got := hasShellShebang(path); got != tc.want {
			t.Errorf("hasShellShebang(%q) = %v, want %v", tc.shebang, got, tc.want)
		}
	}
}

func TestWorkflowsRule(t *testing.T) {
	t.Parallel()

	// The compliant set passes (pinned pieces canonical, seeded pieces present).
	if f := findingByRule(Check(writeRepo(t, compliantFiles()), DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("compliant workflows should pass, got: %s", f.Message)
	}

	// A drifted pinned piece fails — the write-capable workflow is machinery.
	drifted := compliantFiles()
	drifted[pathWorkflowChecksum] = limen.CanonicalWorkflowUpdateAquaChecksum + "\n# local edit\n"

	if f := findingByRule(Check(writeRepo(t, drifted), DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a drifted update-aqua-checksum workflow should fail (content-pinned)")
	}

	// Seeded pieces are presence-only: any content satisfies the rule.
	custom := compliantFiles()
	custom[pathWorkflowCI] = "name: my-own-ci\n"
	custom[pathRenovate] = "{}\n"

	if f := findingByRule(Check(writeRepo(t, custom), DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("customized seeded files should pass, got: %s", f.Message)
	}

	// A missing seeded piece fails.
	missing := compliantFiles()
	delete(missing, pathWorkflowCI)

	if f := findingByRule(Check(writeRepo(t, missing), DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a missing CI workflow should fail")
	}

	// The release workflow is required exactly when goreleaser config exists.
	releasing := compliantFiles()
	releasing[".goreleaser.yaml"] = "version: 2\n"

	if f := findingByRule(Check(writeRepo(t, releasing), DefaultPolicy()), "workflows"); f.OK() {
		t.Error(".goreleaser.yaml without a release workflow should fail")
	}

	releasing[pathWorkflowRelease] = limen.CanonicalWorkflowRelease
	if f := findingByRule(Check(writeRepo(t, releasing), DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("goreleaser with a release workflow should pass, got: %s", f.Message)
	}
}

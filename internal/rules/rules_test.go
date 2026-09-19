package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
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
	files := map[string]string{
		"README.md":           "# Thing",
		"LICENSE":             mitText,
		".editorconfig":       rules.CanonicalEditorconfig,
		".gitignore":          "*.log\n", // any present .gitignore satisfies the rule
		".gitattributes":      rules.CanonicalGitattributes,
		"AGENTS.md":           rules.CanonicalAgents,
		"CLAUDE.md":           limen.CanonicalClaudeSeed,
		"Justfile":            rules.CanonicalJustfileImport + "\n",
		"aqua.yaml":           limen.CanonicalAquaYAML,
		"aqua-checksums.json": "{}\n",
		// Every repository declares the Go-built tools the recipes run (the
		// gotools rule); without a root go.mod, the everywhere set is enough.
		"tools/go.mod": goModToolsEverywhere,
		// The aqua policy, local registry, and lychee config are content-pinned exactly.
		"aqua-policy.yaml":          rules.CanonicalAquaPolicy,
		".limen/aqua-registry.yaml": rules.CanonicalAquaRegistry,
		".limen/lychee.toml":        rules.CanonicalLychee,
		// aqua.yaml is YAML, so the conditional yamlfmt rule fires; satisfy it
		// with the canonical baseline. The shellcheck config is unconditional.
		".limen/.yamlfmt":      rules.CanonicalYamlfmt,
		".limen/.shellcheckrc": rules.CanonicalShellcheckrc,
		// The .github surface: two content-pinned pieces, two seeded ones
		// (any content satisfies the seeded pair — canonical used here).
		".github/workflows/update-aqua-checksum.yaml": limen.CanonicalWorkflowUpdateAquaChecksum,
		".github/actions/setup-aqua/action.yaml":      limen.CanonicalActionSetupAqua,
		".github/workflows/ci.yaml":                   limen.CanonicalWorkflowCI,
		// The seed, with its preset reference pinned to this manifest's limen
		// version — what `limen fix` leaves behind (the renovate rule).
		"renovate.json": rules.CanonicalRenovateFor(limen.CanonicalAquaYAML),
	}
	// Every shared just module (.limen/*.just) must be present.
	for _, m := range limen.JustModules() {
		files[m.Path] = m.Content
	}

	return files
}

func findingByRule(findings []rules.Finding, rule string) rules.Finding {
	for _, f := range findings {
		if f.Rule == rule {
			return f
		}
	}

	return rules.Finding{Rule: rule, Status: rules.StatusFail, Message: "rule not evaluated"}
}

func TestCheckCompliantRepo(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, compliantFiles())

	findings := rules.Check(dir, rules.DefaultPolicy())
	if !rules.AllOK(findings) {
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

	findings := rules.Check(dir, rules.DefaultPolicy())
	if rules.AllOK(findings) {
		t.Fatal("expected failures for an empty repo")
	}

	for _, rule := range []string{
		"git", "readme", "license", "editorconfig", "gitignore", "gitattributes", "agents", "justfile", "aqua", "lychee",
	} {
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
		".editorconfig": rules.CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "license")
	if f.OK() {
		t.Fatal("expected GPL LICENSE to fail")
	}
}

// TestCheckInheritedBSD3License: a fork of a BSD-licensed upstream keeps the
// upstream's LICENSE — here with the personalized clause 3 older projects
// carry — and the check must pass it. Bootstrap still refuses to create one;
// that side of the asymmetry is pinned in the license package's tests.
func TestCheckInheritedBSD3License(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"README.md": "# Thing",
		"LICENSE": `Copyright (c) 2014-2022  Ulrich Kunitz
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice.

* My name, Ulrich Kunitz, may not be used to endorse or promote products
  derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS".`,
		".editorconfig": rules.CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "license")
	if !f.OK() {
		t.Fatalf("inherited BSD-3-Clause LICENSE failed: %s", f.Message)
	}

	if f.Message != "license BSD-3-Clause" {
		t.Errorf("license message = %q, want %q", f.Message, "license BSD-3-Clause")
	}
}

func TestCheckReadmeVariantAccepted(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"README":        "plain readme",
		"LICENSE":       mitText,
		".editorconfig": rules.CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "readme")
	if !f.OK() {
		t.Errorf("plain README should be accepted, got: %s", f.Message)
	}

	if f.Path != "README" {
		t.Errorf("path = %q, want README", f.Path)
	}
}

// TestCheckReadmeAndLicenseCaseInsensitive: readme.md and license.md are the
// canonical files under another spelling, found by their on-disk name. The
// exact lookup used to miss them on a case-sensitive filesystem, and the fixer
// then planted a second README beside the real one.
func TestCheckReadmeAndLicenseCaseInsensitive(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{
		"readme.md":     "# lower case",
		"license.md":    mitText,
		".editorconfig": rules.CanonicalEditorconfig,
		".gitignore":    "*.log",
	})

	findings := rules.Check(dir, rules.DefaultPolicy())

	readme := findingByRule(findings, "readme")
	if !readme.OK() {
		t.Errorf("readme.md should be accepted, got: %s", readme.Message)
	}

	if readme.Path != "readme.md" {
		t.Errorf("readme path = %q, want readme.md", readme.Path)
	}

	if !strings.Contains(readme.Message, "README.md is the canonical name") {
		t.Errorf("readme message should name the canonical spelling, got: %s", readme.Message)
	}

	lic := findingByRule(findings, "license")
	if !lic.OK() {
		t.Errorf("license.md should be accepted, got: %s", lic.Message)
	}

	if lic.Path != "license.md" {
		t.Errorf("license path = %q, want license.md", lic.Path)
	}

	if lic.Message != "license MIT" {
		t.Errorf("license message = %q, want %q", lic.Message, "license MIT")
	}
}

func TestEditorconfigMustMatchExactly(t *testing.T) {
	t.Parallel()

	// The exact canonical passes.
	if f := findingByRule(rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()), "editorconfig"); !f.OK() {
		t.Errorf("the exact canonical .editorconfig should pass: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra section fails.
	extra := compliantFiles()

	extra[".editorconfig"] = rules.CanonicalEditorconfig + "\n[*.lua]\nindent_size = 2\n"
	if f := findingByRule(rules.Check(writeRepo(t, extra), rules.DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("canonical + an extra section should fail (content-pinned, no extras)")
	}

	// A changed value fails.
	changed := compliantFiles()

	changed[".editorconfig"] = strings.Replace(rules.CanonicalEditorconfig, "indent_size = 2", "indent_size = 4", 1)
	if f := findingByRule(rules.Check(writeRepo(t, changed), rules.DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("a changed indent_size should fail")
	}

	// A missing section (truncated canonical) fails.
	partial := compliantFiles()

	cut := strings.Index(rules.CanonicalEditorconfig, "[*.md]")
	if cut < 0 {
		t.Fatal("canonical .editorconfig no longer contains a [*.md] section — update this test")
	}

	partial[".editorconfig"] = rules.CanonicalEditorconfig[:cut]
	if f := findingByRule(rules.Check(writeRepo(t, partial), rules.DefaultPolicy()), "editorconfig"); f.OK() {
		t.Error("a truncated .editorconfig should fail")
	}
}

func TestAgentsPinnedAndClaudeSeeded(t *testing.T) {
	t.Parallel()

	if f := findingByRule(rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()), "agents"); !f.OK() {
		t.Errorf("the canonical AGENTS.md plus a CLAUDE.md should pass: %s", f.Message)
	}

	missing := compliantFiles()
	delete(missing, "AGENTS.md")

	if f := findingByRule(rules.Check(writeRepo(t, missing), rules.DefaultPolicy()), "agents"); f.OK() {
		t.Error("a missing AGENTS.md should fail")
	}

	drifted := compliantFiles()
	drifted["AGENTS.md"] = rules.CanonicalAgents + "\n- my own rule\n"

	if f := findingByRule(rules.Check(writeRepo(t, drifted), rules.DefaultPolicy()), "agents"); f.OK() {
		t.Error("a drifted AGENTS.md should fail (content-pinned)")
	}

	noClaude := compliantFiles()
	delete(noClaude, "CLAUDE.md")

	if f := findingByRule(rules.Check(writeRepo(t, noClaude), rules.DefaultPolicy()), "agents"); f.OK() {
		t.Error("a missing CLAUDE.md should fail (the import is what makes AGENTS.md load)")
	}

	// CLAUDE.md is the project's own after the seed: any content passes.
	own := compliantFiles()
	own["CLAUDE.md"] = "@AGENTS.md\n\n## This repository\n\n- something specific\n"

	if f := findingByRule(rules.Check(writeRepo(t, own), rules.DefaultPolicy()), "agents"); !f.OK() {
		t.Errorf("a project-owned CLAUDE.md should pass: %s", f.Message)
	}
}

func TestGitattributesMustMatchExactly(t *testing.T) {
	t.Parallel()

	// The exact canonical passes.
	canonical := findingByRule(rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()), "gitattributes")
	if !canonical.OK() {
		t.Errorf("the exact canonical .gitattributes should pass: %s", canonical.Message)
	}

	// Missing fails.
	missing := compliantFiles()
	delete(missing, ".gitattributes")

	if f := findingByRule(rules.Check(writeRepo(t, missing), rules.DefaultPolicy()), "gitattributes"); f.OK() {
		t.Error("a missing .gitattributes should fail")
	}

	// Any drift fails: the file is content-pinned, extras included — an extra
	// attribute line would reintroduce the line-ending magic the pin removes.
	extra := compliantFiles()
	extra[".gitattributes"] = rules.CanonicalGitattributes + "\n*.md text\n"

	if f := findingByRule(rules.Check(writeRepo(t, extra), rules.DefaultPolicy()), "gitattributes"); f.OK() {
		t.Error("a drifted .gitattributes should fail (content-pinned)")
	}
}

func TestLycheeMustMatchExactly(t *testing.T) {
	t.Parallel()

	// The exact canonical passes.
	if f := findingByRule(rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("the exact canonical .limen/lychee.toml should pass: %s", f.Message)
	}

	// The rule is unconditional: a repo without the file fails.
	missing := compliantFiles()
	delete(missing, ".limen/lychee.toml")

	if f := findingByRule(rules.Check(writeRepo(t, missing), rules.DefaultPolicy()), "lychee"); f.OK() {
		t.Error("a missing .limen/lychee.toml should fail")
	}

	// It is content-pinned: even the canonical plus an extra setting fails — a
	// project's own exclusions belong in a root .lychee.toml.
	extra := compliantFiles()

	extra[".limen/lychee.toml"] = rules.CanonicalLychee + "\ncache = true\n"
	if f := findingByRule(rules.Check(writeRepo(t, extra), rules.DefaultPolicy()), "lychee"); f.OK() {
		t.Error("canonical + an extra setting should fail (content-pinned, no extras)")
	}

	// A root lychee.toml is the project's own: its presence changes nothing.
	own := compliantFiles()

	own[".lychee.toml"] = "exclude = ['https://example\\.internal/']\n"
	if f := findingByRule(rules.Check(writeRepo(t, own), rules.DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("a project's own root .lychee.toml should not affect the rule: %s", f.Message)
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

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "git"); f.OK() {
		t.Error("a non-git directory should fail the git rule")
	}

	// writeRepo seeds .git, so the same files now pass.
	repo := writeRepo(t, compliantFiles())
	if f := findingByRule(rules.Check(repo, rules.DefaultPolicy()), "git"); !f.OK() {
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

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "git"); !f.OK() {
		t.Errorf("a .git file (worktree) should satisfy the git rule: %s", f.Message)
	}
}

func TestGitignoreAnyContentPasses(t *testing.T) {
	t.Parallel()

	// limen only requires that a .gitignore exists; its patterns are the repo's
	// own. A file sharing nothing with the canonical baseline still passes.
	files := compliantFiles()
	files[".gitignore"] = "# the project's own\nbin/\n"

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if !f.OK() {
		t.Errorf("any present .gitignore should pass, got: %s", f.Message)
	}
}

func TestGitignoreAbsentFails(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, ".gitignore")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if f.OK() {
		t.Error("a repository with no .gitignore should fail")
	}
}

func TestJustfileRequiresImport(t *testing.T) {
	t.Parallel()

	// A Justfile without the shared-baseline import fails.
	files := compliantFiles()
	files["Justfile"] = "info:\n\t@echo hand-rolled\n"

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile")
	if f.OK() {
		t.Fatal("a Justfile without the shared-baseline import should fail")
	}

	// The import plus any amount of the project's own content passes: the
	// root Justfile is the project's own.
	files = compliantFiles()

	files["Justfile"] = "# mine\n" + rules.CanonicalJustfileImport + "\n\nstray:\n\t@echo x\n"
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile"); !f.OK() {
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile")
	if f.OK() {
		t.Fatalf("a missing %s should fail", first.Path)
	}

	if !strings.Contains(f.Message, first.Path) {
		t.Errorf("message did not name the missing module: %s", f.Message)
	}

	// A shared module that drifts from the baseline fails.
	files = compliantFiles()

	files[first.Path] = first.Content + "\nextra:\n\t@echo x\n"
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile"); f.OK() {
		t.Errorf("a drifted %s should fail: %s", first.Path, f.Message)
	}
}

func TestJustfileOwnRecipesNotJudged(t *testing.T) {
	t.Parallel()

	// Whatever the project puts around the import line is its own business.
	files := compliantFiles()
	files["Justfile"] = rules.CanonicalJustfileImport + "\n\nwhatever:\n\t@echo project-specific\n"

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile"); !f.OK() {
		t.Errorf("project recipes in the root Justfile must not be judged: %s", f.Message)
	}
}

func TestAquaRequiresManifest(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "aqua.yaml")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a repo without aqua.yaml should fail the aqua rule")
	}
}

func TestAquaRequiresChecksumsFile(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "aqua-checksums.json")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua"); !f.OK() {
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("an extra registry should fail (registries section is canonical)")
	}
}

func TestAquaRequiresCanonicalPackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	line := canonicalAquaLine(t, "koalaman/shellcheck@")
	files["aqua.yaml"] = canonicalAquaWith(t, line+"\n", "")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
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

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a flow-style manifest should fail (cannot be verified)")
	}

	if !strings.Contains(f.Message, "parsed") {
		t.Errorf("message did not say the manifest is unparseable: %s", f.Message)
	}
}

// TestAquaParserRefusesUnboundedShapes: every YAML-legal line the parser cannot
// bound as a section boundary must refuse to parse. Absorbed into the open
// section's range instead, such lines get relocated, rewritten, or deleted by
// a merge that believes it owns them — a quoted key after packages: once had
// canonical pins appended INSIDE its block, where aqua never saw them, while
// check kept reporting green — and a "packages: []" with a body panicked the
// stitch loop on overlapping replacements when a self-pin move was due.
func TestAquaParserRefusesUnboundedShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
	}{
		{"quoted top-level key", "checksum:\n  enabled: true\n\"my-user-key\":\n  - user data\n"},
		{"dotted top-level key", "checksum:\n  enabled: true\nmy.key:\n  - user data\n"},
		{"spaced top-level key", "checksum:\n  enabled: true\nmy key:\n  - user data\n"},
		{"document marker", "---\nchecksum:\n  enabled: true\n"},
		{"root-level list item", "packages:\n  - name: aaa/bbb@v1.0.0\n- stray\n"},
		{"packages: [] with a body", "packages: []\n  - name: farcloser/limen@v1.0.0\n"},
		{"shallower package entry", "packages:\n    - name: aaa/bbb@v1.0.0\n  - name: aaa/bbb@v2.0.0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := compliantFiles()
			files["aqua.yaml"] = tc.text

			f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
			if f.OK() || !strings.Contains(f.Message, "parsed") {
				t.Errorf("a shape the parser cannot bound must fail as unparseable, got: %+v", f)
			}
		})
	}

	// The tolerated neighbors of those shapes stay parseable: comments and
	// blanks at any level, and a flow-empty packages followed by only those.
	// The manifest fails for what it lacks, never for its shape.
	files := compliantFiles()
	files["aqua.yaml"] = "# c\n\npackages: []\n  # commented-out pins\n"

	if f := findingByRule(
		rules.Check(writeRepo(t, files), rules.DefaultPolicy()),
		"aqua",
	); strings.Contains(
		f.Message,
		"parsed",
	) {
		t.Errorf("comments and blank lines must not fail the parse: %s", f.Message)
	}
}

func TestAquaPinsPolicyAndRegistry(t *testing.T) {
	t.Parallel()

	// Missing aqua-policy.yaml fails.
	noPolicy := compliantFiles()
	delete(noPolicy, "aqua-policy.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, noPolicy), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing aqua-policy.yaml should fail the aqua rule")
	}

	// A drifted aqua-policy.yaml fails (content-pinned).
	badPolicy := compliantFiles()

	badPolicy["aqua-policy.yaml"] = rules.CanonicalAquaPolicy + "\n# local edit\n"
	if f := findingByRule(rules.Check(writeRepo(t, badPolicy), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a drifted aqua-policy.yaml should fail (content-pinned)")
	}

	// Missing .limen/aqua-registry.yaml fails.
	noReg := compliantFiles()
	delete(noReg, ".limen/aqua-registry.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, noReg), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing .limen/aqua-registry.yaml should fail the aqua rule")
	}

	// A drifted registry fails.
	badReg := compliantFiles()

	badReg[".limen/aqua-registry.yaml"] = rules.CanonicalAquaRegistry + "\n# local edit\n"
	if f := findingByRule(rules.Check(writeRepo(t, badReg), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a drifted .limen/aqua-registry.yaml should fail (content-pinned)")
	}
}

func TestYamlfmtConditional(t *testing.T) {
	t.Parallel()

	// No YAML anywhere: the yamlfmt rule produces no finding.
	noYAML := compliantFiles()
	for _, y := range []string{
		"aqua.yaml", "aqua-policy.yaml", ".limen/aqua-registry.yaml",
		".github/workflows/update-aqua-checksum.yaml", ".github/actions/setup-aqua/action.yaml", ".github/workflows/ci.yaml",
	} {
		delete(noYAML, y) // remove every *.yaml/*.yml in the set
	}

	if findingByRule(
		rules.Check(writeRepo(t, noYAML), rules.DefaultPolicy()),
		"yamlfmt",
	).Message != "rule not evaluated" {
		t.Error("yamlfmt rule should not appear when there is no YAML")
	}

	// A YAML file without .limen/.yamlfmt fails.
	noConfig := compliantFiles()
	delete(noConfig, ".limen/.yamlfmt")

	if f := findingByRule(rules.Check(writeRepo(t, noConfig), rules.DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Errorf("YAML present without .limen/.yamlfmt should fail, got: %s", f.Message)
	}

	// A .limen/.yamlfmt that differs from the canonical fails.
	wrong := compliantFiles()

	wrong[".limen/.yamlfmt"] = "formatter:\n  type: basic\n"
	if f := findingByRule(rules.Check(writeRepo(t, wrong), rules.DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Errorf("a .limen/.yamlfmt that differs from the canonical should fail, got: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra directive fails.
	extra := compliantFiles()

	extra[".limen/.yamlfmt"] = rules.CanonicalYamlfmt + "\nline_ending: lf\n"
	if f := findingByRule(rules.Check(writeRepo(t, extra), rules.DefaultPolicy()), "yamlfmt"); f.OK() {
		t.Error("a .limen/.yamlfmt with an extra directive should fail (content-pinned, no extras)")
	}

	// The exact canonical passes.
	exact := compliantFiles() // compliantFiles seeds the canonical .limen/.yamlfmt
	if f := findingByRule(rules.Check(writeRepo(t, exact), rules.DefaultPolicy()), "yamlfmt"); !f.OK() {
		t.Errorf("the exact canonical .limen/.yamlfmt should pass: %s", f.Message)
	}
}

func TestShellcheckContentPinned(t *testing.T) {
	t.Parallel()

	// The rule is unconditional: a repository with no shell at all still needs
	// the config, because `lint shell` passes --rcfile unconditionally and warns
	// when it cannot read it.
	files := compliantFiles()
	delete(files, ".limen/.shellcheckrc")

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("a missing .limen/.shellcheckrc must fail even with no shell, got: %s", f.Message)
	}

	// Shell present and still missing: same failure.
	files["build.sh"] = "#!/bin/sh\necho hi\n"
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("shell present without .limen/.shellcheckrc should fail, got: %s", f.Message)
	}

	// A .limen/.shellcheckrc that differs from the canonical fails.
	files[".limen/.shellcheckrc"] = "disable=SC2034\n"
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "shellcheck"); f.OK() {
		t.Errorf("a .limen/.shellcheckrc that differs from the canonical should fail, got: %s", f.Message)
	}

	// It is content-pinned: even the canonical plus an extra directive fails.
	files[".limen/.shellcheckrc"] = rules.CanonicalShellcheckrc + "\ndisable=SC2034\n"
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "shellcheck"); f.OK() {
		t.Error("a .limen/.shellcheckrc with an extra directive should fail (content-pinned, no extras)")
	}

	// The exact canonical passes.
	files[".limen/.shellcheckrc"] = rules.CanonicalShellcheckrc
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "shellcheck"); !f.OK() {
		t.Errorf("the exact canonical .limen/.shellcheckrc should pass: %s", f.Message)
	}
}

// The source walk must not look inside .git: a repository's object store and
// hooks samples are not the project's content. Exercised through the yamlfmt
// rule, the remaining conditional one (shellcheck became unconditional, so it
// no longer walks the tree at all).
func TestSourceWalkIgnoresGit(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	for name := range files {
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			delete(files, name)
		}
	}

	delete(files, ".limen/.yamlfmt")

	// The only YAML lives under .git: the rule must not fire.
	dir := writeRepo(t, files)
	if err := os.WriteFile(filepath.Join(dir, ".git", "config.yaml"), []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if findingByRule(rules.Check(dir, rules.DefaultPolicy()), "yamlfmt").Message != "rule not evaluated" {
		t.Error("YAML under .git must not trigger the yamlfmt rule")
	}
}

func TestDirectoryNamedLikeFileIsNotAccepted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "LICENSE"), 0o700); err != nil {
		t.Fatal(err)
	}

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "license")
	if f.OK() {
		t.Error("a directory named LICENSE should not satisfy the license rule")
	}
}

func TestWorkflowsRule(t *testing.T) {
	t.Parallel()

	// The compliant set passes (pinned pieces canonical, seeded pieces present).
	if f := findingByRule(rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("compliant workflows should pass, got: %s", f.Message)
	}

	// A drifted pinned piece fails — the write-capable workflow is machinery.
	drifted := compliantFiles()
	drifted[".github/workflows/update-aqua-checksum.yaml"] = limen.CanonicalWorkflowUpdateAquaChecksum + "\n# local edit\n"

	if f := findingByRule(rules.Check(writeRepo(t, drifted), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a drifted update-aqua-checksum workflow should fail (content-pinned)")
	}

	// Seeded pieces are presence-only: any content satisfies the rule.
	custom := compliantFiles()
	custom[".github/workflows/ci.yaml"] = "name: my-own-ci\n"
	custom["renovate.json"] = "{}\n"

	if f := findingByRule(rules.Check(writeRepo(t, custom), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("customized seeded files should pass, got: %s", f.Message)
	}

	// A missing seeded piece fails.
	missing := compliantFiles()
	delete(missing, ".github/workflows/ci.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, missing), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a missing CI workflow should fail")
	}

	// The release workflow is required exactly when goreleaser config exists.
	releasing := compliantFiles()
	releasing[".goreleaser.yaml"] = "version: 2\n"

	if f := findingByRule(rules.Check(writeRepo(t, releasing), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error(".goreleaser.yaml without a release workflow should fail")
	}

	releasing[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease
	if f := findingByRule(rules.Check(writeRepo(t, releasing), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("goreleaser with a release workflow should pass, got: %s", f.Message)
	}
}

package rules_test

import (
	"os"
	"path/filepath"
	"slices"
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
		"README.md":                 "# Thing",
		"LICENSE":                   mitText,
		".editorconfig":             rules.CanonicalEditorconfig,
		".gitignore":                "*.log\n" + requiredGitignore, // the project's own, plus the required patterns
		".gitattributes":            rules.CanonicalGitattributes,
		"AGENTS.md":                 rules.CanonicalAgents,
		"CLAUDE.md":                 limen.CanonicalClaudeSeed,
		".justfile":                 rules.CanonicalJustfileImport + "\n\nsecurity: do::security::default\n",
		".aqua/aqua.yaml":           limen.CanonicalAquaYAML,
		".aqua/aqua-checksums.json": "{}\n",
		// Every repository declares the Go-built tools the recipes run (the
		// gotools rule); without a root go.mod, the everywhere set is enough.
		"tools/go.mod": goModToolsEverywhere,
		// The aqua policy, local registry, and lychee config are content-pinned exactly.
		".aqua/aqua-policy.yaml":    rules.CanonicalAquaPolicy,
		".limen/aqua-registry.yaml": rules.CanonicalAquaRegistry,
		".limen/aqua.yaml":          rules.CanonicalAquaPackages,
		".limen/lychee.toml":        rules.CanonicalLychee,
		// aqua.yaml is YAML, so the conditional yamlfmt rule fires; satisfy it
		// with the canonical baseline. The shellcheck config is unconditional.
		".limen/.yamlfmt":      rules.CanonicalYamlfmt,
		".limen/.shellcheckrc": rules.CanonicalShellcheckrc,
		// The .github surface: two content-pinned pieces, two seeded ones
		// (any content satisfies the seeded pair — canonical used here).
		".github/workflows/update-aqua-checksum.yaml":     limen.CanonicalWorkflowUpdateAquaChecksum,
		".github/actions/setup-aqua/action.yaml":          limen.CanonicalActionSetupAqua,
		".github/actions/windows-cache-image/action.yaml": limen.CanonicalActionWindowsCacheImage,
		".github/workflows/limen-verify.yaml":             limen.CanonicalWorkflowVerify,
		".github/workflows/ci.yaml":                       limen.CanonicalWorkflowCI,
		".github/workflows/security.yaml":                 limen.CanonicalWorkflowSecurity,
		// The shared Renovate configuration, content-pinned, and the seed
		// extending it by the repository's name — what `limen fix` leaves
		// behind (the renovate rule).
		".limen/renovate.json": limen.CanonicalRenovatePreset,
		"renovate.json":        rules.CanonicalRenovateFor(testRepository),
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
	// project's own exclusions belong in a root .lint-links.toml.
	extra := compliantFiles()

	extra[".limen/lychee.toml"] = rules.CanonicalLychee + "\ncache = true\n"
	if f := findingByRule(rules.Check(writeRepo(t, extra), rules.DefaultPolicy()), "lychee"); f.OK() {
		t.Error("canonical + an extra setting should fail (content-pinned, no extras)")
	}

	// A root .lint-links.toml is the project's own: its presence changes nothing.
	own := compliantFiles()

	own[".lint-links.toml"] = "exclude = ['https://example\\.internal/']\n"
	if f := findingByRule(rules.Check(writeRepo(t, own), rules.DefaultPolicy()), "lychee"); !f.OK() {
		t.Errorf("a project's own root .lint-links.toml should not affect the rule: %s", f.Message)
	}

	// The overlay's former name is a stray the recipe no longer reads: it fails,
	// naming the file to rename it to.
	stray := compliantFiles()

	stray[".lychee.toml"] = "exclude = ['https://example\\.internal/']\n"

	f := findingByRule(rules.Check(writeRepo(t, stray), rules.DefaultPolicy()), "lychee")
	if f.OK() || f.Path != ".lychee.toml" || !strings.Contains(f.Message, ".lint-links.toml") {
		t.Errorf("a stray root .lychee.toml should fail naming .lint-links.toml, got %+v", f)
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

// TestGitFlagsStraySigners: the signers file under its former name,
// .allowed_signers, fails the git rule naming it, and fix renames it to
// .lint-signers, content intact; with both present fix leaves them for a
// hand merge.
func TestGitFlagsStraySigners(t *testing.T) {
	t.Parallel()

	const signers = "me@example.com namespaces=\"git\" ssh-ed25519 AAAA\n"

	files := compliantFiles()
	files[".allowed_signers"] = signers
	dir := writeRepo(t, files)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "git")
	if f.OK() || !strings.Contains(f.Message, ".lint-signers") {
		t.Fatalf("a stray .allowed_signers should fail naming .lint-signers: %v %s", f.OK(), f.Message)
	}

	if o := outcomeFor(
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"git",
	); o.Action != rules.ActionMerged {
		t.Fatalf("fix: %s (%s), want merged", o.Action, o.Message)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".lint-signers"))
	if err != nil || string(data) != signers {
		t.Fatalf(".lint-signers after the rename: %q, %v", data, err)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "git"); !f.OK() {
		t.Errorf("git rule after the rename: %s", f.Message)
	}

	files[".lint-signers"] = signers
	both := writeRepo(t, files)

	if o := outcomeFor(
		rules.Fix(t.Context(), both, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"git",
	); o.Action != rules.ActionAdvisory {
		t.Errorf("fix with both files: %s (%s), want advisory", o.Action, o.Message)
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

// requiredGitignore carries the required patterns, spelled as a project
// might: anchored or not, with or without the trailing slash.
const requiredGitignore = "build/\n**/_scratch\n/.claude\n.idea/\n"

func TestGitignoreRequiredPatterns(t *testing.T) {
	t.Parallel()

	// Beyond the required patterns, the file is the project's own: one that
	// shares nothing else with the canonical seed passes.
	files := compliantFiles()
	files[".gitignore"] = "# the project's own\nbin/\n" + requiredGitignore

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if !f.OK() {
		t.Errorf("a .gitignore with the required patterns should pass, got: %s", f.Message)
	}

	// A missing pattern fails, named; a comment or a negation does not count.
	files[".gitignore"] = "# /build\n!/_scratch\n.claude\n.idea\n"

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if f.OK() || !strings.Contains(f.Message, "/build, /_scratch") {
		t.Errorf("missing /build and /_scratch should fail naming both, got: %+v", f)
	}

	// The last matching line wins, as in git: a later negation cancels a
	// required pattern, and a pattern after the negation restores it.
	files[".gitignore"] = requiredGitignore + "!build\n"

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if f.OK() || !strings.Contains(f.Message, "/build") {
		t.Errorf("/build negated after it should fail naming it, got: %+v", f)
	}

	files[".gitignore"] = "!/build/\n" + requiredGitignore

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gitignore")
	if !f.OK() {
		t.Errorf("/build ignored again after its negation should pass, got: %s", f.Message)
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
	files[".justfile"] = "info:\n\t@echo hand-rolled\n"

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile")
	if f.OK() {
		t.Fatal("a Justfile without the shared-baseline import should fail")
	}

	// The import plus any amount of the project's own content passes: the
	// root Justfile is the project's own.
	files = compliantFiles()

	files[".justfile"] = "# mine\n" + rules.CanonicalJustfileImport + "\n\nstray:\n\t@echo x\n\nsecurity: stray\n"
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
	files[".justfile"] = rules.CanonicalJustfileImport + "\n\nwhatever:\n\t@echo project-specific\n\nsecurity *args: whatever\n"

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile"); !f.OK() {
		t.Errorf("project recipes in the root Justfile must not be judged: %s", f.Message)
	}
}

// TestJustfileRequiresSecurityRecipe: the canonical security workflow runs
// `just security`, so a Justfile without that recipe fails, and an assignment
// named security does not count.
func TestJustfileRequiresSecurityRecipe(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".justfile"] = rules.CanonicalJustfileImport + "\n\nsecurity := \"x\"\n"

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "justfile")
	if f.OK() || !strings.Contains(f.Message, "security") {
		t.Fatalf("a Justfile without a security recipe should fail naming it: %v %s", f.OK(), f.Message)
	}
}

func TestAquaRequiresManifest(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, ".aqua/aqua.yaml")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a repo without aqua.yaml should fail the aqua rule")
	}
}

func TestAquaRequiresChecksumsFile(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, ".aqua/aqua-checksums.json")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("aqua.yaml without a committed aqua-checksums.json should fail")
	}

	if !strings.Contains(f.Message, ".aqua/aqua-checksums.json") {
		t.Errorf("message did not name the missing file: %s", f.Message)
	}
}

func TestAquaRequiresCanonicalChecksumSection(t *testing.T) {
	t.Parallel()

	// Dropping require_checksum from the section is drift from the canonical:
	// a missing/mismatched checksum would then not fail the install.
	files := compliantFiles()
	files[".aqua/aqua.yaml"] = canonicalAquaWith(t, "  require_checksum: true\n", "")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("aqua.yaml without require_checksum: true should fail")
	}

	if !strings.Contains(f.Message, "checksum") {
		t.Errorf("message did not name the checksum section: %s", f.Message)
	}
}

// legacyAquaLayout is a compliant repository with aqua's files where limen
// kept them before .aqua/: at the root, the registry path relative to it.
func legacyAquaLayout(manifestName string) map[string]string {
	files := compliantFiles()

	for _, name := range []string{"aqua.yaml", "aqua-checksums.json", "aqua-policy.yaml"} {
		content := files[".aqua/"+name]
		delete(files, ".aqua/"+name)

		if name == "aqua.yaml" {
			name = manifestName
		}

		files[name] = strings.ReplaceAll(content, "../.limen/aqua-registry.yaml", ".limen/aqua-registry.yaml")
	}

	return files
}

// TestAquaLegacyRootLayout: aqua files at the root fail the rule naming the
// file, and a root aqua.yml is legacy too; fix moves the three into .aqua/,
// the registry path corrected by the merge, and the rule passes. With both
// layouts present, fix leaves them for a hand merge.
func TestAquaLegacyRootLayout(t *testing.T) {
	t.Parallel()

	for _, manifest := range []string{"aqua.yaml", "aqua.yml"} {
		dir := writeRepo(t, legacyAquaLayout(manifest))

		f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua")
		if f.OK() || !strings.Contains(f.Message, ".aqua/") {
			t.Fatalf("%s at the root should fail naming .aqua/: %v %s", manifest, f.OK(), f.Message)
		}

		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

		for _, gone := range []string{manifest, "aqua-checksums.json", "aqua-policy.yaml"} {
			if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
				t.Errorf("fix left %s at the root", gone)
			}
		}

		data, err := os.ReadFile(filepath.Join(dir, ".aqua", "aqua.yaml"))
		if err != nil || !strings.Contains(string(data), "path: ../.limen/aqua-registry.yaml") {
			t.Fatalf("moved manifest must point at ../.limen/aqua-registry.yaml: %v\n%s", err, data)
		}

		if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
			t.Errorf("after fix (%s): %s", manifest, f.Message)
		}
	}

	both := legacyAquaLayout("aqua.yaml")
	both[".aqua/aqua.yaml"] = limen.CanonicalAquaYAML

	var advised bool

	for _, o := range rules.Fix(t.Context(), writeRepo(t, both), rules.FixOptions{Policy: rules.DefaultPolicy()}) {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory && strings.Contains(o.Message, "both at the root") {
			advised = true
		}
	}

	if !advised {
		t.Error("both layouts present should leave an advisory")
	}
}

// canonicalImportLine returns the canonical aqua.yaml's import line, exactly as
// it is spelled there.
func canonicalImportLine(t *testing.T) string {
	t.Helper()

	for line := range strings.SplitSeq(limen.CanonicalAquaYAML, "\n") {
		if strings.Contains(line, "- import:") {
			return line
		}
	}

	t.Fatal("the canonical aqua.yaml has no import line — update this test")

	return ""
}

// canonicalPin returns a canonical tool's one-line pin exactly as the embedded
// tool set (.limen/aqua.yaml) spells it, and its version. Fixtures are built
// from these rather than from copied literals: a copied version is a second
// pin of the same tool that Renovate does not know about.
func canonicalPin(t *testing.T, name string) (line, version string) {
	t.Helper()

	prefix := "  - name: " + name + "@"
	for l := range strings.SplitSeq(rules.CanonicalAquaPackages, "\n") {
		if rest, ok := strings.CutPrefix(l, prefix); ok {
			version, _, _ = strings.Cut(rest, " ")

			return l + "\n", version
		}
	}

	t.Fatalf("the canonical .limen/aqua.yaml carries no one-line pin for %s", name)

	return "", ""
}

// withProjectEntries returns the canonical manifest with entries (complete
// lines) placed where a project's own packages go: above the import, which
// closes the list.
func withProjectEntries(t *testing.T, entries string) string {
	t.Helper()

	importLine := canonicalImportLine(t) + "\n"

	return strings.Replace(limen.CanonicalAquaYAML, importLine, entries+importLine, 1)
}

// withoutLineContaining returns text with the one line containing substr
// removed; it fails the test when no line matches.
func withoutLineContaining(t *testing.T, text, substr string) string {
	t.Helper()

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return strings.Join(slices.Delete(lines, i, i+1), "\n")
		}
	}

	t.Fatalf("no line containing %q — update this test", substr)

	return ""
}

// TestAquaProjectOwnedParts: extra packages, an override of a canonical tool's
// version, and the standard registry ref are the project's — none may fail the
// rule.
func TestAquaProjectOwnedParts(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	manifest := withProjectEntries(t,
		"  - name: junegunn/fzf@v0.60.0\n"+ // an extra package
			"  - name: golang/go@go99.0.0\n") // a newer go than limen pins
	manifest = replaceRef(t, manifest, "v9.9.9") // Renovate-bumped registry ref

	files[".aqua/aqua.yaml"] = manifest
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("project-owned packages/override/ref should pass: %s", f.Message)
	}
}

// TestAquaOverrideBelowImport: aqua takes a package's first declaration, so an
// override below the import would be shadowed by it. Check fails; fix moves the
// import to the end of the list, keeps the override, and the rule passes.
func TestAquaOverrideBelowImport(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".aqua/aqua.yaml"] = limen.CanonicalAquaYAML + "  - name: golang/go@go99.0.0\n"
	dir := writeRepo(t, files)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua")
	if f.OK() || !strings.Contains(f.Message, "last package entry") {
		t.Fatalf("an entry below the import must fail, got: %+v", f)
	}

	rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

	data, err := os.ReadFile(filepath.Join(dir, ".aqua", "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	got := string(data)

	override := strings.Index(got, "  - name: golang/go@go99.0.0\n")
	importAt := strings.Index(got, "  - import: ../.limen/aqua.yaml\n")

	if override < 0 || importAt < 0 || override > importAt {
		t.Errorf("fix must keep the override and move the import below it:\n%s", got)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Errorf("after fix: %s", f.Message)
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
	files[".aqua/aqua.yaml"] = replaceRef(t, limen.CanonicalAquaYAML, "main")

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

	files[".aqua/aqua.yaml"] = canonicalAquaWith(
		t,
		"registries:",
		"registries:\n  - name: rogue\n    type: github_content\n    repo_owner: evil\n    repo_name: registry\n    ref: v1.0.0\n    path: registry.yaml",
	)
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("an extra registry should fail (registries section is canonical)")
	}
}

// TestAquaRequiresCanonicalPackages: the manifest must import the canonical
// tool set; without the import every canonical tool is gone.
func TestAquaRequiresCanonicalPackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	line := canonicalImportLine(t)
	files[".aqua/aqua.yaml"] = canonicalAquaWith(t, line+"\n", "")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a missing import should fail")
	}

	if !strings.Contains(f.Message, "import ../.limen/aqua.yaml") {
		t.Errorf("message did not name the missing import: %s", f.Message)
	}
}

func TestAquaRejectsDuplicatePackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".aqua/aqua.yaml"] = withProjectEntries(t,
		"  - name: junegunn/fzf@v0.60.0\n  - name: junegunn/fzf@v0.61.0\n")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("duplicate package entries should fail")
	}

	if !strings.Contains(f.Message, "duplicate") || !strings.Contains(f.Message, "junegunn/fzf") {
		t.Errorf("message did not name the duplicate: %s", f.Message)
	}
}

func TestAquaRejectsUnparseableManifest(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	// Flow-style sections are outside the shape the rule prescribes.
	files[".aqua/aqua.yaml"] = "checksum: {enabled: true, require_checksum: true}\nregistries: [{type: standard, ref: v4.530.0}]\npackages: []\n"

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
			files[".aqua/aqua.yaml"] = tc.text

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
	files[".aqua/aqua.yaml"] = "# c\n\npackages: []\n  # commented-out pins\n"

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
	delete(noPolicy, ".aqua/aqua-policy.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, noPolicy), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing aqua-policy.yaml should fail the aqua rule")
	}

	// A drifted aqua-policy.yaml fails (content-pinned).
	badPolicy := compliantFiles()

	badPolicy[".aqua/aqua-policy.yaml"] = rules.CanonicalAquaPolicy + "\n# local edit\n"
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

	// The canonical tool set: missing or drifted fails (content-pinned).
	noPkgs := compliantFiles()
	delete(noPkgs, ".limen/aqua.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, noPkgs), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a missing .limen/aqua.yaml should fail the aqua rule")
	}

	badPkgs := compliantFiles()

	badPkgs[".limen/aqua.yaml"] = rules.CanonicalAquaPackages + "  - name: junegunn/fzf@v0.60.0\n"
	if f := findingByRule(rules.Check(writeRepo(t, badPkgs), rules.DefaultPolicy()), "aqua"); f.OK() {
		t.Error("a drifted .limen/aqua.yaml should fail (content-pinned)")
	}
}

func TestYamlfmtConditional(t *testing.T) {
	t.Parallel()

	// No YAML anywhere: the yamlfmt rule produces no finding.
	noYAML := compliantFiles()
	for _, y := range []string{
		".aqua/aqua.yaml", ".aqua/aqua-policy.yaml", ".limen/aqua-registry.yaml", ".limen/aqua.yaml",
		".github/workflows/update-aqua-checksum.yaml", ".github/actions/setup-aqua/action.yaml", ".github/workflows/ci.yaml",
		".github/actions/windows-cache-image/action.yaml",
		".github/workflows/limen-verify.yaml",
		".github/workflows/security.yaml",
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

// TestPinsRule: a repository without pins.yaml gets no finding; one whose
// digests match their versions passes; a stale digest (what a Renovate bump
// leaves until the refresh) fails naming the pin and the command; a file
// the commands could not act on fails with the parser's reason. Offline.
func TestPinsRule(t *testing.T) {
	t.Parallel()

	const current = `pins:
  - name: tool
    renovate: github-releases example/tool
    version: 2.0.0
    url: https://example.invalid/tool-${version}.tgz
    verify: download
    digest:
      version: 2.0.0
      sha256: 0000000000000000000000000000000000000000000000000000000000000000
`

	if f := findingByRule(
		rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()),
		"pins",
	); f.Message != "rule not evaluated" {
		t.Errorf("no pins.yaml must produce no finding, got: %+v", f)
	}

	files := compliantFiles()
	files["pins.yaml"] = current

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "pins"); !f.OK() {
		t.Errorf("current digests must pass: %s", f.Message)
	}

	files["pins.yaml"] = strings.Replace(current, "      version: 2.0.0", "      version: 1.0.0", 1)

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "pins")
	if f.OK() || !strings.Contains(f.Message, "tool") || !strings.Contains(f.Message, "limen pins refresh") {
		t.Errorf("a stale digest must fail naming the pin and the refresh, got: %+v", f)
	}

	files["pins.yaml"] = strings.Replace(current, "verify: download", "verify: carrier-pigeon", 1)

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "pins"); f.OK() ||
		!strings.Contains(f.Message, "carrier-pigeon") {
		t.Errorf("an unknown verify method must fail naming it, got: %+v", f)
	}

	// A method that shells out needs its tool pinned through aqua: the
	// canonical tool set pins gh and cosign, one without cosign fails naming
	// the package.
	files["pins.yaml"] = strings.Replace(current, "verify: download",
		"verify: cosign-sha256sums https://e.invalid/S https://e.invalid/B ^x$ https://e.invalid", 1)

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "pins"); !f.OK() {
		t.Errorf("a pinned verifier must pass: %s", f.Message)
	}

	files[".limen/aqua.yaml"] = withoutLineContaining(t, rules.CanonicalAquaPackages, "sigstore/cosign@")

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "pins"); f.OK() ||
		!strings.Contains(f.Message, "sigstore/cosign") {
		t.Errorf("an unpinned verifier must fail naming its package, got: %+v", f)
	}
}

// TestStaleReferences: a reference to a path limen moved, in a file the
// project owns, fails naming the file, the line and the new path; the new
// paths themselves pass, and fix leaves the project's files alone.
func TestStaleReferences(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".github/workflows/ci.yaml"] = "name: ci\n" +
		"# keyed on .aqua/aqua.yaml and .limen/aqua.yaml, which are fine\n" +
		"key: ${{ hashFiles('aqua.yaml', 'aqua-checksums.json') }}\n" +
		"# prose about the old Justfile and aqua.yaml is a comment, not a reference\n" +
		"run: just lint # the Justfile recipes\n"
	files["renovate.json"] = "{\n" +
		`  "description": [` + "\n" +
		`    "a description an earlier limen seeded, naming aqua.yaml and the Justfile"` + "\n" +
		"  ],\n" +
		`  "forkProcessing": "enabled",` + "\n" +
		`  "customManagers": [{"managerFilePatterns": ["/^Justfile$/"]}]` + "\n" +
		"}\n"
	root := writeRepo(t, files)

	f := findingByRule(rules.Check(root, rules.DefaultPolicy()), "workflows")
	for _, want := range []string{
		".github/workflows/ci.yaml:3: aqua.yaml → .aqua/aqua.yaml",
		".github/workflows/ci.yaml:3: aqua-checksums.json → .aqua/aqua-checksums.json",
		"renovate.json:6: Justfile → .justfile",
	} {
		if f.OK() || !strings.Contains(f.Message, want) {
			t.Errorf("want %q in the finding, got: %+v", want, f)
		}
	}

	// The new paths, YAML comments and renovate.json's description are not
	// references.
	for _, prose := range []string{"ci.yaml:2:", "ci.yaml:4:", "ci.yaml:5:", "renovate.json:3:"} {
		if strings.Contains(f.Message, prose) {
			t.Errorf("%s was flagged: %s", prose, f.Message)
		}
	}

	rules.Fix(t.Context(), root, bootstrapOpts())

	if data, _ := os.ReadFile(
		filepath.Join(root, ".github", "workflows", "ci.yaml"),
	); string(
		data,
	) != files[".github/workflows/ci.yaml"] {
		t.Errorf("fix edited the project's workflow:\n%s", data)
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

	// The security workflow is pinned too: a project's scans live in its recipe.
	driftedSecurity := compliantFiles()
	driftedSecurity[".github/workflows/security.yaml"] = limen.CanonicalWorkflowSecurity + "\n# local edit\n"

	if f := findingByRule(rules.Check(writeRepo(t, driftedSecurity), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a drifted security workflow should fail (content-pinned)")
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

	noSecurity := compliantFiles()
	delete(noSecurity, ".github/workflows/security.yaml")

	if f := findingByRule(rules.Check(writeRepo(t, noSecurity), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a missing security workflow should fail")
	}

	// The release workflow is required exactly when goreleaser config exists.
	releasing := compliantFiles()
	releasing[".release-go.yaml"] = releaseGoHeader + "\nversion: 2\n"

	if f := findingByRule(rules.Check(writeRepo(t, releasing), rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error(".release-go.yaml without a release workflow should fail")
	}

	releasing[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease
	if f := findingByRule(rules.Check(writeRepo(t, releasing), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Errorf("goreleaser with a release workflow should pass, got: %s", f.Message)
	}
}

// releaseGoHeader is the schema pointer .release-go.yaml opens with.
const releaseGoHeader = "# yaml-language-server: $schema=https://goreleaser.com/static/schema.json"

// TestReleaseGoNameAndHeader: a goreleaser configuration under a default name
// fails naming .release-go.yaml, as does a .release-go.yaml without its
// schema header; fix renames the first and adds the header to both, content
// intact; with both names present fix leaves them for a hand merge.
func TestReleaseGoNameAndHeader(t *testing.T) {
	t.Parallel()

	const body = "version: 2\nproject_name: x\n"

	for _, stray := range []string{".goreleaser.yaml", ".goreleaser.yml"} {
		files := compliantFiles()
		files[stray] = body
		files[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease
		dir := writeRepo(t, files)

		f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "workflows")
		if f.OK() || !strings.Contains(f.Message, ".release-go.yaml") {
			t.Fatalf("%s should fail naming .release-go.yaml: %v %s", stray, f.OK(), f.Message)
		}

		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

		data, err := os.ReadFile(filepath.Join(dir, ".release-go.yaml"))
		if err != nil || string(data) != releaseGoHeader+"\n"+body {
			t.Fatalf("after fix, .release-go.yaml: %q, %v", data, err)
		}

		if exists(filepath.Join(dir, stray)) {
			t.Errorf("fix left %s behind", stray)
		}

		if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "workflows"); !f.OK() {
			t.Errorf("after fix: %s", f.Message)
		}
	}

	headless := compliantFiles()
	headless[".release-go.yaml"] = body
	headless[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease
	dir := writeRepo(t, headless)

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "workflows"); f.OK() {
		t.Error("a .release-go.yaml without the schema header should fail")
	}

	rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

	if data, _ := os.ReadFile(filepath.Join(dir, ".release-go.yaml")); string(data) != releaseGoHeader+"\n"+body {
		t.Errorf("fix should add the header, content intact: %q", data)
	}

	both := compliantFiles()
	both[".goreleaser.yaml"] = body
	both[".release-go.yaml"] = releaseGoHeader + "\n" + body

	var advised bool

	for _, o := range rules.Fix(t.Context(), writeRepo(t, both), rules.FixOptions{Policy: rules.DefaultPolicy()}) {
		if o.Rule == "workflows" && o.Path == ".goreleaser.yaml" && o.Action == rules.ActionAdvisory {
			advised = true
		}
	}

	if !advised {
		t.Error("fix with both names should leave an advisory")
	}
}

// exists reports whether a path is present.
func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

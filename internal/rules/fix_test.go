// White-box by necessity: these tests exercise unexported check*/remediate*
// helpers and the aquaBin test seam, none of which are part of the package API.

package rules //nolint:testpackage // white-box (see above)

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/license"
)

// bootstrapOpts remediates like `limen bootstrap`: a Closed-source LICENSE is
// generated for a missing one.
func bootstrapOpts() FixOptions {
	return FixOptions{Policy: DefaultPolicy(), License: license.Closed, Holder: "Farcloser", Year: 2026}
}

func outcomeFor(outcomes []Outcome, rule string) Outcome {
	for _, o := range outcomes {
		if o.Rule == rule {
			return o
		}
	}

	return Outcome{Rule: rule, Action: ActionFailed, Message: "rule not remediated"}
}

// TestFixCreatesEverything is the bootstrap path minus `git init`: a repo that is
// only a .git directory is remediated into a fully compliant one, and Check then
// passes every rule. (The .git is pre-seeded so the test does not shell out.)
func TestFixCreatesEverything(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, nil) // just .git

	outcomes := Fix(dir, bootstrapOpts())
	if !AllResolved(outcomes) {
		for _, o := range outcomes {
			if !o.Action.resolved() {
				t.Errorf("unresolved: %s -> %s (%s)", o.Rule, o.Action, o.Message)
			}
		}
	}

	if l := outcomeFor(outcomes, "license"); l.Action != ActionCreated {
		t.Errorf("license action = %s, want created", l.Action)
	}
	// The remediated tree must satisfy Check.
	if findings := Check(dir, DefaultPolicy()); !AllOK(findings) {
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
	Fix(dir, bootstrapOpts())

	second := Fix(dir, bootstrapOpts())
	for _, o := range second {
		if o.Action != ActionNone {
			t.Errorf("second fix touched %s: %s (%s)", o.Rule, o.Action, o.Message)
		}
	}
}

func TestFixMergesGitignore(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, map[string]string{".gitignore": ".DS_Store\n"})

	o := outcomeFor(Fix(dir, bootstrapOpts()), "gitignore")
	if o.Action != ActionMerged {
		t.Fatalf("gitignore action = %s, want merged", o.Action)
	}
	// The repo's own entry is preserved and the baseline is now covered.
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(data), ".DS_Store") {
		t.Error("merge dropped the repo's own pattern")
	}

	if f := checkGitignore(dir, DefaultPolicy()); !f.OK() {
		t.Errorf("gitignore should pass after merge: %s", f.Message)
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
	if o := justfileOutcome(Fix(seeded, bootstrapOpts())); o.Action != ActionCreated {
		t.Fatalf("missing Justfile: %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(seeded, "Justfile"))
	if !strings.Contains(string(data), CanonicalJustfileImport) || !strings.HasSuffix(string(data), "\n") {
		t.Errorf("seed must carry the import and end in a newline, got: %q", data)
	}

	// Present without the import -> merged: import appended, content kept.
	ownRecipes := "greet:\n\t@echo hand-rolled\n"

	merged := writeRepo(t, map[string]string{"Justfile": ownRecipes})
	if o := justfileOutcome(Fix(merged, bootstrapOpts())); o.Action != ActionMerged {
		t.Fatalf("Justfile without the import: %s, want merged", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(merged, "Justfile"))
	if !strings.Contains(string(data), "hand-rolled") || !strings.Contains(string(data), CanonicalJustfileImport) {
		t.Errorf("merge must keep the project's recipes and add the import, got: %q", data)
	}

	// Present with the import -> the project's own, untouched.
	own := CanonicalJustfileImport + "\n\ngreet:\n\t@echo mine\n"

	untouched := writeRepo(t, map[string]string{"Justfile": own})
	if o := justfileOutcome(Fix(untouched, bootstrapOpts())); o.Action != ActionNone {
		t.Fatalf("compliant Justfile: %s, want none", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(untouched, "Justfile"))
	if string(data) != own {
		t.Error("a compliant Justfile must never be modified")
	}
}

func justfileOutcome(outcomes []Outcome) Outcome {
	for _, o := range outcomes {
		if o.Rule == "justfile" && (o.Path == "Justfile" || o.Path == "justfile" || o.Path == ".justfile") {
			return o
		}
	}

	return Outcome{Rule: "justfile", Action: ActionFailed, Message: "no root Justfile outcome"}
}

func TestFixOverwritesDriftedEditorconfig(t *testing.T) {
	t.Parallel()

	// A drifted .editorconfig (extra section) is overwritten to the canonical
	// exactly — content-pinned, no merge, no advisory.
	drifted := CanonicalEditorconfig + "\n[*.lua]\nindent_size = 2\n"
	dir := writeRepo(t, map[string]string{".editorconfig": drifted})

	o := outcomeFor(Fix(dir, bootstrapOpts()), "editorconfig")
	if o.Action != ActionOverwrote {
		t.Fatalf("editorconfig action = %s, want overwrote", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".editorconfig"))
	if string(data) != CanonicalEditorconfig {
		t.Error("editorconfig was not reset to the canonical exactly")
	}

	if f := checkEditorconfig(dir); !f.OK() {
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
	if o := outcomeFor(Fix(dir, FixOptions{Policy: DefaultPolicy()}), "license"); o.Action != ActionAdvisory {
		t.Errorf("fix on missing LICENSE = %s, want advisory", o.Action)
	}

	if _, err := os.Stat(filepath.Join(dir, "LICENSE")); err == nil {
		t.Error("fix must not create a LICENSE")
	}

	// Missing + bootstrap (License=Closed) -> created and recognized.
	dir2 := writeRepo(t, nil)
	if o := outcomeFor(Fix(dir2, bootstrapOpts()), "license"); o.Action != ActionCreated {
		t.Fatalf("bootstrap on missing LICENSE = %s, want created", o.Action)
	}

	if f := checkLicense(dir2, DefaultPolicy()); !f.OK() || f.Message != "license Closed-source" {
		t.Errorf("generated LICENSE not recognized as Closed-source: %s", f.Message)
	}

	// Present-but-disallowed -> advisory, untouched.
	dir3 := writeRepo(t, map[string]string{"LICENSE": "GNU GENERAL PUBLIC LICENSE Version 3\n"})
	if o := outcomeFor(Fix(dir3, bootstrapOpts()), "license"); o.Action != ActionAdvisory {
		t.Errorf("disallowed LICENSE = %s, want advisory", o.Action)
	}
}

func TestFixOverwritesDriftedShellcheck(t *testing.T) {
	t.Parallel()

	// Shell present, a drifted .limen/.shellcheckrc -> overwritten to the canonical
	// exactly (content-pinned: the repo's own directive is not preserved).
	dir := writeRepo(t, map[string]string{
		"build.sh":             "#!/bin/sh\necho hi\n",
		".limen/.shellcheckrc": CanonicalShellcheckrc + "\ndisable=SC2034\n",
	})

	o := outcomeFor(Fix(dir, bootstrapOpts()), "shellcheck")
	if o.Action != ActionOverwrote {
		t.Fatalf("shellcheck action = %s, want overwrote", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", ".shellcheckrc"))
	if string(data) != CanonicalShellcheckrc {
		t.Errorf("shellcheck was not reset to the canonical exactly:\n%s", data)
	}

	if f, _ := checkShellcheck(dir); !f.OK() {
		t.Errorf("shellcheck should pass after overwrite: %s", f.Message)
	}
}

func TestFixGitattributes(t *testing.T) {
	t.Parallel()

	// Missing .gitattributes -> created from the canonical (unconditional rule).
	dir := writeRepo(t, nil)

	o := outcomeFor(Fix(dir, bootstrapOpts()), "gitattributes")
	if o.Action != ActionCreated {
		t.Fatalf("gitattributes action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if string(data) != CanonicalGitattributes {
		t.Error("created .gitattributes does not equal the canonical")
	}

	// Drifted -> overwritten to the canonical exactly (content-pinned: a local
	// addition would reintroduce git's line-ending conversion).
	drifted := writeRepo(t, map[string]string{
		".gitattributes": CanonicalGitattributes + "\n*.md text\n",
	})
	if o := outcomeFor(Fix(drifted, bootstrapOpts()), "gitattributes"); o.Action != ActionOverwrote {
		t.Fatalf("gitattributes action = %s, want overwrote", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, ".gitattributes"))
	if string(data) != CanonicalGitattributes {
		t.Errorf(".gitattributes was not reset to the canonical exactly:\n%s", data)
	}

	if f := checkGitattributes(drifted); !f.OK() {
		t.Errorf("gitattributes should pass after overwrite: %s", f.Message)
	}
}

func TestFixLychee(t *testing.T) {
	t.Parallel()

	// Missing .limen/lychee.toml -> created from the canonical (unconditional rule).
	dir := writeRepo(t, nil)

	o := outcomeFor(Fix(dir, bootstrapOpts()), "lychee")
	if o.Action != ActionCreated {
		t.Fatalf("lychee action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", "lychee.toml"))
	if string(data) != CanonicalLychee {
		t.Error("created .limen/lychee.toml does not equal the canonical")
	}

	// Drifted -> overwritten to the canonical exactly (content-pinned: a local
	// addition is not preserved; project exclusions belong in a root lychee.toml).
	drifted := writeRepo(t, map[string]string{
		".limen/lychee.toml": CanonicalLychee + "\ncache = true\n",
	})
	if o := outcomeFor(Fix(drifted, bootstrapOpts()), "lychee"); o.Action != ActionOverwrote {
		t.Fatalf("lychee action = %s, want overwrote", o.Action)
	}

	data, _ = os.ReadFile(filepath.Join(drifted, ".limen", "lychee.toml"))
	if string(data) != CanonicalLychee {
		t.Errorf("lychee config was not reset to the canonical exactly:\n%s", data)
	}

	if f := checkLychee(drifted); !f.OK() {
		t.Errorf("lychee should pass after overwrite: %s", f.Message)
	}

	// A project's own root lychee.toml is never touched.
	own := "exclude = ['https://example\\.internal/']\n"
	withOwn := writeRepo(t, map[string]string{"lychee.toml": own})
	Fix(withOwn, bootstrapOpts())

	data, _ = os.ReadFile(filepath.Join(withOwn, "lychee.toml"))
	if string(data) != own {
		t.Error("fix modified the project's own root lychee.toml")
	}
}

func TestFixCreatesMissingShellcheck(t *testing.T) {
	t.Parallel()

	// Shell present, no .limen/.shellcheckrc -> created from the canonical.
	dir := writeRepo(t, map[string]string{"build.sh": "#!/bin/sh\necho hi\n"})

	o := outcomeFor(Fix(dir, bootstrapOpts()), "shellcheck")
	if o.Action != ActionCreated {
		t.Fatalf("shellcheck action = %s, want created", o.Action)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".limen", ".shellcheckrc"))
	if string(data) != CanonicalShellcheckrc {
		t.Error("created .limen/.shellcheckrc does not equal the canonical")
	}
}

// stubAqua points remediation at a stand-in aqua binary so tests exercise the
// checksum-regeneration path without network or a real aqua. The stub records
// a recognizable aqua-checksums.json — recognizably NOT limen's canonical one,
// which is the whole point: regenerated, never copied.
const stubChecksums = `{"stub":true}` + "\n"

// aquaStubEnv flips the re-executed test binary into fake-aqua mode (see
// TestMain). Helper-process pattern rather than a generated script: Windows
// cannot exec a shebang script (CreateProcess knows no shebangs, LookPath
// only recognizes PATHEXT extensions).
const aquaStubEnv = "LIMEN_TEST_AQUA_STUB"

func stubAqua(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}

	// Inherited by the children remediation spawns (it sets only cmd.Dir).
	// Setenv also forbids t.Parallel — these tests are serial by design
	// already (they mutate aquaBin).
	t.Setenv(aquaStubEnv, "1")

	prev := aquaBin
	aquaBin = self

	t.Cleanup(func() { aquaBin = prev })
}

// TestMain lets the binary double as the fake aqua (see stubAqua).
func TestMain(m *testing.M) {
	if os.Getenv(aquaStubEnv) != "" {
		// The stub's exit status IS its contract; this branch never reaches
		// the test runner's own exit handling.
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runAquaStub())
	}

	m.Run()
}

// runAquaStub mimics the two invocations remediation makes: `policy allow`
// (silent success) and `update-checksum`, which writes the stub checksums
// into the working directory (remediation sets cmd.Dir to the repo root).
func runAquaStub() int {
	for _, arg := range os.Args[1:] {
		if arg == "update-checksum" {
			if err := os.WriteFile(aquaChecksumsFile, []byte(stubChecksums), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "aqua stub: %v\n", err)

				return 1
			}
		}
	}

	return 0
}

// TestFixMergesAquaManifest: an existing manifest keeps its own packages and
// versions, gains the canonical sections and missing canonical packages without
// duplicates, and has its checksums regenerated — not copied from limen.
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixMergesAquaManifest(t *testing.T) {
	stubAqua(t)
	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "checksum:\n  enabled: false\npackages:\n  - name: junegunn/fzf@v0.60.0\n  - name: casey/just@v99.99.99\n",
	})

	outcomes := Fix(dir, bootstrapOpts())
	if !AllResolved(outcomes) {
		for _, o := range outcomes {
			if !o.Action.resolved() {
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

	if f := checkAqua(dir); !f.OK() {
		t.Errorf("aqua rule should pass after merge: %s", f.Message)
	}
	// A second fix must find nothing left to do.
	for _, o := range Fix(dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action != ActionNone {
			t.Errorf("second fix touched aqua again: %s (%s)", o.Action, o.Message)
		}
	}
}

// TestBootstrapSelfPinRelease: a release build seeds aqua.yaml with the
// farcloser/limen pin rewritten to the running version — the embedded pin
// necessarily lags one release, and seeding it verbatim would hand the repo to
// an older limen than the one that wrote its files. The rewrite forfeits the
// pristine shortcut, so checksums are regenerated, never copied.
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestBootstrapSelfPinRelease(t *testing.T) {
	stubAqua(t)

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := t.TempDir()

	outcomes := Fix(dir, opts)
	for _, o := range outcomes {
		if !o.Action.resolved() {
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

	sums, err := os.ReadFile(filepath.Join(dir, aquaChecksumsFile))
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

	Fix(dir, bootstrapOpts())

	data, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != limen.CanonicalAquaYAML {
		t.Error("a dev build must seed the embedded aqua.yaml verbatim")
	}

	sums, err := os.ReadFile(filepath.Join(dir, aquaChecksumsFile))
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
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixInsertsSelfPinAtRunningVersion(t *testing.T) {
	stubAqua(t)

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "packages:\n  - name: casey/just@v99.99.99\n",
	})

	Fix(dir, opts)

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
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixMovesExistingSelfPin(t *testing.T) {
	stubAqua(t)

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	// The canonical manifest with the limen pin at an older release — the only
	// drift is the pin itself, exercising the standalone one-line rewrite.
	dir := writeRepo(t, map[string]string{
		"aqua.yaml": strings.Join(rewriteSelfPin(strings.Split(limen.CanonicalAquaYAML, "\n"), "v0.0.1"), "\n"),
	})

	outcomes := Fix(dir, opts)
	if !AllResolved(outcomes) {
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

	sums, err := os.ReadFile(filepath.Join(dir, aquaChecksumsFile))
	if err != nil {
		t.Fatalf("aqua-checksums.json not generated: %v", err)
	}

	if string(sums) != stubChecksums {
		t.Errorf("checksums must be regenerated after the pin move:\n%s", sums)
	}
	// A second fix must find nothing left to move.
	for _, o := range Fix(dir, opts) {
		if o.Rule == "aqua" && o.Action != ActionNone {
			t.Errorf("second fix touched aqua again: %s (%s)", o.Action, o.Message)
		}
	}
}

// TestFixMovesExistingSelfPinInReplacedPackages: the pin move folds into the
// wholesale packages-section replacement when canonical packages are also
// missing — the two edits must not produce overlapping replacements.
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixMovesExistingSelfPinInReplacedPackages(t *testing.T) {
	stubAqua(t)

	opts := bootstrapOpts()
	opts.SelfVersion = "v9.9.9"

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": "packages:\n" +
			"  - name: farcloser/limen@v0.0.1 # renovate: depName=farcloser/limen\n" +
			"  - name: casey/just@v99.99.99\n",
	})

	Fix(dir, opts)

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
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixKeepsExistingSelfPinDev(t *testing.T) {
	stubAqua(t)

	dir := writeRepo(t, map[string]string{
		"aqua.yaml": strings.Join(rewriteSelfPin(strings.Split(limen.CanonicalAquaYAML, "\n"), "v0.0.1"), "\n"),
	})

	Fix(dir, bootstrapOpts())

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
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixGeneratesChecksumsForExistingManifest(t *testing.T) {
	stubAqua(t)
	dir := writeRepo(t, map[string]string{"aqua.yaml": limen.CanonicalAquaYAML})

	outcomes := Fix(dir, bootstrapOpts())
	if !AllResolved(outcomes) {
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
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixAquaUnavailableIsAdvisory(t *testing.T) {
	prev := aquaBin
	aquaBin = filepath.Join(t.TempDir(), "no-such-aqua")
	t.Cleanup(func() { aquaBin = prev })

	dir := writeRepo(t, map[string]string{"aqua.yaml": limen.CanonicalAquaYAML})

	outcomes := Fix(dir, bootstrapOpts())
	if AllResolved(outcomes) {
		t.Error("fix without a working aqua should leave the rule unresolved")
	}

	var advisory bool

	for _, o := range outcomes {
		if o.Rule == "aqua" && o.Action == ActionAdvisory {
			advisory = true
		}
	}

	if !advisory {
		t.Error("expected an advisory telling the user to run aqua themselves")
	}

	if exists(filepath.Join(dir, "aqua-checksums.json")) {
		t.Error("no checksums file should have been written")
	}
}

// TestFixLeavesUnparseableAquaAlone: a manifest fix cannot confidently parse is
// never rewritten — advisory only, file byte-identical.
func TestFixLeavesUnparseableAquaAlone(t *testing.T) {
	t.Parallel()

	const flow = "checksum: {enabled: true, require_checksum: true}\npackages: []\n"

	dir := writeRepo(t, map[string]string{"aqua.yaml": flow, "aqua-checksums.json": "{}\n"})
	outcomes := Fix(dir, bootstrapOpts())

	var advisory bool

	for _, o := range outcomes {
		if o.Rule == "aqua" && o.Action == ActionAdvisory {
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

// TestFixAquaDuplicatesAdvisory: duplicate package entries cannot be resolved
// automatically (which version would win?) — fix reports them instead.
func TestFixAquaDuplicatesAdvisory(t *testing.T) {
	t.Parallel()

	line := canonicalAquaLine(t, "casey/just@")
	manifest := strings.Replace(limen.CanonicalAquaYAML, line+"\n", line+"\n  - name: casey/just@v0.0.1\n", 1)
	dir := writeRepo(t, map[string]string{"aqua.yaml": manifest, "aqua-checksums.json": "{}\n"})

	var advisory bool

	for _, o := range Fix(dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action == ActionAdvisory {
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

	outcomes := Fix(dir, bootstrapOpts())

	for _, o := range outcomes {
		if o.Rule != "workflows" {
			continue
		}

		switch o.Path {
		case pathWorkflowChecksum:
			if o.Action != ActionOverwrote {
				t.Errorf("tampered checksum workflow: %s, want overwrote", o.Action)
			}
		case pathWorkflowCI:
			if o.Action != ActionNone {
				t.Errorf("existing ci workflow: %s, want none (left untouched)", o.Action)
			}
		case pathActionSetupAqua, pathRenovate:
			if o.Action != ActionCreated {
				t.Errorf("%s: %s, want created", o.Path, o.Action)
			}
		case pathWorkflowRelease:
			if o.Action != ActionNone {
				t.Errorf("release workflow without goreleaser: %s, want none", o.Action)
			}
		default:
			// Other rule-adjacent paths (project.just seeding): not under test.
		}
	}

	data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pathWorkflowChecksum)))
	if string(data) != limen.CanonicalWorkflowUpdateAquaChecksum {
		t.Error("checksum workflow was not reset to the canonical")
	}

	custom, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pathWorkflowCI)))
	if string(custom) != "name: my-own-ci\n" {
		t.Error("the project's own ci workflow was modified")
	}

	// goreleaser present -> the release workflow is seeded.
	releasing := writeRepo(t, map[string]string{".goreleaser.yaml": "version: 2\n"})
	for _, o := range Fix(releasing, bootstrapOpts()) {
		if o.Rule == "workflows" && o.Path == pathWorkflowRelease && o.Action != ActionCreated {
			t.Errorf("release workflow with goreleaser: %s, want created", o.Action)
		}
	}
}

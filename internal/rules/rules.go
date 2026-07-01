// Package rules verifies a repository against Farcloser's mandatory-files
// policy: every repository must carry a recognized LICENSE, an .editorconfig, a
// .gitignore, a README, a Justfile, and an aqua manifest pinning its tooling.
// The policy is documented in book/mandatory-files.md and book/tooling.md.
package rules

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/license"
)

// Well-known file names the rules act on, shared between check and fix.
const (
	gitDirName        = ".git"
	readmeFileName    = "README.md"
	licenseFileName   = "LICENSE"
	aquaChecksumsFile = "aqua-checksums.json"
)

// matchesCanonicalMsg suffixes the OK message of every content-pinned rule.
const matchesCanonicalMsg = " matches the canonical baseline"

// shebangReadLimit is how much of a file's head is enough to hold any
// realistic "#!" interpreter line.
const shebangReadLimit = 128

// CanonicalEditorconfig is the exact .editorconfig every repository must carry,
// byte for byte — the rule is content-pinned, so extra sections or edited
// values are not allowed (the canonical is comprehensive; see checkEditorconfig).
// It is this repo's own .editorconfig, embedded; editing that file updates the
// rule.
//
// Each file type uses the indentation its own tooling treats as canonical, so
// the config never fights the formatter: tabs for Go (gofmt) and Makefiles,
// four spaces for Justfiles (just --fmt) and Rust (rustfmt, 100-col), two
// spaces for the data formats (jq/prettier/yamllint) and the JS/TS, CSS, and
// HTML families (Prettier/Biome). The [*] fallback (two-space) applies to
// everything without a section of its own. Sections only ever match files that
// are present, so a repository carries the full baseline harmlessly even when
// it uses none of a given language.
var CanonicalEditorconfig = limen.CanonicalEditorconfig //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalShellcheckrc is the exact .just/.shellcheckrc a repository must carry
// verbatim (when the repo ships shell). It is this repo's .just/.shellcheckrc,
// embedded — the rule is content-pinned, so extras are not allowed.
var CanonicalShellcheckrc = limen.CanonicalShellcheckrc //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalYamlfmt is the exact .just/.yamlfmt a repository must carry verbatim
// (when the repo ships YAML). It is this repo's .just/.yamlfmt, embedded — the
// rule is content-pinned, so extras are not allowed.
var CanonicalYamlfmt = limen.CanonicalYamlfmt //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalLychee is the exact .just/lychee.toml a repository must carry
// verbatim: the canonical lychee (link checker) configuration. It is this
// repo's .just/lychee.toml, embedded — the rule is content-pinned, so extras
// are not allowed; a project's own exclusions go in a root lychee.toml, which
// is not checked.
var CanonicalLychee = limen.CanonicalLycheeToml //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalJustfile is the standard root Justfile every repository must carry
// verbatim. It is the repo-root Justfile, embedded.
var CanonicalJustfile = limen.CanonicalJustfile //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalAquaPolicy and CanonicalAquaRegistry are the canonical aqua policy
// (aqua-policy.yaml, root) and local registry (.just/aqua-registry.yaml) every
// repository must carry verbatim — the shared catalog of authorized registries
// and local tools. Unlike aqua.yaml (a per-project package list) they are
// content-pinned. They are this repo's own files, embedded.
var (
	CanonicalAquaPolicy   = limen.CanonicalAquaPolicy   //nolint:gochecknoglobals // immutable alias of embedded canonical data.
	CanonicalAquaRegistry = limen.CanonicalAquaRegistry //nolint:gochecknoglobals // immutable alias of embedded canonical data.
)

// DefaultRequiredGitignore is the set of patterns every repository's .gitignore
// must ignore: every non-comment, non-blank line of the repo-root .gitignore
// (which limen embeds), in declaration order. A repository may ignore more; it
// may not omit any of these. book/mandatory-files.md mirrors this baseline in
// prose.
var DefaultRequiredGitignore = gitignoreLines(limen.CanonicalGitignore) //nolint:gochecknoglobals // from embedded data.

// gitignoreLines returns the meaningful patterns of a .gitignore — every line
// that is neither blank nor a comment — preserving declaration order.
func gitignoreLines(text string) []string {
	var out []string

	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		out = append(out, line)
	}

	return out
}

// Status is the outcome of evaluating a single rule.
type Status string

// The two possible outcomes of a rule.
const (
	StatusOK   Status = "ok"
	StatusFail Status = "fail"
)

// Finding is the result of one rule against one repository.
type Finding struct {
	Rule    string `json:"rule"`
	Status  Status `json:"status"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// OK reports whether the finding is a pass.
func (f Finding) OK() bool { return f.Status == StatusOK }

// Policy is the configurable part of the mandatory-files rule.
type Policy struct {
	// AllowedLicenses is the set of license IDs a repository's LICENSE may be.
	AllowedLicenses []license.ID
	// RequiredGitignore is the set of patterns a repository's .gitignore must
	// ignore, each written as it would appear in the file (e.g. "_*").
	RequiredGitignore []string
}

// DefaultPolicy returns the policy described in book/mandatory-files.md: the
// allowed software licenses (MIT, Apache-2.0, AGPL-3.0, Closed-source) and
// content licenses (CC-BY-SA-4.0, CC-BY-ND-4.0). limen enforces membership in
// this set; which license to choose for a given repository is guidance in the
// book, not a machine-checkable rule.
func DefaultPolicy() Policy {
	return Policy{
		AllowedLicenses: []license.ID{
			license.MIT,
			license.Apache20,
			license.AGPL30,
			license.Closed,
			license.CCBYSA40,
			license.CCBYND40,
		},
		RequiredGitignore: DefaultRequiredGitignore,
	}
}

// Check evaluates every applicable rule against the repository rooted at root
// and returns the findings in a stable order. The mandatory rules always
// produce a finding; per-language rules (such as shellcheck) only produce one
// when their language is present in the tree.
func Check(root string, policy Policy) []Finding {
	findings := []Finding{
		checkGit(root),
		checkReadme(root),
		checkLicense(root, policy),
		checkEditorconfig(root),
		checkGitignore(root, policy),
		checkJustfile(root),
		checkAqua(root),
		checkLychee(root),
	}
	if f, ok := checkShellcheck(root); ok {
		findings = append(findings, f)
	}

	if f, ok := checkYamlfmt(root); ok {
		findings = append(findings, f)
	}

	return findings
}

// AllOK reports whether every finding passed.
func AllOK(findings []Finding) bool {
	for _, f := range findings {
		if !f.OK() {
			return false
		}
	}

	return true
}

func checkGit(root string) Finding {
	const rule = "git"
	// .git is a directory in a normal clone and a file ("gitdir: …") in a
	// worktree or submodule; either means the project root is a git repository.
	if _, err := os.Stat(filepath.Join(root, gitDirName)); err == nil {
		return Finding{Rule: rule, Status: StatusOK, Path: ".git", Message: "git repository"}
	}

	return fail(rule, "", "not a git repository (no .git in project root)")
}

func checkReadme(root string) Finding {
	const rule = "readme"

	name, ok := findFirst(root, readmeFileName, "README", "README.txt")
	if !ok {
		return fail(rule, "", "no README found (expected README.md)")
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: "README present"}
}

// checkEditorconfig content-pins the .editorconfig: it must equal the canonical
// baseline exactly (no extra sections or edited values). The canonical is
// comprehensive — it already covers every language we work in — so an exact match
// keeps editor behavior identical across every repo.
func checkEditorconfig(root string) Finding {
	const (
		rule = "editorconfig"
		name = ".editorconfig"
	)

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "no .editorconfig found")
	}

	if string(data) != CanonicalEditorconfig {
		return fail(rule, name, name+" does not match the canonical baseline (it is content-pinned; do not modify it)")
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: ".editorconfig matches the canonical baseline"}
}

// checkPinned returns a failing Finding when the file at relPath (under root) is
// missing or differs from canonical byte for byte, or nil when it matches. It is
// the shared helper for content-pinned files that are not their own rule.
func checkPinned(root, rule, relPath, canonical string) *Finding {
	data, err := readRepoFile(root, relPath)
	if err != nil {
		f := fail(rule, "", relPath+" is missing")

		return &f
	}

	if string(data) != canonical {
		f := fail(rule, relPath, relPath+" does not match the canonical baseline (content-pinned; do not modify it)")

		return &f
	}

	return nil
}

func checkGitignore(root string, policy Policy) Finding {
	const (
		rule = "gitignore"
		name = ".gitignore"
	)

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "no .gitignore found")
	}

	have := gitignorePatterns(string(data))

	var missing []string

	for _, want := range policy.RequiredGitignore {
		if !have[normalizeIgnore(want)] {
			missing = append(missing, want)
		}
	}

	if len(missing) > 0 {
		return fail(rule, name, "missing required pattern(s): "+strings.Join(missing, ", "))
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: ".gitignore present with required patterns"}
}

// gitignorePatterns returns the set of normalized patterns declared in a
// .gitignore, skipping blank lines and comments.
func gitignorePatterns(text string) map[string]bool {
	set := map[string]bool{}

	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		set[normalizeIgnore(line)] = true
	}

	return set
}

// normalizeIgnore reduces a .gitignore pattern to a canonical form so that
// equivalent spellings of the same ignore compare equal: a leading "/" or
// "**/" anchor and a trailing "/" directory marker are stripped, so ".idea/",
// "/.idea", and "**/.idea" all normalize to ".idea".
func normalizeIgnore(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	pattern = strings.TrimSuffix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "**/")

	return pattern
}

// checkJustfile content-pins the task runner: the root Justfile is the standard
// shell (identical in every repo, carrying the orientation recipes and a mod line
// per shared module), and every *.just file under .just/ is a shared module that
// must match the embedded canonical exactly. A repo's own recipes live in a root
// project.just, which is not checked.
func checkJustfile(root string) Finding {
	const rule = "justfile"

	name, ok := findFirst(root, "Justfile", "justfile", ".justfile")
	if !ok {
		return fail(rule, "", "no Justfile found")
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, name, fmt.Sprintf("could not read Justfile: %v", err))
	}

	if string(data) != CanonicalJustfile {
		return fail(
			rule,
			name,
			name+" does not match the canonical Justfile (it must be the standard shell; put project recipes in project.just)",
		)
	}

	for _, mod := range limen.JustModules() {
		mdata, err := readRepoFile(root, mod.Path)
		if err != nil {
			return fail(rule, mod.Path, "no "+mod.Path+" found (a shared just module)")
		}

		if string(mdata) != mod.Content {
			return fail(rule, mod.Path, mod.Path+" does not match the canonical baseline")
		}
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: "Justfile and shared just modules match the canonical baseline",
	}
}

// checkAqua verifies that a repository pins its build/CI tooling through aqua
// the way book/tooling.md prescribes. aqua.yaml is subset-pinned: its checksum
// section must equal the canonical baseline exactly, its registries section
// likewise except the standard registry ref (project-owned — Renovate bumps it
// per repo — but always an exact pin), and its packages must include at least
// every canonical package by name; versions and extra per-project packages are
// the project's. The generated aqua-checksums.json must be committed alongside,
// and the aqua policy and local registry are content-pinned exactly.
func checkAqua(root string) Finding {
	const rule = "aqua"

	name, ok := findFirst(root, "aqua.yaml", "aqua.yml")
	if !ok {
		return fail(rule, "", "no aqua.yaml found (project tooling must be pinned via aqua)")
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, name, fmt.Sprintf("could not read aqua.yaml: %v", err))
	}

	manifest, parsed := parseAquaManifest(string(data))
	if !parsed {
		return fail(
			rule,
			name,
			name+" could not be parsed (checksum/registries/packages must be block-style top-level sections; see book/tooling.md)",
		)
	}

	if f := checkAquaManifest(name, manifest); f != nil {
		return *f
	}

	if !exists(filepath.Join(root, aquaChecksumsFile)) {
		return fail(rule, name, "aqua-checksums.json is missing (run `aqua update-checksum` and commit it)")
	}

	if f := checkPinned(root, rule, "aqua-policy.yaml", CanonicalAquaPolicy); f != nil {
		return *f
	}

	if f := checkPinned(root, rule, ".just/aqua-registry.yaml", CanonicalAquaRegistry); f != nil {
		return *f
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: "aqua.yaml carries the canonical baseline with checksum enforcement; policy, registry, and checksums committed",
	}
}

// checkLychee content-pins .just/lychee.toml, the canonical configuration of
// the lychee link checker behind `just lint links`. It is unconditional: every
// repository carries a README, so every repository has markdown whose links can
// be checked. A project's own exclusions live in a root lychee.toml (merged by
// the recipe), which limen does not check.
func checkLychee(root string) Finding {
	const (
		rule = "lychee"
		name = ".just/lychee.toml"
	)
	if f := checkPinned(root, rule, name, CanonicalLychee); f != nil {
		return *f
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: name + matchesCanonicalMsg}
}

// checkShellcheck is a per-language rule: a project that contains shell sources
// must carry a .just/.shellcheckrc that matches the canonical baseline exactly,
// so the linter's configuration is identical everywhere. It returns ok=false when
// the project contains no shell, so the caller omits the finding entirely rather
// than reporting a rule that does not apply.
func checkShellcheck(root string) (Finding, bool) {
	const (
		rule = "shellcheck"
		name = ".just/.shellcheckrc"
	)

	shell, found := findShellSource(root)
	if !found {
		return Finding{}, false
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "shell sources present (e.g. "+shell+") but no "+name), true
	}

	if string(data) != CanonicalShellcheckrc {
		return fail(
			rule,
			name,
			name+" does not match the canonical baseline (it is content-pinned; do not modify it)",
		), true
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: "shell sources present and " + name + matchesCanonicalMsg,
	}, true
}

// findShellSource walks the tree below root and returns the path (relative to
// root) of the first shell source it finds, skipping .git and common vendored
// dependency directories. A shell source is a *.sh or *.bash file, or an
// extensionless file whose first line is a shell shebang.
func findShellSource(root string) (string, bool) {
	skip := map[string]bool{gitDirName: true, "node_modules": true, "vendor": true}

	var found string

	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are simply skipped
		}

		if entry.IsDir() {
			if path != root && skip[entry.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		if isShellSource(path, entry.Name()) {
			if rel, e := filepath.Rel(root, path); e == nil {
				found = rel
			} else {
				found = entry.Name()
			}

			return filepath.SkipAll
		}

		return nil
	})

	return found, found != ""
}

func isShellSource(path, name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sh", ".bash":
		return true
	case "":
		return hasShellShebang(path)
	default:
		return false
	}
}

// hasShellShebang reports whether the file's first line is a shebang for a
// shell ShellCheck can lint — sh, bash, dash, ksh, its supported dialects.
// "Counts as shell" deliberately means "the required shellcheck config will
// actually be used on it": a zsh (or fish) script does not trigger the rule,
// because ShellCheck has no dialect for it and the config would be dead weight.
// The interpreter is matched as a whole token (never by substring — "zsh"
// contains "sh"), resolving through "env" and skipping its flags/assignments.
func hasShellShebang(path string) bool {
	// The path comes from walking the repository under check — the tool's contract.
	file, err := os.Open(path) //nolint:gosec // G304: see above.
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close()
	}()

	buf := make([]byte, shebangReadLimit)
	n, _ := file.Read(buf)

	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}

	if !strings.HasPrefix(line, "#!") {
		return false
	}

	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return false
	}

	interp := filepath.Base(fields[0])
	if interp == "env" {
		interp = ""

		for _, arg := range fields[1:] {
			if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
				continue // env flags (-S, -i) and VAR=value assignments
			}

			interp = filepath.Base(arg)

			break
		}
	}

	switch interp {
	case "sh", "bash", "dash", "ksh":
		return true
	}

	return false
}

// checkYamlfmt is a per-language rule: a project that contains YAML must carry a
// .just/.yamlfmt that matches the canonical baseline exactly, so YAML formatting
// is identical everywhere. It returns ok=false when the project contains no YAML,
// so the caller omits the finding entirely rather than reporting a rule that does
// not apply.
func checkYamlfmt(root string) (Finding, bool) {
	const (
		rule = "yamlfmt"
		name = ".just/.yamlfmt"
	)

	yaml, found := findYAMLSource(root)
	if !found {
		return Finding{}, false
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "YAML files present (e.g. "+yaml+") but no "+name), true
	}

	if string(data) != CanonicalYamlfmt {
		return fail(
			rule,
			name,
			name+" does not match the canonical baseline (it is content-pinned; do not modify it)",
		), true
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: "YAML files present and " + name + matchesCanonicalMsg,
	}, true
}

// findYAMLSource walks the tree below root and returns the path (relative to
// root) of the first YAML file it finds, skipping .git and common vendored
// dependency directories. A YAML file is any *.yaml or *.yml.
func findYAMLSource(root string) (string, bool) {
	skip := map[string]bool{gitDirName: true, "node_modules": true, "vendor": true}

	var found string

	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are simply skipped
		}

		if entry.IsDir() {
			if path != root && skip[entry.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".yaml", ".yml":
			if rel, e := filepath.Rel(root, path); e == nil {
				found = rel
			} else {
				found = entry.Name()
			}

			return filepath.SkipAll
		}

		return nil
	})

	return found, found != ""
}

func checkLicense(root string, policy Policy) Finding {
	const rule = "license"

	name, ok := findFirst(root, licenseFileName, "LICENSE.md", "LICENSE.txt", "COPYING")
	if !ok {
		return fail(rule, "", "no LICENSE found")
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, name, fmt.Sprintf("could not read LICENSE: %v", err))
	}

	licenseID := license.Identify(string(data))
	if licenseID == license.Unknown {
		return fail(rule, name, "LICENSE is not a recognized license (allowed: "+joinIDs(policy.AllowedLicenses)+")")
	}

	if !allowed(licenseID, policy.AllowedLicenses) {
		return fail(
			rule,
			name,
			fmt.Sprintf("license %s is not allowed (allowed: %s)", licenseID, joinIDs(policy.AllowedLicenses)),
		)
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: "license " + string(licenseID)}
}

func allowed(id license.ID, set []license.ID) bool {
	return slices.Contains(set, id)
}

func joinIDs(ids []license.ID) string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = string(id)
	}

	return strings.Join(names, ", ")
}

// readRepoFile reads a file under the repository being checked or fixed. The
// path is always root plus a rule-known relative name; examining files inside
// a caller-designated repository is this tool's entire purpose, so the taint
// gosec's G304 sees is the contract, not a flaw. The raw os error already
// carries the failing path, so it travels unwrapped.
func readRepoFile(root, relPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath))) //nolint:gosec,wrapcheck // See doc comment.
}

// findFirst returns the first of names that exists directly under root.
func findFirst(root string, names ...string) (string, bool) {
	for _, name := range names {
		if exists(filepath.Join(root, name)) {
			return name, true
		}
	}

	return "", false
}

func exists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func fail(rule, path, msg string) Finding {
	return Finding{Rule: rule, Status: StatusFail, Path: path, Message: msg}
}

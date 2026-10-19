// Package rules verifies a repository against Farcloser's mandatory-files
// policy: every repository must carry a recognized LICENSE, an .editorconfig, a
// .gitignore, a .gitattributes, a README, a .justfile, and an aqua manifest
// pinning its tooling.
// The policy is documented in book/mandatory-files.md and book/tooling.md.
package rules

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/license"
)

// Well-known file names the rules act on, shared between check and fix.
const (
	gitDirName      = ".git"
	justfileName    = ".justfile"
	carriageReturn  = "\r"
	readmeFileName  = "README.md"
	licenseFileName = "LICENSE"
	ruleAqua        = "aqua"

	// The project's pins live in .hermetic/, where hermetic's own walk finds
	// them (aqua's finder never looks there: only hermetic resolves them); a
	// local registry path inside them is relative to .hermetic/. The
	// manifest and the policy carry hermetic's names; the checksums keep
	// aqua's, the library writing that file beside the config under a fixed
	// name.
	aquaDir           = ".hermetic"
	aquaManifestFile  = aquaDir + "/hermetic.yaml"
	aquaChecksumsFile = aquaDir + "/" + legacyAquaChecksums
	aquaPolicyFile    = aquaDir + "/policy.yaml"

	// The canonical tool set, content-pinned, which the manifest imports, and
	// the local registry beside it; hermetic's names, like the project's.
	aquaPackagesFile = ".limen/hermetic.yaml"
	aquaRegistryFile = ".limen/hermetic-registry.yaml"

	// The same two under aqua's names, where limen kept them before. A
	// manifest still importing the old tool set, or naming the old registry,
	// passes while the old files stand at the canonical content: fix on a
	// Renovate branch leaves the manifest alone and keeps them so, and fix on
	// main moves the manifest and removes them. The release after this one
	// retires the old names: a manifest still on them fails then.
	legacyAquaPackagesFile = ".limen/aqua.yaml"
	legacyAquaRegistryFile = ".limen/aqua-registry.yaml"

	// The same files where limen kept them before: at the repository root,
	// and then under .aqua/ with aqua's names.
	legacyAquaDir         = ".aqua"
	legacyAquaManifest    = "aqua.yaml"
	legacyAquaManifestYml = "aqua.yml"
	legacyAquaChecksums   = "aqua-checksums.json"
	legacyAquaPolicy      = "aqua-policy.yaml"

	// The .github surface (see book/mandatory-files.md): the first two are
	// content-pinned limen machinery, the rest are seeded once and then the
	// project's own.
	pathWorkflowChecksum = ".github/workflows/update-aqua-checksum.yaml"
	pathActionSetupAqua  = ".github/actions/setup-hermetic/action.yaml"
	// Where the action lived while it installed upstream aqua; a project's
	// own workflow naming it is a stale reference, and the directory a stray.
	pathActionSetupAquaOld = ".github/actions/setup-aqua"
	pathActionWinCache     = ".github/actions/windows-cache-image/action.yaml"
	pathActionCache        = ".github/actions/cache/action.yaml"
	pathWorkflowCI         = ".github/workflows/ci.yaml"
	pathWorkflowVerify     = ".github/workflows/limen-verify.yaml"
	pathWorkflowSecurity   = ".github/workflows/security.yaml"
	pathWorkflowCaches     = ".github/workflows/caches.yaml"
	pathWorkflowRelease    = ".github/workflows/release.yaml"
	pathRenovate           = "renovate.json"
)

// matchesCanonicalMsg suffixes the OK message of every content-pinned rule.
const matchesCanonicalMsg = " matches the canonical baseline"

// CanonicalEditorconfig is the exact .editorconfig every repository must carry,
// byte for byte — the rule is content-pinned, so extra sections or edited
// values are not allowed (the canonical is comprehensive; see checkEditorconfig).
// It is this repo's own .editorconfig, embedded; editing that file updates the
// rule.
//
// Each file type uses the indentation its own tooling treats as canonical, so
// the config never fights the formatter: tabs for Go (gofmt) and Makefiles,
// four spaces for .justfiles (just --fmt) and Rust (rustfmt, 100-col), two
// spaces for the data formats (jq/prettier/yamllint) and the JS/TS, CSS, and
// HTML families (Prettier/Biome). The [*] fallback (two-space) applies to
// everything without a section of its own. Sections only ever match files that
// are present, so a repository carries the full baseline harmlessly even when
// it uses none of a given language.
var CanonicalEditorconfig = limen.CanonicalEditorconfig //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalGitattributes is the exact .gitattributes every repository must
// carry verbatim: `* -text` turns off git's line-ending conversion entirely,
// so the working tree is byte-identical on every platform (a Windows checkout
// with core.autocrlf=true otherwise rewrites text files to CRLF and every
// format checker fails). LF is enforced by the pinned .editorconfig and the
// format linters, not by git magic. Vendored diffs are exempt from git's
// whitespace check, which is not a line-ending rule. It is this repo's own
// .gitattributes, embedded — the rule is content-pinned, so extras are not
// allowed.
var CanonicalGitattributes = limen.CanonicalGitattributes //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalAgents is the exact AGENTS.md every repository must carry verbatim:
// the working agreement for a coding agent (book/agents.md), identical
// everywhere so the same rules load whichever repository an agent starts in.
// It is this repo's own AGENTS.md, embedded — content-pinned, so a project's
// own notes for the agent go in CLAUDE.md, below its import line.
var CanonicalAgents = limen.CanonicalAgents //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalShellcheckrc is the exact .limen/.shellcheckrc a repository must carry
// verbatim (when the repo ships shell). It is this repo's .limen/.shellcheckrc,
// embedded — the rule is content-pinned, so extras are not allowed.
var CanonicalShellcheckrc = limen.CanonicalShellcheckrc //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalYamlfmt is the exact .limen/.yamlfmt a repository must carry verbatim
// (when the repo ships YAML). It is this repo's .limen/.yamlfmt, embedded — the
// rule is content-pinned, so extras are not allowed.
var CanonicalYamlfmt = limen.CanonicalYamlfmt //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalLintGo is the exact .limen/lint-go.yaml a Go module must carry
// verbatim: the Go lint baseline limen-lint-go renders with the project's
// root .lint-go.yaml. Content-pinned, so extras are not allowed; a project's
// carve-outs go in the overlay.
var CanonicalLintGo = limen.CanonicalLintGo //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalLychee is the exact .limen/lychee.toml a repository must carry
// verbatim: the canonical lychee (link checker) configuration. It is this
// repo's .limen/lychee.toml, embedded — the rule is content-pinned, so extras
// are not allowed; a project's own exclusions go in a root .lint-links.toml, which
// is not checked.
var CanonicalLychee = limen.CanonicalLycheeToml //nolint:gochecknoglobals // immutable alias of embedded canonical data.

// CanonicalJustfileImport is the one line every repository's root .justfile
// must carry: the import that mounts the shared baseline. The rest of the
// root .justfile is the project's own.
const CanonicalJustfileImport = "import '.limen/just/main.just'"

// CanonicalAquaPolicy, CanonicalAquaRegistry and CanonicalAquaPackages are the
// canonical policy (.hermetic/policy.yaml), local registry
// (.limen/hermetic-registry.yaml) and tool set (.limen/hermetic.yaml) every repository
// must carry verbatim. Unlike aqua.yaml (the project's manifest, which imports
// the tool set) they are content-pinned. They are this repo's own files,
// embedded.
var (
	CanonicalAquaPolicy   = limen.CanonicalAquaPolicy   //nolint:gochecknoglobals // immutable alias of embedded canonical data.
	CanonicalAquaRegistry = limen.CanonicalAquaRegistry //nolint:gochecknoglobals // immutable alias of embedded canonical data.
	CanonicalAquaPackages = limen.CanonicalAquaPackages //nolint:gochecknoglobals // immutable alias of embedded canonical data.
)

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
	// UpdateAppIdentity is the commit author address of the organization's
	// update-aqua-checksum App ("<id>+<slug>[bot]@users.noreply.github.com"),
	// resolved by the caller (see cmd/limen; the rules package is offline).
	// Empty means unknown, and the renovate rule then passes without enforcing.
	UpdateAppIdentity string
	// Repository is the repository's own "owner/name", resolved by the caller
	// from the origin remote: renovate.json extends the shared configuration
	// by that name. Empty means unknown, and the renovate rule then does not
	// enforce the reference.
	Repository string
}

// DefaultPolicy returns the policy described in book/mandatory-files.md: the
// allowed software licenses (MIT, Apache-2.0, GPL-2.0, AGPL-3.0, BSD-3-Clause
// for inherited code, Closed-source) and content licenses (CC-BY-SA-4.0,
// CC-BY-ND-4.0). limen enforces membership in this set; which license to
// choose for a given repository is guidance in the book, not a
// machine-checkable rule. BSD-3-Clause is acceptance-only: forks of
// BSD-licensed upstreams cannot relicense, so the check passes them, but
// bootstrap never offers it for new code (see license.CanGenerate).
func DefaultPolicy() Policy {
	return Policy{
		AllowedLicenses: []license.ID{
			license.MIT,
			license.Apache20,
			license.GPL20,
			license.AGPL30,
			license.BSD3,
			license.Closed,
			license.CCBYSA40,
			license.CCBYND40,
		},
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
		checkGitignore(root),
		checkTracked(root),
		checkGitattributes(root),
		checkAgents(root),
		checkJustfile(root),
		checkAqua(root),
		checkLychee(root),
		checkWorkflows(root),
		checkRenovate(root, policy),
		checkShellcheck(root),
	}

	findings = append(findings, checkGoTools(root), checkBareTools(root))

	if f, ok := checkLintGo(root); ok {
		findings = append(findings, f)
	}

	if f, ok := checkYamlfmt(root); ok {
		findings = append(findings, f)
	}

	if f, ok := checkPins(root); ok {
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
	if _, err := os.Stat(filepath.Join(root, gitDirName)); err != nil {
		return fail(rule, "", "not a git repository (no .git in project root)")
	}

	if exists(filepath.Join(root, straySignersFile)) {
		return fail(rule, straySignersFile, straySignersFile+straySignersMessage)
	}

	return Finding{Rule: rule, Status: StatusOK, Path: ".git", Message: "git repository"}
}

// signersFile lists everyone who authors commits here, email and key, in
// git's allowed-signers format; `lint commits` reads it. straySignersFile
// is its former name, which nothing reads anymore.
const (
	signersFile         = ".lint-signers"
	straySignersFile    = ".allowed_signers"
	straySignersMessage = " is the former name of " + signersFile + ", which `lint commits` reads" +
		" — rename it (limen fix does)"
)

func checkReadme(root string) Finding {
	const rule = "readme"

	name, ok := findFirstFold(root, readmeFileName, "README", "README.txt")
	if !ok {
		return fail(rule, "", "no README found (expected README.md)")
	}

	msg := "README present"
	if name != readmeFileName {
		msg = fmt.Sprintf("README present as %s (README.md is the canonical name)", name)
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: msg}
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

// checkGitignore requires a .gitignore carrying the required patterns
// (gitignoreRequired). The rest of its contents are the repository's own:
// the canonical file is only a seed (see remediateGitignore).
func checkGitignore(root string) Finding {
	const (
		rule = "gitignore"
		name = ".gitignore"
	)

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "no .gitignore found")
	}

	if missing := missingGitignorePatterns(string(data)); len(missing) > 0 {
		return fail(rule, name, ".gitignore lacks required pattern(s): "+strings.Join(missing, ", ")+
			" (limen fix appends them; see book/mandatory-files.md)")
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: ".gitignore carries the required patterns"}
}

// gitignoreRequired are the patterns every .gitignore carries: what the
// recipes and the working sessions write inside a checkout, which must never be
// committed. The rest of the canonical file is a seed, the project's to keep
// or drop.
//
//nolint:gochecknoglobals // immutable table.
var gitignoreRequired = []string{"/.build", "/_scratch", ".claude", ".idea"}

// missingGitignorePatterns lists the required patterns the file does not
// carry. Spellings are compared normalized (normalizeIgnorePattern), so
// `.idea`, `.idea/`, `/.idea` and `**/.idea` all count.
func missingGitignorePatterns(content string) []string {
	// Git applies the last line that matches: a later `!pattern` un-ignores
	// what an earlier line ignored, and a pattern after it ignores it again.
	ignored := map[string]bool{}

	for line := range strings.SplitSeq(content, "\n") {
		if pattern, negated := normalizeIgnorePattern(line); pattern != "" {
			ignored[pattern] = !negated
		}
	}

	var missing []string

	for _, required := range gitignoreRequired {
		if pattern, _ := normalizeIgnorePattern(required); !ignored[pattern] {
			missing = append(missing, required)
		}
	}

	return missing
}

// normalizeIgnorePattern drops what does not change whether the checkout-root
// path is ignored: surrounding space, a leading `/` or `**/`, a trailing `/`,
// and reports a leading `!` as a negation. A comment normalizes to "".
func normalizeIgnorePattern(line string) (pattern string, negated bool) {
	pattern = strings.TrimSpace(line)
	if strings.HasPrefix(pattern, "#") {
		return "", false
	}

	pattern, negated = strings.CutPrefix(pattern, "!")
	pattern = strings.TrimPrefix(pattern, "**/")
	pattern = strings.TrimPrefix(pattern, "/")

	return strings.TrimSuffix(pattern, "/"), negated
}

// strayJustModules lists the *.just files directly under .limen/just/ that
// are not canonical modules, in directory order: a module an earlier limen
// shipped and has since renamed or dropped (v0.0.1's _lib.just, lib.just
// since), which the fixer seeds under its new name and the old copy outlives.
// The directory is limen's alone, so a file there is never the project's.
func strayJustModules(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, ".limen", "just"))
	if err != nil {
		return nil
	}

	canonical := map[string]bool{}
	for _, mod := range limen.JustModules() {
		canonical[mod.Path] = true
	}

	var stray []string

	for _, entry := range entries {
		path := ".limen/just/" + entry.Name()
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".just") && !canonical[path] {
			stray = append(stray, path)
		}
	}

	return stray
}

// strayJustModulesMessage names them, and who removes them.
func strayJustModulesMessage(stray []string) string {
	return strings.Join(stray, ", ") + ": not a canonical module (left by an earlier limen; limen fix removes it)"
}

// checkJustfile verifies the task runner in its two regimes: the root
// .justfile is the PROJECT's own — a shim that must carry the canonical
// import line (mounting the shared baseline) and is otherwise never judged —
// while every *.just file under .limen/just/ is a shared module that must
// match the embedded canonical exactly.
func checkJustfile(root string) Finding {
	const rule = "justfile"

	if legacy, found := findFirstFold(root, legacyJustfileName); found {
		return fail(rule, legacy, legacy+legacyJustfileMessage)
	}

	name := justfileName

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "no "+name+" found")
	}

	if !containsLine(string(data), CanonicalJustfileImport) {
		return fail(
			rule,
			name,
			name+" must carry the shared-baseline import ("+CanonicalJustfileImport+") — the rest of the file is the project's own (limen fix adds it)",
		)
	}

	if !definesSecurityRecipe(string(data)) {
		return fail(
			rule,
			name,
			name+" defines no `security` recipe, and the security workflow runs `just security` (limen fix appends `"+
				securityRecipeLine+"`)",
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

	if stray := strayJustModules(root); len(stray) > 0 {
		return fail(rule, stray[0], strayJustModulesMessage(stray))
	}

	if renamed := renamedJustVariables(name, string(data)); len(renamed) > 0 {
		return fail(rule, name, renamedJustVariablesMessage(renamed))
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: ".justfile carries the shared-baseline import; the shared just modules match the canonical baseline",
	}
}

// renamedJustVariables lists, as "file:line: old → new", the shared module's
// variables the project's .justfile reads under a name limen renamed. The
// import is flat, so a root recipe or re-export may read main.just's
// variables; a rename there is a reference the project has to follow, and
// just refuses the whole file until it does ("variable not defined"), which
// no recipe can report since none runs. Comments are not references.
func renamedJustVariables(name, data string) []string {
	var renamed []string

	for index, line := range strings.Split(data, "\n") {
		// Once per line and name: a PATH expression reads the variable in
		// both branches of its conditional.
		seen := map[string]bool{}

		for _, match := range renamedJustVarRE.FindAllStringSubmatch(stripYAMLComment(line), -1) {
			if seen[match[1]] {
				continue
			}

			seen[match[1]] = true
			renamed = append(renamed, fmt.Sprintf("%s:%d: %s → %s", name, index+1, match[1], renamedJustVars[match[1]]))
		}
	}

	return renamed
}

// renamedJustVarRE finds, as whole words, the main.just variables limen
// renamed with the move to hermetic; renamedJustVars names what each is now.
var renamedJustVarRE = regexp.MustCompile(`\b(aqua_bin)\b`)

var renamedJustVars = map[string]string{ //nolint:gochecknoglobals // immutable rename table.
	"aqua_bin": "hermetic_bin",
}

// renamedJustVariablesMessage names them; the file is the project's, so the
// edit is its own.
func renamedJustVariablesMessage(renamed []string) string {
	return "references to shared just variables limen renamed (" + strings.Join(renamed, "; ") +
		"): update them by hand, the file is the project's own; just refuses it as it is (variable not defined)"
}

// legacyJustfileName is the root justfile's former name, matched
// case-insensitively as just matches it (Justfile, justfile, JUSTFILE): just
// refuses a directory holding it beside .justfile.
const (
	legacyJustfileName    = "justfile"
	legacyJustfileMessage = " is the root justfile's former name: it is " + justfileName +
		" now, beside the other dot-named tooling — rename it (limen fix does)"
)

// securityRecipeLine is the root .justfile's `security` recipe as the seed
// writes it: the shared scans, which a project extends with its own.
const securityRecipeLine = "security: do::security::default"

// securityRecipe matches a root .justfile line defining a `security` recipe,
// with or without parameters or dependencies, and not a `security :=`
// assignment.
var securityRecipe = regexp.MustCompile(`^security(\s[^:=]*)?:([^=]|$)`)

// definesSecurityRecipe reports whether a root .justfile defines `security`.
func definesSecurityRecipe(justfile string) bool {
	for raw := range strings.SplitSeq(justfile, "\n") {
		if securityRecipe.MatchString(strings.TrimSuffix(raw, carriageReturn)) {
			return true
		}
	}

	return false
}

// containsLine reports whether any line of text, trimmed, equals want.
func containsLine(text, want string) bool {
	for raw := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(strings.TrimSuffix(raw, carriageReturn)) == want {
			return true
		}
	}

	return false
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

	if stray, found := findFirst(root, legacyAquaPaths()...); found {
		return fail(
			rule,
			stray,
			stray+" belongs in "+aquaDir+"/, where limen keeps the pins hermetic resolves (limen fix moves them)",
		)
	}

	name := aquaManifestFile

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", "no "+name+" found (project tooling must be pinned in it, resolved by hermetic)")
	}

	// The canonical files under their old names, while the manifest still
	// names them: judged as if it named the new ones, provided the old files
	// stand at the canonical content, which is what keeps the pins resolving.
	text, legacyNamed, f := acceptLegacyCanonical(root, name, string(data))
	if f != nil {
		return *f
	}

	manifest, parsed := parseAquaManifest(text)
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

	for _, old := range []string{legacyAquaPackagesFile, legacyAquaRegistryFile} {
		if !slices.Contains(legacyNamed, old) && exists(filepath.Join(root, filepath.FromSlash(old))) {
			return fail(rule, old, old+" is the old name of "+legacyCanonicalTarget(old)+
				", which nothing names any more (limen fix removes it)")
		}
	}

	if !exists(filepath.Join(root, filepath.FromSlash(aquaChecksumsFile))) {
		return fail(rule, name, aquaChecksumsFile+" is missing (run `hermetic update-checksum` and commit it)")
	}

	if f := checkPinned(root, rule, aquaPolicyFile, policyFor(legacyNamed)); f != nil {
		return *f
	}

	if f := checkPinned(root, rule, aquaRegistryFile, CanonicalAquaRegistry); f != nil {
		return *f
	}

	if f := checkPinned(root, rule, aquaPackagesFile, CanonicalAquaPackages); f != nil {
		return *f
	}

	message := name + " carries the canonical baseline with checksum enforcement; policy, registry, and checksums committed"
	if len(legacyNamed) > 0 {
		message = name + " names the canonical files under their old names (" + strings.Join(legacyNamed, ", ") +
			"), kept at the canonical content until limen fix on main moves it"
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: message}
}

// policyFor is the policy a manifest needs: the canonical one, or, while the
// manifest names the local registry under its old name, the same with that
// path. The library admits a local registry only under the path the policy
// names, one path per registry name, so the policy follows the manifest for
// the one release the old names are accepted; fix writes the manifest first
// and the policy after it, from what the manifest then says.
func policyFor(legacyNamed []string) string {
	if slices.Contains(legacyNamed, legacyAquaRegistryFile) {
		return strings.ReplaceAll(CanonicalAquaPolicy, "../"+aquaRegistryFile, "../"+legacyAquaRegistryFile)
	}

	return CanonicalAquaPolicy
}

// legacyNamedBy lists the canonical files the manifest on disk names under
// their old names, in a fixed order; nothing when it names none or there is
// no manifest.
func legacyNamedBy(root string) []string {
	data, err := readRepoFile(root, aquaManifestFile)
	if err != nil {
		return nil
	}

	var named []string

	for _, old := range []string{legacyAquaRegistryFile, legacyAquaPackagesFile} {
		if strings.Contains(string(data), "../"+old) {
			named = append(named, old)
		}
	}

	return named
}

// acceptLegacyCanonical rewrites, for the check alone, a manifest's import of
// the canonical tool set and its local registry path from the old names to
// the current ones, so the rest of the rule judges the shape and not the
// spelling, and names which old files the manifest relies on. An old file
// the manifest names and that is missing or drifted from the canonical is
// a failure: the import or the registry would not resolve.
func acceptLegacyCanonical(root, name, text string) (string, []string, *Finding) {
	var named []string

	for old, current := range map[string]string{
		legacyAquaPackagesFile: aquaPackagesFile,
		legacyAquaRegistryFile: aquaRegistryFile,
	} {
		if !strings.Contains(text, "../"+old) {
			continue
		}

		canonical := CanonicalAquaPackages
		if old == legacyAquaRegistryFile {
			canonical = CanonicalAquaRegistry
		}

		data, err := readRepoFile(root, old)
		if err != nil || string(data) != canonical {
			f := fail(ruleAqua, name, name+" names "+old+", the old name of "+current+
				", and that file is missing or not the canonical: limen fix moves the manifest (on main) or restores the file")

			return text, nil, &f
		}

		named = append(named, old)
		text = strings.ReplaceAll(text, "../"+old, "../"+current)
	}

	slices.Sort(named)

	return text, named, nil
}

// readAquaManifest reads the project's manifest: .hermetic/hermetic.yaml, or a
// root one a repository not yet migrated still carries, so the rules that only
// read it do not fail twice for a move the aqua rule already reports. The
// .aqua/ layout is not a candidate: fix moves it before any reader runs, and
// no check reads the manifest for anything the import does not carry.
func readAquaManifest(root string) (data []byte, err error) {
	for _, candidate := range []string{aquaManifestFile, legacyAquaManifest, legacyAquaManifestYml} {
		if data, err = readRepoFile(root, candidate); err == nil {
			return data, nil
		}
	}

	return nil, err
}

// readAquaPins reads every pin the repository's aqua setup declares: its
// manifest and the canonical tool set the manifest imports. A rule looking
// for a package (golang/go, the verifiers) must search both — the canonical
// ones are in the import.
func readAquaPins(root string) []byte {
	manifest, _ := readAquaManifest(root)
	imported, _ := readRepoFile(root, aquaPackagesFile)

	return append(append(manifest, '\n'), imported...)
}

// legacyAquaFiles are aqua's files at the repository root, where limen kept
// them first.
var legacyAquaFiles = []string{ //nolint:gochecknoglobals // read-only list.
	legacyAquaManifest, legacyAquaManifestYml, legacyAquaChecksums, legacyAquaPolicy,
}

// legacyAquaPaths are every place limen kept the pins before .hermetic/: the
// root, and .aqua/ under aqua's names.
func legacyAquaPaths() []string {
	paths := make([]string, 0, 2*len(legacyAquaFiles))
	for _, name := range legacyAquaFiles {
		paths = append(paths, legacyAquaDir+"/"+name)
	}

	return append(paths, legacyAquaFiles...)
}

// legacyCanonicalTarget is the name a canonical file under its old name
// carries now.
func legacyCanonicalTarget(old string) string {
	if old == legacyAquaRegistryFile {
		return aquaRegistryFile
	}

	return aquaPackagesFile
}

// legacyAquaTarget is where a legacy pin file lives now.
func legacyAquaTarget(from string) string {
	switch strings.TrimPrefix(from, legacyAquaDir+"/") {
	case legacyAquaManifest, legacyAquaManifestYml:
		return aquaManifestFile
	case legacyAquaChecksums:
		return aquaChecksumsFile
	default:
		return aquaPolicyFile
	}
}

// checkGitattributes content-pins .gitattributes (see CanonicalGitattributes:
// `* -text`, no line-ending magic anywhere). It is unconditional: the failure
// it prevents is a platform property, not a language one — any repository
// checked out on Windows with core.autocrlf=true drifts to CRLF and fails
// its format checkers.
func checkGitattributes(root string) Finding {
	const (
		rule = "gitattributes"
		name = ".gitattributes"
	)
	if f := checkPinned(root, rule, name, CanonicalGitattributes); f != nil {
		return *f
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: name + matchesCanonicalMsg}
}

// checkAgents verifies the agent surface in its two regimes: AGENTS.md is
// content-pinned (the working agreement is the same in every repository —
// see CanonicalAgents), and CLAUDE.md need only exist (seeded as the one-line
// import Claude Code needs to read AGENTS.md at all; the content is the
// project's own after that: repo-specific notes for the agent go below the
// import). Unconditional: every repository is somewhere an agent may work.
func checkAgents(root string) Finding {
	const (
		rule       = "agents"
		agentsName = "AGENTS.md"
		claudeName = "CLAUDE.md"
	)

	if f := checkPinned(root, rule, agentsName, CanonicalAgents); f != nil {
		return *f
	}

	if _, err := readRepoFile(root, claudeName); err != nil {
		return fail(
			rule,
			"",
			claudeName+" is missing (the import that makes Claude Code read AGENTS.md; limen fix seeds it)",
		)
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    agentsName,
		Message: agentsName + matchesCanonicalMsg + "; " + claudeName + " present",
	}
}

// checkLychee content-pins .limen/lychee.toml, the canonical configuration of
// the lychee link checker behind `just do security links`. It is unconditional: every
// repository carries a README, so every repository has markdown whose links can
// be checked. A project's own exclusions live in a root .lint-links.toml (merged
// by the recipe), which limen does not check; the name the overlay had before,
// .lychee.toml, is a stray the recipe no longer reads.
func checkLychee(root string) Finding {
	const (
		rule = "lychee"
		name = ".limen/lychee.toml"
	)
	if f := checkPinned(root, rule, name, CanonicalLychee); f != nil {
		return *f
	}

	if exists(filepath.Join(root, strayLycheeOverlay)) {
		return fail(rule, strayLycheeOverlay, strayLycheeOverlay+strayLycheeMessage)
	}

	return Finding{Rule: rule, Status: StatusOK, Path: name, Message: name + matchesCanonicalMsg}
}

// checkWorkflows verifies the .github surface in its two regimes: the
// checksum-update workflow, the composite actions, the shared CI lanes, the
// security and caches workflows and the release-notes configuration are
// content-pinned (limen machinery — the write-capable workflow's hardening
// must never drift), while the CI workflow and the renovate config need only
// exist (limen fix seeds the canonical ones; their content is the project's
// own after that). The release workflow is required exactly when the
// repository carries a goreleaser config — releasing is opt-in.
func checkWorkflows(root string) Finding {
	const rule = "workflows"

	for _, pinned := range []struct{ path, content string }{
		{pathWorkflowChecksum, limen.CanonicalWorkflowUpdateAquaChecksum},
		{pathActionSetupAqua, limen.CanonicalActionSetupAqua},
		{pathActionWinCache, limen.CanonicalActionWindowsCacheImage},
		{pathActionCache, limen.CanonicalActionCache},
		{pathWorkflowVerify, limen.CanonicalWorkflowVerify},
		{pathWorkflowSecurity, limen.CanonicalWorkflowSecurity},
		{pathWorkflowCaches, limen.CanonicalWorkflowCaches},
		{pathReleaseNotes, limen.CanonicalReleaseNotes},
	} {
		if f := checkPinned(root, rule, pinned.path, pinned.content); f != nil {
			return *f
		}
	}

	// A directory, which exists() does not answer for.
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pathActionSetupAquaOld))); err == nil {
		return fail(rule, pathActionSetupAquaOld, pathActionSetupAquaOld+" is the action's old home, which nothing"+
			" canonical calls any more: the action is "+pathActionSetupAqua+" (limen fix removes the old directory)")
	}

	for _, seeded := range []string{pathWorkflowCI, pathRenovate} {
		if !exists(filepath.Join(root, filepath.FromSlash(seeded))) {
			return fail(
				rule,
				"",
				"no "+seeded+" (limen fix seeds the canonical one; the content is the project's afterwards)",
			)
		}
	}

	if tag, unedited := staleCISeed(root); unedited {
		return fail(rule, pathWorkflowCI, pathWorkflowCI+" is limen "+tag+"'s seed, never edited: "+
			"limen fix replaces it with the current one, which calls "+pathWorkflowVerify)
	}

	if f := checkReleaseGo(root, rule); f != nil {
		return *f
	}

	if f := checkReleasedCommands(root, rule); f != nil {
		return *f
	}

	if exists(filepath.Join(root, formerLintGithubFile)) {
		return fail(rule, formerLintGithubFile, formerLintGithubMessage)
	}

	if stale := staleReferences(root); len(stale) > 0 {
		return fail(rule, "", staleReferencesMessage(stale))
	}

	if exists(filepath.Join(root, releaseGoFile)) &&
		!exists(filepath.Join(root, filepath.FromSlash(pathWorkflowRelease))) {
		return fail(
			rule,
			"",
			releaseGoFile+" is present but "+pathWorkflowRelease+" is missing (limen fix seeds it)",
		)
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    pathWorkflowCI,
		Message: "workflows present; the canonical pieces match the baseline",
	}
}

// movedPaths maps each path limen has moved to where it lives now. fix moves
// the file; a reference to the old path in a file the project owns (a
// Renovate manager matching ^Justfile$, a CI cache key hashing the old
// manifest, a job calling the action under its old name) then matches
// nothing, silently: the manager stops proposing, the key never changes, the
// job fails on a missing action.
//
//nolint:gochecknoglobals // immutable table.
var movedPaths = map[string]string{
	"aqua.yaml":                 aquaManifestFile,
	"aqua-checksums.json":       aquaChecksumsFile,
	"aqua-policy.yaml":          aquaPolicyFile,
	".aqua/aqua.yaml":           aquaManifestFile,
	".aqua/aqua-checksums.json": aquaChecksumsFile,
	".aqua/aqua-policy.yaml":    aquaPolicyFile,
	legacyAquaPackagesFile:      aquaPackagesFile,
	legacyAquaRegistryFile:      aquaRegistryFile,
	"actions/setup-aqua":        "actions/setup-hermetic",
	"Justfile":                  justfileName,
	".allowed_signers":          signersFile,
	".goreleaser.yaml":          releaseGoFile,
	".goreleaser.yml":           releaseGoFile,
	formerLintGithubFile:        lintGithubFile,
}

// movedPathRE finds a moved path as a whole name: what precedes it is no part
// of a path, so `.limen/hermetic.yaml` is not a reference to the old root
// manifest, while `.aqua/…` and `.limen/aqua…` are matched with their
// directory, the whole of the old location. A name escaped inside a regular
// expression (aqua\.yaml) is not recognized.
var movedPathRE = regexp.MustCompile(
	`(?:^|[^\w./-])(\.aqua/aqua\.yaml|\.aqua/aqua-checksums\.json|\.aqua/aqua-policy\.yaml` +
		`|\.limen/aqua\.yaml|\.limen/aqua-registry\.yaml` +
		`|aqua\.yaml|aqua-checksums\.json|aqua-policy\.yaml|Justfile|\.allowed_signers|\.goreleaser\.ya?ml|limen\.yaml)\b`,
)

// movedActionRE finds the setup action under its old name, which a workflow
// always names inside a path (`./.github/actions/setup-aqua`), so the
// whole-name guard above cannot carry it.
var movedActionRE = regexp.MustCompile(`\b(actions/setup-aqua)\b`)

// staleReferences lists, as "file:line: old → new", the references to moved
// paths in the files a project owns that name paths: renovate.json and its
// workflows, the content-pinned checksum workflow excepted.
func staleReferences(root string) []string {
	files := []string{pathRenovate}

	workflows, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	for _, workflow := range workflows {
		rel := filepath.ToSlash(strings.TrimPrefix(workflow, root+string(filepath.Separator)))
		if rel != pathWorkflowChecksum {
			files = append(files, rel)
		}
	}

	var stale []string

	for _, name := range files {
		data, err := readRepoFile(root, name)
		if err != nil {
			continue
		}

		for index, line := range referenceLines(name, string(data)) {
			for _, re := range []*regexp.Regexp{movedPathRE, movedActionRE} {
				for _, match := range re.FindAllStringSubmatch(line, -1) {
					stale = append(stale, fmt.Sprintf("%s:%d: %s → %s", name, index+1, match[1], movedPaths[match[1]]))
				}
			}
		}
	}

	return stale
}

// referenceLines is the file's lines with prose blanked, one per line so
// numbers stay true: a workflow's YAML comments, and renovate.json's
// description array. Prose naming an old path (a seeded comment, the
// description an earlier limen wrote) misleads nobody's tooling; a manager
// pattern or a cache key does.
func referenceLines(name, data string) []string {
	lines := strings.Split(data, "\n")
	inDescription := false

	for index, line := range lines {
		switch {
		case name != pathRenovate:
			lines[index] = stripYAMLComment(line)
		case inDescription:
			inDescription = !strings.Contains(line, "]")
			lines[index] = ""
		case strings.Contains(line, `"`+descriptionKey+`"`):
			inDescription = strings.Contains(line, "[") && !strings.Contains(line, "]")
			lines[index] = ""
		}
	}

	return lines
}

// stripYAMLComment cuts a YAML comment off a line: a # at the start or after
// whitespace, outside quotes.
func stripYAMLComment(line string) string {
	var quote rune

	for index, char := range line {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#' && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t'):
			return line[:index]
		}
	}

	return line
}

// staleReferencesMessage names them; the files are the project's, so the
// edit is its own.
func staleReferencesMessage(stale []string) string {
	return "references to paths limen moved (" + strings.Join(stale, "; ") +
		"): update them by hand, these files are the project's own"
}

// releaseGoFile is the project's goreleaser configuration, named for its
// lane like .lint-go.yaml; the release recipes pass it with --config. Away
// from goreleaser's default name, an editor finds the schema only through the
// header on its first line.
const (
	releaseGoFile   = ".release-go.yaml"
	releaseGoHeader = "# yaml-language-server: $schema=https://goreleaser.com/static/schema.json"
)

// lintGithubFile is the project's exceptions to `limen github check`, one
// `check: reason` per line. formerLintGithubFile is its former name, whose
// entries sat indented under a `github:` line; the audit refuses to run while
// it is there rather than read no exceptions.
const (
	lintGithubFile          = ".lint-github.yaml"
	formerLintGithubFile    = "limen.yaml"
	formerLintGithubMessage = formerLintGithubFile + " is the former name of " + lintGithubFile +
		", which `limen github check` reads — rename it and drop its github: line (limen fix does)"
)

// strayGoreleaserFiles are goreleaser's default names, which nothing reads
// anymore.
var strayGoreleaserFiles = []string{".goreleaser.yaml", ".goreleaser.yml"} //nolint:gochecknoglobals // read-only list.

// checkReleaseGo fails a goreleaser configuration under a default name, or a
// .release-go.yaml without its schema header or the canonical `changelog:`
// section; nil when none applies
// (releasing is opt-in).
func checkReleaseGo(root, rule string) *Finding {
	if stray, found := findFirst(root, strayGoreleaserFiles...); found {
		f := fail(rule, stray, stray+" is goreleaser's default name, which nothing reads: the release lane reads "+
			releaseGoFile+" — rename it and add the schema line (limen fix does)")

		return &f
	}

	data, err := readRepoFile(root, releaseGoFile)
	if err != nil {
		return nil
	}

	if first, _, _ := strings.Cut(string(data), "\n"); strings.TrimSuffix(first, carriageReturn) != releaseGoHeader {
		f := fail(rule, releaseGoFile, releaseGoFile+" must open with `"+releaseGoHeader+
			"`: without goreleaser's own filename, the editor finds the schema only through it (limen fix adds it)")

		return &f
	}

	if why, drifted := changelogDrift(string(data)); drifted {
		f := fail(rule, releaseGoFile, releaseGoFile+" "+why+": a release's notes are GitHub's, from the titles "+
			"of the pull requests it merged (limen fix sets it)")

		return &f
	}

	return nil
}

// checkShellcheck requires .limen/.shellcheckrc in every repository, matching
// the canonical baseline exactly, so the linter's configuration is identical
// everywhere.
//
// Deliberately unconditional, unlike the YAML twin below. Gating it on "does
// this repo currently contain shell" made the file appear the day someone
// added a first script — by which point `lint shell` had already been warning
// `unable to read --rcfile .limen/.shellcheckrc`, which reads like a broken
// setup rather than a rule that does not apply yet. Projects grow shell; a
// baseline config that is simply always there is one less thing to explain,
// and an unused config file costs nothing.
func checkShellcheck(root string) Finding {
	const (
		rule = "shellcheck"
		name = ".limen/.shellcheckrc"
	)

	data, err := readRepoFile(root, name)
	if err != nil {
		return fail(rule, "", name+" is missing")
	}

	if string(data) != CanonicalShellcheckrc {
		return fail(
			rule,
			name,
			name+" does not match the canonical baseline (it is content-pinned; do not modify it)",
		)
	}

	return Finding{
		Rule:    rule,
		Status:  StatusOK,
		Path:    name,
		Message: name + matchesCanonicalMsg,
	}
}

// skippedDirs is every directory name the per-language source walkers prune:
// .git, vendored dependencies, and the generated/build output the canonical
// .gitignore excludes — a generated file must not be what triggers a
// per-language rule. Name-based (any depth): a superset of the gitignore's
// root-anchored entries, deliberately.
func skippedDirs() map[string]bool {
	return map[string]bool{
		gitDirName:     true,
		"node_modules": true,
		"vendor":       true,
		".build":       true,
		"tmp":          true,
		"target":       true,
		".svelte-kit":  true,
		".vite":        true,
		"_scratch":     true,
		".idea":        true,
		".vscode":      true,
	}
}

// checkYamlfmt is a per-language rule: a project that contains YAML must carry a
// .limen/.yamlfmt that matches the canonical baseline exactly, so YAML formatting
// is identical everywhere. It returns ok=false when the project contains no YAML,
// so the caller omits the finding entirely rather than reporting a rule that does
// not apply.
func checkYamlfmt(root string) (Finding, bool) {
	const (
		rule = "yamlfmt"
		name = ".limen/.yamlfmt"
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
// root) of the first YAML file it finds, skipping .git, vendored, and
// generated directories (skippedDirs). A YAML file is any *.yaml or *.yml.
func findYAMLSource(root string) (string, bool) {
	skip := skippedDirs()

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

	name, ok := findFirstFold(root, licenseFileName, "LICENSE.md", "LICENSE.txt", "COPYING")
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
	// #nosec G304 -- See doc comment.
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath))) //nolint:wrapcheck // See doc comment.
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

// findFirstFold is findFirst matched case-insensitively, returning the name as
// it is on disk. Only for files whose readers do not care about case (a
// README, a LICENSE) — never for one a tool looks up by exact name (aqua.yaml,
// .release-go.yaml), where a folded match would pass a file the tool ignores.
// The on-disk name comes from the directory listing, not from a Stat of the
// wanted name: on a case-insensitive filesystem that Stat succeeds for
// readme.md too and would report README.md. The exact spelling wins when a
// case-sensitive filesystem holds both.
func findFirstFold(root string, names ...string) (string, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}

	for _, name := range names {
		if found := foldMatch(root, entries, name); found != "" {
			return found, true
		}
	}

	return "", false
}

// foldMatch is the entry that is a file and spells name, the exact spelling
// preferred over a case-insensitive one; "" when none does.
func foldMatch(root string, entries []fs.DirEntry, name string) string {
	found := ""

	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), name) || !exists(filepath.Join(root, entry.Name())) {
			continue
		}

		if entry.Name() == name {
			return name
		}

		if found == "" {
			found = entry.Name()
		}
	}

	return found
}

func exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func fail(rule, path, msg string) Finding {
	return Finding{Rule: rule, Status: StatusFail, Path: path, Message: msg}
}

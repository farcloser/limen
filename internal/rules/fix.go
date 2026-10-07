package rules

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/license"
)

// Action is what a rule's remediation did (or could not do).
type Action string

const (
	// ActionNone means the rule already passed; nothing was written.
	ActionNone Action = "none"
	// ActionCreated means a missing file was written from the embedded canonical.
	ActionCreated Action = "created"
	// ActionOverwrote means a content-pinned-exact file had drifted and was
	// replaced with the canonical (safe: the whole file is defined by limen).
	ActionOverwrote Action = "overwrote"
	// ActionMerged means missing baseline bits were added to a subset-pinned file
	// while the repository's own additions were preserved.
	ActionMerged Action = "merged"
	// ActionAdvisory means the rule fails but cannot be auto-fixed safely; a human
	// must act. The message says what to do.
	ActionAdvisory Action = "advisory"
	// ActionFailed means remediation was attempted but errored (e.g. a write or
	// `git init` failed). The message carries the error.
	ActionFailed Action = "failed"
)

// resolved reports whether the action left the rule compliant. Advisory and
// failed do not; the rest do.
func (a Action) resolved() bool {
	return a == ActionNone || a == ActionCreated || a == ActionOverwrote || a == ActionMerged
}

// Outcome is the result of remediating one rule (or, for the .justfile rule, one
// file of a multi-file rule).
type Outcome struct {
	Rule    string `json:"rule"`
	Action  Action `json:"action"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// FixOptions parameterizes remediation. Policy is the same policy Check uses.
// License, Holder, and Year are consulted only when a LICENSE must be created —
// i.e. by bootstrap; fix leaves License empty and so never writes a LICENSE.
type FixOptions struct {
	License license.ID // empty for fix; bootstrap's chosen license
	Holder  string     // copyright holder for a generated LICENSE
	Policy  Policy
	Year    int // copyright year for a generated LICENSE
	// SelfVersion is the exact release version (vX.Y.Z[-pre]) of the running
	// limen, or empty for dev builds. When set, any farcloser/limen pin this
	// remediation seeds, inserts, or finds already in the manifest is set to
	// it: the embedded pin necessarily lags one release (its checksums cannot
	// exist before the release is cut), and a repo is coherent only when the
	// limen that wrote its canonical files is the limen it pins — see
	// mergeAquaManifest for the full argument.
	SelfVersion string
	// ToolPins are the versions limen's own tools modules require (see
	// ToolPins); a missing tool directive is seeded at them. Zero for a
	// development build, which then refuses to seed one.
	ToolPins ToolPins
}

// Fix remediates the repository rooted at root and returns one outcome per rule
// (the .justfile rule may contribute several). It is the single engine behind
// both `limen fix` (an existing repo) and `limen bootstrap` (an empty one): the
// only difference is that bootstrap sets up the directory and passes a License,
// so on an empty tree every rule takes its "create" path. The rule order matches
// Check, and aqua runs before the conditional YAML rule so a bootstrapped repo's
// freshly written aqua.yaml is seen by the yamlfmt rule.
func Fix(ctx context.Context, root string, opts FixOptions) []Outcome {
	var outcomes []Outcome

	add := func(entries ...Outcome) { outcomes = append(outcomes, entries...) }

	add(remediateGit(ctx, root))
	add(remediateReadme(root))
	add(remediateLicense(root, opts))
	add(remediateEditorconfig(root))
	add(remediateGitignore(root))
	add(remediateGitattributes(root))
	add(remediateAgents(root)...)
	add(remediateJustfile(root)...)
	add(remediateAqua(ctx, root, opts.SelfVersion)...)
	add(remediateGoTools(ctx, root, opts.ToolPins))
	add(remediateLintGo(root)...)
	add(remediateLychee(root)...)
	add(remediateWorkflows(root)...)
	add(remediateRenovate(root, opts)...)

	add(remediateShellcheck(root))

	if o, ok := remediateYamlfmt(root); ok {
		add(o)
	}

	return outcomes
}

// AllResolved reports whether every outcome left its rule compliant (no advisory
// or failure remains).
func AllResolved(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if !o.Action.resolved() {
			return false
		}
	}

	return true
}

func remediateGit(ctx context.Context, root string) Outcome {
	const rule = "git"
	if _, err := os.Stat(filepath.Join(root, gitDirName)); err == nil {
		return renameStraySigners(root, rule)
	}

	// The rules API carries no context; Background is the honest choice.
	cmd := exec.CommandContext(ctx, "git", "init")

	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return Outcome{
			Rule:    rule,
			Action:  ActionFailed,
			Message: fmt.Sprintf("git init failed: %v: %s", err, strings.TrimSpace(string(out))),
		}
	}

	return Outcome{Rule: rule, Action: ActionCreated, Path: gitDirName, Message: "git init"}
}

// renameStraySigners moves the signers file from its former name to the one
// `lint commits` reads; with both present, the project merges them by hand.
func renameStraySigners(root, rule string) Outcome {
	stray := filepath.Join(root, straySignersFile)
	if !exists(stray) {
		return Outcome{Rule: rule, Action: ActionNone, Path: gitDirName, Message: "already a git repository"}
	}

	if exists(filepath.Join(root, signersFile)) {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Path:    straySignersFile,
			Message: "both " + straySignersFile + " and " + signersFile + " exist: merge the first into the second and delete it",
		}
	}

	if err := os.Rename(stray, filepath.Join(root, signersFile)); err != nil {
		return failed(rule, straySignersFile, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionMerged,
		Path:    signersFile,
		Message: "renamed " + straySignersFile + " to " + signersFile + " (the name `lint commits` reads)",
	}
}

func remediateReadme(root string) Outcome {
	const rule = "readme"
	if checkReadme(root).OK() {
		return Outcome{Rule: rule, Action: ActionNone, Message: "README present"}
	}

	name := filepath.Base(root)
	if abs, err := filepath.Abs(root); err == nil {
		name = filepath.Base(abs)
	}

	body := fmt.Sprintf("# %s\n\nTODO: describe this project.\n", name)
	if err := writeFile(root, readmeFileName, body); err != nil {
		return failed(rule, readmeFileName, err)
	}

	return Outcome{Rule: rule, Action: ActionCreated, Path: readmeFileName, Message: "scaffolded README.md"}
}

func remediateLicense(root string, opts FixOptions) Outcome {
	const rule = "license"

	finding := checkLicense(root, opts.Policy)
	if finding.OK() {
		return Outcome{Rule: rule, Action: ActionNone, Path: finding.Path, Message: finding.Message}
	}
	// Present but not allowed/recognized: we cannot relicense on someone's behalf.
	if finding.Path != "" {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Path:    finding.Path,
			Message: finding.Message + " (replace it with an allowed license)",
		}
	}
	// Missing. fix (no License chosen) never invents a LICENSE; bootstrap writes
	// the chosen one when limen can generate it.
	if opts.License == "" {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Message: "no LICENSE — add one of the allowed licenses (see book/mandatory-files.md)",
		}
	}

	text, ok := license.Notice(opts.License, opts.Year, opts.Holder)
	if !ok {
		return Outcome{
			Rule:   rule,
			Action: ActionAdvisory,
			Message: fmt.Sprintf(
				"cannot generate a %s LICENSE — add its text manually (see book/mandatory-files.md)",
				opts.License,
			),
		}
	}

	if err := writeFile(root, licenseFileName, text); err != nil {
		return failed(rule, licenseFileName, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionCreated,
		Path:    licenseFileName,
		Message: "wrote " + string(opts.License) + " LICENSE",
	}
}

// remediateEditorconfig content-pins the .editorconfig exactly: created if
// missing, overwritten if it drifted. The canonical is comprehensive, so a repo
// never needs its own additions.
func remediateEditorconfig(root string) Outcome {
	return pinExact(root, "editorconfig", ".editorconfig", CanonicalEditorconfig)
}

// remediateGitignore seeds the canonical .gitignore when a repository has
// none, and appends to an existing one the required patterns it lacks (see
// gitignoreRequired); the rest of an existing file is the project's own.
func remediateGitignore(root string) Outcome {
	const (
		rule = "gitignore"
		name = ".gitignore"
	)

	if data, err := readRepoFile(root, name); err == nil {
		missing := missingGitignorePatterns(string(data))
		if len(missing) == 0 {
			return Outcome{
				Rule:    rule,
				Action:  ActionNone,
				Path:    name,
				Message: ".gitignore carries the required patterns",
			}
		}

		// Appended, never rewritten: everything else in the file is the
		// project's own, comments and order included.
		content := string(data)
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		content += "\n# Required by limen (book/mandatory-files.md, \".gitignore\").\n" +
			strings.Join(missing, "\n") + "\n"

		if err := writeFile(root, name, content); err != nil {
			return failed(rule, name, err)
		}

		return Outcome{
			Rule: rule, Action: ActionMerged, Path: name,
			Message: "appended required pattern(s): " + strings.Join(missing, ", "),
		}
	}

	if err := writeFile(root, name, limen.CanonicalGitignore); err != nil {
		return failed(rule, name, err)
	}

	return Outcome{Rule: rule, Action: ActionCreated, Path: name, Message: "wrote canonical .gitignore"}
}

// justfileSeed is the root .justfile a fresh repository starts from: the
// canonical import plus guidance — the file is the project's own from then
// on. The seed must end in a newline (just --fmt rejects a file without one —
// `just do lint just` runs that check on every just file, this one included)
// and its examples must parse when uncommented: a dependency names a recipe
// (do::lint::default), never a bare module path.
const justfileSeed = "# This file is the project's own.\n" +
	"# Add recipes leveraging provided `do` ready-made recipes, or create your own.\n" +
	"# The import must be kept: it mounts every shared limen task under `just do ...`.\n" +
	CanonicalJustfileImport + "\n" +
	"\n" +
	"# The FIRST recipe defined here becomes `just`'s default.\n" +
	"lint: do::lint::default\n" +
	"fix: do::fix::default\n" +
	"test:\n" +
	securityRecipeLine + "\n"

// remediateJustfile handles the task runner's two regimes: the root .justfile
// is the project's own — seeded when missing, and when present only ever
// MERGED (the canonical import line is appended if absent; nothing is
// overwritten) — while every shared just module is content-pinned exactly.
// It returns one outcome per file so `fix` reports each precisely.
func remediateJustfile(root string) []Outcome {
	const rule = "justfile"

	var out []Outcome

	out = append(out, remediateRootJustfile(root, rule))
	for _, mod := range limen.JustModules() {
		out = append(out, pinExact(root, rule, mod.Path, mod.Content))
	}

	// A stray module is removed, not left for a manual sweep: .limen/just/ is
	// limen's alone, so the file is one an earlier limen wrote.
	for _, stray := range strayJustModules(root) {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(stray))); err != nil {
			out = append(out, failed(rule, stray, err))

			continue
		}

		out = append(out, Outcome{
			Rule: rule, Action: ActionMerged, Path: stray,
			Message: "removed " + stray + ", not a canonical module (left by an earlier limen)",
		})
	}

	return out
}

// remediateRootJustfile renames a root justfile from its former name to
// .justfile, seeds a missing one, appends to one that lacks them the
// canonical import and the `security` recipe the security workflow runs, and
// otherwise leaves the file alone — it is the project's own.
func remediateRootJustfile(root, rule string) Outcome {
	name := justfileName

	var done []string

	if legacy, found := findFirstFold(root, legacyJustfileName); found {
		if exists(filepath.Join(root, name)) {
			return Outcome{
				Rule:    rule,
				Action:  ActionAdvisory,
				Path:    legacy,
				Message: "both " + legacy + " and " + name + " exist, which just refuses: merge the first into the second and delete it",
			}
		}

		if err := os.Rename(filepath.Join(root, legacy), filepath.Join(root, name)); err != nil {
			return failed(rule, legacy, err)
		}

		done = append(done, "renamed "+legacy+" to "+name)
	} else if !exists(filepath.Join(root, name)) {
		if err := writeFile(root, name, justfileSeed); err != nil {
			return failed(rule, name, err)
		}

		return Outcome{
			Rule:    rule,
			Action:  ActionCreated,
			Path:    name,
			Message: "seeded the root " + name + " (the content is the project's own from here)",
		}
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return failed(rule, name, err)
	}

	content := string(data)

	if !containsLine(content, CanonicalJustfileImport) {
		content = ensureTrailingNewline(content) +
			"\n# --- added by limen fix: the shared-baseline import ---\n" + CanonicalJustfileImport + "\n"
		done = append(done, "appended the shared-baseline import ("+CanonicalJustfileImport+")")
	}

	// Unconditional, never read off security.yaml: fix may pin that file
	// after this, over a former copy that ran another command.
	if !definesSecurityRecipe(content) {
		content = ensureTrailingNewline(content) +
			"\n# --- added by limen fix: the recipe the security workflow runs ---\n" + securityRecipeLine + "\n"
		done = append(done, "appended the recipe the security workflow runs ("+securityRecipeLine+")")
	}

	if len(done) == 0 {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    name,
			Message: name + " carries the shared-baseline import (the rest is the project's own)",
		}
	}

	if err := writeFile(root, name, content); err != nil {
		return failed(rule, name, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionMerged,
		Path:    name,
		Message: strings.Join(done, "; "),
	}
}

// seedIfMissing writes the canonical content when the file is absent and never
// touches an existing one: the file is the project's own after the seed.
func seedIfMissing(root, rule, relPath, content, createdMessage string) Outcome {
	if exists(filepath.Join(root, filepath.FromSlash(relPath))) {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    relPath,
			Message: relPath + " present (left untouched)",
		}
	}

	if e := writeFile(root, relPath, content); e != nil {
		return failed(rule, relPath, e)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionCreated,
		Path:    relPath,
		Message: createdMessage,
	}
}

// remediateCI seeds ci.yaml when it is missing and replaces it when it is a
// released seed nobody edited; a ci.yaml the project edited stays its own,
// with a note when it does not call the shared lanes yet. Never advisory:
// the checksum workflow runs fix on every Renovate branch, and an
// unresolved outcome would fail every bump until a hand edit.
func remediateCI(root, rule string) Outcome {
	if tag, unedited := staleCISeed(root); unedited {
		if e := writeFile(root, pathWorkflowCI, limen.CanonicalWorkflowCI); e != nil {
			return failed(rule, pathWorkflowCI, e)
		}

		return Outcome{
			Rule:    rule,
			Action:  ActionOverwrote,
			Path:    pathWorkflowCI,
			Message: "replaced limen " + tag + "'s unedited seed with the current one, which calls " + pathWorkflowVerify,
		}
	}

	outcome := seedIfMissing(root, rule, pathWorkflowCI, limen.CanonicalWorkflowCI,
		"seeded the canonical CI workflow (the content is the project's own from here)")
	if outcome.Action != ActionNone {
		return outcome
	}

	// #nosec G304 -- a fixed path under the caller-designated repository.
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pathWorkflowCI)))
	if err == nil && !strings.Contains(string(data), "uses: ./"+pathWorkflowVerify) {
		outcome.Message = pathWorkflowCI + " is the project's own and does not call " + pathWorkflowVerify +
			" yet: realign it by hand (book/mandatory-files.md)"
	}

	return outcome
}

// remediateWorkflows brings the .github surface up to the baseline in its two
// regimes (see checkWorkflows): the checksum-update workflow and the
// composite actions, the shared CI lanes and the security workflow are
// content-pinned exactly; the CI workflow and the renovate config are seeded
// once and never overwritten; the release
// workflow is seeded only where a goreleaser config makes it applicable.
func remediateWorkflows(root string) []Outcome {
	const rule = "workflows"

	out := []Outcome{
		pinExact(root, rule, pathWorkflowChecksum, limen.CanonicalWorkflowUpdateAquaChecksum),
		pinExact(root, rule, pathActionSetupAqua, limen.CanonicalActionSetupAqua),
		pinExact(root, rule, pathActionWinCache, limen.CanonicalActionWindowsCacheImage),
		pinExact(root, rule, pathWorkflowVerify, limen.CanonicalWorkflowVerify),
		pinExact(root, rule, pathWorkflowSecurity, limen.CanonicalWorkflowSecurity),
		remediateCI(root, rule),
		seedIfMissing(root, rule, pathRenovate, renovateSeed,
			"seeded the canonical renovate config (the content is the project's own from here)"),
	}

	out = append(out, remediateReleaseGo(root, rule), remediateLintGithub(root, rule))

	if exists(filepath.Join(root, releaseGoFile)) {
		out = append(out,
			seedIfMissing(root, rule, pathWorkflowRelease, limen.CanonicalWorkflowRelease,
				"seeded the canonical release workflow (the content is the project's own from here)"),
			pinExact(root, rule, pathReleaseNotes, limen.CanonicalReleaseNotes))
	} else {
		out = append(out, Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    pathWorkflowRelease,
			Message: "not applicable: no " + releaseGoFile + " (releasing is opt-in)",
		})
	}

	return out
}

// formerSectionLine is the `github:` header of the former file, a trailing
// comment allowed.
var formerSectionLine = regexp.MustCompile(`^github:\s*(?:#.*)?$`)

// remediateLintGithub moves the audit's exceptions from limen.yaml to
// .lint-github.yaml: the `github:` header goes and its entries come out to
// the top level, comments kept. Anything else unindented is not the former
// format, and neither is two files at once: both are left to the project.
func remediateLintGithub(root, rule string) Outcome {
	data, err := readRepoFile(root, formerLintGithubFile)
	if err != nil {
		return Outcome{Rule: rule, Action: ActionNone, Path: lintGithubFile, Message: "no " + formerLintGithubFile}
	}

	if exists(filepath.Join(root, lintGithubFile)) {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Path:    formerLintGithubFile,
			Message: "both " + formerLintGithubFile + " and " + lintGithubFile + " exist: merge the first into the second and delete it",
		}
	}

	lines := strings.Split(string(data), "\n")
	converted := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		switch {
		case formerSectionLine.MatchString(trimmed) && line == strings.TrimLeft(line, " \t"):
			continue
		case trimmed != "" && !strings.HasPrefix(trimmed, "#") && line == strings.TrimLeft(line, " \t"):
			return Outcome{
				Rule:    rule,
				Action:  ActionAdvisory,
				Path:    formerLintGithubFile,
				Message: formerLintGithubMessage + " — `" + trimmed + "` is not an entry under github:, so convert it by hand",
			}
		}

		converted = append(converted, strings.TrimLeft(line, " \t"))
	}

	if err := writeFile(root, lintGithubFile, strings.Join(converted, "\n")); err != nil {
		return failed(rule, lintGithubFile, err)
	}

	if err := os.Remove(filepath.Join(root, formerLintGithubFile)); err != nil {
		return failed(rule, formerLintGithubFile, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionMerged,
		Path:    lintGithubFile,
		Message: "renamed " + formerLintGithubFile + " to " + lintGithubFile + " and brought its entries out of the github: section",
	}
}

// remediateReleaseGo moves a goreleaser configuration from a default name to
// .release-go.yaml and gives it its schema header and the canonical
// `changelog:` section; the content is otherwise the project's, untouched.
// With both names present, the project merges them by hand.
func remediateReleaseGo(root, rule string) Outcome {
	stray, found := findFirst(root, strayGoreleaserFiles...)
	if found && exists(filepath.Join(root, releaseGoFile)) {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Path:    stray,
			Message: "both " + stray + " and " + releaseGoFile + " exist: merge the first into the second and delete it",
		}
	}

	source := releaseGoFile
	if found {
		source = stray
	}

	data, err := readRepoFile(root, source)
	if err != nil {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    releaseGoFile,
			Message: "not applicable: no " + releaseGoFile,
		}
	}

	content := string(data)
	headed := strings.HasPrefix(content, releaseGoHeader+"\n") || strings.HasPrefix(content, releaseGoHeader+"\r\n")
	_, drifted := changelogDrift(content)

	if !found && headed && !drifted {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    releaseGoFile,
			Message: releaseGoFile + " carries its schema line and the canonical changelog section",
		}
	}

	var done []string

	if !headed {
		content = releaseGoHeader + "\n" + content

		done = append(done, "added the schema line")
	}

	if drifted {
		content = withCanonicalChangelog(content)

		done = append(done, "set the changelog section to `use: github-native`")
	}

	if err := writeFile(root, releaseGoFile, content); err != nil {
		return failed(rule, releaseGoFile, err)
	}

	if !found {
		return Outcome{
			Rule:    rule,
			Action:  ActionMerged,
			Path:    releaseGoFile,
			Message: releaseGoFile + ": " + strings.Join(done, "; "),
		}
	}

	if err := os.Remove(filepath.Join(root, stray)); err != nil {
		return failed(rule, stray, err)
	}

	return Outcome{
		Rule:   rule,
		Action: ActionMerged,
		Path:   releaseGoFile,
		Message: strings.Join(append([]string{"renamed " + stray + " to " + releaseGoFile +
			" (the name the release lane reads)"}, done...), "; "),
	}
}

// pinExact enforces that the file at relPath equals canonical exactly: create it
// if absent, overwrite it if it drifted, otherwise leave it.
func pinExact(root, rule, relPath, canonical string) Outcome {
	data, err := readRepoFile(root, relPath)
	if err != nil {
		if e := writeFile(root, relPath, canonical); e != nil {
			return failed(rule, relPath, e)
		}

		return Outcome{Rule: rule, Action: ActionCreated, Path: relPath, Message: "wrote canonical " + relPath}
	}

	if string(data) == canonical {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    relPath,
			Message: relPath + matchesCanonicalMsg,
		}
	}

	if e := writeFile(root, relPath, canonical); e != nil {
		return failed(rule, relPath, e)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionOverwrote,
		Path:    relPath,
		Message: "reset " + relPath + " to the canonical baseline",
	}
}

// remediateAqua brings a repo's aqua setup up to the baseline in
// book/tooling.md. When no manifest exists it seeds limen's canonical
// aqua.yaml and — only in that pristine case, where the two provably match —
// the canonical aqua-checksums.json with it, so a fresh bootstrap is compliant
// offline. A release build (selfVersion set) additionally rewrites the seeded
// farcloser/limen pin to the running version: the embedded pin necessarily
// lags one release, and seeding it verbatim would hand the repo to an older
// limen than the one that wrote its files. The rewrite forfeits the pristine
// shortcut — the seeded checksums no longer match — so checksums are
// regenerated (below) instead, which needs network; a dev build (selfVersion
// empty) keeps the embedded, provably matching pair. An existing manifest is
// merged instead: the checksum and registries sections are reset to the
// canonical (a valid exact standard-registry ref of the project's survives),
// missing canonical packages are appended by name — farcloser/limen at
// selfVersion when set — without ever duplicating one the project already
// pins, and the project's own packages and versions are left alone, with one
// exception: an existing farcloser/limen pin moves to selfVersion when set,
// because that version is baseline-owned (see mergeAquaManifest). Checksums
// are then regenerated with the real tool (`aqua policy allow` + `aqua
// update-checksum --prune`, in that order — the policy must be allowed before
// aqua can read the local registry) whenever the manifest changed or the
// checksums file is missing; they are never copied from limen, whose checksums
// describe a different package set. A manifest that cannot be parsed, a failed
// regeneration, or anything merging cannot resolve (duplicate package entries)
// ends as an advisory.
func remediateAqua(ctx context.Context, root, selfVersion string) []Outcome {
	var (
		out           []Outcome
		advised       bool
		manifestWrote bool
		pristine      bool
	)

	moved, stop := moveLegacyAqua(root)
	if stop != nil {
		return []Outcome{*stop}
	}

	if moved != nil {
		out = append(out, *moved)
	}

	name := aquaManifestFile
	if !exists(filepath.Join(root, filepath.FromSlash(name))) {
		// A seed that is not an advisory wrote the manifest.
		var seeded []Outcome

		seeded, pristine, advised = seedAqua(root, name, selfVersion)
		out = append(out, seeded...)
		manifestWrote = !advised
	} else {
		var merged []Outcome

		merged, manifestWrote, advised = mergeAquaFile(root, name, selfVersion)
		out = append(out, merged...)
	}

	// Canonical everywhere: content-pinned exactly. The tool set is the one
	// whose change moves pins, so its rewrite regenerates checksums too.
	packages := pinExact(root, ruleAqua, aquaPackagesFile, limen.CanonicalAquaPackages)
	out = append(out,
		pinExact(root, ruleAqua, aquaPolicyFile, limen.CanonicalAquaPolicy),
		pinExact(root, ruleAqua, ".limen/aqua-registry.yaml", limen.CanonicalAquaRegistry),
		packages,
	)

	// A move always regenerates: it is what allows the policy at its new path,
	// which every aqua command run after this one in the same job needs.
	if !advised && !pristine &&
		(moved != nil || manifestWrote || packages.Action != ActionNone ||
			!exists(filepath.Join(root, filepath.FromSlash(aquaChecksumsFile)))) {
		outcome := regenerateAquaChecksumsOutcome(ctx, root)
		out = append(out, outcome)
		advised = outcome.Action == ActionAdvisory
	}

	// Surface any residual failure (e.g. duplicate package entries, which
	// merging cannot resolve safely), so fix never reports a broken aqua setup
	// as resolved. Skipped when an advisory was already issued above.
	if !advised {
		if f := checkAqua(root); !f.OK() {
			out = append(out, Outcome{Rule: ruleAqua, Action: ActionAdvisory, Path: f.Path, Message: f.Message})
		}
	}

	return out
}

// moveLegacyAqua moves aqua's files from the repository root, where limen kept
// them before, into .aqua/: the manifest, its checksums, and the policy (which
// pinExact then resets to the canonical). The registries section, local path
// included, is reset by the merge that follows. Reports the move (nil when
// there was nothing to move), or the outcome that stops the rule: both
// locations populated, which the project merges by hand, or a failed move.
func moveLegacyAqua(root string) (moved, stop *Outcome) {
	legacy, found := findFirst(root, legacyAquaFiles...)
	if !found {
		return nil, nil
	}

	if exists(filepath.Join(root, filepath.FromSlash(aquaManifestFile))) {
		return nil, &Outcome{
			Rule:    ruleAqua,
			Action:  ActionAdvisory,
			Path:    legacy,
			Message: "aqua files exist both at the root and in " + aquaDir + "/: merge " + legacy + " into " + aquaDir + "/ and delete it",
		}
	}

	targets := map[string]string{
		legacyAquaManifest:    aquaManifestFile,
		legacyAquaManifestYml: aquaManifestFile,
		legacyAquaChecksums:   aquaChecksumsFile,
		legacyAquaPolicy:      aquaPolicyFile,
	}

	var names []string

	for _, from := range legacyAquaFiles {
		if !exists(filepath.Join(root, from)) {
			continue
		}

		destination := filepath.Join(root, filepath.FromSlash(targets[from]))
		if err := os.MkdirAll(filepath.Dir(destination), dirPermissions); err != nil {
			f := failed(ruleAqua, from, err)

			return nil, &f
		}

		if err := os.Rename(filepath.Join(root, from), destination); err != nil {
			f := failed(ruleAqua, from, err)

			return nil, &f
		}

		names = append(names, from)
	}

	return &Outcome{
		Rule:   ruleAqua,
		Action: ActionMerged,
		Path:   aquaDir,
		Message: "moved " + strings.Join(
			names,
			", ",
		) + " into " + aquaDir + "/ (allow the policy at its new path: aqua policy allow " + aquaPolicyFile + ")",
	}, nil
}

// seedAqua writes the canonical manifest a repository without one starts
// from and, in the pristine case where the two provably match (a dev build,
// no checksums file yet), the canonical checksums with it. Reports what was
// written, whether the pair is pristine, and whether a write failed.
func seedAqua(root, name, selfVersion string) (out []Outcome, pristine, advised bool) {
	seed, seedMsg := seededAquaManifest(selfVersion)

	if err := writeFile(root, name, seed); err != nil {
		return []Outcome{failed(ruleAqua, name, err)}, false, true
	}

	out = []Outcome{{Rule: ruleAqua, Action: ActionCreated, Path: name, Message: seedMsg}}

	// Only the untouched canonical pair provably matches; a rewritten pin
	// means the checksums must be regenerated, not seeded.
	if selfVersion != "" || exists(filepath.Join(root, aquaChecksumsFile)) {
		return out, false, false
	}

	if err := writeFile(root, aquaChecksumsFile, limen.CanonicalAquaChecksums); err != nil {
		return append(out, failed(ruleAqua, aquaChecksumsFile, err)), false, true
	}

	return append(out, Outcome{
		Rule:    ruleAqua,
		Action:  ActionCreated,
		Path:    aquaChecksumsFile,
		Message: "wrote canonical " + aquaChecksumsFile + " (matches the seeded " + aquaManifestFile + ")",
	}), true, false
}

// mergeAquaFile merges the baseline into an existing manifest (see
// mergeAquaManifest). Reports whether the file was rewritten and whether the
// rule ends as an advisory: an unparseable manifest, or a failed write.
func mergeAquaFile(root, name, selfVersion string) (out []Outcome, wrote, advised bool) {
	data, err := readRepoFile(root, name)
	if err != nil {
		return []Outcome{failed(ruleAqua, name, err)}, false, true
	}

	manifest, parsed := parseAquaManifest(string(data))
	if !parsed {
		return []Outcome{
			{
				Rule:    ruleAqua,
				Action:  ActionAdvisory,
				Path:    name,
				Message: name + " could not be parsed, so it was left untouched — restructure it into block-style checksum/registries/packages sections (see book/tooling.md)",
			},
		}, false, true
	}

	merged, edits := mergeAquaManifest(manifest, selfVersion)
	if len(edits) == 0 {
		return nil, false, false
	}

	if err := writeFile(root, name, merged); err != nil {
		return []Outcome{failed(ruleAqua, name, err)}, false, true
	}

	return []Outcome{
		{Rule: ruleAqua, Action: ActionMerged, Path: name, Message: strings.Join(edits, "; ")},
	}, true, false
}

// regenerateAquaChecksumsOutcome rebuilds aqua-checksums.json with the real
// tool and reports it as created or regenerated; when aqua cannot run, the
// advisory carries the command to run by hand.
func regenerateAquaChecksumsOutcome(ctx context.Context, root string) Outcome {
	existed := exists(filepath.Join(root, aquaChecksumsFile))

	if err := regenerateAquaChecksums(ctx, root); err != nil {
		return Outcome{
			Rule:   ruleAqua,
			Action: ActionAdvisory,
			Path:   aquaChecksumsFile,
			Message: fmt.Sprintf(
				"could not regenerate checksums (%v) — run `aqua policy allow "+aquaPolicyFile+" && aqua update-checksum --prune` and commit the result",
				err,
			),
		}
	}

	action, msg := ActionCreated, "generated "+aquaChecksumsFile+" (aqua update-checksum --prune)"
	if existed {
		action, msg = ActionOverwrote, "regenerated "+aquaChecksumsFile+" (aqua update-checksum --prune)"
	}

	return Outcome{Rule: ruleAqua, Action: action, Path: aquaChecksumsFile, Message: msg}
}

// seededAquaManifest renders the canonical aqua.yaml a pristine repo is seeded
// with, and the outcome message describing it: verbatim for dev builds, the
// limen pin rewritten to the running release otherwise (see
// FixOptions.SelfVersion).
func seededAquaManifest(selfVersion string) (seed, message string) {
	if selfVersion == "" {
		return limen.CanonicalAquaYAML, "wrote canonical " + aquaManifestFile
	}

	seed = strings.Join(rewriteSelfPin(strings.Split(limen.CanonicalAquaYAML, "\n"), selfVersion), "\n")

	return seed, "wrote canonical " + aquaManifestFile + " (limen pinned at the running " + selfVersion + ")"
}

// regenerateAquaChecksums authorizes the (content-pinned) policy, then has aqua
// rebuild aqua-checksums.json for whatever the manifest now pins.
func regenerateAquaChecksums(ctx context.Context, root string) error {
	// --log-level warn: on failure the output lands in the advisory message, and
	// aqua's per-package INFO lines would drown the actual error there.
	for _, args := range [][]string{
		{"--log-level", "warn", "policy", "allow", aquaPolicyFile},
		{"--log-level", "warn", "update-checksum", "--prune"},
	} {
		// aqua on the hermetic PATH; args come from the fixed lists above.
		cmd := exec.CommandContext(ctx, "aqua", args...) // #nosec G204 -- see above.

		cmd.Dir = root
		if combined, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("aqua %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(combined)))
		}
	}

	return nil
}

// remediateGitattributes content-pins .gitattributes exactly: created if
// missing, overwritten if drifted (safe: the whole file is defined by limen —
// see checkGitattributes for why no local additions are allowed).
func remediateGitattributes(root string) Outcome {
	return pinExact(root, "gitattributes", ".gitattributes", CanonicalGitattributes)
}

// remediateAgents brings the agent surface to the baseline in its two regimes
// (see checkAgents): AGENTS.md content-pinned exactly — created if missing,
// overwritten if drifted — and CLAUDE.md seeded once as the import line,
// then the project's own.
func remediateAgents(root string) []Outcome {
	const rule = "agents"

	return []Outcome{
		pinExact(root, rule, "AGENTS.md", CanonicalAgents),
		seedIfMissing(root, rule, "CLAUDE.md", limen.CanonicalClaudeSeed,
			"seeded CLAUDE.md as the AGENTS.md import (the content is the project's own from here)"),
	}
}

// The links overlay before it was named after the lane. Neither the recipe
// nor limen reads it now: a project's own exclusions are its owner's to move,
// so a stray is an advisory, never a rename by the fixer.
const (
	strayLycheeOverlay = ".lychee.toml"
	strayLycheeMessage = " is a link-check overlay nothing here reads: the links recipe reads" +
		" .lint-links.toml — rename it"
)

// remediateLychee content-pins .limen/lychee.toml exactly: created if missing,
// overwritten if it drifted. A project's own exclusions belong in a root
// .lint-links.toml, which is never touched.
func remediateLychee(root string) []Outcome {
	out := []Outcome{pinExact(root, "lychee", ".limen/lychee.toml", CanonicalLychee)}

	if exists(filepath.Join(root, strayLycheeOverlay)) {
		out = append(out, Outcome{
			Rule:    "lychee",
			Action:  ActionAdvisory,
			Path:    strayLycheeOverlay,
			Message: strayLycheeOverlay + strayLycheeMessage,
		})
	}

	return out
}

// remediateShellcheck content-pins .limen/.shellcheckrc in every repository —
// created if missing, overwritten if it drifted, exactly like the just modules.
// A local modification is not preserved; the file is entirely limen's.
// Unconditional by design: see checkShellcheck.
func remediateShellcheck(root string) Outcome {
	return pinExact(root, "shellcheck", ".limen/.shellcheckrc", CanonicalShellcheckrc)
}

// remediateYamlfmt is the per-language twin: when the repo ships YAML, the
// config is content-pinned exactly to the canonical.

func remediateYamlfmt(root string) (Outcome, bool) {
	const (
		rule = "yamlfmt"
		name = ".limen/.yamlfmt"
	)

	if _, found := findYAMLSource(root); !found {
		return Outcome{}, false
	}

	return pinExact(root, rule, name, CanonicalYamlfmt), true
}

// Owner-only permissions for everything limen creates — anyone needing wider
// permissions on a checkout loosens them deliberately; the tool never decides
// that for them.
const (
	dirPermissions  = 0o700
	filePermissions = 0o600
)

// writeFile writes content to relPath under root, creating parent directories as
// needed (relPath may be slash-separated, e.g. ".limen/just/tools.just"). The raw os
// errors already carry the failing path; callers fold them into Outcomes.
func writeFile(root, relPath, content string) error {
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), dirPermissions); err != nil {
		return err //nolint:wrapcheck // see doc comment.
	}

	return os.WriteFile(full, []byte(content), filePermissions) //nolint:wrapcheck // see doc comment.
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}

	return s + "\n"
}

func failed(rule, path string, err error) Outcome {
	return Outcome{Rule: rule, Action: ActionFailed, Path: path, Message: err.Error()}
}

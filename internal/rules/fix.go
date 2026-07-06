package rules

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Outcome is the result of remediating one rule (or, for the Justfile rule, one
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
}

// Fix remediates the repository rooted at root and returns one outcome per rule
// (the Justfile rule may contribute several). It is the single engine behind
// both `limen fix` (an existing repo) and `limen bootstrap` (an empty one): the
// only difference is that bootstrap sets up the directory and passes a License,
// so on an empty tree every rule takes its "create" path. The rule order matches
// Check, and aqua runs before the conditional YAML rule so a bootstrapped repo's
// freshly written aqua.yaml is seen by the yamlfmt rule.
func Fix(root string, opts FixOptions) []Outcome {
	var outcomes []Outcome

	add := func(entries ...Outcome) { outcomes = append(outcomes, entries...) }

	add(remediateGit(root))
	add(remediateReadme(root))
	add(remediateLicense(root, opts))
	add(remediateEditorconfig(root))
	add(remediateGitignore(root, opts.Policy))
	add(remediateGitattributes(root))
	add(remediateJustfile(root)...)
	add(remediateAqua(root, opts.SelfVersion)...)
	add(remediateLychee(root))
	add(remediateWorkflows(root)...)

	if o, ok := remediateShellcheck(root); ok {
		add(o)
	}

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

func remediateGit(root string) Outcome {
	const rule = "git"
	if checkGit(root).OK() {
		return Outcome{Rule: rule, Action: ActionNone, Path: gitDirName, Message: "already a git repository"}
	}

	// The rules API carries no context; Background is the honest choice.
	cmd := exec.CommandContext(context.Background(), "git", "init")

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

func remediateGitignore(root string, policy Policy) Outcome {
	const (
		rule = "gitignore"
		name = ".gitignore"
	)

	data, err := readRepoFile(root, name)
	if err != nil {
		if e := writeFile(root, name, limen.CanonicalGitignore); e != nil {
			return failed(rule, name, e)
		}

		return Outcome{Rule: rule, Action: ActionCreated, Path: name, Message: "wrote canonical .gitignore"}
	}

	have := gitignorePatterns(string(data))

	var missing []string

	for _, want := range policy.RequiredGitignore {
		if !have[normalizeIgnore(want)] {
			missing = append(missing, want)
		}
	}

	if len(missing) == 0 {
		return Outcome{Rule: rule, Action: ActionNone, Path: name, Message: ".gitignore covers the baseline"}
	}

	appended := ensureTrailingNewline(
		string(data),
	) + "\n# --- added by limen fix: baseline patterns ---\n" + strings.Join(
		missing,
		"\n",
	) + "\n"
	if err := writeFile(root, name, appended); err != nil {
		return failed(rule, name, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionMerged,
		Path:    name,
		Message: "appended missing baseline pattern(s): " + strings.Join(missing, ", "),
	}
}

// justfileSeed is the root Justfile a fresh repository starts from: the
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
	"test:\n"

// remediateJustfile handles the task runner's two regimes: the root Justfile
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

	return out
}

// remediateRootJustfile seeds a missing root Justfile, appends the canonical
// import to one that lacks it, and otherwise leaves the file alone — it is
// the project's own.
func remediateRootJustfile(root, rule string) Outcome {
	name, found := findFirst(root, justfileName, "justfile", ".justfile")
	if !found {
		if err := writeFile(root, justfileName, justfileSeed); err != nil {
			return failed(rule, justfileName, err)
		}

		return Outcome{
			Rule:    rule,
			Action:  ActionCreated,
			Path:    justfileName,
			Message: "seeded the root Justfile (the content is the project's own from here)",
		}
	}

	data, err := readRepoFile(root, name)
	if err != nil {
		return failed(rule, name, err)
	}

	if containsLine(string(data), CanonicalJustfileImport) {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    name,
			Message: name + " carries the shared-baseline import (the rest is the project's own)",
		}
	}

	appended := ensureTrailingNewline(string(data)) +
		"\n# --- added by limen fix: the shared-baseline import ---\n" + CanonicalJustfileImport + "\n"
	if err := writeFile(root, name, appended); err != nil {
		return failed(rule, name, err)
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionMerged,
		Path:    name,
		Message: "appended the shared-baseline import (" + CanonicalJustfileImport + ")",
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

// remediateWorkflows brings the .github surface up to the baseline in its two
// regimes (see checkWorkflows): the checksum-update workflow and the
// setup-aqua action are content-pinned exactly; the CI workflow and renovate
// config are seeded once and never overwritten; the release workflow is
// seeded only where a goreleaser config makes it applicable.
func remediateWorkflows(root string) []Outcome {
	const rule = "workflows"

	out := []Outcome{
		pinExact(root, rule, pathWorkflowChecksum, limen.CanonicalWorkflowUpdateAquaChecksum),
		pinExact(root, rule, pathActionSetupAqua, limen.CanonicalActionSetupAqua),
		seedIfMissing(root, rule, pathWorkflowCI, limen.CanonicalWorkflowCI,
			"seeded the canonical CI workflow (the content is the project's own from here)"),
		seedIfMissing(root, rule, pathRenovate, limen.CanonicalRenovate,
			"seeded the canonical renovate config (the content is the project's own from here)"),
	}

	if _, hasGoreleaser := findFirst(root, ".goreleaser.yaml", ".goreleaser.yml"); hasGoreleaser {
		out = append(out, seedIfMissing(root, rule, pathWorkflowRelease, limen.CanonicalWorkflowRelease,
			"seeded the canonical release workflow (the content is the project's own from here)"))
	} else {
		out = append(out, Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Path:    pathWorkflowRelease,
			Message: "not applicable: no .goreleaser.yaml (releasing is opt-in)",
		})
	}

	return out
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

// aquaBin is the aqua executable remediation shells out to when regenerating
// aqua-checksums.json; a package var so tests can substitute a stub.
var aquaBin = "aqua" //nolint:gochecknoglobals // test seam: tests substitute a stub binary.

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
func remediateAqua(root, selfVersion string) []Outcome {
	const rule = "aqua"

	var out []Outcome

	advised := false
	manifestWrote := false
	pristine := false

	name, had := findFirst(root, "aqua.yaml", "aqua.yml")
	if !had {
		name = "aqua.yaml"
		seed, seedMsg := seededAquaManifest(selfVersion)

		if err := writeFile(root, name, seed); err != nil {
			out = append(out, failed(rule, name, err))
			advised = true
		} else {
			out = append(
				out,
				Outcome{Rule: rule, Action: ActionCreated, Path: name, Message: seedMsg},
			)
			manifestWrote = true

			// Only the untouched canonical pair provably matches; a rewritten
			// pin means the checksums must be regenerated, not seeded.
			if selfVersion == "" && !exists(filepath.Join(root, aquaChecksumsFile)) {
				if err := writeFile(root, aquaChecksumsFile, limen.CanonicalAquaChecksums); err != nil {
					out = append(out, failed(rule, aquaChecksumsFile, err))
					advised = true
				} else {
					out = append(
						out,
						Outcome{
							Rule:    rule,
							Action:  ActionCreated,
							Path:    aquaChecksumsFile,
							Message: "wrote canonical aqua-checksums.json (matches the seeded aqua.yaml)",
						},
					)
					pristine = true
				}
			}
		}
	} else {
		data, err := readRepoFile(root, name)
		if err != nil {
			out = append(out, failed(rule, name, err))
			advised = true
		} else if manifest, parsed := parseAquaManifest(string(data)); !parsed {
			out = append(
				out,
				Outcome{
					Rule:    rule,
					Action:  ActionAdvisory,
					Path:    name,
					Message: name + " could not be parsed, so it was left untouched — restructure it into block-style checksum/registries/packages sections (see book/tooling.md)",
				},
			)
			advised = true
		} else if merged, edits := mergeAquaManifest(manifest, selfVersion); len(edits) > 0 {
			if err := writeFile(root, name, merged); err != nil {
				out = append(out, failed(rule, name, err))
				advised = true
			} else {
				out = append(
					out,
					Outcome{Rule: rule, Action: ActionMerged, Path: name, Message: strings.Join(edits, "; ")},
				)
				manifestWrote = true
			}
		}
	}

	// Canonical everywhere: content-pinned exactly.
	out = append(out, pinExact(root, rule, "aqua-policy.yaml", limen.CanonicalAquaPolicy))
	out = append(out, pinExact(root, rule, ".limen/aqua-registry.yaml", limen.CanonicalAquaRegistry))

	if !advised && !pristine && (manifestWrote || !exists(filepath.Join(root, aquaChecksumsFile))) {
		existed := exists(filepath.Join(root, aquaChecksumsFile))
		if err := regenerateAquaChecksums(root); err != nil {
			out = append(
				out,
				Outcome{
					Rule:   rule,
					Action: ActionAdvisory,
					Path:   aquaChecksumsFile,
					Message: fmt.Sprintf(
						"could not regenerate checksums (%v) — run `aqua policy allow aqua-policy.yaml && aqua update-checksum --prune` and commit the result",
						err,
					),
				},
			)
			advised = true
		} else {
			action, msg := ActionCreated, "generated aqua-checksums.json (aqua update-checksum --prune)"
			if existed {
				action, msg = ActionOverwrote, "regenerated aqua-checksums.json (aqua update-checksum --prune)"
			}

			out = append(out, Outcome{Rule: rule, Action: action, Path: aquaChecksumsFile, Message: msg})
		}
	}

	// Surface any residual failure (e.g. duplicate package entries, which
	// merging cannot resolve safely), so fix never reports a broken aqua setup
	// as resolved. Skipped when an advisory was already issued above.
	if !advised {
		if f := checkAqua(root); !f.OK() {
			out = append(out, Outcome{Rule: rule, Action: ActionAdvisory, Path: f.Path, Message: f.Message})
		}
	}

	return out
}

// seededAquaManifest renders the canonical aqua.yaml a pristine repo is seeded
// with, and the outcome message describing it: verbatim for dev builds, the
// limen pin rewritten to the running release otherwise (see
// FixOptions.SelfVersion).
func seededAquaManifest(selfVersion string) (seed, message string) {
	if selfVersion == "" {
		return limen.CanonicalAquaYAML, "wrote canonical aqua.yaml"
	}

	seed = strings.Join(rewriteSelfPin(strings.Split(limen.CanonicalAquaYAML, "\n"), selfVersion), "\n")

	return seed, "wrote canonical aqua.yaml (limen pinned at the running " + selfVersion + ")"
}

// regenerateAquaChecksums authorizes the (content-pinned) policy, then has aqua
// rebuild aqua-checksums.json for whatever the manifest now pins.
func regenerateAquaChecksums(root string) error {
	// --log-level warn: on failure the output lands in the advisory message, and
	// aqua's per-package INFO lines would drown the actual error there.
	for _, args := range [][]string{
		{"--log-level", "warn", "policy", "allow", "aqua-policy.yaml"},
		{"--log-level", "warn", "update-checksum", "--prune"},
	} {
		// aquaBin is "aqua" outside tests (a package-level seam, not user
		// input), and args come from the fixed lists above.
		cmd := exec.CommandContext(context.Background(), aquaBin, args...) //nolint:gosec // G204: see above.

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

// remediateLychee content-pins .limen/lychee.toml exactly: created if missing,
// overwritten if it drifted. A project's own exclusions belong in a root
// lychee.toml, which is never touched.
func remediateLychee(root string) Outcome {
	return pinExact(root, "lychee", ".limen/lychee.toml", CanonicalLychee)
}

// remediateShellcheck / remediateYamlfmt are per-language rules: when the repo
// ships shell / YAML, the config is content-pinned exactly to the canonical (like
// the just modules) — created if missing, overwritten if it drifted. A local
// modification is not preserved; the file is entirely limen's.
func remediateShellcheck(root string) (Outcome, bool) {
	const (
		rule = "shellcheck"
		name = ".limen/.shellcheckrc"
	)

	if _, found := findShellSource(root); !found {
		return Outcome{}, false
	}

	return pinExact(root, rule, name, CanonicalShellcheckrc), true
}

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

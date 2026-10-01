package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/farcloser/limen"
)

// The renovate rule content-pins the shared configuration at
// .limen/renovate.json and maintains three things in renovate.json: the
// `extends` reference to it (see selfPresetRef below), forkProcessing, and
// the gitIgnoredAuthors array, kept in step with the
// identities that commit onto Renovate's branches. Renovate treats a commit
// by any other author as a human edit and stops rebasing the branch — so the
// update-aqua-checksum fix-up commit, made as the org's update-App bot user
// (book/tooling.md), must be listed or every aqua bump PR quietly goes stale.
//
// The identity is not known to the rules package: it belongs to the org (its
// App), and resolving it takes the network. The caller resolves it (see
// cmd/limen) and passes the address through Policy/FixOptions; empty means
// unknown, and the rule then passes without enforcing — never fails a repo
// for what could not be looked up. renovate.json itself is seeded by the
// workflows rule (seeded once, the project's own afterwards): this rule edits
// exactly the keys below and leaves every other key untouched.
const ruleRenovate = "renovate"

// The keys this rule maintains.
const (
	ignoredAuthorsKey = "gitIgnoredAuthors"
	extendsKey        = "extends"
	forkProcessingKey = "forkProcessing"
)

// forkProcessingValue is what forkProcessing must say. Renovate skips forked
// repositories by default under an all-repositories App installation, and it
// decides that from this file alone, fetched through the platform API before
// any preset is resolved — so the setting cannot be inherited from the shared
// preset, and a fork without it is never processed at all. Every farcloser
// and forkcloser repository is or may become a fork, and the failure is
// silent: no PR, no issue, no log the repository can see.
const forkProcessingValue = "enabled"

// The shared configuration, content-pinned in every repository.
const pathRenovatePreset = ".limen/renovate.json"

// selfPresetRef is the `extends` entry that reads the shared configuration:
// Renovate refuses a relative reference in a repository's own config, so the
// repository names itself, and the entry is wrong the moment the repository
// is renamed, transferred or forked — which is why the rule rewrites it from
// the origin remote rather than seeding it once.
func selfPresetRef(repository string) string {
	return "local>" + repository + "//" + strings.TrimSuffix(pathRenovatePreset, ".json")
}

// presetRefPattern matches any reference to the shared configuration: the
// in-repository form under any name, and the retired farcloser/limen preset
// (default.json at limen's root, gone from every later release).
var presetRefPattern = regexp.MustCompile(
	`^(?:(?:github|local)>farcloser/limen|local>[^/\s]+/[^/\s]+//\.limen/renovate)(?:#.*)?$`,
)

// retiredPresetPattern matches the retired farcloser/limen preset alone.
var retiredPresetPattern = regexp.MustCompile(`^(?:github|local)>farcloser/limen(?:#.*)?$`)

// config is a parsed renovate.json. Renovate's schema is open-ended and most
// of the file is the project's own, so it is carried as a map and written
// back whole: only the maintained keys are ever replaced. json.Number keeps
// integers from round-tripping through float64.
type config map[string]any

// parseConfig decodes renovate.json.
func parseConfig(data []byte) (config, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var cfg config
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}

	// A JSON `null` decodes without error into a nil map, which every write
	// below would then panic on.
	if cfg == nil {
		return nil, errNotAnObject
	}

	return cfg, nil
}

// errNotAnObject is a renovate.json whose top level is not a JSON object.
var errNotAnObject = errors.New("not a JSON object")

// render writes a config back out. HTML escaping is off because a preset
// reference contains `>` and would otherwise come back as >; keys are
// emitted in the encoder's stable (sorted) order, so a fix is deterministic.
func render(cfg config) (string, error) {
	var buf bytes.Buffer

	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(cfg); err != nil {
		return "", fmt.Errorf("cannot render renovate.json: %w", err)
	}

	return buf.String(), nil
}

// strings returns the value at key as a slice of strings; ok is false when
// the key is absent or is not an array of strings.
func (cfg config) strings(key string) ([]string, bool) {
	raw, present := cfg[key]
	if !present {
		return nil, false
	}

	items, isArray := raw.([]any)
	if !isArray {
		return nil, false
	}

	out := make([]string, 0, len(items))

	for _, item := range items {
		text, isString := item.(string)
		if !isString {
			return nil, false
		}

		out = append(out, text)
	}

	return out, true
}

// renovateSeed is the renovate.json a repository starts from, seeded once by
// the workflows rule and the project's own afterwards. It carries no extends
// and no org App identity: both name what the seed cannot know, and this rule
// writes them. limen's own renovate.json is this seed as `limen fix` leaves
// it in limen, plus the one manager only the preset's author needs — a test
// holds the two together. Plain JSON, not JSON5: Renovate reads
// forkProcessing only from this exact filename, so prose goes in the
// `description` array the schema provides for it.
const renovateSeed = `{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "description": [
    "The canonical Renovate configuration is .limen/renovate.json, content-pinned like the rest of .limen/, so a fix there reaches this repository with its next limen bump, in the bump's diff. extends reads it by this repository's own name (local>owner/name//.limen/renovate): Renovate refuses a relative reference in a repository's own config. The ` + "`renovate`" + ` rule writes that reference from the origin remote and rewrites it after a rename or a fork. Everything below is the project's own — overrides and additions go here, next to the reference.",
    "forkProcessing: Renovate skips forked repositories by default under an all-repositories App installation, and it skips them before reading any config beyond this file — so the setting cannot live in the shared preset, and cannot live in a renovate.json5 either: only the onboarding config file name, renovate.json, is read through the platform API at that point. Every repository that is a GitHub fork is silently never processed without it.",
    "gitIgnoredAuthors: the update-aqua-checksum workflow pushes a fix-up commit onto Renovate's branches; without this, Renovate treats the branch as human-modified and stops rebasing it. The org's App identity is the org's, so the array stays here (the ` + "`renovate`" + ` rule maintains it) rather than in the preset."
  ],
  "forkProcessing": "enabled",
  "gitIgnoredAuthors": [
    "41898282+github-actions[bot]@users.noreply.github.com"
  ]
}
`

// CanonicalRenovateFor returns the seeded renovate.json as `limen fix` leaves
// it in the named repository ("owner/name"): the seed extending the shared
// configuration by that name. An unknown repository ("") gets the seed as is.
// Exported for the command's tests, which build compliant repositories from
// the canonical files.
func CanonicalRenovateFor(repository string) string {
	if repository == "" {
		return renovateSeed
	}

	cfg, err := parseConfig([]byte(renovateSeed))
	if err != nil {
		return renovateSeed
	}

	cfg.setPresetRef(selfPresetRef(repository))

	pinned, err := render(cfg)
	if err != nil {
		return renovateSeed
	}

	return pinned
}

// hasRetiredPreset reports whether extends still names the retired
// farcloser/limen preset.
func (cfg config) hasRetiredPreset() bool {
	refs, _ := cfg.strings(extendsKey)

	return slices.ContainsFunc(refs, retiredPresetPattern.MatchString)
}

// hasPresetRef reports whether extends carries exactly the given reference
// and no other reference to the shared configuration.
func (cfg config) hasPresetRef(ref string) bool {
	refs, ok := cfg.strings(extendsKey)
	if !ok {
		return false
	}

	found := false

	for _, entry := range refs {
		if !presetRefPattern.MatchString(entry) {
			continue
		}

		if entry != ref {
			return false
		}

		found = true
	}

	return found
}

// setPresetRef rewrites every reference to the preset to ref, or prepends it
// when extends carries none. A non-array extends is replaced outright: it is
// the maintained key, and the rule's job is to leave it correct.
func (cfg config) setPresetRef(ref string) {
	existing, ok := cfg.strings(extendsKey)
	if !ok {
		cfg[extendsKey] = []any{ref}

		return
	}

	out := make([]any, 0, len(existing)+1)
	replaced := false

	for _, entry := range existing {
		if presetRefPattern.MatchString(entry) {
			if !replaced {
				out = append(out, ref)
				replaced = true
			}

			continue
		}

		out = append(out, entry)
	}

	if !replaced {
		out = append([]any{ref}, out...)
	}

	cfg[extendsKey] = out
}

// hasIgnoredAuthor reports whether the address is listed in gitIgnoredAuthors.
func (cfg config) hasIgnoredAuthor(email string) bool {
	authors, ok := cfg.strings(ignoredAuthorsKey)
	if !ok {
		return false
	}

	return slices.Contains(authors, email)
}

// addIgnoredAuthor puts the address first in gitIgnoredAuthors, keeping the
// existing entries in order.
func (cfg config) addIgnoredAuthor(email string) {
	existing, _ := cfg.strings(ignoredAuthorsKey)

	out := make([]any, 0, len(existing)+1)
	out = append(out, email)

	for _, author := range existing {
		if author != email {
			out = append(out, author)
		}
	}

	cfg[ignoredAuthorsKey] = out
}

// hasForkProcessing reports whether forkProcessing is enabled.
func (cfg config) hasForkProcessing() bool {
	value, ok := cfg[forkProcessingKey].(string)

	return ok && value == forkProcessingValue
}

// supersededConfigs are the other files Renovate would read its config from,
// in its own precedence order, every one of them behind renovate.json: once
// renovate.json exists they are dead config that still looks authoritative.
//
//nolint:gochecknoglobals // immutable table.
var supersededConfigs = []string{
	"renovate.jsonc",
	"renovate.json5",
	".github/renovate.json",
	".github/renovate.jsonc",
	".github/renovate.json5",
	".gitlab/renovate.json",
	".gitlab/renovate.jsonc",
	".gitlab/renovate.json5",
	".renovaterc",
	".renovaterc.json",
	".renovaterc.jsonc",
	".renovaterc.json5",
}

// deadConfigs names the superseded config files the repository carries.
func deadConfigs(root string) []string {
	var dead []string

	for _, name := range supersededConfigs {
		if exists(filepath.Join(root, filepath.FromSlash(name))) {
			dead = append(dead, name)
		}
	}

	return dead
}

// doneSeparator joins the edits of one run in a message.
const doneSeparator = "; "

// deadConfigsMessage says what to do about them; the fixer removes nothing,
// so the hand is named.
func deadConfigsMessage(dead []string) string {
	return strings.Join(dead, ", ") + ": dead config — Renovate reads renovate.json first and never this file;" +
		" move anything still wanted into renovate.json and remove it (limen fix removes nothing)"
}

// repositoryUnknownMessage names why the reference cannot be enforced.
const repositoryUnknownMessage = "repository unknown (no github.com origin remote)"

// retiredPresetMessage names the retired preset and what replaces it.
const retiredPresetMessage = "extends names the retired farcloser/limen preset; the shared configuration is " +
	pathRenovatePreset + " now, read as " + "local>owner/name//.limen/renovate"

// checkPresetRef verifies extends: exactly this repository's reference to the
// shared configuration when the repository is known; otherwise only that the
// retired preset is gone, since nothing else can be told apart offline.
func checkPresetRef(cfg config, repository string) *Finding {
	if repository == "" {
		if cfg.hasRetiredPreset() {
			f := fail(ruleRenovate, pathRenovate, retiredPresetMessage+
				" — "+repositoryUnknownMessage+", so limen fix cannot set it")

			return &f
		}

		return nil
	}

	if ref := selfPresetRef(repository); !cfg.hasPresetRef(ref) {
		f := fail(ruleRenovate, pathRenovate, "extends must carry "+ref+
			", this repository's reference to the shared configuration, and no other (limen fix sets it)")

		return &f
	}

	return nil
}

// checkRenovate verifies the shared configuration's content pin, and in
// renovate.json the reference to it (when the repository is known),
// forkProcessing, and the update-App identity among gitIgnoredAuthors (when
// known); and that no superseded config file sits beside renovate.json.
func checkRenovate(root string, policy Policy) Finding {
	if f := checkPinned(root, ruleRenovate, pathRenovatePreset, limen.CanonicalRenovatePreset); f != nil {
		return *f
	}

	data, err := readRepoFile(root, pathRenovate)
	if err != nil {
		// Presence is the workflows rule's verdict; do not double-report.
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "no renovate.json yet (the workflows rule seeds it) — not evaluated",
		}
	}

	if dead := deadConfigs(root); len(dead) > 0 {
		return fail(ruleRenovate, dead[0], deadConfigsMessage(dead))
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return fail(ruleRenovate, pathRenovate, err.Error())
	}

	if f := checkPresetRef(cfg, policy.Repository); f != nil {
		return *f
	}

	if !cfg.hasForkProcessing() {
		return fail(
			ruleRenovate,
			pathRenovate,
			forkProcessingKey+" must be \""+forkProcessingValue+
				"\" — Renovate skips a forked repository otherwise, and decides that from this file "+
				"before any preset is read, so the shared preset cannot carry it (limen fix sets it)",
		)
	}

	extends := "extends the shared configuration"
	if policy.Repository == "" {
		extends = repositoryUnknownMessage + " — extends not enforced"
	}

	if policy.UpdateAppIdentity == "" {
		return Finding{
			Rule:   ruleRenovate,
			Status: StatusOK,
			Path:   pathRenovate,
			Message: extends + "; forks are processed; update-App identity unknown " +
				"(no org or App resolvable) — gitIgnoredAuthors not enforced",
		}
	}

	if cfg.hasIgnoredAuthor(policy.UpdateAppIdentity) {
		return Finding{
			Rule:   ruleRenovate,
			Status: StatusOK,
			Path:   pathRenovate,
			Message: extends + "; forks are processed; gitIgnoredAuthors carries the " +
				"update-App identity " + policy.UpdateAppIdentity,
		}
	}

	return fail(
		ruleRenovate,
		pathRenovate,
		"gitIgnoredAuthors lacks the update-App identity "+policy.UpdateAppIdentity+
			" — Renovate stops rebasing branches the App commits onto (limen fix adds it)",
	)
}

// remediateRenovate content-pins the shared configuration, sets the reference
// to it (when the repository is known) and forkProcessing, and adds the
// update-App identity to gitIgnoredAuthors when it is known and missing. A
// superseded config file beside renovate.json is named, never removed: what
// it carries is the project's to move over or drop.
func remediateRenovate(root string, opts FixOptions) []Outcome {
	pinned := pinExact(root, ruleRenovate, pathRenovatePreset, limen.CanonicalRenovatePreset)
	out := remediateRenovateValues(root, opts)

	if dead := deadConfigs(root); len(dead) > 0 && out.Action != ActionFailed {
		out.Action = ActionAdvisory
		out.Message += doneSeparator + deadConfigsMessage(dead)
	}

	return []Outcome{pinned, out}
}

// remediateRenovateValues is the edit itself: the three maintained keys.
func remediateRenovateValues(root string, opts FixOptions) Outcome {
	data, err := readRepoFile(root, pathRenovate)
	if err != nil {
		return Outcome{
			Rule:    ruleRenovate,
			Action:  ActionNone,
			Path:    pathRenovate,
			Message: "no renovate.json (the workflows rule seeds it) — nothing to edit",
		}
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return Outcome{
			Rule:    ruleRenovate,
			Action:  ActionAdvisory,
			Path:    pathRenovate,
			Message: err.Error() + " — fix the syntax by hand, then run limen fix again",
		}
	}

	var (
		done       []string
		changed    bool
		unresolved bool
	)

	switch ref := selfPresetRef(opts.Policy.Repository); {
	case opts.Policy.Repository == "" && cfg.hasRetiredPreset():
		unresolved = true

		done = append(done, retiredPresetMessage+" — "+repositoryUnknownMessage+
			": set it by hand, or add the origin remote and run limen fix again")
	case opts.Policy.Repository == "":
		done = append(done, repositoryUnknownMessage+" — extends left as is")
	case !cfg.hasPresetRef(ref):
		cfg.setPresetRef(ref)

		changed = true

		done = append(done, "set extends to the shared configuration "+ref)
	}

	if !cfg.hasForkProcessing() {
		cfg[forkProcessingKey] = forkProcessingValue

		changed = true

		done = append(done, "set "+forkProcessingKey+" to \""+forkProcessingValue+"\"")
	}

	switch {
	case opts.Policy.UpdateAppIdentity == "":
		done = append(done, "update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors left as is")
	case cfg.hasIgnoredAuthor(opts.Policy.UpdateAppIdentity):
		done = append(done, "gitIgnoredAuthors already carries "+opts.Policy.UpdateAppIdentity)
	default:
		cfg.addIgnoredAuthor(opts.Policy.UpdateAppIdentity)

		changed = true

		done = append(done, "added the update-App identity "+opts.Policy.UpdateAppIdentity+" to "+ignoredAuthorsKey)
	}

	// Decided on values, never on bytes: render is limen's serialization
	// (sorted keys, raw UTF-8), not the project's, and comparing it to the
	// file rewrote every hand-edited config on every run — the checksum
	// workflow then carried that formatting diff into each Renovate branch.
	action := ActionNone

	if changed {
		content, err := render(cfg)
		if err != nil {
			return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
		}

		if err := writeFile(root, pathRenovate, content); err != nil {
			return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
		}

		action = ActionMerged
	}

	if unresolved {
		action = ActionAdvisory
	}

	return Outcome{
		Rule:    ruleRenovate,
		Action:  action,
		Path:    pathRenovate,
		Message: strings.Join(done, doneSeparator),
	}
}

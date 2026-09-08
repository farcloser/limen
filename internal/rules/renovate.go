package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/farcloser/limen"
)

// The renovate rule maintains three things in renovate.json: the `extends`
// reference to the shared canonical preset (see presetRepo below),
// forkProcessing, and the gitIgnoredAuthors array, kept in step with the
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

// The rule's first charge: the `extends` reference to the shared preset.
// The canonical Renovate configuration lives in limen's default.json, and
// each repository's renovate.json extends it — pinned to the repository's
// own limen version (the farcloser/limen pin in aqua.yaml), so the preset
// moves with the release exactly like the content-pinned files do, and a
// fix to the canonical configuration reaches every repository at its next
// limen bump instead of waiting on a seeded-once file nobody rewrites.
// limen itself, the preset's author, extends its own default branch.
const (
	presetRepo       = "farcloser/limen"
	presetLocalRef   = "local>" + presetRepo
	presetModulePath = "github.com/farcloser/limen"
)

// presetRefPattern matches a reference to the shared preset, tagged or not,
// from either source.
var presetRefPattern = regexp.MustCompile(`^(?:github|local)>` + regexp.QuoteMeta(presetRepo) + `(?:#.*)?$`)

// limenPinPattern finds the repository's farcloser/limen pin in aqua.yaml.
var limenPinPattern = regexp.MustCompile(`(?m)^\s*-\s+name:\s*"?` + regexp.QuoteMeta(presetRepo) + `@([^"\s#]+)`)

// goModulePattern reads the module path off go.mod.
var goModulePattern = regexp.MustCompile(`(?m)^module\s+(\S+)`)

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

	return cfg, nil
}

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

// canonicalPresetRef returns the preset reference this repository must
// extend: the local default branch when the repository IS limen, else the
// preset at the repository's pinned limen version. Empty when no limen pin
// can be read: nothing to pin to, nothing enforced.
func canonicalPresetRef(root string) string {
	if gomod, err := readRepoFile(root, goModFile); err == nil {
		if m := goModulePattern.FindSubmatch(gomod); m != nil && string(m[1]) == presetModulePath {
			return presetLocalRef
		}
	}

	name, ok := findFirst(root, "aqua.yaml", "aqua.yml")
	if !ok {
		return ""
	}

	manifest, err := readRepoFile(root, name)
	if err != nil {
		return ""
	}

	return presetRefFor(string(manifest))
}

// presetRefFor returns the pinned preset reference for a manifest's
// farcloser/limen pin, or "" when the manifest carries none.
func presetRefFor(manifest string) string {
	m := limenPinPattern.FindStringSubmatch(manifest)
	if m == nil {
		return ""
	}

	return "github>" + presetRepo + "#" + m[1]
}

// CanonicalRenovateFor returns the seeded renovate.json as `limen fix` leaves
// it in a repository whose aqua.yaml is the given manifest: the seed with its
// preset reference pinned to that manifest's limen version. A manifest
// without a limen pin gets the seed as is. Exported for the command's tests,
// which build compliant repositories from the canonical files.
func CanonicalRenovateFor(manifest string) string {
	ref := presetRefFor(manifest)
	if ref == "" {
		return limen.CanonicalRenovate
	}

	cfg, err := parseConfig([]byte(limen.CanonicalRenovate))
	if err != nil {
		return limen.CanonicalRenovate
	}

	cfg.setPresetRef(ref)

	pinned, err := render(cfg)
	if err != nil {
		return limen.CanonicalRenovate
	}

	return pinned
}

// hasPresetRef reports whether extends carries exactly the given reference
// and no other reference to the preset.
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

// checkRenovate verifies the preset reference, forkProcessing, and — when the
// identity is known — that it is among gitIgnoredAuthors.
func checkRenovate(root string, policy Policy) Finding {
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

	cfg, err := parseConfig(data)
	if err != nil {
		return fail(ruleRenovate, pathRenovate, err.Error())
	}

	if ref := canonicalPresetRef(root); ref != "" && !cfg.hasPresetRef(ref) {
		return fail(
			ruleRenovate,
			pathRenovate,
			"extends must carry the shared preset at the repository's limen pin, "+ref+
				" — the canonical Renovate configuration is inherited from there (limen fix sets it)",
		)
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

	if policy.UpdateAppIdentity == "" {
		return Finding{
			Rule:   ruleRenovate,
			Status: StatusOK,
			Path:   pathRenovate,
			Message: "extends the shared preset; forks are processed; update-App identity unknown " +
				"(no org or App resolvable) — gitIgnoredAuthors not enforced",
		}
	}

	if cfg.hasIgnoredAuthor(policy.UpdateAppIdentity) {
		return Finding{
			Rule:   ruleRenovate,
			Status: StatusOK,
			Path:   pathRenovate,
			Message: "extends the shared preset; forks are processed; gitIgnoredAuthors carries the " +
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

// remediateRenovate sets the preset reference and forkProcessing, and adds
// the update-App identity to gitIgnoredAuthors when it is known and missing.
func remediateRenovate(root string, opts FixOptions) Outcome {
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

	var done []string

	if ref := canonicalPresetRef(root); ref != "" && !cfg.hasPresetRef(ref) {
		cfg.setPresetRef(ref)

		done = append(done, "set extends to the shared preset "+ref)
	}

	if !cfg.hasForkProcessing() {
		cfg[forkProcessingKey] = forkProcessingValue

		done = append(done, "set "+forkProcessingKey+" to \""+forkProcessingValue+"\"")
	}

	switch {
	case opts.Policy.UpdateAppIdentity == "":
		done = append(done, "update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors left as is")
	case cfg.hasIgnoredAuthor(opts.Policy.UpdateAppIdentity):
		done = append(done, "gitIgnoredAuthors already carries "+opts.Policy.UpdateAppIdentity)
	default:
		cfg.addIgnoredAuthor(opts.Policy.UpdateAppIdentity)

		done = append(done, "added the update-App identity "+opts.Policy.UpdateAppIdentity+" to "+ignoredAuthorsKey)
	}

	content, err := render(cfg)
	if err != nil {
		return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
	}

	if content == string(data) {
		return Outcome{Rule: ruleRenovate, Action: ActionNone, Path: pathRenovate, Message: strings.Join(done, "; ")}
	}

	if err := writeFile(root, pathRenovate, content); err != nil {
		return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
	}

	return Outcome{Rule: ruleRenovate, Action: ActionMerged, Path: pathRenovate, Message: strings.Join(done, "; ")}
}

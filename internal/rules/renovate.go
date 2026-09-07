package rules

import (
	"regexp"
	"strings"

	"github.com/farcloser/limen"
)

// The renovate rule maintains two things in renovate.json5: the `extends`
// reference to the shared canonical preset (see presetRepo below), and the
// gitIgnoredAuthors array, kept in step with the identities that commit onto
// Renovate's branches. Renovate treats a commit by
// any other author as a human edit and stops rebasing the branch — so the
// update-aqua-checksum fix-up commit, made as the org's update-App bot user
// (book/tooling.md), must be listed or every aqua bump PR quietly goes stale.
//
// The identity is not known to the rules package: it belongs to the org (its
// App), and resolving it takes the network. The caller resolves it (see
// cmd/limen) and passes the address through Policy/FixOptions; empty means
// unknown, and the rule then passes without enforcing — never fails a repo
// for what could not be looked up. renovate.json5 itself is seeded by the
// workflows rule (seeded once, the project's own afterwards): this rule edits
// exactly one array in it and touches nothing else.

const ruleRenovate = "renovate"

// ignoredAuthorsKey is the renovate.json5 key this rule maintains.
const ignoredAuthorsKey = "gitIgnoredAuthors"

// The rule's second charge: the `extends` reference to the shared preset.
// The canonical Renovate configuration lives in limen's default.json, and
// each repository's renovate.json5 extends it — pinned to the repository's
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

// quote wraps a JSON5 string value.
const quote = `"`

// presetRefPattern matches any quoted reference to the shared preset, tagged
// or not, from either source.
var presetRefPattern = regexp.MustCompile(`"(?:github|local)>` + regexp.QuoteMeta(presetRepo) + `(?:#[^"]*)?"`)

// limenPinPattern finds the repository's farcloser/limen pin in aqua.yaml.
var limenPinPattern = regexp.MustCompile(`(?m)^\s*-\s+name:\s*"?` + regexp.QuoteMeta(presetRepo) + `@([^"\s#]+)`)

// goModulePattern reads the module path off go.mod.
var goModulePattern = regexp.MustCompile(`(?m)^module\s+(\S+)`)

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

// CanonicalRenovateFor returns the seeded renovate.json5 as `limen fix`
// leaves it in a repository whose aqua.yaml is the given manifest: the seed
// with its preset reference pinned to that manifest's limen version. A
// manifest without a limen pin gets the seed as is. Exported for the
// command's tests, which build compliant repositories from the canonical
// files.
func CanonicalRenovateFor(manifest string) string {
	ref := presetRefFor(manifest)
	if ref == "" {
		return limen.CanonicalRenovate
	}

	pinned, ok := ensurePresetRef(limen.CanonicalRenovate, ref)
	if !ok {
		return limen.CanonicalRenovate
	}

	return pinned
}

// hasPresetRef reports whether the content extends exactly the given
// reference and no other reference to the preset.
func hasPresetRef(content, ref string) bool {
	refs := presetRefPattern.FindAllString(content, -1)
	if len(refs) == 0 {
		return false
	}

	for _, found := range refs {
		if found != quote+ref+quote {
			return false
		}
	}

	return true
}

// ensurePresetRef returns the content extending ref: every existing
// reference to the preset is rewritten to it; absent one, ref is inserted as
// the first element of `extends`, and absent that array, an `extends` line
// goes right after the opening brace. False only when the file has no
// opening brace to anchor on.
func ensurePresetRef(content, ref string) (string, bool) {
	quoted := quote + ref + quote

	if presetRefPattern.MatchString(content) {
		return presetRefPattern.ReplaceAllLiteralString(content, quoted), true
	}

	if loc := extendsKeyPattern.FindStringSubmatchIndex(content); loc != nil {
		open := loc[1] - 1 // index of '['
		if closingBracket(content, open) < 0 {
			return content, false
		}

		rest := strings.TrimLeft(content[open+1:], " \t\n")
		separator := ", "

		if strings.HasPrefix(rest, "]") {
			separator = ""
		} else if strings.HasPrefix(content[open+1:], "\n") {
			// Multi-line array: give the new element its own line.
			indent := content[loc[2]:loc[3]]

			return content[:open+1] + "\n" + indent + "  " + quoted + "," + content[open+1:], true
		}

		return content[:open+1] + quoted + separator + content[open+1:], true
	}

	brace := strings.Index(content, "{")
	if brace < 0 {
		return content, false
	}

	return content[:brace+1] + "\n  extends: [" + quoted + "]," + content[brace+1:], true
}

// extendsKeyPattern finds `extends: [` with its indentation.
var extendsKeyPattern = regexp.MustCompile(`(?m)^([ \t]*)"?extends"?\s*:\s*\[`)

// checkRenovate verifies the update-App identity is among gitIgnoredAuthors,
// when the identity is known.
func checkRenovate(root string, policy Policy) Finding {
	data, err := readRepoFile(root, pathRenovate)
	if err != nil {
		// Presence is the workflows rule's verdict; do not double-report.
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "no renovate.json5 yet (the workflows rule seeds it) — not evaluated",
		}
	}

	if policy.UpdateAppIdentity == "" {
		if ref := canonicalPresetRef(root); ref != "" && !hasPresetRef(string(data), ref) {
			return fail(
				ruleRenovate,
				pathRenovate,
				"extends must carry the shared preset at the repository's limen pin, "+ref+
					" — the canonical Renovate configuration is inherited from there (limen fix sets it)",
			)
		}

		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "extends the shared preset; update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors not enforced",
		}
	}

	if ref := canonicalPresetRef(root); ref != "" && !hasPresetRef(string(data), ref) {
		return fail(
			ruleRenovate,
			pathRenovate,
			"extends must carry the shared preset at the repository's limen pin, "+ref+
				" — the canonical Renovate configuration is inherited from there (limen fix sets it)",
		)
	}

	if hasIgnoredAuthor(string(data), policy.UpdateAppIdentity) {
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "extends the shared preset; gitIgnoredAuthors carries the update-App identity " + policy.UpdateAppIdentity,
		}
	}

	return fail(
		ruleRenovate,
		pathRenovate,
		"gitIgnoredAuthors lacks the update-App identity "+policy.UpdateAppIdentity+
			" — Renovate stops rebasing branches the App commits onto (limen fix adds it)",
	)
}

// remediateRenovate adds the update-App identity to gitIgnoredAuthors when it
// is known and missing.
func remediateRenovate(root string, opts FixOptions) Outcome {
	data, err := readRepoFile(root, pathRenovate)
	if err != nil {
		return Outcome{
			Rule:    ruleRenovate,
			Action:  ActionNone,
			Path:    pathRenovate,
			Message: "no renovate.json5 (the workflows rule seeds it) — nothing to edit",
		}
	}

	content := string(data)

	var done []string

	if ref := canonicalPresetRef(root); ref != "" && !hasPresetRef(content, ref) {
		updated, ok := ensurePresetRef(content, ref)
		if !ok {
			return Outcome{
				Rule:    ruleRenovate,
				Action:  ActionAdvisory,
				Path:    pathRenovate,
				Message: "could not find an extends array (or an opening brace) to edit — add \"" + ref + "\" to extends by hand",
			}
		}

		content = updated

		done = append(done, "set extends to the shared preset "+ref)
	}

	switch {
	case opts.Policy.UpdateAppIdentity == "":
		done = append(done, "update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors left as is")
	case hasIgnoredAuthor(content, opts.Policy.UpdateAppIdentity):
		done = append(done, "gitIgnoredAuthors already carries "+opts.Policy.UpdateAppIdentity)
	default:
		updated, ok := ensureIgnoredAuthor(content, opts.Policy.UpdateAppIdentity)
		if !ok {
			return Outcome{
				Rule:   ruleRenovate,
				Action: ActionAdvisory,
				Path:   pathRenovate,
				Message: "could not find a " + ignoredAuthorsKey + " array to edit — add \"" +
					opts.Policy.UpdateAppIdentity + "\" to it by hand",
			}
		}

		content = updated

		done = append(done, "added the update-App identity "+opts.Policy.UpdateAppIdentity+" to "+ignoredAuthorsKey)
	}

	if content == string(data) {
		return Outcome{Rule: ruleRenovate, Action: ActionNone, Path: pathRenovate, Message: strings.Join(done, "; ")}
	}

	if err := writeFile(root, pathRenovate, content); err != nil {
		return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
	}

	return Outcome{Rule: ruleRenovate, Action: ActionMerged, Path: pathRenovate, Message: strings.Join(done, "; ")}
}

// hasIgnoredAuthor reports whether the address appears as a quoted string
// anywhere in the file. A textual test, deliberately: renovate.json5 is
// JSON5 with comments and the seed's array has one well-known shape; the
// address is specific enough (a numeric id, a [bot] slug, the noreply
// domain) that a match outside gitIgnoredAuthors is not a realistic false
// positive.
func hasIgnoredAuthor(content, email string) bool {
	return strings.Contains(content, `"`+email+`"`)
}

// ignoredAuthorsArray finds `gitIgnoredAuthors: [ ... ]` and returns the
// indices of the opening bracket and its matching close, plus the key's line
// indentation. Nested brackets are not a thing here (an array of strings),
// so the first `]` after the `[` closes it — but the scan skips string
// contents anyway, in case an address ever carried one.
var ignoredAuthorsKeyPattern = regexp.MustCompile(`(?m)^([ \t]*)"?` + ignoredAuthorsKey + `"?\s*:\s*\[`)

// ensureIgnoredAuthor returns the content with email added as the FIRST
// element of the gitIgnoredAuthors array, and false when no such array is
// found. The array is rewritten in the canonical multi-line shape (one
// element per line, trailing commas), whatever shape it had — the seed's
// one-line form grows into it on the first addition. Existing elements are
// kept in order; comments inside the array (none in the seed) are dropped,
// which is the one editorial liberty this takes.
func ensureIgnoredAuthor(content, email string) (string, bool) {
	loc := ignoredAuthorsKeyPattern.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, false
	}

	indent := content[loc[2]:loc[3]]
	open := loc[1] - 1 // index of '['

	closeIdx := closingBracket(content, open)
	if closeIdx < 0 {
		return content, false
	}

	existing := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`).FindAllString(content[open+1:closeIdx], -1)

	lines := make([]string, 0, len(existing)+1)
	for _, element := range append([]string{`"` + email + `"`}, existing...) {
		lines = append(lines, indent+"  "+element+",\n")
	}

	return content[:open+1] + "\n" + strings.Join(lines, "") + indent + content[closeIdx:], true
}

// closingBracket returns the index of the `]` closing the array whose `[` is
// at open, skipping string contents (and escapes within them); -1 if none.
func closingBracket(content string, open int) int {
	inString := false

	for pos := open + 1; pos < len(content); pos++ {
		switch content[pos] {
		case '\\':
			if inString {
				pos++ // skip the escaped character
			}
		case '"':
			inString = !inString
		case ']':
			if !inString {
				return pos
			}
		default:
		}
	}

	return -1
}

package rules

import (
	"regexp"
	"strings"
)

// The renovate rule keeps renovate.json5's gitIgnoredAuthors in step with the
// identities that commit onto Renovate's branches. Renovate treats a commit by
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

// checkRenovate verifies the update-App identity is among gitIgnoredAuthors,
// when the identity is known.
func checkRenovate(root string, policy Policy) Finding {
	if policy.UpdateAppIdentity == "" {
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors not enforced",
		}
	}

	data, err := readRepoFile(root, pathRenovate)
	if err != nil {
		// Presence is the workflows rule's verdict; do not double-report.
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "no renovate.json5 yet (the workflows rule seeds it) — gitIgnoredAuthors not evaluated",
		}
	}

	if hasIgnoredAuthor(string(data), policy.UpdateAppIdentity) {
		return Finding{
			Rule:    ruleRenovate,
			Status:  StatusOK,
			Path:    pathRenovate,
			Message: "gitIgnoredAuthors carries the update-App identity " + policy.UpdateAppIdentity,
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
	if opts.Policy.UpdateAppIdentity == "" {
		return Outcome{
			Rule:    ruleRenovate,
			Action:  ActionNone,
			Path:    pathRenovate,
			Message: "update-App identity unknown (no org or App resolvable) — gitIgnoredAuthors left as is",
		}
	}

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
	if hasIgnoredAuthor(content, opts.Policy.UpdateAppIdentity) {
		return Outcome{
			Rule:    ruleRenovate,
			Action:  ActionNone,
			Path:    pathRenovate,
			Message: "gitIgnoredAuthors already carries " + opts.Policy.UpdateAppIdentity,
		}
	}

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

	if err := writeFile(root, pathRenovate, updated); err != nil {
		return Outcome{Rule: ruleRenovate, Action: ActionFailed, Path: pathRenovate, Message: err.Error()}
	}

	return Outcome{
		Rule:    ruleRenovate,
		Action:  ActionMerged,
		Path:    pathRenovate,
		Message: "added the update-App identity " + opts.Policy.UpdateAppIdentity + " to " + ignoredAuthorsKey,
	}
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

package github

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OverridePath is the committed, project-owned exceptions file for the
// settings audit, at the repository root: where — and WHY — the project
// deviates from what `limen github check` enforces. Each entry names a check
// identifier and the reason it is exempt:
//
//	wiki: hosts the operations runbook
//	org-admins: apostasie is the sole owner
//
// An exempted check reports ok (with the reason), visibly — the escape hatch
// lives in review, never in a UI click. Unknown identifiers and empty reasons
// fail the file: a broken escape hatch must not silently exempt nothing — or
// everything.
//
// One entry is not an escape hatch at all: `org-admins` keeps enforcing after
// it is written. Its reason is parsed for owner logins and compared against
// the live roster on every run, so an owner the text does not name re-raises
// the finding (see auditOrgAdmins). A reason that names nobody therefore
// silences nothing — it reports every owner as undeclared.
const OverridePath = ".lint-github.yaml"

// FormerOverridePath is the file's former name, whose entries sat indented
// under a `github:` section. Read as absent, its exceptions would vanish and
// every exempted check fail with no word about why — so its presence is an
// error of its own.
const FormerOverridePath = "limen.yaml"

var (
	errUnknownCheck   = errors.New("unknown check identifier")
	errEmptyReason    = errors.New("an exception requires a reason")
	errFormerSection  = errors.New("the github: section is gone: entries are top-level `check: reason` lines")
	errFormerOverride = errors.New(FormerOverridePath + " is the former name of " + OverridePath +
		" — rename it and drop its github: line (limen fix does)")
)

// overrideErrFormat is the uniform file:line prefix the parse errors carry.
const overrideErrFormat = "%s:%d: %w"

// stripInlineComment drops a YAML-style inline comment — everything from the
// first '#' preceded by whitespace. The file advertises .yaml and editors
// highlight such trailers as commentary; the parser must not quietly read
// them as content (a reason, or junk that rejects a section header).
func stripInlineComment(line string) string {
	for idx := 1; idx < len(line); idx++ {
		if line[idx] == '#' && (line[idx-1] == ' ' || line[idx-1] == '\t') {
			return strings.TrimSpace(line[:idx])
		}
	}

	return line
}

// LoadOverrides reads the exceptions file under dir. A missing file means no
// exceptions; a malformed one is an error, and so is the file under its former
// name. The format is a deliberately tiny YAML subset — `check: reason`
// entries, comments (full-line and inline) and blank lines — parsed by hand so
// limen keeps zero dependencies.
func LoadOverrides(dir string) (map[string]string, error) {
	path := filepath.Join(dir, filepath.FromSlash(OverridePath))

	data, err := os.ReadFile(path) // #nosec G304 -- caller-designated repository, the tool's contract.
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("reading %s: %w", OverridePath, err)
		}

		if _, statErr := os.Stat(filepath.Join(dir, FormerOverridePath)); statErr == nil {
			return nil, errFormerOverride
		}

		return map[string]string{}, nil
	}

	parser := overrideParser{known: knownChecks(), overrides: map[string]string{}}

	for lineNumber, raw := range strings.Split(string(data), "\n") {
		if err := parser.line(lineNumber+1, strings.TrimSuffix(raw, "\r")); err != nil {
			return nil, err
		}
	}

	return parser.overrides, nil
}

// overrideParser walks the exceptions file line by line: each `check: reason`
// is an override.
type overrideParser struct {
	known     map[string]bool
	overrides map[string]string
}

// line consumes one line, numbered from one for the messages.
func (p *overrideParser) line(lineNumber int, line string) error {
	trimmed := strings.TrimSpace(line)

	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}

	trimmed = stripInlineComment(trimmed)

	key, value, found := strings.Cut(trimmed, ":")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	if key == "github" && value == "" {
		return fmt.Errorf(overrideErrFormat, OverridePath, lineNumber, errFormerSection)
	}

	if !found || value == "" {
		return fmt.Errorf(overrideErrFormat, OverridePath, lineNumber, errEmptyReason)
	}

	if !p.known[key] {
		return fmt.Errorf(overrideErrFormat+": %q", OverridePath, lineNumber, errUnknownCheck, key)
	}

	p.overrides[key] = value

	return nil
}

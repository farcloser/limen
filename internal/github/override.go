package github

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OverridePath is the committed, project-owned declarations file, at the
// repository root: the single place a project records where — and WHY — it
// deviates from what limen enforces. Sectioned by concern; `github:` holds
// the settings-audit exceptions today, and future limen judgments get their
// own sections rather than their own files. Each entry names a check
// identifier and the reason it is exempt:
//
//	github:
//	  wiki: hosts the operations runbook
//	  org-admins: apostasie is the sole owner
//
// An exempted check reports ok (with the reason), visibly — the escape hatch
// lives in review, never in a UI click. Unknown sections, unknown
// identifiers, and empty reasons fail the file: a broken escape hatch must
// not silently exempt nothing — or everything.
const OverridePath = "limen.yaml"

var (
	errUnknownCheck   = errors.New("unknown check identifier")
	errEmptyReason    = errors.New("an exception requires a reason")
	errUnknownSection = errors.New("unknown section (known: github)")
	errNoSection      = errors.New("entries live under a section (e.g. github:)")
)

// sectionGithub is the settings-audit section of the override file, and
// overrideErrFormat the uniform file:line prefix its parse errors carry.
const (
	sectionGithub     = "github"
	overrideErrFormat = "%s:%d: %w"
)

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

// LoadOverrides reads the github section of the declarations file under dir.
// A missing file means no exceptions; a malformed one is an error. The format
// is a deliberately tiny YAML subset — unindented `section:` headers, indented
// `key: reason` entries, comments (full-line and inline) and blank lines —
// parsed by hand so limen keeps zero dependencies.
func LoadOverrides(dir string) (map[string]string, error) {
	path := filepath.Join(dir, filepath.FromSlash(OverridePath))

	data, err := os.ReadFile(path) //nolint:gosec // G304: caller-designated repository, the tool's contract.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}

		return nil, fmt.Errorf("reading %s: %w", OverridePath, err)
	}

	known := knownChecks()
	overrides := map[string]string{}
	section := ""

	for lineNumber, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		trimmed = stripInlineComment(trimmed)

		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")

		key, value, found := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if !indented {
			// A section header: a bare `name:` with nothing after the colon.
			if !found || value != "" {
				return nil, fmt.Errorf(overrideErrFormat, OverridePath, lineNumber+1, errNoSection)
			}

			if key != sectionGithub {
				return nil, fmt.Errorf(overrideErrFormat+": %q", OverridePath, lineNumber+1, errUnknownSection, key)
			}

			section = key

			continue
		}

		if section != sectionGithub {
			return nil, fmt.Errorf(overrideErrFormat, OverridePath, lineNumber+1, errNoSection)
		}

		if !found || value == "" {
			return nil, fmt.Errorf(overrideErrFormat, OverridePath, lineNumber+1, errEmptyReason)
		}

		if !known[key] {
			return nil, fmt.Errorf(overrideErrFormat+": %q", OverridePath, lineNumber+1, errUnknownCheck, key)
		}

		overrides[key] = value
	}

	return overrides, nil
}

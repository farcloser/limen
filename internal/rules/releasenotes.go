package rules

import (
	"regexp"
	"slices"
	"strings"
)

// pathReleaseNotes is GitHub's release-notes configuration: how the notes
// goreleaser asks GitHub for are grouped.
const pathReleaseNotes = ".github/release.yml"

// changelogBlock is the `changelog:` section of .release-go.yaml, limen's
// inside a file otherwise the project's: the notes are GitHub's, from the
// titles of the merged pull requests.
const changelogBlock = "changelog:\n  use: github-native\n"

// changelogKey matches the top-level `changelog:` line, a trailing comment
// allowed.
var changelogKey = regexp.MustCompile(`^changelog:\s*(?:#.*)?$`)

// findChangelog returns the line range [start, end) of the top-level
// `changelog:` section in lines, trailing blank lines excluded; found is
// false when there is none. A section ends at the next line that is neither
// blank nor indented, so a column-0 comment after it is not part of it.
func findChangelog(lines []string) (start, end int, found bool) {
	start = -1

	for i, line := range lines {
		if changelogKey.MatchString(strings.TrimSuffix(line, carriageReturn)) {
			start = i

			break
		}
	}

	if start < 0 {
		return 0, 0, false
	}

	end = start + 1
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], carriageReturn)
		if strings.TrimSpace(line) == "" {
			continue
		}

		if line == strings.TrimLeft(line, " \t") {
			break
		}

		end = i + 1
	}

	return start, end, true
}

// changelogIsCanonical reports whether the section in lines[start:end] says
// exactly what changelogBlock says: comment-only lines and trailing comments
// aside, the same lines.
func changelogIsCanonical(lines []string, start, end int) bool {
	var got []string

	for _, line := range lines[start:end] {
		line = strings.TrimSuffix(line, carriageReturn)
		if code, _, _ := strings.Cut(line, " #"); strings.TrimSpace(code) != "" &&
			!strings.HasPrefix(strings.TrimSpace(code), "#") {
			got = append(got, strings.TrimRight(code, " \t"))
		}
	}

	return strings.Join(got, "\n")+"\n" == changelogBlock
}

// changelogDrift reports whether a .release-go.yaml lacks the canonical
// `changelog:` section, and if so why.
func changelogDrift(content string) (string, bool) {
	lines := strings.Split(content, "\n")

	start, end, found := findChangelog(lines)
	if !found {
		return "has no `changelog:` section", true
	}

	if !changelogIsCanonical(lines, start, end) {
		return "has a `changelog:` section other than `use: github-native`", true
	}

	return "", false
}

// withCanonicalChangelog returns content with its `changelog:` section
// replaced by the canonical one, or the canonical one appended.
func withCanonicalChangelog(content string) string {
	lines := strings.Split(content, "\n")

	start, end, found := findChangelog(lines)
	if !found {
		return ensureTrailingNewline(content) + "\n" + changelogBlock
	}

	block := strings.Split(strings.TrimSuffix(changelogBlock, "\n"), "\n")

	return strings.Join(slices.Concat(lines[:start], block, lines[end:]), "\n")
}

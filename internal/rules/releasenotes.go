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

// sbomsBlock is the `sboms:` section of .release-go.yaml, limen's the same
// way: one SBOM per archive by the toolset's syft, with hermetic's two
// variables handed through goreleaser's stripped environment (the reasoning
// is in limen's own .release-go.yaml, beside the section).
const sbomsBlock = "sboms:\n" +
	"  - artifacts: archive\n" +
	"    env:\n" +
	"      - SYFT_FILE_METADATA_CATALOGER_ENABLED=true\n" +
	"      - SYFT_CHECK_FOR_APP_UPDATE=false\n" +
	"      - HERMETIC_ROOT_DIR={{ envOrDefault \"HERMETIC_ROOT_DIR\" \"\" }}\n" +
	"      - HERMETIC_CONFIG={{ envOrDefault \"HERMETIC_CONFIG\" \"\" }}\n"

// The top-level keys the two sections and the archives test are found by, a
// trailing comment allowed.
var (
	changelogKey = regexp.MustCompile(`^changelog:\s*(?:#.*)?$`)
	sbomsKey     = regexp.MustCompile(`^sboms:\s*(?:#.*)?$`)
	// noArchivesKey is an explicit `archives: []`: a release with no archive
	// has nothing to catalog, and goreleaser makes archives by default
	// otherwise.
	noArchivesKey = regexp.MustCompile(`^archives:\s*\[\s*\]\s*(?:#.*)?$`)
)

// findChangelog returns the line range of the top-level `changelog:` section.
func findChangelog(lines []string) (start, end int, found bool) {
	return findSection(lines, changelogKey)
}

// findSection returns the line range [start, end) of the top-level section
// whose first line key matches, trailing blank lines excluded; found is false
// when there is none. A section ends at the next line that is neither blank
// nor indented, so a column-0 comment after it is not part of it.
func findSection(lines []string, key *regexp.Regexp) (start, end int, found bool) {
	start = -1

	for i, line := range lines {
		if key.MatchString(strings.TrimSuffix(line, carriageReturn)) {
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
// exactly what changelogBlock says.
func changelogIsCanonical(lines []string, start, end int) bool {
	return sectionIsCanonical(lines, start, end, changelogBlock)
}

// sectionIsCanonical reports whether the section in lines[start:end] says
// exactly what block says: comment-only lines and trailing comments aside,
// the same lines.
func sectionIsCanonical(lines []string, start, end int, block string) bool {
	var got []string

	for _, line := range lines[start:end] {
		line = strings.TrimSuffix(line, carriageReturn)
		if code, _, _ := strings.Cut(line, " #"); strings.TrimSpace(code) != "" &&
			!strings.HasPrefix(strings.TrimSpace(code), "#") {
			got = append(got, strings.TrimRight(code, " \t"))
		}
	}

	return strings.Join(got, "\n")+"\n" == block
}

// archivesProduced reports whether the release makes archives: every
// configuration does, by goreleaser's default, except one that says
// `archives: []`.
func archivesProduced(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		if noArchivesKey.MatchString(strings.TrimSuffix(line, carriageReturn)) {
			return false
		}
	}

	return true
}

// sbomsDrift reports whether a .release-go.yaml that makes archives lacks the
// canonical `sboms:` section, and if so why.
func sbomsDrift(content string) (string, bool) {
	if !archivesProduced(content) {
		return "", false
	}

	lines := strings.Split(content, "\n")

	start, end, found := findSection(lines, sbomsKey)
	if !found {
		return "has no `sboms:` section", true
	}

	if !sectionIsCanonical(lines, start, end, sbomsBlock) {
		return "has an `sboms:` section other than the canonical one", true
	}

	return "", false
}

// withCanonicalSboms returns content with its `sboms:` section replaced by
// the canonical one, or the canonical one appended.
func withCanonicalSboms(content string) string {
	return withCanonicalSection(content, sbomsKey, sbomsBlock)
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
	return withCanonicalSection(content, changelogKey, changelogBlock)
}

// withCanonicalSection returns content with the section key names replaced
// by block, or block appended.
func withCanonicalSection(content string, key *regexp.Regexp, block string) string {
	lines := strings.Split(content, "\n")

	start, end, found := findSection(lines, key)
	if !found {
		return ensureTrailingNewline(content) + "\n" + block
	}

	replacement := strings.Split(strings.TrimSuffix(block, "\n"), "\n")

	return strings.Join(slices.Concat(lines[:start], replacement, lines[end:]), "\n")
}

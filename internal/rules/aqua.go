package rules

// aqua.yaml is subset-pinned rather than content-pinned: the checksum and
// registries sections and the canonical package set are limen's, while package
// versions, extra per-project packages, and the standard registry ref (bumped
// per project by Renovate) are the project's. This file holds the conservative
// line-oriented parser and merge logic behind that rule. It understands exactly
// the shape the rule prescribes — block-style top-level checksum/registries/
// packages keys with "- name:" package entries — and refuses anything else, so
// remediation never rewrites a manifest it does not fully understand.

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/farcloser/limen"
)

// aquaSection is one governed top-level section: the line range [start, end)
// from its key line to the next top-level key (or EOF).
type aquaSection struct {
	present    bool
	start, end int
}

// aquaPkg is one "- name:" entry of the packages section: its versionless name
// (owner/repo or module path) and the line range [start, end) covering the
// entry line and its continuation lines (e.g. "registry: local").
type aquaPkg struct {
	name       string
	start, end int
	indent     int
}

type aquaManifest struct {
	lines         []string
	pkgs          []aquaPkg
	checksum      aquaSection
	registries    aquaSection
	packages      aquaSection
	packagesEmpty bool // the packages key was the flow-style empty list "packages: []"
}

var (
	aquaTopKeyRE  = regexp.MustCompile(`^([A-Za-z0-9_-]+):(.*)$`)
	aquaPkgNameRE = regexp.MustCompile(`^(\s*)-\s+name:\s*(.+)$`)
	aquaRefKeyRE  = regexp.MustCompile(`^(\s*ref:).*$`)
	aquaRefValRE  = regexp.MustCompile(`^(\s*ref:\s*)(\S+)(.*)$`)
	// An exact pin: a plain semver tag or a full commit SHA. Branches and
	// "latest" are moving targets and fail the rule.
	aquaExactRefRE = regexp.MustCompile(`^(v\d+\.\d+\.\d+|[0-9a-f]{40})$`)
	// The canonical farcloser/limen package pin, version excluded — trailing
	// content (the renovate comment) survives a rewrite.
	aquaSelfPinRE = regexp.MustCompile(`^(\s*-\s+name:\s*farcloser/limen@)[^\s#]+`)
)

// rewriteSelfPin returns lines with any farcloser/limen pin set to version —
// see FixOptions.SelfVersion for why. It runs on canonical-sourced lines (a
// seeded manifest, or the canonical entries a merge appends) and on a
// project's existing limen pin: unlike every other version in a manifest, the
// limen version is baseline-owned, not project-owned (see mergeAquaManifest).
// A copy is returned; the input is never mutated. No-op when version is empty
// (dev builds keep the embedded pin and never touch an existing one).
func rewriteSelfPin(lines []string, version string) []string {
	if version == "" {
		return lines
	}

	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = aquaSelfPinRE.ReplaceAllString(line, "${1}"+version)
	}

	return out
}

// canonicalAqua is the parsed embedded aqua.yaml — the baseline the rule
// enforces. The canonical file is limen's own, so failing to parse it is a
// build defect, caught the first time the package loads.
var canonicalAqua = mustParseCanonicalAqua() //nolint:gochecknoglobals // parsed once from embedded canonical data.

func mustParseCanonicalAqua() aquaManifest {
	m, ok := parseAquaManifest(limen.CanonicalAquaYAML)
	if !ok || !m.checksum.present || !m.registries.present || !m.packages.present || len(m.pkgs) == 0 {
		panic("limen: the embedded aqua.yaml does not parse as the canonical shape")
	}

	return m
}

// parseAquaManifest reads an aqua.yaml into its governed sections and package
// names. ok is false when the file does not have the prescribed shape (a
// governed section in flow style — except the empty "packages: []" — or a
// duplicated top-level key); callers must then fail or advise, never guess.
func parseAquaManifest(text string) (aquaManifest, bool) {
	manifest := aquaManifest{lines: strings.Split(text, "\n")}
	sections := map[string]*aquaSection{
		"checksum":   &manifest.checksum,
		"registries": &manifest.registries,
		"packages":   &manifest.packages,
	}

	var open *aquaSection

	for lineIndex, line := range manifest.lines {
		match := aquaTopKeyRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		if open != nil {
			open.end = lineIndex
			open = nil
		}

		sec := sections[match[1]]
		if sec == nil {
			continue
		}

		if sec.present {
			return manifest, false // duplicated top-level key
		}

		if rest := strings.TrimSpace(stripAquaComment(match[2])); rest != "" {
			if match[1] != "packages" || rest != "[]" {
				return manifest, false // flow style
			}

			manifest.packagesEmpty = true
		}

		sec.present = true
		sec.start = lineIndex
		sec.end = len(manifest.lines)
		open = sec
	}

	if manifest.packages.present && !manifest.packagesEmpty {
		if !manifest.parsePackages() {
			return manifest, false
		}
	}

	return manifest, true
}

// parsePackages extracts the "- name:" entries of the packages section. Only
// entries at the indent of the first one count — a deeper "- name:" belongs to
// some entry's own attributes, not to the package list.
func (m *aquaManifest) parsePackages() bool {
	entryIndent := -1

	for lineIndex := m.packages.start + 1; lineIndex < m.packages.end; lineIndex++ {
		match := aquaPkgNameRE.FindStringSubmatch(m.lines[lineIndex])
		if match == nil {
			continue
		}

		indent := len(match[1])
		if entryIndent == -1 {
			entryIndent = indent
		}

		if indent != entryIndent {
			continue
		}

		value := strings.Trim(strings.TrimSpace(stripAquaComment(match[2])), `"'`)

		name, _, _ := strings.Cut(value, "@")
		if name == "" {
			return false
		}

		end := lineIndex + 1
		for end < m.packages.end && strings.TrimSpace(m.lines[end]) != "" && lineIndent(m.lines[end]) > indent {
			end++
		}

		m.pkgs = append(m.pkgs, aquaPkg{name: name, start: lineIndex, end: end, indent: indent})
	}

	return true
}

func (m *aquaManifest) section(s aquaSection) []string { return m.lines[s.start:s.end] }

// registriesRef returns the value of the first ref: line in the registries
// section (comment and quotes stripped), or "" when there is none.
func (m *aquaManifest) registriesRef() string {
	for i := m.registries.start; i < m.registries.end; i++ {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(m.lines[i]), "ref:"); ok {
			return strings.Trim(strings.TrimSpace(stripAquaComment(rest)), `"'`)
		}
	}

	return ""
}

// duplicatePkgNames returns every package name declared more than once, in
// first-appearance order. Duplicates cannot be auto-resolved (which version
// would win?), so they surface as failures/advisories.
func (m *aquaManifest) duplicatePkgNames() []string {
	count := map[string]int{}

	var dupes []string

	for _, p := range m.pkgs {
		count[p.name]++
		if count[p.name] == 2 {
			dupes = append(dupes, p.name)
		}
	}

	return dupes
}

// missingCanonicalPkgs returns the canonical package names the manifest does
// not declare, in canonical order. Matching is by versionless name: a project
// that pins a canonical package at its own version satisfies the rule.
func (m *aquaManifest) missingCanonicalPkgs() []string {
	have := map[string]bool{}
	for _, p := range m.pkgs {
		have[p.name] = true
	}

	var missing []string

	for _, p := range canonicalAqua.pkgs {
		if !have[p.name] {
			missing = append(missing, p.name)
		}
	}

	return missing
}

// checkAquaManifest evaluates a parsed manifest against the canonical baseline
// and returns the first failure, or nil when it complies.
func checkAquaManifest(name string, manifest aquaManifest) *Finding {
	const rule = "aqua"
	if !manifest.checksum.present ||
		normalizeAquaBlock(
			manifest.section(manifest.checksum),
		) != normalizeAquaBlock(
			canonicalAqua.section(canonicalAqua.checksum),
		) {
		finding := fail(
			rule,
			name,
			name+": the checksum section must equal the canonical baseline exactly (see book/tooling.md)",
		)

		return &finding
	}

	if !manifest.registries.present ||
		normalizeAquaBlockMaskingRefs(
			manifest.section(manifest.registries),
		) != normalizeAquaBlockMaskingRefs(
			canonicalAqua.section(canonicalAqua.registries),
		) {
		finding := fail(
			rule,
			name,
			name+": the registries section must equal the canonical baseline (only the standard registry ref is project-owned)",
		)

		return &finding
	}

	if ref := manifest.registriesRef(); !aquaExactRefRE.MatchString(ref) {
		finding := fail(
			rule,
			name,
			fmt.Sprintf(
				"%s: the standard registry ref must be an exact version (vX.Y.Z) or a full commit SHA, got %q",
				name,
				ref,
			),
		)

		return &finding
	}

	if dupes := manifest.duplicatePkgNames(); len(dupes) > 0 {
		finding := fail(rule, name, name+": duplicate package entries: "+strings.Join(dupes, ", "))

		return &finding
	}

	if missing := manifest.missingCanonicalPkgs(); len(missing) > 0 {
		finding := fail(rule, name, name+": missing canonical package(s): "+strings.Join(missing, ", "))

		return &finding
	}

	return nil
}

// mergeAquaManifest merges the canonical baseline into a parsed project
// manifest: the checksum and registries sections are reset to the canonical
// when they drifted (the project's standard registry ref survives when it is a
// valid exact pin), and every canonical package the manifest lacks is appended
// at its canonical version — except farcloser/limen, which is inserted at
// selfVersion when set (see FixOptions.SelfVersion) — while a package the
// project already pins, at whatever version, is left alone, so no duplicate
// entries are ever created and no project-owned version is ever rewritten.
// The one exception is an existing farcloser/limen pin, moved to selfVersion
// when set: the limen version is baseline-owned, not project-owned — the
// limen that wrote a repo's canonical files must be the limen the repo pins,
// or the repo goes red in one direction or the other (an old pinned limen
// flags the new files as drift and would "repair" them backwards; a bumped
// pin without a fix flags the old files as drift). Moving it here carries the
// pin, the files, and the checksums (the caller regenerates them on any
// manifest edit) in the same fix; Renovate still proposes the day-to-day
// bumps. It returns the new content and a summary of the edits; the summary
// is empty when the manifest already carries the baseline.
func mergeAquaManifest(manifest aquaManifest, selfVersion string) (string, []string) {
	var reps []aquaReplacement

	var tail [][]string // sections to append at EOF, canonical order

	var summary []string

	canonChecksum := trimBlankTail(canonicalAqua.section(canonicalAqua.checksum))
	canonRegistries := trimBlankTail(canonicalAqua.section(canonicalAqua.registries))

	if !manifest.checksum.present {
		tail = append(tail, canonChecksum)
		summary = append(summary, "added the canonical checksum section")
	} else if normalizeAquaBlock(manifest.section(manifest.checksum)) != normalizeAquaBlock(canonChecksum) {
		reps = append(
			reps,
			aquaReplacement{
				start: manifest.checksum.start,
				end:   manifest.checksum.end,
				lines: withBlankTail(canonChecksum),
			},
		)
		summary = append(summary, "reset the checksum section to the canonical baseline")
	}

	projRef := manifest.registriesRef()
	refValid := aquaExactRefRE.MatchString(projRef)

	newRegistries := canonRegistries
	if refValid && projRef != canonicalAqua.registriesRef() {
		newRegistries = substituteAquaRef(canonRegistries, projRef)
	}

	switch {
	case !manifest.registries.present:
		tail = append(tail, newRegistries)
		summary = append(summary, "added the canonical registries section")
	case normalizeAquaBlockMaskingRefs(manifest.section(manifest.registries)) != normalizeAquaBlockMaskingRefs(canonRegistries):
		msg := "reset the registries section to the canonical baseline"
		if refValid {
			msg += " (kept standard registry ref " + projRef + ")"
		}

		reps = append(
			reps,
			aquaReplacement{
				start: manifest.registries.start,
				end:   manifest.registries.end,
				lines: withBlankTail(newRegistries),
			},
		)
		summary = append(summary, msg)
	case !refValid:
		// Shape matches but the ref is a moving target: pin it to the canonical.
		reps = append(
			reps,
			aquaReplacement{
				start: manifest.registries.start,
				end:   manifest.registries.end,
				lines: withBlankTail(canonRegistries),
			},
		)
		summary = append(summary, "pinned the standard registry ref to the canonical "+canonicalAqua.registriesRef())
	default:
		// Registries already match the canonical and the ref is a valid exact
		// pin — nothing to do.
	}

	// Does an existing limen pin move to selfVersion (the baseline-owned
	// exception in the doc above)? Detected up front: when the packages
	// section is replaced wholesale below, the move must fold into that
	// replacement — a second, one-line replacement over the same range would
	// overlap it.
	selfPinMoves := false

	if selfVersion != "" && manifest.packages.present {
		for line := manifest.packages.start; line < manifest.packages.end; line++ {
			if rewriteSelfPin(manifest.lines[line:line+1], selfVersion)[0] != manifest.lines[line] {
				selfPinMoves = true

				break
			}
		}
	}

	packagesReplaced := false

	if missing := manifest.missingCanonicalPkgs(); len(missing) > 0 {
		switch {
		case !manifest.packages.present:
			tail = append(
				tail,
				rewriteSelfPin(trimBlankTail(canonicalAqua.section(canonicalAqua.packages)), selfVersion),
			)
		case manifest.packagesEmpty:
			lines := append([]string{"packages:"}, rewriteSelfPin(canonicalEntryLines(missing, 0), selfVersion)...)
			reps = append(
				reps,
				aquaReplacement{
					start: manifest.packages.start,
					end:   manifest.packages.end,
					lines: withBlankTail(lines),
				},
			)
		default:
			shift := manifest.pkgEntryIndent() - canonicalAqua.pkgs[0].indent
			lines := append(
				rewriteSelfPin(trimBlankTail(manifest.section(manifest.packages)), selfVersion),
				rewriteSelfPin(canonicalEntryLines(missing, shift), selfVersion)...)
			reps = append(
				reps,
				aquaReplacement{
					start: manifest.packages.start,
					end:   manifest.packages.end,
					lines: withBlankTail(lines),
				},
			)

			packagesReplaced = true
		}

		summary = append(summary, "added canonical package(s): "+strings.Join(missing, ", "))
	}

	if selfPinMoves {
		if !packagesReplaced {
			for line := manifest.packages.start; line < manifest.packages.end; line++ {
				rewritten := rewriteSelfPin(manifest.lines[line:line+1], selfVersion)[0]
				if rewritten == manifest.lines[line] {
					continue
				}

				reps = append(reps, aquaReplacement{start: line, end: line + 1, lines: []string{rewritten}})

				break
			}
		}

		summary = append(summary, "moved the farcloser/limen pin to "+selfVersion+" (the running limen's version)")
	}

	if len(summary) == 0 {
		return strings.Join(manifest.lines, "\n"), nil
	}

	// Stitch: copy every line outside the replaced ranges, swap in the new
	// blocks in place, then append the sections the file did not have at all.
	// Sections may appear in any order in the file, so replacements are applied
	// in position order, not the order they were planned in.
	slices.SortFunc(reps, func(left, right aquaReplacement) int { return cmp.Compare(left.start, right.start) })

	var out []string

	cursor := 0
	for _, r := range reps {
		out = append(out, manifest.lines[cursor:r.start]...)
		out = append(out, r.lines...)
		cursor = r.end
	}

	out = append(out, manifest.lines[cursor:]...)

	out = trimBlankTail(out)
	for _, block := range tail {
		out = append(out, "")
		out = append(out, block...)
	}

	return strings.Join(out, "\n") + "\n", summary
}

// aquaReplacement swaps the line range [start, end) for the given lines when
// the manifest is rebuilt.
type aquaReplacement struct {
	lines      []string
	start, end int
}

// pkgEntryIndent returns the indent of the first sequence entry in the packages
// section (any "- " line, so non-name entries count too), or the canonical
// entry indent when the section has none — appended entries must sit at the
// same indent as existing ones or the YAML sequence becomes invalid.
func (m *aquaManifest) pkgEntryIndent() int {
	for i := m.packages.start + 1; i < m.packages.end; i++ {
		trimmed := strings.TrimLeft(m.lines[i], " ")
		if strings.HasPrefix(trimmed, "- ") {
			return lineIndent(m.lines[i])
		}
	}

	return canonicalAqua.pkgs[0].indent
}

// canonicalEntryLines renders the canonical entries for the given package
// names (canonical order preserved by the caller), re-indented by shift so
// they match the project's own entry indent.
func canonicalEntryLines(names []string, shift int) []string {
	byName := map[string]aquaPkg{}
	for _, p := range canonicalAqua.pkgs {
		byName[p.name] = p
	}

	var out []string

	for _, name := range names {
		p := byName[name]
		for _, line := range canonicalAqua.lines[p.start:p.end] {
			out = append(out, reindent(line, shift))
		}
	}

	return out
}

// normalizeAquaBlock reduces a section to a comparable form: lines are
// right-trimmed and blank lines dropped (they carry no YAML meaning).
// Everything else, comments included, is content: drift means drift.
func normalizeAquaBlock(lines []string) string {
	var out []string

	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

// normalizeAquaBlockMaskingRefs is normalizeAquaBlock for the registries
// section: the value of any ref: line is masked before comparing, because the
// standard registry ref is project-owned (Renovate bumps it per repo).
func normalizeAquaBlockMaskingRefs(lines []string) string {
	var out []string

	for _, line := range lines {
		if aquaRefKeyRE.MatchString(line) {
			line = aquaRefKeyRE.ReplaceAllString(line, "$1 <project-ref>")
		}

		out = append(out, line)
	}

	return normalizeAquaBlock(out)
}

// substituteAquaRef rewrites the ref: value in a rendered canonical registries
// block to the project's own pin, keeping the canonical line's comment.
func substituteAquaRef(lines []string, ref string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if aquaRefValRE.MatchString(line) {
			line = aquaRefValRE.ReplaceAllString(line, "${1}"+ref+"${3}")
		}

		out[i] = line
	}

	return out
}

// stripAquaComment removes a trailing "# …" comment. It is only used on values
// the rule owns (package names, registry refs, section key lines), none of
// which can contain a literal '#'.
func stripAquaComment(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "#") {
		return ""
	}

	if before, _, ok := strings.Cut(value, " #"); ok {
		return before
	}

	return value
}

func lineIndent(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func reindent(line string, shift int) string {
	if strings.TrimSpace(line) == "" || shift == 0 {
		return line
	}

	indent := max(lineIndent(line)+shift, 0)

	return strings.Repeat(" ", indent) + strings.TrimLeft(line, " ")
}

func trimBlankTail(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return lines[:end]
}

// withBlankTail returns a copy ending in exactly one blank line — a copy,
// because the input is usually a sub-slice of a live manifest's (or the
// canonical's) line array, and appending in place would clobber it.
func withBlankTail(lines []string) []string { return append(copyLines(trimBlankTail(lines)), "") }

func copyLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)

	return out
}

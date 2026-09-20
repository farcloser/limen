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
	// content (a closing quote, the renovate comment) survives a rewrite. The
	// optional quote mirrors parsePackages, which strips quotes when extracting
	// names: a quoted pin that satisfies the presence check must also be the
	// pin the baseline-owned version move finds.
	aquaSelfPinRE = regexp.MustCompile(`^(\s*-\s+name:\s*["']?farcloser/limen@)[^\s#"']+`)
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

// selfPinReplacement is the one-line replacement moving a project's existing
// farcloser/limen pin to version — the path taken when the packages section
// is NOT rebuilt wholesale (when it is, the move is folded into that
// replacement). Empty when no line moves.
func selfPinReplacement(manifest aquaManifest, version string) []aquaReplacement {
	for line := manifest.packages.start; line < manifest.packages.end; line++ {
		rewritten := rewriteSelfPin(manifest.lines[line:line+1], version)[0]
		if rewritten == manifest.lines[line] {
			continue
		}

		return []aquaReplacement{{start: line, end: line + 1, lines: []string{rewritten}}}
	}

	return nil
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
// governed section in flow style — except the empty "packages: []", and only
// with nothing but comments in its body — a duplicated top-level key, or any
// top-level line that is not a bare-word key, a comment, or blank); callers
// must then fail or advise, never guess.
func parseAquaManifest(text string) (aquaManifest, bool) {
	manifest := aquaManifest{lines: strings.Split(text, "\n")}
	sections := map[string]*aquaSection{
		"checksum":   &manifest.checksum,
		"registries": &manifest.registries,
		"packages":   &manifest.packages,
	}

	if !manifest.scanSections(sections) {
		return manifest, false
	}

	if manifest.packagesEmpty && !manifest.packagesBodyInert() {
		return manifest, false
	}

	if manifest.packages.present && !manifest.packagesEmpty {
		if !manifest.parsePackages() {
			return manifest, false
		}
	}

	return manifest, true
}

// scanSections bounds the governed sections by their top-level keys; false
// when a top-level line is one this parser cannot bound, or a section opens
// in a shape it refuses.
func (m *aquaManifest) scanSections(sections map[string]*aquaSection) bool {
	var open *aquaSection

	for lineIndex, line := range m.lines {
		match := aquaTopKeyRE.FindStringSubmatch(line)
		if match == nil {
			// Only shapes the line parser can bound may sit at the top level:
			// a bare-word key (matched above), a comment, a blank line, or an
			// indented continuation. Anything else — a quoted or dotted key,
			// a document marker, a root-level list — is YAML this parser does
			// not understand as a section boundary; absorbed into the open
			// section's range, it would be relocated, rewritten, or deleted by
			// a merge that believes it owns those lines. Refuse instead.
			if !aquaInertLine(line) && lineIndent(line) == 0 {
				return false
			}

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

		if !m.openSection(sec, match[1], match[2], lineIndex) {
			return false
		}

		open = sec
	}

	return true
}

// openSection starts the section keyed key at lineIndex, rest being what
// follows the colon: nothing, or the one flow form accepted, an empty
// packages list. A duplicated key or any other flow style is refused.
func (m *aquaManifest) openSection(sec *aquaSection, key, rest string, lineIndex int) bool {
	if sec.present {
		return false // duplicated top-level key
	}

	if rest := strings.TrimSpace(stripAquaComment(rest)); rest != "" {
		if key != "packages" || rest != "[]" {
			return false // flow style
		}

		m.packagesEmpty = true
	}

	sec.present = true
	sec.start = lineIndex
	sec.end = len(m.lines)

	return true
}

// packagesBodyInert reports whether nothing but comments and blanks follows
// a "packages: []": that form already IS the whole section, an indented body
// after it is YAML aqua rejects, and the merge would replace the section
// wholesale while the self-pin scan still sees the body — the two edits
// overlap, so the shape is refused outright.
func (m *aquaManifest) packagesBodyInert() bool {
	for i := m.packages.start + 1; i < m.packages.end; i++ {
		if !aquaInertLine(m.lines[i]) {
			return false
		}
	}

	return true
}

// parsePackages extracts the "- name:" entries of the packages section. Only
// entries at the indent of the first one count — a deeper "- name:" belongs to
// some entry's own attributes, not to the package list. A SHALLOWER one is no
// legal sibling of anything (the section key sits at column 0, the list at one
// indent): skipping it would blind the duplicate and missing-package checks to
// an entry aqua may still see, so the shape is refused instead.
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

		if indent < entryIndent {
			return false
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

// retiredCanonicalPkgs are packages the baseline once required and now
// forbids: every Go-built tool moved to tools/go.mod `tool` directives — see
// book/tooling.md, "Go-built tools are go.mod tools", and the gotools rule.
// aqua compiles a go_install package once per tool version, with whichever
// project's pinned go ran it first, and then shares that binary across
// projects; nothing pins the compiler behind the binary that runs, which only
// the module's tool directive guarantees. A manifest still pinning one fails
// check (the recipes no longer look for it on PATH), and fix removes the
// entry.
//
// The set is the gotools rule's own tables read from the other side — every
// tool it requires, every directive it has retired, and the aqua packages
// its isolated modules replaced — so the two rules enforce one doctrine from
// one source and cannot drift apart.
func retiredCanonicalPkgs() []string {
	pkgs := slices.Concat(goToolsEverywhere, goSourceAnalyzers, isolatedAquaRetired)
	for pkg := range retiredGoTools {
		pkgs = append(pkgs, pkg)
	}

	return pkgs
}

// retiredPkgs returns the retired canonical packages the manifest still
// declares, in manifest order.
func (m *aquaManifest) retiredPkgs() []string {
	var retired []string

	for _, p := range m.pkgs {
		if slices.Contains(retiredCanonicalPkgs(), p.name) {
			retired = append(retired, p.name)
		}
	}

	return retired
}

// withoutPkgs returns the manifest with the named packages' entries (entry
// line plus continuation lines) removed, re-parsed so every section range is
// current. The bool is false when the stripped text no longer parses, which
// the parser's shape rules make impossible for a removal — reported rather
// than trusted.
func (m *aquaManifest) withoutPkgs(names []string) (aquaManifest, bool) {
	drop := make([]bool, len(m.lines))

	for _, p := range m.pkgs {
		if !slices.Contains(names, p.name) {
			continue
		}

		for i := p.start; i < p.end; i++ {
			drop[i] = true
		}
	}

	kept := make([]string, 0, len(m.lines))

	for i, line := range m.lines {
		if !drop[i] {
			kept = append(kept, line)
		}
	}

	return parseAquaManifest(strings.Join(kept, "\n"))
}

// retiredPkgsMessage is the check failure / fix summary wording for retired
// canonical packages still present.
func retiredPkgsMessage(retired []string) string {
	return "retired canonical package(s): " + strings.Join(retired, ", ") +
		" (now go.mod tool directives, see book/tooling.md; limen fix removes the entry)"
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

	if retired := manifest.retiredPkgs(); len(retired) > 0 {
		finding := fail(rule, name, name+": "+retiredPkgsMessage(retired))

		return &finding
	}

	if twoLine := manifest.twoLinePinNames(); len(twoLine) > 0 {
		finding := fail(rule, name, name+": "+twoLinePinMessage(twoLine))

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
	var plan aquaPlan

	// Retired packages go first, as a whole-text pass: every range planned
	// below is then computed on the stripped manifest, so no replacement can
	// straddle a removed entry.
	if retired := manifest.retiredPkgs(); len(retired) > 0 {
		if stripped, ok := manifest.withoutPkgs(retired); ok {
			manifest = stripped

			plan.summary = append(plan.summary, "removed "+retiredPkgsMessage(retired))
		}
	}

	plan.checksum(manifest)
	plan.registries(manifest)
	plan.packages(manifest, selfVersion)

	if len(plan.summary) == 0 {
		return strings.Join(manifest.lines, "\n"), nil
	}

	return plan.stitch(manifest), plan.summary
}

// aquaPlan accumulates a merge: the line ranges to replace, the sections the
// file lacks (appended at EOF in canonical order), and the summary of edits.
type aquaPlan struct {
	reps    []aquaReplacement
	tail    [][]string
	summary []string
}

// replace swaps a section's whole range for lines.
func (p *aquaPlan) replace(sec aquaSection, lines []string, message string) {
	p.reps = append(p.reps, aquaReplacement{start: sec.start, end: sec.end, lines: withBlankTail(lines)})
	p.summary = append(p.summary, message)
}

// add appends a section the file did not have.
func (p *aquaPlan) add(lines []string, message string) {
	p.tail = append(p.tail, lines)
	p.summary = append(p.summary, message)
}

// checksum resets the checksum section to the canonical, or adds it.
func (p *aquaPlan) checksum(manifest aquaManifest) {
	canon := trimBlankTail(canonicalAqua.section(canonicalAqua.checksum))

	switch {
	case !manifest.checksum.present:
		p.add(canon, "added the canonical checksum section")
	case normalizeAquaBlock(manifest.section(manifest.checksum)) != normalizeAquaBlock(canon):
		p.replace(manifest.checksum, canon, "reset the checksum section to the canonical baseline")
	default:
		// Already canonical.
	}
}

// registries resets the registries section to the canonical — keeping a
// valid exact standard-registry ref of the project's — or pins a moving ref.
func (p *aquaPlan) registries(manifest aquaManifest) {
	canon := trimBlankTail(canonicalAqua.section(canonicalAqua.registries))

	projRef := manifest.registriesRef()
	refValid := aquaExactRefRE.MatchString(projRef)

	newRegistries := canon
	if refValid && projRef != canonicalAqua.registriesRef() {
		newRegistries = substituteAquaRef(canon, projRef)
	}

	switch {
	case !manifest.registries.present:
		p.add(newRegistries, "added the canonical registries section")
	case normalizeAquaBlockMaskingRefs(manifest.section(manifest.registries)) != normalizeAquaBlockMaskingRefs(canon):
		msg := "reset the registries section to the canonical baseline"
		if refValid {
			msg += " (kept standard registry ref " + projRef + ")"
		}

		p.replace(manifest.registries, newRegistries, msg)
	case !refValid:
		// Shape matches but the ref is a moving target: pin it to the canonical.
		p.replace(manifest.registries, canon,
			"pinned the standard registry ref to the canonical "+canonicalAqua.registriesRef())
	default:
		// Registries already match the canonical and the ref is a valid exact
		// pin — nothing to do.
	}
}

// packages adds the missing canonical packages, moves an existing limen pin
// to selfVersion (the baseline-owned exception in the doc above) and folds
// two-line version: pins into one line. The last two are replacements of
// their own unless the section is rebuilt wholesale, in which case they fold
// into that rebuild: two replacements over one range would overlap, and the
// stitch would panic on it.
func (p *aquaPlan) packages(manifest aquaManifest, selfVersion string) {
	// The self-pin move is detected up front, before the section may be
	// replaced.
	selfPinMoves := false

	if selfVersion != "" && manifest.packages.present {
		for line := manifest.packages.start; line < manifest.packages.end; line++ {
			if rewriteSelfPin(manifest.lines[line:line+1], selfVersion)[0] != manifest.lines[line] {
				selfPinMoves = true

				break
			}
		}
	}

	twoLinePins := manifest.twoLinePins()

	packagesReplaced := false

	if missing := manifest.missingCanonicalPkgs(); len(missing) > 0 {
		packagesReplaced = p.addPackages(manifest, missing, twoLinePins, selfVersion)
		p.summary = append(p.summary, "added canonical package(s): "+strings.Join(missing, ", "))
	}

	if selfPinMoves {
		if !packagesReplaced {
			p.reps = append(p.reps, selfPinReplacement(manifest, selfVersion)...)
		}

		p.summary = append(p.summary, "moved the farcloser/limen pin to "+selfVersion+" (the running limen's version)")
	}

	if len(twoLinePins) > 0 {
		if !packagesReplaced {
			p.reps = append(p.reps, twoLineReplacements(twoLinePins)...)
		}

		p.summary = append(p.summary, twoLineSummary(twoLinePins))
	}
}

// addPackages plans the missing canonical packages in: the whole canonical
// section appended when the file has none, the section rebuilt otherwise.
// Reports whether the section's range was replaced.
func (p *aquaPlan) addPackages(
	manifest aquaManifest,
	missing []string,
	twoLinePins []twoLinePin,
	selfVersion string,
) bool {
	switch {
	case !manifest.packages.present:
		p.tail = append(
			p.tail,
			rewriteSelfPin(trimBlankTail(canonicalAqua.section(canonicalAqua.packages)), selfVersion),
		)

		return false
	case manifest.packagesEmpty:
		lines := append([]string{"packages:"}, rewriteSelfPin(canonicalEntryLines(missing, 0), selfVersion)...)
		p.reps = append(
			p.reps,
			aquaReplacement{
				start: manifest.packages.start,
				end:   manifest.packages.end,
				lines: withBlankTail(lines),
			},
		)

		// The whole section range is replaced here too, so the self-pin move
		// folds into it. Unreachable today — parse refuses a "packages: []"
		// with a body, so no self-pin line can sit inside this range — but
		// the invariant belongs to this branch, not to the parser.
		return true
	default:
		shift := manifest.pkgEntryIndent() - canonicalAqua.pkgs[0].indent
		section := rewriteTwoLinePins(
			trimBlankTail(manifest.section(manifest.packages)), twoLinePins, manifest.packages.start,
		)
		lines := append(
			rewriteSelfPin(section, selfVersion),
			rewriteSelfPin(canonicalEntryLines(missing, shift), selfVersion)...,
		)
		p.reps = append(
			p.reps,
			aquaReplacement{
				start: manifest.packages.start,
				end:   manifest.packages.end,
				lines: withBlankTail(lines),
			},
		)

		return true
	}
}

// stitch copies every line outside the replaced ranges, swaps in the new
// blocks in place, then appends the sections the file did not have at all.
// Sections may appear in any order in the file, so replacements are applied
// in position order, not the order they were planned in.
func (p *aquaPlan) stitch(manifest aquaManifest) string {
	slices.SortFunc(p.reps, func(left, right aquaReplacement) int { return cmp.Compare(left.start, right.start) })

	var out []string

	cursor := 0
	for _, r := range p.reps {
		out = append(out, manifest.lines[cursor:r.start]...)
		out = append(out, r.lines...)
		cursor = r.end
	}

	out = append(out, manifest.lines[cursor:]...)

	out = trimBlankTail(out)
	for _, block := range p.tail {
		out = append(out, "")
		out = append(out, block...)
	}

	return strings.Join(out, "\n") + "\n"
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

// aquaInertLine reports whether a line carries no YAML content — blank or a
// comment — and so can sit anywhere without affecting the parse.
func aquaInertLine(line string) bool {
	trimmed := strings.TrimSpace(line)

	return trimmed == "" || strings.HasPrefix(trimmed, "#")
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

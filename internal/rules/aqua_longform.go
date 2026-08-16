package rules

import (
	"regexp"
	"strings"
)

// Renovate's aqua preset can only bump a version it can parse. Most tools tag
// semver ("v2.97.0", "1.58.0") and the one-line "name: owner/repo@version"
// form works. A few tag with a prefix — golang/go ("go1.26.6"), jqlang/jq
// ("jq-1.8.2") — and for those the preset carries a dedicated manager that
// strips the prefix, which matches ONLY the two-line form:
//
//	- name: golang/go
//	  version: go1.26.6 # renovate: depName=golang/go
//
// In the one-line form the preset reads "go1.26.6" as the version, cannot
// order it, and silently never proposes a bump: the pin looks maintained and
// is not. So the canonical manifest carries those packages in the two-line
// form, and this file makes that shape stick: the check flags a one-line pin
// of any package the canonical carries in two-line form, and the fix rewrites
// it — keeping the project's own version, moving only the shape.
//
// Which packages: derived from the canonical manifest, never listed here.
// A canonical entry whose second line is "version: <v> # renovate: depName=<name>"
// is a two-line pin; anything else is not.

// aquaShortPinRE is a one-line pin, capturing indent, quote, name, version, and
// whatever follows the version (a closing quote and/or comment).
var aquaShortPinRE = regexp.MustCompile(`^(\s*)-\s+name:\s*(["']?)([^\s"'@#]+)@([^\s"'#]+)(.*)$`)

// aquaVersionLineRE is the second line of a two-line pin, with the renovate
// hook the preset's dedicated managers key on.
var aquaVersionLineRE = regexp.MustCompile(`^\s*version:\s*["']?[^\s"'#]+["']?\s*# renovate: depName=(\S+)\s*$`)

// twoLineCanonicalPkgs maps each package name the canonical manifest pins in
// two-line form to the renovate depName its version line carries — the
// package's own name for the preset's dedicated managers (golang/go,
// jqlang/jq), or "_go/<module>" for go_install modules routed to Renovate's go
// datasource. Computed once from the embedded canonical.
var twoLineCanonicalPkgs = computeTwoLineCanonicalPkgs() //nolint:gochecknoglobals // derived once from embedded canonical data.

func computeTwoLineCanonicalPkgs() map[string]string {
	out := map[string]string{}

	for _, pkg := range canonicalAqua.pkgs {
		if pkg.end-pkg.start < 2 {
			continue
		}

		match := aquaVersionLineRE.FindStringSubmatch(canonicalAqua.lines[pkg.start+1])
		if match == nil {
			continue
		}

		if match[1] == pkg.name || match[1] == "_go/"+pkg.name {
			out[pkg.name] = match[1]
		}
	}

	return out
}

// shortFormPin describes a project entry that pins a two-line-canonical
// package in the one-line form: the line to replace and the two lines that
// replace it.
type shortFormPin struct {
	name      string
	line      int
	rewritten []string
}

// shortFormPins lists, in file order, every one-line pin of a package the
// canonical carries in two-line form: the entry's first line is
// "name: <pkg>@<version>". Continuation lines (registry: local, …) are the
// project's and stay; only that first line is split in two. An entry that
// already carries a version: line has its own shape and is left alone.
func (m *aquaManifest) shortFormPins() []shortFormPin {
	var pins []shortFormPin

	for _, pkg := range m.pkgs {
		depName, twoLine := twoLineCanonicalPkgs[pkg.name]
		if !twoLine {
			continue
		}

		match := aquaShortPinRE.FindStringSubmatch(m.lines[pkg.start])
		if match == nil || match[3] != pkg.name {
			continue
		}

		if hasVersionLine(m.lines[pkg.start+1 : pkg.end]) {
			continue
		}

		indent, quote, version := match[1], match[2], match[4]

		pins = append(pins, shortFormPin{
			name: pkg.name,
			line: pkg.start,
			rewritten: []string{
				indent + "- name: " + quote + pkg.name + quote,
				indent + "  version: " + version + " # renovate: depName=" + depName,
			},
		})
	}

	return pins
}

// hasVersionLine reports whether any continuation line is a version: key.
func hasVersionLine(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "version:") {
			return true
		}
	}

	return false
}

// shortFormPinNames is the names of shortFormPins, for messages.
func (m *aquaManifest) shortFormPinNames() []string {
	var names []string
	for _, pin := range m.shortFormPins() {
		names = append(names, pin.name)
	}

	return names
}

// rewriteShortFormPins returns lines with every one-line pin of a
// two-line-canonical package rewritten to the two-line form. Used when the
// packages section is rebuilt wholesale; the per-line path is in
// mergeAquaManifest. lines are the packages section's lines starting at its
// key line, so entry line indices are relative to sectionStart.
func rewriteShortFormPins(lines []string, pins []shortFormPin, sectionStart int) []string {
	if len(pins) == 0 {
		return lines
	}

	byLine := map[int][]string{}
	for _, pin := range pins {
		byLine[pin.line-sectionStart] = pin.rewritten
	}

	out := make([]string, 0, len(lines)+len(pins))

	for i, line := range lines {
		if rewritten, found := byLine[i]; found {
			out = append(out, rewritten...)

			continue
		}

		out = append(out, line)
	}

	return out
}

// shortFormPinMessage is the check's explanation, shared with the fix summary.
func shortFormPinMessage(names []string) string {
	return "pinned in one-line name@version form, which Renovate cannot bump (prefixed upstream tags): " +
		strings.Join(names, ", ") +
		" — limen fix rewrites them to the two-line form the aqua preset's dedicated managers match"
}

// shortFormReplacements is one replacement per one-line pin — the path taken
// when the packages section is NOT rebuilt wholesale (when it is, the rewrite
// is folded into that replacement by rewriteShortFormPins).
func shortFormReplacements(pins []shortFormPin) []aquaReplacement {
	reps := make([]aquaReplacement, 0, len(pins))
	for _, pin := range pins {
		reps = append(reps, aquaReplacement{start: pin.line, end: pin.line + 1, lines: pin.rewritten})
	}

	return reps
}

// shortFormSummary is the fix summary line for the rewritten pins.
func shortFormSummary(pins []shortFormPin) string {
	names := make([]string, 0, len(pins))
	for _, pin := range pins {
		names = append(names, pin.name)
	}

	return "rewrote to the two-line form Renovate can bump: " + strings.Join(names, ", ")
}

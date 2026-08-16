package rules

import (
	"regexp"
	"strings"
)

// Every aqua.yaml pin is the one-line "name: owner/repo@version" form, and this
// file makes that shape stick. The reason is `aqua update` — what
// `just do tools update <cmd>` runs: it rewrites only that form, and treats a
// version on its own `version:` line as a hand-held pin it must not touch
// (aqua's documented way to freeze a tool). A two-line pin therefore looks
// maintained and is not: the update exits 0 and changes nothing.
//
// Renovate needs nothing from the two-line form either. Its aqua preset reads
// the one-line pin of every package the canonical carries — the generic
// owner/repo@vX manager, the prefix-aware managers for golang/go (go1.x) and
// jqlang/jq (jq-1.x), the golang.org/ manager for the x/ tools — and the
// canonical renovate.json5 adds a manager for the github.com/… go modules the
// preset's generic manager cannot read (a dotted owner). The `# renovate:
// depName=` hook the two-line form carried is not needed and is dropped.
//
// The check flags any package entry with a `version:` line; the fix collapses
// it into the name line — the project's own version kept, quotes kept, every
// other continuation line (registry: local, …) untouched.

// aquaOneLineNameRE is the name line of a two-line pin: no @version, capturing
// indent, quote, name, and a trailing comment if any.
var aquaOneLineNameRE = regexp.MustCompile(`^(\s*)-\s+name:\s*(["']?)([^\s"'@#]+)["']?\s*(#.*)?$`)

// aquaVersionLineRE is a version: continuation line, capturing the version and
// a trailing comment if any.
var aquaVersionLineRE = regexp.MustCompile(`^\s*version:\s*["']?([^\s"'#]+)["']?\s*(#.*)?$`)

// twoLinePin describes a project entry that pins its version on a separate
// version: line: the entry's line range and the lines that replace it.
type twoLinePin struct {
	name      string
	start     int // the entry's name line
	end       int // one past the entry's last continuation line
	rewritten []string
}

// twoLinePins lists, in file order, every package entry that carries a
// version: line. The rewrite keeps every other line of the entry in place and
// folds the version into the name line. A version-line comment that is not the
// two-line form's own "# renovate: depName=" hook is a note of the project's
// and moves to the name line; the hook itself has no one-line meaning and goes.
func (m *aquaManifest) twoLinePins() []twoLinePin {
	var pins []twoLinePin

	for _, pkg := range m.pkgs {
		lines := m.lines[pkg.start:pkg.end]

		nameMatch := aquaOneLineNameRE.FindStringSubmatch(lines[0])
		if nameMatch == nil {
			continue // already name@version (or a shape this rule does not judge)
		}

		versionAt := -1

		var versionMatch []string

		for i := 1; i < len(lines); i++ {
			if match := aquaVersionLineRE.FindStringSubmatch(lines[i]); match != nil {
				versionAt, versionMatch = i, match

				break
			}
		}

		if versionAt == -1 {
			continue
		}

		indent, quote, name, nameComment := nameMatch[1], nameMatch[2], nameMatch[3], nameMatch[4]
		version, versionComment := versionMatch[1], versionMatch[2]

		first := indent + "- name: " + quote + name + "@" + version + quote

		switch {
		case nameComment != "":
			first += " " + nameComment
		case versionComment != "" && !strings.HasPrefix(versionComment, "# renovate:"):
			first += " " + versionComment
		default:
			// No comment to carry, or only the two-line renovate hook, which goes.
		}

		rewritten := make([]string, 0, len(lines)-1)
		rewritten = append(rewritten, first)
		rewritten = append(rewritten, lines[1:versionAt]...)
		rewritten = append(rewritten, lines[versionAt+1:]...)

		pins = append(pins, twoLinePin{name: pkg.name, start: pkg.start, end: pkg.end, rewritten: rewritten})
	}

	return pins
}

// twoLinePinNames is the names of twoLinePins, for messages.
func (m *aquaManifest) twoLinePinNames() []string {
	var names []string
	for _, pin := range m.twoLinePins() {
		names = append(names, pin.name)
	}

	return names
}

// rewriteTwoLinePins returns lines with every two-line pin collapsed to the
// one-line form. Used when the packages section is rebuilt wholesale; the
// per-entry path is in mergeAquaManifest. lines are the packages section's
// lines starting at its key line, so entry line indices are relative to
// sectionStart.
func rewriteTwoLinePins(lines []string, pins []twoLinePin, sectionStart int) []string {
	if len(pins) == 0 {
		return lines
	}

	byStart := map[int]twoLinePin{}
	for _, pin := range pins {
		byStart[pin.start-sectionStart] = pin
	}

	out := make([]string, 0, len(lines))

	for index := 0; index < len(lines); {
		if pin, found := byStart[index]; found {
			out = append(out, pin.rewritten...)
			index += pin.end - pin.start

			continue
		}

		out = append(out, lines[index])
		index++
	}

	return out
}

// twoLinePinMessage is the check's explanation, shared with the fix summary.
func twoLinePinMessage(names []string) string {
	return "pinned on a separate version: line, which `aqua update` (just do tools update) never touches: " +
		strings.Join(names, ", ") +
		" — limen fix collapses them to the one-line name@version form (the form Renovate reads for every package here)"
}

// twoLineReplacements is one replacement per two-line pin — the path taken when
// the packages section is NOT rebuilt wholesale (when it is, the rewrite is
// folded into that replacement by rewriteTwoLinePins).
func twoLineReplacements(pins []twoLinePin) []aquaReplacement {
	reps := make([]aquaReplacement, 0, len(pins))
	for _, pin := range pins {
		reps = append(reps, aquaReplacement{start: pin.start, end: pin.end, lines: pin.rewritten})
	}

	return reps
}

// twoLineSummary is the fix summary line for the collapsed pins.
func twoLineSummary(pins []twoLinePin) string {
	names := make([]string, 0, len(pins))
	for _, pin := range pins {
		names = append(names, pin.name)
	}

	return "collapsed to the one-line name@version form aqua update bumps: " + strings.Join(names, ", ")
}

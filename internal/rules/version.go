package rules

import (
	"strconv"
	"strings"
)

// parsedModuleVersion is a Go module version as semantic versioning orders it: the
// three numbers, and the prerelease identifiers, which is where a
// pseudo-version carries its timestamp and commit.
type parsedModuleVersion struct {
	core       [3]uint64
	prerelease []string
}

// parseModuleVersion reads vMAJOR.MINOR.PATCH[-prerelease][+build]. Build
// metadata (Go's +incompatible) does not order and is dropped.
func parseModuleVersion(version string) (parsedModuleVersion, bool) {
	var parsed parsedModuleVersion

	text, hasV := strings.CutPrefix(version, "v")
	if !hasV {
		return parsed, false
	}

	text, _, _ = strings.Cut(text, "+")
	core, prerelease, hasPrerelease := strings.Cut(text, "-")

	numbers := strings.Split(core, ".")
	if len(numbers) != len(parsed.core) {
		return parsed, false
	}

	for i, number := range numbers {
		value, err := strconv.ParseUint(number, 10, 64)
		if err != nil {
			return parsed, false
		}

		parsed.core[i] = value
	}

	if hasPrerelease {
		if prerelease == "" {
			return parsed, false
		}

		parsed.prerelease = strings.Split(prerelease, ".")
	}

	return parsed, true
}

// moduleVersionOlder reports whether module version have comes before want
// in semantic version precedence, the order Go gives module versions,
// pseudo-versions included. A version either side cannot be read as one is
// not judged: the answer is no.
func moduleVersionOlder(have, want string) bool {
	left, leftOK := parseModuleVersion(have)
	right, rightOK := parseModuleVersion(want)

	if !leftOK || !rightOK {
		return false
	}

	for i := range left.core {
		if left.core[i] != right.core[i] {
			return left.core[i] < right.core[i]
		}
	}

	return prereleaseOlder(left.prerelease, right.prerelease)
}

// prereleaseOlder orders two prerelease lists of one version: a version with
// one comes before the version without; identifiers compare left to right,
// numbers by value and before text, text by bytes; a list that is a prefix
// of the other comes first.
func prereleaseOlder(have, want []string) bool {
	if len(have) == 0 || len(want) == 0 {
		return len(have) > 0 && len(want) == 0
	}

	for i := 0; i < len(have) && i < len(want); i++ {
		if have[i] == want[i] {
			continue
		}

		left, leftErr := strconv.ParseUint(have[i], 10, 64)
		right, rightErr := strconv.ParseUint(want[i], 10, 64)

		switch {
		case leftErr == nil && rightErr == nil:
			return left < right
		case leftErr == nil:
			return true
		case rightErr == nil:
			return false
		default:
			return have[i] < want[i]
		}
	}

	return len(have) < len(want)
}

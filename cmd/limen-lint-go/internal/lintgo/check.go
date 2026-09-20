package lintgo

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The module a golangci-lint binary is built from, and how a version reads.
const (
	golangciModule   = "github.com/golangci/golangci-lint/v2"
	windowsExe       = ".exe"
	notRelease       = "%w: %q is not a release version"
	versionPrefix    = "v"
	versionSeparator = "."
	versionParts     = 3
	checkArgs        = 1
)

// check fails when the golangci-lint named by args is older than the
// baseline's floor: the baseline names linters, and a name unknown to an
// older release fails the run. The argument is the binary, or a bare
// vMAJOR.MINOR.PATCH to ask about a release before pinning it.
func check(args []string, raw []byte) error {
	if len(args) != checkArgs {
		return fmt.Errorf("%w: %s takes the golangci-lint binary (or a version) and nothing else", ErrUsage, cmdCheck)
	}

	base, err := parseBaseline(raw, "")
	if err != nil {
		return err
	}

	version := args[0]
	if _, isVersion := parseVersion(version); isVersion != nil {
		version, err = golangciVersion(args[0])
		if err != nil {
			return err
		}
	}

	older, err := olderThan(version, base.floor)
	if err != nil {
		return err
	}

	if older {
		return fmt.Errorf("%w: %s is %s, the baseline is written for %s and later"+
			" — just do tools update golangci-lint", ErrFloor, args[0], version, base.floor)
	}

	return nil
}

// golangciVersion is the golangci-lint module version stamped in the binary
// at path (or path.exe, the name go gives it on windows).
func golangciVersion(path string) (string, error) {
	info, err := buildinfo.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		info, err = buildinfo.ReadFile(path + windowsExe)
	}

	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBinary, err)
	}

	if info.Main.Path == golangciModule {
		return info.Main.Version, nil
	}

	for _, dep := range info.Deps {
		if dep.Path == golangciModule {
			return dep.Version, nil
		}
	}

	return "", fmt.Errorf(
		"%w: %s is not built from %s (main module %s)", ErrBinary, path, golangciModule, info.Main.Path,
	)
}

// olderThan compares two vMAJOR.MINOR.PATCH versions, a pre-release suffix
// ignored: a floor is a release, and a pre-release of it is close enough.
func olderThan(version, floor string) (bool, error) {
	have, err := parseVersion(version)
	if err != nil {
		return false, err
	}

	want, err := parseVersion(floor)
	if err != nil {
		return false, fmt.Errorf("%w: floor %w", ErrBaseline, err)
	}

	for index := range versionParts {
		if have[index] != want[index] {
			return have[index] < want[index], nil
		}
	}

	return false, nil
}

// parseVersion is the three numbers of a vMAJOR.MINOR.PATCH[-pre][+meta].
func parseVersion(version string) ([versionParts]int, error) {
	var parts [versionParts]int

	core, hasPrefix := strings.CutPrefix(version, versionPrefix)
	if !hasPrefix {
		return parts, fmt.Errorf(notRelease, ErrBinary, version)
	}

	core, _, _ = strings.Cut(core, "-")
	core, _, _ = strings.Cut(core, "+")

	numbers := strings.Split(core, versionSeparator)
	if len(numbers) != versionParts {
		return parts, fmt.Errorf(notRelease, ErrBinary, version)
	}

	for index, number := range numbers {
		value, err := strconv.Atoi(number)
		if err != nil {
			return parts, fmt.Errorf(notRelease, ErrBinary, version)
		}

		parts[index] = value
	}

	return parts, nil
}

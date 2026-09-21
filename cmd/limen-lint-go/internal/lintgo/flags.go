package lintgo

import (
	"fmt"
	"io"
	"strings"
)

// The flags the lanes splice in: go-licenses' and NilAway's.
const (
	flagAllowedLicenses = "--allowed_licenses="
	flagIgnore          = "--ignore="
	flagIncludePkgs     = "-include-pkgs="
	flagExcludePkgs     = "-exclude-pkgs="
	flagExcludeFiles    = "-exclude-errors-in-files="
	flagPlainOutput     = "-pretty-print=false"
	listSeparator       = ","
	flagSeparator       = " "
	flagsArgs           = 1
)

// flags prints one lane's flags for dir's module, on one line.
func flags(args []string, dir string, stdout io.Writer) error {
	if len(args) != flagsArgs {
		return fmt.Errorf("%w: %s takes one lane, %s or %s", ErrUsage, cmdFlags, laneLicenses, laneNilaway)
	}

	base, _, err := load(dir)
	if err != nil {
		return err
	}

	var parts []string

	switch args[0] {
	case laneLicenses:
		parts, err = licensesFlags(base)
	case laneNilaway:
		parts, err = nilawayFlags(base)
	default:
		return fmt.Errorf("%w: %s takes one lane, %s or %s", ErrUsage, cmdFlags, laneLicenses, laneNilaway)
	}

	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, strings.Join(parts, flagSeparator))

	return nil
}

// licensesFlags is go-licenses' allowed list and one --ignore per ignored
// module.
func licensesFlags(base baseline) ([]string, error) {
	allowed, err := stringList(base.licenses[keyAllowed], keyLicenses+keyJoin+keyAllowed)
	if err != nil {
		return nil, err
	}

	ignored, err := stringList(base.licenses[keyIgnore], keyLicenses+keyJoin+keyIgnore)
	if err != nil {
		return nil, err
	}

	parts := []string{flagAllowedLicenses + strings.Join(allowed, listSeparator)}
	for _, module := range ignored {
		parts = append(parts, flagIgnore+module)
	}

	return parts, nil
}

// nilawayFlags bounds the analysis to the module (without -include-pkgs
// NilAway walks the whole dependency graph, and its fact store grows with
// it), keeps the output plain for the log, and carries the two exclude
// lists when the project declares any.
func nilawayFlags(base baseline) ([]string, error) {
	parts := []string{flagIncludePkgs + base.module, flagPlainOutput}

	for _, lane := range []struct{ key, flag string }{
		{keyExcludePkgs, flagExcludePkgs},
		{keyExcludeFiles, flagExcludeFiles},
	} {
		entries, err := stringList(base.nilaway[lane.key], keyNilaway+keyJoin+lane.key)
		if err != nil {
			return nil, err
		}

		if len(entries) > 0 {
			parts = append(parts, lane.flag+strings.Join(entries, listSeparator))
		}
	}

	return parts, nil
}

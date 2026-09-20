package lintgo

import (
	"fmt"
	"io"
	"strings"
)

// The go-licenses flags the licenses lane splices in.
const (
	flagAllowedLicenses = "--allowed_licenses="
	flagIgnore          = "--ignore="
	listSeparator       = ","
	flagSeparator       = " "
	flagsArgs           = 1
)

// flags prints one lane's flags for dir's module, on one line: for licenses,
// the allowed list and one --ignore per ignored module.
func flags(args []string, dir string, raw []byte, stdout io.Writer) error {
	if len(args) != flagsArgs || args[0] != laneLicenses {
		return fmt.Errorf("%w: %s takes one lane, %s", ErrUsage, cmdFlags, laneLicenses)
	}

	base, _, err := load(dir, raw)
	if err != nil {
		return err
	}

	allowed, err := stringList(base.licenses[keyAllowed], keyLicenses+keyJoin+keyAllowed)
	if err != nil {
		return err
	}

	ignored, err := stringList(base.licenses[keyIgnore], keyLicenses+keyJoin+keyIgnore)
	if err != nil {
		return err
	}

	parts := []string{flagAllowedLicenses + strings.Join(allowed, listSeparator)}
	for _, module := range ignored {
		parts = append(parts, flagIgnore+module)
	}

	fmt.Fprintln(stdout, strings.Join(parts, flagSeparator))

	return nil
}

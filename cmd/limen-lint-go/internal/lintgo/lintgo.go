// Package lintgo is the limen-lint-go driver: it renders the Go lint
// baseline with a project's carve-outs into the configuration the lint
// recipes run, checks the pinned golangci-lint against the baseline's floor,
// and prints the go-licenses flags. The baseline is handed in by the caller
// (main embeds it); the carve-outs are the .lint-go.yaml of the module
// limen-lint-go runs in, absent meaning none.
package lintgo

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// OverlayFile is the project's carve-outs, at the root of the module
// limen-lint-go runs in: the baseline's sections, in the baseline's shape,
// holding only what this project adds, changes or takes out.
const OverlayFile = ".lint-go.yaml"

// The subcommands and their arguments.
const (
	cmdRender    = "render"
	cmdCheck     = "check"
	cmdFlags     = "flags"
	cmdBaseline  = "baseline"
	laneLicenses = "licenses"
	flagDir      = "C"
	flagOut      = "o"
)

// Exit statuses.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// Every failure wraps one of these.
var (
	// ErrUsage is a command line limen-lint-go does not understand.
	ErrUsage = errors.New("limen-lint-go: usage")
	// ErrOverlay is a .lint-go.yaml that is not a carve-out: a key outside
	// the vocabulary, a value of the wrong shape.
	ErrOverlay = errors.New("limen-lint-go: " + OverlayFile)
	// ErrBaseline is a baseline limen-lint-go cannot read; it ships inside
	// the binary, so this is a limen defect.
	ErrBaseline = errors.New("limen-lint-go: baseline")
	// ErrModule is a working directory without a readable go.mod.
	ErrModule = errors.New("limen-lint-go: go.mod")
	// ErrBinary is a golangci-lint binary whose build information cannot be
	// read, or is not golangci-lint's.
	ErrBinary = errors.New("limen-lint-go: golangci-lint binary")
	// ErrFloor is a golangci-lint older than the baseline's floor.
	ErrFloor = errors.New("limen-lint-go: golangci-lint too old")
)

const usage = `usage:
  limen-lint-go [-C DIR] render [-o FILE]   the golangci-lint configuration: the baseline plus ` + OverlayFile + `
  limen-lint-go [-C DIR] flags licenses     the go-licenses flags: the allowed licenses and the ignored modules
  limen-lint-go check GOLANGCI-BINARY       fail when the binary is older than the baseline's floor
  limen-lint-go baseline                    the baseline as shipped, before any carve-out
-C DIR is the module to run in (its go.mod and ` + OverlayFile + `); the working directory by default.`

// Run executes one subcommand with the baseline and returns the exit status;
// diagnostics go to stderr, the render, the flags and the baseline to stdout.
func Run(args []string, baseline []byte, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("limen-lint-go", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	dir := global.String(flagDir, ".", "the module to run in")

	if err := global.Parse(args); err != nil || global.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, usage)

		return exitUsage
	}

	rest := global.Args()

	var err error

	switch rest[0] {
	case cmdRender:
		err = render(rest[1:], *dir, baseline, stdout, stderr)
	case cmdCheck:
		err = check(rest[1:], baseline)
	case cmdFlags:
		err = flags(rest[1:], *dir, baseline, stdout)
	case cmdBaseline:
		err = printBaseline(rest[1:], baseline, stdout)
	default:
		err = fmt.Errorf("%w: unknown command %q", ErrUsage, rest[0])
	}

	if err == nil {
		return exitOK
	}

	_, _ = fmt.Fprintln(stderr, err)

	if errors.Is(err, ErrUsage) {
		_, _ = fmt.Fprintln(stderr, usage)

		return exitUsage
	}

	return exitError
}

// printBaseline writes the baseline as shipped, placeholders and comments
// included: what a project carves out from, readable without the source.
func printBaseline(args []string, baseline []byte, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: %s takes no argument", ErrUsage, cmdBaseline)
	}

	if _, err := stdout.Write(baseline); err != nil {
		return fmt.Errorf("%w: writing: %w", ErrBaseline, err)
	}

	return nil
}

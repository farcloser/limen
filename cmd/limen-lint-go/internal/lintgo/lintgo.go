// Package lintgo is the limen-lint-go driver: it renders the Go lint
// baseline with a project's carve-outs into the configuration the lint
// recipes run, checks the pinned golangci-lint against the baseline's floor,
// and prints the go-licenses flags. The baseline is the repository's
// .limen/lint-go.yaml, content-pinned by limen and found in the nearest
// .limen/ at or above the module limen-lint-go runs in; the carve-outs are
// that module's .lint-go.yaml, absent meaning none.
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
	cmdMode      = "mode"
	cmdDisabled  = "disabled"
	laneLicenses = "licenses"
	laneNilaway  = "nilaway"
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
	// ErrBaseline is a baseline limen-lint-go cannot find or read: the file
	// is content-pinned by limen, so a missing one wants `limen fix` and a
	// malformed one is a limen defect.
	ErrBaseline = errors.New("limen-lint-go: " + BaselineFile)
	// ErrModule is a working directory without a readable go.mod.
	ErrModule = errors.New("limen-lint-go: go.mod")
	// ErrBinary is a golangci-lint binary whose build information cannot be
	// read, or is not golangci-lint's.
	ErrBinary = errors.New("limen-lint-go: golangci-lint binary")
	// ErrFloor is a golangci-lint older than the baseline's floor.
	ErrFloor = errors.New("limen-lint-go: golangci-lint too old")
)

const usage = `usage:
  limen-lint-go [-C DIR] render [-o FILE]        the golangci-lint configuration: the baseline plus ` + OverlayFile + `
  limen-lint-go [-C DIR] flags licenses          the go-licenses flags: the allowed licenses and the ignored modules
  limen-lint-go [-C DIR] flags nilaway           the NilAway flags: the module to analyze and the exclusions
  limen-lint-go [-C DIR] mode nilaway            whether NilAway's findings fail the run: blocking or informational
  limen-lint-go [-C DIR] disabled revive         the revive rules the configuration turns off, one per line
  limen-lint-go [-C DIR] check GOLANGCI-BINARY   fail when the binary is older than the baseline's floor
-C DIR is the module to run in (its go.mod and ` + OverlayFile + `); the working directory by default.
The baseline is ` + BaselineFile + ` in the nearest .limen/ at or above DIR: the repository's, placed by limen fix.`

// Run executes one subcommand and returns the exit status; diagnostics go to
// stderr, the render and the flags to stdout.
func Run(args []string, stdout, stderr io.Writer) int {
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
		err = render(rest[1:], *dir, stdout, stderr)
	case cmdCheck:
		err = check(rest[1:], *dir)
	case cmdFlags:
		err = flags(rest[1:], *dir, stdout)
	case cmdMode:
		err = mode(rest[1:], *dir, stdout)
	case cmdDisabled:
		err = disabled(rest[1:], *dir, stdout)
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

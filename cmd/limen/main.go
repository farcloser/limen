// Command limen verifies a repository against Farcloser's engineering rules.
//
// Usage:
//
//	limen check [-json] [path]
//
// It exits 0 when the repository complies and 1 when any rule fails, so it can
// be dropped into pre-commit, CI, or an agent's workflow unchanged.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/farcloser/limen/internal/license"
	"github.com/farcloser/limen/internal/rules"
)

// version is stamped at build time via -X main.version: goreleaser injects the
// release version, `just build go` injects `git describe`; a plain `go build`
// reports "dev".
var version = "dev"

// releaseVersionRE is an exact release version, optionally missing the v
// prefix (goreleaser's {{ .Version }} strips it).
var releaseVersionRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$`)

// describeSuffixRE is git describe's commits-since-tag suffix (-N-g<sha>),
// which the prerelease grammar of releaseVersionRE would otherwise accept.
var describeSuffixRE = regexp.MustCompile(`-\d+-g[0-9a-f]{4,}$`)

// releaseVersion returns the running binary's version as an exact release tag
// (vX.Y.Z[-pre]), or "" for anything else: "dev" (plain go build / go run), a
// bare commit sha (no tags), git describe's -N-g<sha> or -dirty forms, and
// goreleaser -SNAPSHOT builds. Misclassifying a release as dev is safe (the
// seeded pin falls back to the embedded manifest's); the guards below keep the
// reverse from happening.
func releaseVersion() string {
	stamp := version
	if stamp == "" || stamp == "dev" ||
		strings.HasSuffix(
			stamp,
			"-dirty",
		) || strings.Contains(stamp, "SNAPSHOT") || describeSuffixRE.MatchString(stamp) {
		return ""
	}

	if !strings.HasPrefix(stamp, "v") {
		stamp = "v" + stamp
	}

	if !releaseVersionRE.MatchString(stamp) {
		return ""
	}

	return stamp
}

// Subcommand and flag names, shared between dispatch, flag setup, and reporting.
const (
	cmdCheck     = "check"
	cmdFix       = "fix"
	cmdBootstrap = "bootstrap"
	flagJSON     = "json"
)

// errFormat is the uniform "limen: <error>" stderr line.
const errFormat = "limen: %v\n"

// aquaBinary is the aqua executable bootstrap shells out to.
const aquaBinary = "aqua"

// dirPermissions is owner-only, like everything limen creates — loosening a
// checkout is the user's deliberate act, never the tool's.
const dirPermissions = 0o700

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)

		return 2
	}

	switch args[0] {
	case cmdCheck:
		return runCheck(args[1:], stdout, stderr)
	case cmdFix:
		return runFix(args[1:], stdout, stderr)
	case cmdBootstrap:
		return runBootstrap(args[1:], stdout, stderr)
	case cmdGithub:
		return runGithub(args[1:], stdout, stderr)
	case "version", "-v", "--version":
		_, _ = fmt.Fprintln(stdout, "limen "+version)

		return 0
	case "-h", "--help", "help":
		usage(stdout)

		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "limen: unknown command %q\n\n", args[0])
		usage(stderr)

		return 2
	}
}

// splitPathFromFlags separates the single optional positional (the path) from
// flag tokens, so the path may come before or after flags (`limen fix . -json`
// and `limen fix -json .` both work). Only safe for subcommands whose flags
// are boolean (or given as -flag=value): a flag never consumes the following
// token — which is why bootstrap, whose -license and -holder take a value,
// keeps standard flags-then-path parsing. A "--" token ends flag parsing, so
// a path that begins with "-" can still be passed; a lone "-" is a path.
func splitPathFromFlags(args []string) (flags, positional []string) {
	endOfFlags := false

	for _, arg := range args {
		switch {
		case endOfFlags:
			positional = append(positional, arg)
		case arg == "--":
			endOfFlags = true
		case arg != "-" && strings.HasPrefix(arg, "-"):
			flags = append(flags, arg)
		default:
			positional = append(positional, arg)
		}
	}

	return flags, positional
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdCheck, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	asJSON := flagSet.Bool(flagJSON, false, "emit findings as JSON")
	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: limen check [-json] [path]")

		flagSet.PrintDefaults()
	}

	flags, positional := splitPathFromFlags(args)

	if err := flagSet.Parse(flags); err != nil {
		return 2
	}

	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "limen: too many paths (got %d)\n", len(positional))

		return 2
	}

	root := "."
	if len(positional) == 1 {
		root = positional[0]
	}

	// The user-designated path IS the program input — checking it is the point.
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		_, _ = fmt.Fprintf(stderr, "limen: %s is not a directory\n", root)

		return 2
	}

	findings := rules.Check(root, rules.DefaultPolicy())

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")

		if err := enc.Encode(findings); err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}
	} else {
		printFindings(stdout, root, findings)
	}

	if rules.AllOK(findings) {
		return 0
	}

	return 1
}

func runFix(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdFix, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	asJSON := flagSet.Bool(flagJSON, false, "emit outcomes as JSON")

	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: limen fix [-json] [path]")

		flagSet.PrintDefaults()
	}

	flags, positional := splitPathFromFlags(args)

	if err := flagSet.Parse(flags); err != nil {
		return 2
	}

	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "limen: too many paths (got %d)\n", len(positional))

		return 2
	}

	root := "."
	if len(positional) == 1 {
		root = positional[0]
	}

	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		_, _ = fmt.Fprintf(stderr, "limen: %s is not a directory\n", root)

		return 2
	}

	// fix never creates a LICENSE: no License in the options.
	outcomes := rules.Fix(root, rules.FixOptions{Policy: rules.DefaultPolicy(), SelfVersion: releaseVersion()})

	return reportOutcomes(stdout, stderr, cmdFix, root, outcomes, *asJSON)
}

func runBootstrap(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdBootstrap, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	asJSON := flagSet.Bool(flagJSON, false, "emit outcomes as JSON")
	force := flagSet.Bool("force", false, "proceed even if the target directory is not empty")
	licenseID := flagSet.String("license", string(license.Closed), "license for the new repository")
	holder := flagSet.String("holder", "Farcloser", "copyright holder for a generated LICENSE")

	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(
			stderr,
			"Usage: limen bootstrap [-license id] [-holder name] [-force] [-json] <path>",
		)

		flagSet.PrintDefaults()
	}
	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	if flagSet.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "limen: bootstrap needs exactly one path")

		return 2
	}

	root := flagSet.Arg(0)

	if !license.CanGenerate(license.ID(*licenseID)) {
		_, _ = fmt.Fprintf(stderr, "limen: cannot generate a %q LICENSE (allowed: %s)\n", *licenseID, allowedLicenses())

		return 2
	}

	if !dirEmpty(root) && !*force {
		_, _ = fmt.Fprintf(stderr, "limen: %s is not empty (use -force to bootstrap anyway)\n", root)

		return 2
	}

	if err := os.MkdirAll(root, dirPermissions); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	// A dev build cannot pin itself: there is no release (and no checksums)
	// matching the working tree. Warn — the files seeded from this tree may
	// disagree with the embedded pin's release until that pin is bumped.
	if releaseVersion() == "" {
		_, _ = fmt.Fprintln(
			stderr,
			"limen: dev build — the seeded aqua.yaml keeps the embedded limen pin, which may predate the files seeded from this working tree",
		)
	}

	outcomes := rules.Fix(root, rules.FixOptions{
		Policy:      rules.DefaultPolicy(),
		License:     license.ID(*licenseID),
		Holder:      *holder,
		Year:        time.Now().Year(),
		SelfVersion: releaseVersion(),
	})

	code := reportOutcomes(stdout, stderr, cmdBootstrap, root, outcomes, *asJSON)
	if code != 0 {
		return code
	}
	// Install the pinned tooling: authorize the local registry, then link the
	// tools (aqua downloads each lazily on first use). All output goes to stderr
	// so -json keeps a clean machine-readable stdout.
	if err := installTooling(root, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)
		_, _ = fmt.Fprintf(stderr, "the repository is set up; once aqua is available run, from %s:\n", root)
		_, _ = fmt.Fprint(
			stderr,
			"  aqua policy allow aqua-policy.yaml && aqua update-checksum --prune && aqua install --only-link\n",
		)

		return 1
	}

	return 0
}

// installTooling runs aqua to make the repo's pinned tools available: it
// authorizes the committed local-registry policy, refreshes the checksums for
// whatever the manifest pins (a no-op on the pristine seed), then installs
// (links) every tool. It runs with the repo as the working directory so aqua
// finds aqua.yaml.
func installTooling(root string, progress io.Writer) error {
	steps := [][]string{
		{"policy", "allow", "aqua-policy.yaml"},
		{"update-checksum", "--prune"},
		{"install", "--only-link"},
	}
	for _, step := range steps {
		// --log-level warn: aqua's per-package INFO lines drown the bootstrap
		// output; warnings and errors still come through.
		args := append([]string{"--log-level", "warn"}, step...)
		command := aquaBinary + " " + strings.Join(args, " ")

		_, _ = fmt.Fprintf(progress, "\n$ %s\n", command)

		// aquaBinary is a constant; every argument is from the fixed lists above.
		cmd := exec.CommandContext(context.Background(), aquaBinary, args...) //nolint:gosec // G204: see above.
		cmd.Dir = root
		cmd.Stdout = progress

		cmd.Stderr = progress
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", command, err)
		}
	}

	return nil
}

// allowedLicenses is the comma-separated list of license ids bootstrap accepts,
// for use in error messages.
func allowedLicenses() string {
	var ids []string
	for _, id := range rules.DefaultPolicy().AllowedLicenses {
		ids = append(ids, string(id))
	}

	return strings.Join(ids, ", ")
}

// dirEmpty reports whether root does not exist or is an empty directory. A path
// that exists but is not a directory counts as non-empty (bootstrap will reject
// it rather than overwrite).
func dirEmpty(root string) bool {
	info, err := os.Stat(root)
	if err != nil {
		return true // does not exist yet
	}

	if !info.IsDir() {
		return false
	}

	entries, err := os.ReadDir(root)

	return err == nil && len(entries) == 0
}

// The suppression below is revive's own directive rather than a golangci one,
// deliberately: nolintlint polices golangci directives, and this finding is
// environment-nondeterministic across the per-GOOS legs — the policing itself
// then flakes.
//
//revive:disable:flag-parameter

// reportOutcomes prints remediation outcomes and returns the process exit code:
// 0 when every rule is now compliant, 1 when any advisory or failure remains.
// asJSON is the user's -json flag: an output mode is domain data, and every
// call site passes the self-describing *asJSON — not the opaque-literal
// control coupling the rule guards against.
func reportOutcomes(
	stdout, stderr io.Writer,
	verb, root string,
	outcomes []rules.Outcome,
	asJSON bool,
) int {
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")

		if err := enc.Encode(outcomes); err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}
	} else {
		printOutcomes(stdout, verb, root, outcomes)
	}

	if rules.AllResolved(outcomes) {
		return 0
	}

	return 1
}

//revive:enable:flag-parameter

func printOutcomes(writer io.Writer, verb, root string, outcomes []rules.Outcome) {
	_, _ = fmt.Fprintf(writer, "limen %s %s\n", verb, root)
	unresolved := 0

	for _, outcome := range outcomes {
		var mark string

		switch outcome.Action {
		case rules.ActionNone:
			mark = "✓"
		case rules.ActionCreated, rules.ActionOverwrote, rules.ActionMerged:
			mark = "✎"
		case rules.ActionAdvisory:
			mark = "!"
			unresolved++
		case rules.ActionFailed:
			mark = "✗"
			unresolved++
		default:
			// An action this version does not know: show it neutrally rather
			// than guessing whether it resolved the rule.
			mark = "•"
		}

		_, _ = fmt.Fprintf(writer, "  %s  %-12s %-10s %s\n", mark, outcome.Rule, outcome.Action, outcome.Message)
	}

	if unresolved == 0 {
		_, _ = fmt.Fprint(writer, "\nall rules resolved\n")

		return
	}

	_, _ = fmt.Fprintf(writer, "\n%d rule(s) need manual attention\n", unresolved)
}

func printFindings(writer io.Writer, root string, findings []rules.Finding) {
	_, _ = fmt.Fprintf(writer, "limen check %s\n", root)
	failed := 0

	for _, finding := range findings {
		mark := "✓"
		if !finding.OK() {
			mark = "✗"
			failed++
		}

		_, _ = fmt.Fprintf(writer, "  %s  %-12s %s\n", mark, finding.Rule, finding.Message)
	}

	if failed == 0 {
		_, _ = fmt.Fprintf(writer, "\n%d/%d rules passed\n", len(findings), len(findings))

		return
	}

	_, _ = fmt.Fprintf(writer, "\n%d/%d rules failed\n", failed, len(findings))
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `limen — verify a repository against Farcloser engineering rules

Usage:
  limen check [-json] [path]              Check the repository at path (default ".")
  limen fix [-json] [path]                Remediate the repository at path (default ".")
  limen bootstrap [flags] <path>          Create a new compliant repository at path
  limen github check [-repo owner/name] [-org name]   Audit GitHub repository or organization settings (via gh)
  limen github fix [-repo] [-org] [-yes]  Repair the fixable GitHub settings
  limen version                           Print the limen version
  limen help                              Show this help

bootstrap flags:
  -license id     License for the new repo (default "Closed-source")
  -holder name    Copyright holder for a generated LICENSE (default "Farcloser")
  -force          Proceed even if the target directory is not empty

After writing files, bootstrap runs "aqua policy allow aqua-policy.yaml",
"aqua update-checksum --prune", and "aqua install --only-link" to install the
pinned tooling.

Exit codes:
  0  success (check: all passed; fix/bootstrap: all rules resolved)
  1  check: a rule failed; fix/bootstrap: a rule needs manual attention
  2  usage error
`)
}

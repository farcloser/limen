package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/farcloser/limen/internal/github"
)

// cmdGithub is the settings-audit subcommand family (design/LIMEN-GITHUB.md).
const cmdGithub = "github"

func runGithub(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		githubUsage(stderr)

		return 2
	}

	switch args[0] {
	case cmdCheck:
		return runGithubCheck(args[1:], stdout, stderr)
	case cmdFix:
		return runGithubFix(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "limen: unknown github command %q\n\n", args[0])
		githubUsage(stderr)

		return 2
	}
}

func githubUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `Usage:
  limen github check [-repo owner/name] [-org name] [-json]         Audit GitHub settings
  limen github fix   [-repo owner/name] [-org name] [-yes] [-json]  Repair what is safe to repair

Without -org, the target is a repository (-repo, or inferred from the origin
remote). With -org, the organization's own settings are audited instead
(membership floor, org-wide Actions policy, security configuration, standing
inventories, and the org .github community-health repository).

The baseline is a floor: stricter than it passes, looser fails. Exceptions are
declared, with reasons, in `+github.OverridePath+` (org runs read it from the
working directory — canonically the org's .github repository).
Verdicts: ok · fail (auto-fixable) · advisory (never auto-fixed) · unverifiable
(the token cannot answer — which does not count as passing).
`)
}

// auditRunner is the audit a command run performs, bound to its target.
type auditRunner func(overrides map[string]string) ([]github.Finding, []github.Change)

// githubAudit resolves the audit target from the mutually exclusive -repo and
// -org flags: the named organization when -org is given, a repository (named
// or inferred from origin) otherwise. It returns the audit to run and the
// label findings are reported under.
func githubAudit(repoFlag, orgFlag string, stderr io.Writer) (auditRunner, string, bool) {
	if orgFlag != "" && repoFlag != "" {
		_, _ = fmt.Fprintln(stderr, "limen: -repo and -org are mutually exclusive — audit one target per run")

		return nil, "", false
	}

	if orgFlag != "" {
		runner := func(overrides map[string]string) ([]github.Finding, []github.Change) {
			return github.AuditOrg(orgFlag, overrides)
		}

		return runner, "org " + orgFlag, true
	}

	repo, resolved := githubTarget(repoFlag, stderr)
	if !resolved {
		return nil, "", false
	}

	runner := func(overrides map[string]string) ([]github.Finding, []github.Change) {
		return github.Audit(repo, overrides)
	}

	return runner, repo, true
}

// githubNoPositional rejects positional arguments: the github subcommands
// name their target through -repo/-org only. Silently dropping them once had
// `limen github check owner/name` audit whatever repository the current
// directory's origin pointed at — reporting for the wrong target as if the
// request had been honored (and stdlib flag parsing stops at the first
// non-flag token, so flags after the stray argument were dropped with it).
func githubNoPositional(flagSet *flag.FlagSet, stderr io.Writer) bool {
	if flagSet.NArg() == 0 {
		return true
	}

	_, _ = fmt.Fprintf(stderr,
		"limen: unexpected argument %q — name the target with -repo owner/name or -org name\n",
		flagSet.Arg(0))

	return false
}

// githubTarget resolves the repository slug: the -repo flag when given, the
// origin remote of the current directory otherwise.
func githubTarget(repoFlag string, stderr io.Writer) (string, bool) {
	if repoFlag != "" {
		return repoFlag, true
	}

	slug, err := github.InferRepo(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return "", false
	}

	return slug, true
}

func runGithubCheck(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdGithub+" "+cmdCheck, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	repoFlag := flagSet.String("repo", "", "repository slug (owner/name); default: inferred from origin")
	orgFlag := flagSet.String("org", "", "organization name; audits the org's own settings instead of a repository")
	asJSON := flagSet.Bool(flagJSON, false, "emit findings as JSON")

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	if !githubNoPositional(flagSet, stderr) {
		return 2
	}

	audit, label, resolved := githubAudit(*repoFlag, *orgFlag, stderr)
	if !resolved {
		return 2
	}

	overrides, err := github.LoadOverrides(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	findings, _ := audit(overrides)

	printer := printGithubFindingsText
	if *asJSON {
		printer = printGithubFindingsJSON
	}

	return reportGithubOutcome(stdout, stderr, label, findings, printer)
}

func runGithubFix(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdGithub+" "+cmdFix, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	repoFlag := flagSet.String("repo", "", "repository slug (owner/name); default: inferred from origin")
	orgFlag := flagSet.String("org", "", "organization name; repairs the org's own settings instead of a repository")
	asJSON := flagSet.Bool(flagJSON, false, "emit the post-fix findings as JSON")
	yes := flagSet.Bool("yes", false, "apply the plan without prompting")

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	if !githubNoPositional(flagSet, stderr) {
		return 2
	}

	audit, label, resolved := githubAudit(*repoFlag, *orgFlag, stderr)
	if !resolved {
		return 2
	}

	overrides, err := github.LoadOverrides(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	printer := printGithubFindingsText

	// With -json, stdout carries the findings document and nothing else —
	// the plan, the prompt, and "nothing to fix" are progress, and progress
	// goes to stderr (the same split bootstrap makes).
	progress := stdout

	if *asJSON {
		printer = printGithubFindingsJSON
		progress = stderr
	}

	findings, changes := audit(overrides)
	if len(changes) == 0 {
		_, _ = fmt.Fprintln(progress, "nothing to fix")

		return reportGithubOutcome(stdout, stderr, label, findings, printer)
	}

	_, _ = fmt.Fprintf(progress, "limen github fix %s — plan:\n", label)

	for _, planned := range changes {
		_, _ = fmt.Fprintf(progress, "  ✎  %-32s %s\n", planned.Check, planned.Summary)
	}

	if !*yes && !confirm(progress) {
		_, _ = fmt.Fprintln(stderr, "not applied (confirm with y, or pass -yes)")

		return 1
	}

	failed := 0

	for _, planned := range changes {
		if applyErr := planned.Apply(); applyErr != nil {
			failed++

			_, _ = fmt.Fprintf(stderr, "limen: %s: %v\n", planned.Check, applyErr)
		}
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(stderr, "limen: %d change(s) failed to apply\n", failed)
	}

	// Re-audit: the post-state, not the intent, is what gets reported.
	final, _ := audit(overrides)

	return reportGithubOutcome(stdout, stderr, label, final, printer)
}

// confirm asks for interactive consent on stdin (the plan was just printed
// to the same writer — stdout normally, stderr under -json).
func confirm(writer io.Writer) bool {
	_, _ = fmt.Fprint(writer, "apply? [y/N] ")

	reader := bufio.NewReader(os.Stdin)

	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y" || answer == "yes"
}

// findingsPrinter renders findings to the output stream. The -json flag picks
// the printer where it is parsed, so no output-mode boolean travels through
// the call graph (and no linter suppression has to, either).
type findingsPrinter func(writer io.Writer, repo string, findings []github.Finding) error

// reportGithubOutcome prints the findings and derives the exit code.
func reportGithubOutcome(
	stdout, stderr io.Writer,
	repo string,
	findings []github.Finding,
	printer findingsPrinter,
) int {
	if err := printer(stdout, repo, findings); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	if github.AllOK(findings) {
		return 0
	}

	return 1
}

func printGithubFindingsJSON(writer io.Writer, _ string, findings []github.Finding) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	//nolint:wrapcheck // the caller folds this into the uniform "limen: <error>" line.
	return encoder.Encode(findings)
}

func printGithubFindingsText(writer io.Writer, repo string, findings []github.Finding) error {
	_, _ = fmt.Fprintf(writer, "limen github check %s\n", repo)

	counts := map[github.Status]int{}

	for _, finding := range findings {
		counts[finding.Status]++

		var mark string

		switch finding.Status {
		case github.StatusOK:
			mark = "✓"
		case github.StatusFail:
			mark = "✗"
		case github.StatusAdvisory:
			mark = "!"
		case github.StatusUnverifiable:
			mark = "?"
		default:
			mark = "•"
		}

		_, _ = fmt.Fprintf(writer, "  %s  %-32s %s\n", mark, finding.Check, finding.Message)
	}

	_, _ = fmt.Fprintf(writer, "\n%d ok · %d fail · %d advisory · %d unverifiable\n",
		counts[github.StatusOK], counts[github.StatusFail],
		counts[github.StatusAdvisory], counts[github.StatusUnverifiable])

	return nil
}

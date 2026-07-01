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

// cmdGithub is the settings-audit subcommand family (design/LIMEN-GH.md).
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
  limen github check [-repo owner/name] [-json]         Audit GitHub repository settings
  limen github fix   [-repo owner/name] [-yes] [-json]  Repair what is safe to repair

The baseline is a floor: stricter than it passes, looser fails. Exceptions are
declared, with reasons, in `+github.OverridePath+`.
Verdicts: ok · fail (auto-fixable) · advisory (never auto-fixed) · unverifiable
(the token cannot answer — which does not count as passing).
`)
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
	asJSON := flagSet.Bool(flagJSON, false, "emit findings as JSON")

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	repo, resolved := githubTarget(*repoFlag, stderr)
	if !resolved {
		return 2
	}

	overrides, err := github.LoadOverrides(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	findings, _ := github.Audit(repo, overrides)

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(findings); err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}
	} else {
		printGithubFindings(stdout, repo, findings)
	}

	if github.AllOK(findings) {
		return 0
	}

	return 1
}

func runGithubFix(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdGithub+" "+cmdFix, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	repoFlag := flagSet.String("repo", "", "repository slug (owner/name); default: inferred from origin")
	asJSON := flagSet.Bool(flagJSON, false, "emit the post-fix findings as JSON")
	yes := flagSet.Bool("yes", false, "apply the plan without prompting")

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	repo, resolved := githubTarget(*repoFlag, stderr)
	if !resolved {
		return 2
	}

	overrides, err := github.LoadOverrides(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	findings, changes := github.Audit(repo, overrides)
	if len(changes) == 0 {
		_, _ = fmt.Fprintln(stdout, "nothing to fix")

		return reportGithubOutcome(stdout, stderr, repo, findings, *asJSON)
	}

	_, _ = fmt.Fprintf(stdout, "limen github fix %s — plan:\n", repo)

	for _, planned := range changes {
		_, _ = fmt.Fprintf(stdout, "  ✎  %-32s %s\n", planned.Check, planned.Summary)
	}

	if !*yes && !confirm(stdout) {
		_, _ = fmt.Fprintln(stderr, "not applied (confirm with y, or pass -yes)")

		return 1
	}

	failed := 0

	for _, planned := range changes {
		if applyErr := planned.Apply(repo); applyErr != nil {
			failed++

			_, _ = fmt.Fprintf(stderr, "limen: %s: %v\n", planned.Check, applyErr)
		}
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(stderr, "limen: %d change(s) failed to apply\n", failed)
	}

	// Re-audit: the post-state, not the intent, is what gets reported.
	final, _ := github.Audit(repo, overrides)

	return reportGithubOutcome(stdout, stderr, repo, final, *asJSON)
}

// confirm asks for interactive consent on stdin (the plan was just printed).
func confirm(stdout io.Writer) bool {
	_, _ = fmt.Fprint(stdout, "apply? [y/N] ")

	reader := bufio.NewReader(os.Stdin)

	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y" || answer == "yes"
}

// reportGithubOutcome prints the findings and derives the exit code. asJSON is
// the user's -json flag: an output mode is domain data, and every call site
// passes the self-describing *asJSON — not opaque-literal control coupling.
func reportGithubOutcome( //nolint:revive // flag-parameter: see doc comment above.
	stdout, stderr io.Writer,
	repo string,
	findings []github.Finding,
	asJSON bool,
) int {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(findings); err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}
	} else {
		printGithubFindings(stdout, repo, findings)
	}

	if github.AllOK(findings) {
		return 0
	}

	return 1
}

func printGithubFindings(writer io.Writer, repo string, findings []github.Finding) {
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
}

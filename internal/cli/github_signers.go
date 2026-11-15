package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/farcloser/limen/internal/github"
)

const (
	cmdRollout = "rollout"

	githubRolloutUsage = "Usage: limen github signers rollout (-org name | -repo owner/name) " +
		"-valid-before time [-yes] <fingerprint>"
)

func runGithubSigners(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != cmdRollout {
		githubSignersUsage(stderr)

		return 2
	}

	return runGithubSignersRollout(ctx, args[1:], stdout, stderr)
}

func githubSignersUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, githubRolloutUsage+`

Retires a signing key where it cannot wait: in every repository of the
organization that lists the key in .lint-signers (-org), or in the one named
(-repo), it ends the key's window at the time given, as `+"`limen signers retire`"+`
does, in one commit and one pull request. Nothing is merged: each pull
request is reviewed as any other.

The commit is made by your git as it is configured, so it is signed with your
key and signed off as you, and it is pushed only when the repository's own
.lint-signers accepts that signature: the key being retired cannot sign its
own retirement once its window has ended.

A repository that does not list the key, has it retired already, or has the
pull request open is passed over, so a run can be run again, and a run that
was stopped after its push gets its pull request from the next. A branch
found under the rollout's name is held to what a run pushes, one commit with
that edit and a signature the file accepts, before it gets a pull request or
has its open one counted: anything else is an error. A key no repository
lists is an error: the fingerprint is the one `+"`ssh-keygen -l`"+` prints,
SHA256: and all.
`)
}

func runGithubSignersRollout(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdGithub+" "+cmdSigners+" "+cmdRollout, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	repoFlag := flagSet.String("repo", "", "the one repository (owner/name)")
	orgFlag := flagSet.String("org", "", "every non-archived repository of the organization")
	validBefore := flagSet.String("valid-before", "", "the key signs nothing from this time on (UTC, ending in Z)")
	yes := flagSet.Bool("yes", false, "open the pull requests without prompting")

	flagSet.Usage = func() {
		githubSignersUsage(stderr)

		flagSet.PrintDefaults()
	}

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	// One of the two, named: a command that pushes infers no target.
	if flagSet.NArg() != 1 || *validBefore == "" || (*repoFlag == "") == (*orgFlag == "") {
		_, _ = fmt.Fprintln(stderr, githubRolloutUsage)

		return 2
	}

	fingerprint := flagSet.Arg(0)

	retirement, err := github.NewSignerRetirement(fingerprint, *validBefore)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	if err := github.Preflight(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	repos := []string{*repoFlag}

	if *orgFlag != "" {
		listed, err := github.OrgRepos(ctx, *orgFlag)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}

		repos = listed
	}

	steps := retirement.Plan(ctx, repos)

	_, _ = fmt.Fprintf(stdout, "limen github signers rollout: retire %s at %s\n", fingerprint, *validBefore)

	planned, failed, listed := printRolloutPlan(stdout, steps)

	switch {
	case !listed && failed == 0:
		_, _ = fmt.Fprintf(
			stderr,
			"limen: no repository lists %s: is it the fingerprint `ssh-keygen -l` prints?\n",
			fingerprint,
		)

		return 1
	case len(planned) == 0:
		_, _ = fmt.Fprintln(stdout, "nothing to roll out")

		return rolloutCode(failed)
	case !*yes && !confirm(stdout):
		_, _ = fmt.Fprintln(stderr, "not applied (confirm with y, or pass -yes)")

		return 1
	}

	return rolloutCode(failed + applyRollout(ctx, stdout, stderr, planned))
}

// rolloutCode is the exit code of a run in which that many repositories
// could not be read, edited or opened.
func rolloutCode(failed int) int {
	if failed > 0 {
		return 1
	}

	return 0
}

// printRolloutPlan lists every repository with what the run does there, and
// returns the steps that open a pull request, how many repositories could
// not be judged, and whether any lists the key at all.
func printRolloutPlan(
	stdout io.Writer,
	steps []github.SignerStep,
) (planned []github.SignerStep, failed int, listed bool) {
	for _, step := range steps {
		listed = listed || step.Listed

		switch {
		case step.Err != nil:
			failed++

			_, _ = fmt.Fprintf(stdout, "  ✗  %-32s %v\n", step.Repo, step.Err)
		case step.Planned():
			planned = append(planned, step)

			_, _ = fmt.Fprintf(stdout, "  ✎  %-32s %s\n", step.Repo, step.Summary)
		default:
			_, _ = fmt.Fprintf(stdout, "  ·  %-32s %s\n", step.Repo, step.Summary)
		}
	}

	return planned, failed, listed
}

// applyRollout opens the planned pull requests, one repository at a time,
// each outcome said as it happens, and returns how many failed. A commit
// signed by the retired key ends the run: every other would be too.
func applyRollout(ctx context.Context, stdout, stderr io.Writer, planned []github.SignerStep) int {
	failed := 0

	for index, step := range planned {
		opened, err := step.Apply(ctx)
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "  ✓  %-32s %s\n", step.Repo, opened)

			continue
		}

		failed++

		_, _ = fmt.Fprintf(stderr, "limen: %s: %v\n", step.Repo, err)

		if errors.Is(err, github.ErrRetiredKeySigns) {
			left := len(planned) - index - 1
			failed += left

			_, _ = fmt.Fprintf(
				stderr,
				"limen: stopped, %d repositories not attempted: this git signs with the key being retired\n",
				left,
			)

			break
		}
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(stderr, "limen: %d of %d pull requests were not opened\n", failed, len(planned))
	}

	return failed
}

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/farcloser/limen/internal/github"
)

const (
	cmdWatch = "watch"

	githubWatchUsage = "Usage: limen github watch -org name [-org name ...] [-as login] [-yes]"
)

// names is a flag given several times: each use adds a name.
type names []string

func (n *names) String() string { return strings.Join(*n, ", ") }

func (n *names) Set(value string) error {
	*n = append(*n, value)

	return nil
}

func githubWatchHelp(writer io.Writer) {
	_, _ = fmt.Fprint(writer, githubWatchUsage+`

Watches every repository of the organizations named, for the account the
token belongs to: all activity, as the Watch button's "All Activity" does.
GitHub has no setting that watches an organization's repositories to come,
so a run watches the ones that exist and the next run picks up the new ones.

A repository already watched is left alone, and so is one the account
ignores, which is somebody's choice. Archived repositories are passed over.
The plan is printed first and applied on y, or at once with -yes.

The account is whoever the token is: GH_TOKEN, or the gh that is signed in.
-as login refuses to run as anybody else, for a run nobody is watching.
`)
}

func runGithubWatch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdGithub+" "+cmdWatch, flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	var orgs names

	flagSet.Var(&orgs, "org", "an organization whose repositories to watch; may be given several times")
	asLogin := flagSet.String("as", "", "refuse to run unless the token's account is this login")
	yes := flagSet.Bool("yes", false, "watch without prompting")

	flagSet.Usage = func() {
		githubWatchHelp(stderr)

		flagSet.PrintDefaults()
	}

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	if flagSet.NArg() != 0 || len(orgs) == 0 {
		_, _ = fmt.Fprintln(stderr, githubWatchUsage)

		return 2
	}

	if err := github.Preflight(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	login, err := github.AuthenticatedUser(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	if *asLogin != "" && !strings.EqualFold(*asLogin, login) {
		_, _ = fmt.Fprintf(
			stderr,
			"limen: the token is %s's, and this run is to watch as %s: nothing done\n",
			login,
			*asLogin,
		)

		return 2
	}

	var repos []string

	for _, org := range orgs {
		listed, err := github.OrgRepos(ctx, org)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, errFormat, err)

			return 2
		}

		repos = append(repos, listed...)
	}

	_, _ = fmt.Fprintf(stdout, "limen github watch, as %s: %s\n", login, orgs.String())

	planned, watched, failed := printWatchPlan(stdout, github.PlanWatch(ctx, repos))

	_, _ = fmt.Fprintf(stdout, "%d of %d repositories already watched\n", watched, len(repos))

	switch {
	case len(planned) == 0:
		return rolloutCode(failed)
	case !*yes && !confirm(stdout):
		_, _ = fmt.Fprintln(stderr, "not applied (confirm with y, or pass -yes)")

		return 1
	}

	return rolloutCode(failed + applyWatch(ctx, stdout, stderr, planned))
}

// printWatchPlan lists the repositories a run does something in, or cannot
// read, or leaves ignored; the ones already watched are only counted, since
// a run that is repeated would list them all every time. It returns the
// steps to apply, how many repositories are watched already, and how many
// could not be read.
func printWatchPlan(stdout io.Writer, steps []github.WatchStep) (planned []github.WatchStep, watched, failed int) {
	for _, step := range steps {
		switch {
		case step.Err != nil:
			failed++

			_, _ = fmt.Fprintf(stdout, "  ✗  %-32s %v\n", step.Repo, step.Err)
		case step.Planned():
			planned = append(planned, step)

			_, _ = fmt.Fprintf(stdout, "  ✎  %-32s %s\n", step.Repo, step.Summary)
		case step.Watched:
			watched++
		default:
			_, _ = fmt.Fprintf(stdout, "  ·  %-32s %s\n", step.Repo, step.Summary)
		}
	}

	return planned, watched, failed
}

// applyWatch subscribes the account to each planned repository and returns
// how many failed. A token that may not touch subscriptions ends the run:
// every other repository would answer the same.
func applyWatch(ctx context.Context, stdout, stderr io.Writer, planned []github.WatchStep) int {
	failed := 0

	for index, step := range planned {
		err := step.Apply(ctx)
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "  ✓  %s\n", step.Repo)

			continue
		}

		failed++

		_, _ = fmt.Fprintf(stderr, "limen: %s: %v\n", step.Repo, err)

		if errors.Is(err, github.ErrNoNotificationsScope) {
			failed += len(planned) - index - 1

			break
		}
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(stderr, "limen: %d of %d repositories were not watched\n", failed, len(planned))
	}

	return failed
}

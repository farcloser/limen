package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Watching is a per-account setting GitHub keeps per repository, with no
// way to say "every repository of this organization, the ones to come
// included": a run watches the repositories that exist, and a later run
// picks up the new ones.

// ErrNoNotificationsScope is a write GitHub answered with a 404: what it
// says to a token that may not touch subscriptions, whatever the repository.
// Every other write of the run would get the same answer.
var ErrNoNotificationsScope = errors.New(
	"GitHub answers 404: the token lacks the notifications scope (gh auth refresh -h github.com -s notifications)",
)

// subscription is GET /repos/{owner}/{repo}/subscription.
type subscription struct {
	Subscribed bool `json:"subscribed"`
	Ignored    bool `json:"ignored"`
}

// WatchStep is what a run does in one repository, or why it does nothing.
type WatchStep struct {
	// Err is a repository whose subscription could not be read.
	Err error
	// apply subscribes the account; nil when the repository needs nothing.
	apply func(ctx context.Context) error
	Repo  string
	// Summary says the reason there is no step, or what the step is.
	Summary string
	// Watched reports a repository the account already watches.
	Watched bool
}

// Planned reports whether the step subscribes the account.
func (s WatchStep) Planned() bool { return s.apply != nil }

// Apply performs the step.
func (s WatchStep) Apply(ctx context.Context) error {
	if s.apply == nil {
		return nil
	}

	return s.apply(ctx)
}

// AuthenticatedUser is the login of the account the token belongs to: the
// one a watch subscribes.
func AuthenticatedUser(ctx context.Context) (string, error) {
	outcome := request(ctx, methodGet, "user", nil)

	switch {
	case outcome.err != nil:
		return "", outcome.err
	case outcome.notFound:
		return "", fmt.Errorf("GET user: %w", errEndpointNotFound)
	}

	var user struct {
		Login string `json:"login"`
	}

	if err := json.Unmarshal(outcome.body, &user); err != nil {
		return "", fmt.Errorf("GET user: decoding response: %w", err)
	}

	return user.Login, nil
}

// PlanWatch reads the account's subscription to each repository and returns
// one step per repository, in the order given. Nothing is written. A
// repository the account ignores is left ignored: that is a choice someone
// made, where no subscription at all is only GitHub's default.
func PlanWatch(ctx context.Context, repos []string) []WatchStep {
	steps := make([]WatchStep, 0, len(repos))

	for _, repo := range repos {
		steps = append(steps, subscriptionStep(ctx, repo))
	}

	return steps
}

// subscriptionStep judges one repository.
func subscriptionStep(ctx context.Context, repo string) WatchStep {
	step := WatchStep{Repo: repo}
	api := repoClient(repo)

	var current subscription

	// A 404 is the answer for a repository the account does not subscribe to.
	outcome := api.getJSON(ctx, "/subscription", &current)

	switch {
	case outcome.err != nil:
		step.Err = outcome.err
	case !outcome.notFound && current.Ignored:
		step.Summary = "ignored: left as it is"
	case !outcome.notFound && current.Subscribed:
		step.Watched = true
		step.Summary = "watched"
	default:
		step.Summary = "watch"
		step.apply = func(ctx context.Context) error {
			payload := map[string]bool{"subscribed": true, "ignored": false}

			err := api.writeJSON(ctx, http.MethodPut, "/subscription", payload)
			if errors.Is(err, errEndpointNotFound) {
				return ErrNoNotificationsScope
			}

			return err
		}
	}

	return step
}

package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/farcloser/limen/internal/signers"
)

// The rollout retires one signing key in every repository that lists it,
// for the key that cannot wait a release: in each, one commit that ends the
// key's window in the allowed-signers file, signed by whoever runs it, and
// one pull request. It deletes nothing and merges nothing: review is the
// normal flow.
//
// A run is told apart from the next by what it leaves on GitHub. The branch
// is named after the key and the time, so a second run finds the first one's
// pull request, or its branch when the pull request did not get opened, and
// goes on from there. The name is one anybody with push access can write, so
// it says nothing by itself: a branch found under it is held to what a run
// pushes before it is taken for one (heldBranch).

const (
	// retireBranchPrefix opens the branch name; the key and the time follow.
	retireBranchPrefix = "limen/retire-signer-"
	// retireSlugLength is how much of the fingerprint the branch name carries.
	retireSlugLength = 16
	// retireLabel marks a pull request that changes nothing for a consumer.
	retireLabel = "internal"
)

// ErrRetiredKeySigns is a commit of the rollout signed by the very key it
// retires, at a time its new window excludes: every repository's lint would
// refuse it, in every repository of the run.
var ErrRetiredKeySigns = errors.New("signed by the key being retired")

// errArchived is a repository GitHub takes no push to and no pull request in.
var errArchived = errors.New("archived: nothing can be pushed to it")

// errNotASigner is a commit of the rollout the repository's own file does
// not accept: the lint of that repository would refuse it.
var errNotASigner = errors.New("not signed by a key the file lists for the committer")

// errNotARollout is a branch with the retirement's name that is not what a
// run pushes: no pull request is opened for it, and none it has is counted.
var errNotARollout = errors.New("is not what a rollout pushes")

// errOtherEdit is a commit whose file is not the retirement's.
var errOtherEdit = errors.New("its file is not the one retiring the key gives")

// errBranchMoved is a pull request opened on another commit than the one
// that was held to the rollout's own.
var errBranchMoved = errors.New("the branch moved while its pull request was being opened")

// SignerStep is what the rollout does in one repository, or why it does
// nothing there.
type SignerStep struct {
	// Err is a repository the rollout could not read, or whose file refuses
	// the edit: neither planned nor passed over.
	Err error
	// apply opens the pull request; nil when the repository needs none.
	apply func(ctx context.Context) (string, error)
	Repo  string
	// Summary says the step, or the reason there is none, in a line.
	Summary string
	// Listed reports that the repository's file has the key, retired or not.
	Listed bool
}

// Planned reports whether the step opens a pull request.
func (s SignerStep) Planned() bool { return s.apply != nil }

// Apply performs the step and returns the pull request's URL.
func (s SignerStep) Apply(ctx context.Context) (string, error) {
	if s.apply == nil {
		return "", nil
	}

	return s.apply(ctx)
}

// A SignerRetirement is one key's window ended at a time, to roll out.
type SignerRetirement struct {
	fingerprint string
	validBefore string
}

// NewSignerRetirement is the retirement of the key with that SHA256
// fingerprint at validBefore, refused when the time is not one the
// allowed-signers file takes. Nothing is read.
func NewSignerRetirement(fingerprint, validBefore string) (SignerRetirement, error) {
	if err := signers.CheckTime(validBefore); err != nil {
		return SignerRetirement{}, err //nolint:wrapcheck // the argument's own error, as the signers commands say it.
	}

	return SignerRetirement{fingerprint: fingerprint, validBefore: validBefore}, nil
}

// Plan reads, in each repository, the allowed-signers file of the default
// branch and what an earlier run left, and returns one step per repository,
// in the order given. Nothing is written.
func (r SignerRetirement) Plan(ctx context.Context, repos []string) []SignerStep {
	steps := make([]SignerStep, 0, len(repos))

	for _, repo := range repos {
		steps = append(steps, planRetirement(ctx, repo, r))
	}

	return steps
}

// title is the title of every pull request the rollout opens for the key,
// and the subject of its commit: fixed, so the same retirement reads the
// same in every repository's history.
func (r SignerRetirement) title() string { return "Retire the signing key " + r.fingerprint }

// branch names the retirement: the same in every repository and in every
// run, so a run finds what an earlier one pushed.
func (r SignerRetirement) branch() string {
	slug := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			return char
		default:
			return -1
		}
	}, strings.TrimPrefix(r.fingerprint, "SHA256:"))

	if len(slug) > retireSlugLength {
		slug = slug[:retireSlugLength]
	}

	return retireBranchPrefix + slug + "-" + r.validBefore
}

// message is the commit's: the fixed subject, then what the edit means.
func (r SignerRetirement) message() string {
	return r.title() + "\n\n" +
		signers.File + " ends the key's window at " + r.validBefore + ": what it signed\n" +
		"before that time keeps verifying, and a commit it signs from then on is\n" +
		"refused. No line is deleted.\n"
}

// body is the pull request's.
func (r SignerRetirement) body() string {
	return "`" + signers.File + "` ends the window of the signing key `" + r.fingerprint + "` at `" +
		r.validBefore + "` (`valid-before`). What the key signed before that time keeps verifying; " +
		"a commit it signs from then on is refused by the commit lint. No line is deleted.\n\n" +
		"Opened by `limen github signers rollout`, which makes this edit in every repository that lists the key.\n"
}

// repoFacts is what the rollout reads of GET /repos/{owner}/{repo}.
type repoFacts struct {
	DefaultBranch string `json:"default_branch"`
	SSHURL        string `json:"ssh_url"`
	Archived      bool   `json:"archived"`
}

// fileContent is GET /repos/{owner}/{repo}/contents/{path} for a file.
type fileContent struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// pullRequest is what the rollout reads of a pull request.
type pullRequest struct {
	HTMLURL string   `json:"html_url"`
	Head    pullHead `json:"head"`
	Number  int      `json:"number"`
}

// pullHead is where a pull request comes from: the commit, and the
// repository that holds its branch.
type pullHead struct {
	SHA  string   `json:"sha"`
	Repo headRepo `json:"repo"`
}

// headRepo names the repository of a pull request's head.
type headRepo struct {
	FullName string `json:"full_name"`
}

// comparison is what the rollout reads of
// GET /repos/{owner}/{repo}/compare/{base}...{head}.
type comparison struct {
	AheadBy int `json:"ahead_by"`
}

// planRetirement judges one repository.
func planRetirement(ctx context.Context, repo string, retire SignerRetirement) SignerStep {
	step := SignerStep{Repo: repo}
	api := repoClient(repo)

	var facts repoFacts

	switch outcome := api.getJSON(ctx, "", &facts); {
	case outcome.err != nil:
		step.Err = outcome.err

		return step
	case outcome.notFound:
		step.Err = fmt.Errorf("%w: no such repository, or the token does not see it", errEndpointNotFound)

		return step
	case facts.Archived:
		step.Err = errArchived

		return step
	}

	list, found, err := signersOf(ctx, api, facts.DefaultBranch)
	if err != nil {
		step.Err = err

		return step
	}

	if !found {
		step.Summary = "no " + signers.File

		return step
	}

	changed, err := list.Retire(retire.fingerprint, retire.validBefore)

	switch {
	case errors.Is(err, signers.ErrNotListed):
		step.Summary = "does not list the key"

		return step
	case err != nil:
		step.Listed = true
		step.Err = err

		return step
	case !changed:
		step.Listed = true
		step.Summary = "already retired at " + retire.validBefore

		return step
	}

	step.Listed = true

	return planPullRequest(ctx, step, api, facts, retire)
}

// planPullRequest finds what an earlier run left for a repository that
// needs the edit, and plans what is still missing.
func planPullRequest(
	ctx context.Context, step SignerStep, api client, facts repoFacts, retire SignerRetirement,
) SignerStep {
	branch := retire.branch()

	pushed := api.api(ctx, methodGet, "/git/ref/heads/"+branch, nil)
	if pushed.err != nil {
		step.Err = pushed.err

		return step
	}

	if pushed.notFound {
		step.Summary = "open a pull request"
		step.apply = func(ctx context.Context) (string, error) {
			tip, err := pushRetirement(ctx, facts, retire)
			if err != nil {
				return "", err
			}

			return openRetirement(ctx, api, facts.DefaultBranch, retire, tip)
		}

		return step
	}

	opened, err := openPullRequest(ctx, api, step.Repo, branch)
	if err != nil {
		step.Err = err

		return step
	}

	tip, err := heldBranch(ctx, api, facts, retire)

	switch {
	case err != nil && opened != "":
		step.Err = fmt.Errorf("%w, and has a pull request open: %s", err, opened)
	case err != nil:
		step.Err = err
	case opened != "":
		step.Summary = "pull request open: " + opened
	default:
		// A run that pushed and was stopped before it opened one.
		step.Summary = "open the pull request of the branch an earlier run pushed (" + branch + ")"
		step.apply = func(ctx context.Context) (string, error) {
			return openRetirement(ctx, api, facts.DefaultBranch, retire, tip)
		}
	}

	return step
}

// openPullRequest is the URL of the open pull request of the repository's
// own branch of that name, or "" when it has none.
func openPullRequest(ctx context.Context, api client, repo, branch string) (string, error) {
	owner, _, _ := strings.Cut(repo, "/")

	var open []pullRequest

	query := "/pulls?state=open&per_page=100&head=" + url.QueryEscape(owner+":"+branch)

	switch outcome := api.getJSONAllPages(ctx, query, &open); {
	case outcome.err != nil:
		return "", outcome.err
	case outcome.notFound:
		return "", errEndpointNotFound
	}

	// The filter is by owner: a fork the same owner holds answers to it too.
	// GitHub's names do not mind case, and answers in its own.
	for _, pull := range open {
		if strings.EqualFold(pull.Head.Repo.FullName, repo) {
			return pull.HTMLURL, nil
		}
	}

	return "", nil
}

// heldBranch holds the branch of the retirement's name, as it is on the
// remote, to what a run pushes, and returns its tip: one commit over the
// default branch, which changes the allowed-signers file alone, into what
// retiring the key makes of the file before it, and whose signature that
// file accepts. Anything else is errNotARollout, with what differs. The
// clone comes first and every question after it is of the commit cloned.
func heldBranch(ctx context.Context, api client, facts repoFacts, retire SignerRetirement) (string, error) {
	branch := retire.branch()
	refuse := func(why string, args ...any) (string, error) {
		return "", fmt.Errorf("the branch %s %w: %s", branch, errNotARollout, fmt.Sprintf(why, args...))
	}

	scratch, err := os.MkdirTemp("", "limen-rollout-*")
	if err != nil {
		return "", fmt.Errorf("a directory to clone into: %w", err)
	}

	defer func() { _ = os.RemoveAll(scratch) }()

	clone := filepath.Join(scratch, "clone")

	// Two commits deep: the tip and the commit it was made on.
	if _, err = git(ctx, scratch, "clone", "--quiet", "--depth", "2", "--no-tags", "--single-branch",
		"--branch", branch, "--", facts.SSHURL, clone); err != nil {
		return "", err
	}

	head, err := git(ctx, clone, "log", "-1", "--format=%H%n%P")
	if err != nil {
		return "", err
	}

	tip, parents, _ := strings.Cut(head, "\n")

	// The commit cloned, by its name and not the branch's: everything below
	// is said of that commit, and the branch may be pushed to at any moment.
	var ahead comparison

	switch outcome := api.getJSON(ctx, "/compare/"+facts.DefaultBranch+"..."+tip, &ahead); {
	case outcome.err != nil:
		return "", outcome.err
	case outcome.notFound:
		return "", errEndpointNotFound
	}

	switch {
	case ahead.AheadBy != 1:
		return refuse("it is %d commits over %s, not one", ahead.AheadBy, facts.DefaultBranch)
	case len(strings.Fields(parents)) != 1:
		return refuse("its commit is a merge")
	}

	changed, err := git(ctx, clone, "diff", "--name-only", "HEAD~1", "HEAD")
	if err != nil {
		return "", err
	}

	if changed != signers.File {
		return refuse("its commit changes %s, not %s alone", strings.Join(strings.Fields(changed), ", "), signers.File)
	}

	if err = sameEdit(ctx, clone, retire); err != nil {
		return refuse("%v", err)
	}

	if _, err = git(ctx, clone, "config", "gpg.ssh.allowedSignersFile", signers.File); err != nil {
		return "", err
	}

	// %v: a branch found signed by the retired key says nothing of the key
	// this run signs with, which is what ErrRetiredKeySigns stops a run on.
	if err = acceptedBy(ctx, clone, retire); err != nil {
		return refuse("%v", err)
	}

	return tip, nil
}

// sameEdit reports whether the clone's allowed-signers file is what
// retiring the key makes of the file one commit before.
func sameEdit(ctx context.Context, clone string, retire SignerRetirement) error {
	before, err := gitExact(ctx, clone, "show", "HEAD~1:"+signers.File)
	if err != nil {
		return err
	}

	list, err := signers.Parse(before)
	if err != nil {
		return fmt.Errorf("%s, before the commit: %w", signers.File, err)
	}

	if _, err = list.Retire(retire.fingerprint, retire.validBefore); err != nil {
		return fmt.Errorf("%s, before the commit: %w", signers.File, err)
	}

	// The blob, not the file on disk: what the commit holds, whatever a
	// checkout makes of it.
	after, err := gitExact(ctx, clone, "show", "HEAD:"+signers.File)
	if err != nil {
		return err
	}

	if after != list.String() {
		return errOtherEdit
	}

	return nil
}

// signersOf reads the allowed-signers file of a branch; found is false when
// the branch has none.
func signersOf(ctx context.Context, api client, branch string) (list signers.List, found bool, err error) {
	var file fileContent

	path := "/contents/" + signers.File + "?ref=" + url.QueryEscape(branch)

	switch outcome := api.getJSON(ctx, path, &file); {
	case outcome.err != nil:
		return signers.List{}, false, outcome.err
	case outcome.notFound:
		return signers.List{}, false, nil
	}

	if file.Encoding != "base64" {
		return signers.List{}, false, fmt.Errorf(
			"%w: %s comes back as %q, not base64",
			errAPI,
			signers.File,
			file.Encoding,
		)
	}

	text, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return signers.List{}, false, fmt.Errorf("%s: decoding what the API returned: %w", signers.File, err)
	}

	list, err = signers.Parse(string(text))
	if err != nil {
		return signers.List{}, false, fmt.Errorf("%s: %w", signers.File, err)
	}

	return list, true, nil
}

// pushRetirement makes the commit in a clone of its own and pushes its
// branch: the default branch alone, at its tip, the edit, one commit signed
// and signed off by whoever runs this, with their git as it is configured.
// Nothing is pushed unless the repository's own file accepts the signature,
// which is what its lint will ask. It returns the commit pushed.
func pushRetirement(ctx context.Context, facts repoFacts, retire SignerRetirement) (string, error) {
	scratch, err := os.MkdirTemp("", "limen-rollout-*")
	if err != nil {
		return "", fmt.Errorf("a directory to clone into: %w", err)
	}

	defer func() { _ = os.RemoveAll(scratch) }()

	clone := filepath.Join(scratch, "clone")
	branch := retire.branch()

	_, err = git(ctx, scratch, "clone", "--quiet", "--depth", "1", "--no-tags", "--single-branch",
		"--branch", facts.DefaultBranch, "--", facts.SSHURL, clone)
	if err == nil {
		err = commitRetirement(ctx, clone, retire)
	}

	if err == nil {
		err = acceptedBy(ctx, clone, retire)
	}

	if err == nil {
		_, err = git(ctx, clone, "push", "--quiet", "origin", branch+":refs/heads/"+branch)
	}

	if err != nil {
		return "", err
	}

	return git(ctx, clone, "rev-parse", "HEAD")
}

// commitRetirement edits the clone's allowed-signers file and commits the
// edit on the retirement's branch.
func commitRetirement(ctx context.Context, clone string, retire SignerRetirement) error {
	file := filepath.Join(clone, signers.File)

	list, err := signers.Load(file)
	if err == nil {
		_, err = list.Retire(retire.fingerprint, retire.validBefore)
	}

	if err == nil {
		err = signers.Save(file, list)
	}

	if err != nil {
		return err //nolint:wrapcheck // the signers package's own sentence, the file named.
	}

	for _, args := range [][]string{
		// The clone's own, and the clone is thrown away. An SSH signature,
		// whatever the runner's git signs with elsewhere: it is the one kind
		// the file can accept, and git would say G of an OpenPGP one from
		// the runner's own keyring, the file never asked.
		{"config", "gpg.format", "ssh"},
		{"config", "gpg.ssh.allowedSignersFile", signers.File},
		{"checkout", "--quiet", "-b", retire.branch()},
		{"add", "--", signers.File},
		{"commit", "--quiet", "--gpg-sign", "--signoff", "--message", retire.message()},
	} {
		if _, err = git(ctx, clone, args...); err != nil {
			return err
		}
	}

	return nil
}

// acceptedBy asks the clone's own allowed-signers file, as its tip has it,
// whether it accepts the signature of that commit.
func acceptedBy(ctx context.Context, clone string, retire SignerRetirement) error {
	said, err := git(ctx, clone, "log", "-1", "--format=%G?%n%GF%n%ce")
	if err != nil {
		return err
	}

	const want = 3

	lines := strings.Split(said, "\n")
	if len(lines) != want {
		return fmt.Errorf("%w: git said %q of the commit's signature", errNotASigner, said)
	}

	status, signer, committer := lines[0], lines[1], lines[2]

	switch {
	// An SSH key's fingerprint: the file lists no other kind of key, so a G
	// with another kind of signer is git's own keyring speaking.
	case status == "G" && strings.HasPrefix(signer, "SHA256:"):
		return nil
	case signer == retire.fingerprint:
		return fmt.Errorf("%w (%s): sign with another key, git's user.signingkey", ErrRetiredKeySigns, signer)
	default:
		return fmt.Errorf("%w: %s lists no key that signs for %s now (git says %q of the signature)",
			errNotASigner, signers.File, committer, status)
	}
}

// openRetirement opens the pull request of the branch, whose tip was held
// to the rollout's own, and labels it. A pull request GitHub opened on
// another commit is an error: the branch was pushed to in between.
func openRetirement(ctx context.Context, api client, base string, retire SignerRetirement, tip string) (string, error) {
	payload := map[string]any{
		"title": retire.title(),
		"head":  retire.branch(),
		"base":  base,
		"body":  retire.body(),
	}

	var opened pullRequest

	if err := api.createJSON(ctx, "/pulls", payload, &opened); err != nil {
		return "", err
	}

	if opened.Head.SHA != tip {
		return "", fmt.Errorf("%w: %s is of %s, and %s was read; close it",
			errBranchMoved, opened.HTMLURL, opened.Head.SHA, tip)
	}

	labels := map[string]any{"labels": []string{retireLabel}}
	if err := api.writeJSON(ctx, http.MethodPost, fmt.Sprintf("/issues/%d/labels", opened.Number), labels); err != nil {
		return "", fmt.Errorf("opened %s, without its %q label, which no later run sets: %w",
			opened.HTMLURL, retireLabel, err)
	}

	return opened.HTMLURL, nil
}

// git runs git in dir and returns what it printed, less the newlines that
// end it, or its complaint.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	printed, err := gitExact(ctx, dir, args...)

	return strings.TrimRight(printed, "\n"), err
}

// gitExact runs git in dir and returns what it printed, to the byte.
func gitExact(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- git, with arguments this file builds.
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		command, _, _ := strings.Cut(strings.Join(args, " "), " ")

		return "", fmt.Errorf("git %s: %w: %s", command, err, condenseStderr(stderr.String()))
	}

	return stdout.String(), nil
}

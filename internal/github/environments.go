package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/farcloser/limen/internal/workflow"
)

// The secrets a workflow reads stay behind an environment (see
// book/mandatory-files.md, the secrets rule, for why a repository or
// organization secret is every write account's). The tree rule holds the
// workflows to it; these checks hold GitHub's side: no secret at the
// repository, and every environment a job deploys to gated, the signing
// policy's `release` admitting release tags alone and waiting for an owner
// (book/releasing.md).
const (
	releaseEnvironment    = workflow.ReleaseEnvironment
	releaseTagPattern     = "v*"
	policyTypeTag         = "tag"
	ruleRequiredReviewers = "required_reviewers"
	reviewerTypeUser      = "User"
	workflowsListing      = "/contents/.github/workflows"
	maxEnvironmentReviews = 6
)

// environment is one entry of GET /repos/{owner}/{repo}/environments.
type environment struct {
	DeploymentBranchPolicy *deploymentBranchPolicy `json:"deployment_branch_policy"`
	Name                   string                  `json:"name"`
	ProtectionRules        []protectionRule        `json:"protection_rules"`
}

type protectionRule struct {
	Type              string                `json:"type"`
	Reviewers         []environmentReviewer `json:"reviewers"`
	WaitTimer         int                   `json:"wait_timer"`
	PreventSelfReview bool                  `json:"prevent_self_review"`
}

type environmentReviewer struct {
	Type     string           `json:"type"`
	Reviewer reviewerIdentity `json:"reviewer"`
}

type reviewerIdentity struct {
	ID int64 `json:"id"`
}

// deploymentBranchPolicy is null when every branch may deploy.
type deploymentBranchPolicy struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

// branchPolicy is one entry of the environment's deployment-branch-policies.
type branchPolicy struct {
	Name string `json:"name"`
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

// reviewers returns the required reviewers the environment's rules name,
// and the rule's other settings, which a reconciling PUT must resend.
func (e environment) reviewers() (reviewers []map[string]any, wait int, preventSelf bool) {
	for _, rule := range e.ProtectionRules {
		if rule.Type != ruleRequiredReviewers {
			continue
		}

		preventSelf = rule.PreventSelfReview

		for _, reviewer := range rule.Reviewers {
			reviewers = append(reviewers, map[string]any{jsonTypeKey: reviewer.Type, "id": reviewer.Reviewer.ID})
		}
	}

	for _, rule := range e.ProtectionRules {
		if rule.Type == "wait_timer" {
			wait = rule.WaitTimer
		}
	}

	return reviewers, wait, preventSelf
}

// environmentNeeds is what the default branch's workflows ask of one
// environment: the secrets the jobs deploying to it read.
type environmentNeeds struct {
	secrets []string
	all     bool
}

// slug is the repository as owner/name.
func (c client) slug() string { return strings.TrimPrefix(c.base, "repos/") }

// auditActionsSecrets: a repository secret is one every write account reads
// from a branch of its own, so any present is an advisory naming the move.
func (a *auditor) auditActionsSecrets() {
	secrets, outcome := listPages[orgNamed](a.ctx, a.client, "/actions/secrets?per_page=100", "secrets")
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkActionsSecrets)

		return
	}

	if len(secrets) == 0 {
		a.flag(checkActionsSecrets, StatusOK, "", "", "no repository secret", nil)

		return
	}

	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		names = append(names, secret.Name)
	}

	a.flag(checkActionsSecrets, StatusAdvisory, strings.Join(names, listSeparator), "(none: an environment's)",
		"a repository secret is read by any workflow run in the repository's own context, so every account "+
			"with write access prints it from a branch of its own: each belongs to an environment gated to the refs "+
			"that may use it (`gh secret set "+names[0]+" --env <environment> --repo "+a.client.slug()+
			"`, then `gh secret delete "+names[0]+" --repo "+a.client.slug()+"`); a signing credential's environment is `"+
			releaseEnvironment+"`", nil)
}

// auditEnvironments: every environment the default branch's workflows deploy
// to exists and admits only the refs that may use its secrets, holding the
// secrets its jobs read; the release environment admits v* tags alone and
// waits for an owner. The first departure is the finding, as with rulesets.
func (a *auditor) auditEnvironments() {
	environments, outcome := listPages[environment](a.ctx, a.client, "/environments?per_page=100", "environments")
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkEnvironments)

		return
	}

	needs, err := a.workflowEnvironments()
	if err != nil {
		a.unverifiable(err, checkEnvironments)

		return
	}

	byName := map[string]environment{}
	for _, env := range environments {
		byName[env.Name] = env
	}

	for _, name := range environmentOrder(byName, needs) {
		env, exists := byName[name]
		if name == releaseEnvironment {
			if a.auditReleaseEnvironment(env, exists, needs[name]) {
				return
			}

			continue
		}

		if a.auditPlainEnvironment(env, exists, name, needs[name]) {
			return
		}
	}

	a.flag(checkEnvironments, StatusOK, "", "", "every environment admits only the refs that may use its secrets", nil)
}

// environmentOrder lists the environments to judge: release first (it has
// the fix), then the rest by name, existing or only named by a job.
func environmentOrder(existing map[string]environment, needs map[string]environmentNeeds) []string {
	seen := map[string]bool{}

	var names []string

	for name := range existing {
		seen[name] = true
		names = append(names, name)
	}

	for name := range needs {
		if !seen[name] {
			names = append(names, name)
		}
	}

	sort.Slice(names, func(i, j int) bool {
		if (names[i] == releaseEnvironment) != (names[j] == releaseEnvironment) {
			return names[i] == releaseEnvironment
		}

		return names[i] < names[j]
	})

	return names
}

// auditPlainEnvironment judges an environment other than release: it exists,
// admits some refs rather than all, and holds what its jobs read. True when
// it flagged.
func (a *auditor) auditPlainEnvironment(env environment, exists bool, name string, needs environmentNeeds) bool {
	switch {
	case !exists:
		a.flag(checkEnvironments, StatusFail, "missing", "an environment admitting the refs that may deploy to it",
			"environment "+name+" is deployed to by a workflow job but does not exist: GitHub would create it on "+
				"the first run, admitting every branch; create it with a deployment branch policy naming the refs "+
				"that may use its secrets", nil)

		return true
	case env.DeploymentBranchPolicy == nil:
		a.flag(checkEnvironments, StatusFail, "every branch", "a deployment branch policy",
			"environment "+name+" admits every branch, so it gates nothing: a deployment branch policy naming the "+
				"refs that may use its secrets (custom branch policies, or protected branches)", nil)

		return true
	}

	return a.auditEnvironmentSecrets(name, needs)
}

// auditEnvironmentSecrets fails when a job reads a named secret the
// environment does not hold, with the command that sets it. True when it
// flagged.
func (a *auditor) auditEnvironmentSecrets(name string, needs environmentNeeds) bool {
	if len(needs.secrets) == 0 {
		return false
	}

	held, outcome := listPages[orgNamed](a.ctx, a.client, "/environments/"+name+"/secrets?per_page=100", "secrets")
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkEnvironments)

		return true
	}

	for _, secret := range needs.secrets {
		if slices.ContainsFunc(held, func(entry orgNamed) bool { return entry.Name == secret }) {
			continue
		}

		a.flag(checkEnvironments, StatusFail, "no "+secret+" in "+name, secret+" set in "+name,
			"a job deploying to "+name+" reads "+secret+", which the environment does not hold (a repository "+
				"or organization secret of that name would be the exposure): `gh secret set "+secret+" --env "+name+
				" --repo "+a.client.slug()+"`", nil)

		return true
	}

	return false
}

// auditReleaseEnvironment judges the signing policy's environment: custom
// branch policies, exactly the v* tag policy, a required reviewer, and the
// secrets its jobs read. The shape is reconciled by one change; the secrets
// stay the human's. True when it flagged.
func (a *auditor) auditReleaseEnvironment(env environment, exists bool, needs environmentNeeds) bool {
	if !exists && !needs.all && len(needs.secrets) == 0 {
		return false
	}

	policies, outcome := a.releasePolicies(env, exists)
	if outcome.err != nil {
		a.unverifiable(outcome.err, checkEnvironments)

		return true
	}

	current, desired, drifted := releaseDrift(env, exists, policies)
	if !drifted {
		return a.auditEnvironmentSecrets(releaseEnvironment, needs)
	}

	reviewers, wait, preventSelf := env.reviewers()
	if len(reviewers) == 0 {
		var err error

		reviewers, err = a.ownerReviewers()
		if err != nil {
			a.flag(checkEnvironments, StatusFail, current, desired,
				"the release environment "+current+", and the owners who would review it could not be read: "+
					err.Error(), nil)

			return true
		}
	}

	target := releaseTarget{reviewers: reviewers, wait: wait, preventSelf: preventSelf, policies: policies}

	a.flag(checkEnvironments, StatusFail, current, desired,
		"the release environment "+current+": the signing credentials' environment admits release tags alone "+
			"and waits for an owner's review (book/releasing.md)",
		&Change{
			Check:   checkEnvironments,
			Summary: "environment release: " + current + " → " + desired,
			apply:   target.apply,
		})

	return true
}

// releasePolicies lists the release environment's deployment branch
// policies; none for an environment that does not exist or has no custom
// policies, which GitHub answers with a 404.
func (a *auditor) releasePolicies(env environment, exists bool) ([]branchPolicy, apiOutcome) {
	if !exists || env.DeploymentBranchPolicy == nil || !env.DeploymentBranchPolicy.CustomBranchPolicies {
		return nil, apiOutcome{}
	}

	policies, outcome := listPages[branchPolicy](a.ctx, a.client,
		"/environments/"+releaseEnvironment+"/deployment-branch-policies?per_page=100", "branch_policies")
	if outcome.notFound {
		return nil, apiOutcome{}
	}

	return policies, outcome
}

// releaseDrift is the first departure of the release environment from its
// shape, phrased as the finding's current and desired values.
func releaseDrift(env environment, exists bool, policies []branchPolicy) (current, desired string, drifted bool) {
	const want = "admits " + releaseTagPattern + " tags alone, owner review required"

	switch {
	case !exists:
		return "does not exist", want, true
	case env.DeploymentBranchPolicy == nil:
		return "admits every branch", want, true
	case !env.DeploymentBranchPolicy.CustomBranchPolicies:
		return "admits protected branches, not tags", want, true
	case !isReleaseTagPolicy(policies):
		return "admits " + describePolicies(policies), want, true
	}

	if reviewers, _, _ := env.reviewers(); len(reviewers) == 0 {
		return "requires no reviewer", want, true
	}

	return "", "", false
}

// isReleaseTagPolicy is exactly one policy, the v* tag.
func isReleaseTagPolicy(policies []branchPolicy) bool {
	return len(policies) == 1 && policies[0].Name == releaseTagPattern && policies[0].Type == policyTypeTag
}

func describePolicies(policies []branchPolicy) string {
	if len(policies) == 0 {
		return "no ref"
	}

	parts := make([]string, 0, len(policies))
	for _, policy := range policies {
		parts = append(parts, policy.Type+" "+policy.Name)
	}

	return strings.Join(parts, listSeparator)
}

// ownerReviewers are the repository owner's humans: the organization's
// owners, or the user the repository belongs to, up to the six GitHub takes.
func (a *auditor) ownerReviewers() ([]map[string]any, error) {
	var owners []struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}

	outcome := orgClient(a.owner).getJSONAllPages(a.ctx, "/members?role=admin&per_page=100", &owners)

	switch {
	case outcome.notFound:
		var user struct {
			ID int64 `json:"id"`
		}

		if userOutcome := (client{base: "users/" + a.owner}).getJSON(a.ctx, "", &user); userOutcome.err != nil {
			return nil, userOutcome.err
		}

		return []map[string]any{{jsonTypeKey: reviewerTypeUser, "id": user.ID}}, nil
	case outcome.err != nil:
		return nil, outcome.err
	case len(owners) == 0:
		return nil, fmt.Errorf("%w: %s has no owner the token can see", errFieldNotVisible, a.owner)
	}

	reviewers := make([]map[string]any, 0, len(owners))
	for _, owner := range owners[:min(len(owners), maxEnvironmentReviews)] {
		reviewers = append(reviewers, map[string]any{jsonTypeKey: reviewerTypeUser, "id": owner.ID})
	}

	return reviewers, nil
}

// releaseTarget is the release environment's reconciled shape: the PUT that
// sets its reviewers and policy kind, then the tag policy added and every
// other policy removed.
type releaseTarget struct {
	reviewers   []map[string]any
	policies    []branchPolicy
	wait        int
	preventSelf bool
}

func (t releaseTarget) apply(ctx context.Context, apiClient client) error {
	err := apiClient.writeJSON(ctx, "PUT", "/environments/"+releaseEnvironment, map[string]any{
		"wait_timer":          t.wait,
		"prevent_self_review": t.preventSelf,
		"reviewers":           t.reviewers,
		"deployment_branch_policy": map[string]any{
			"protected_branches": false, "custom_branch_policies": true,
		},
	})
	if err != nil {
		return err
	}

	base := "/environments/" + releaseEnvironment + "/deployment-branch-policies"
	present := false

	for _, policy := range t.policies {
		if policy.Name == releaseTagPattern && policy.Type == policyTypeTag {
			present = true

			continue
		}

		if err := apiClient.deleteResource(ctx, fmt.Sprintf("%s/%d", base, policy.ID)); err != nil {
			return err
		}
	}

	if present {
		return nil
	}

	return apiClient.writeJSON(
		ctx,
		"POST",
		base,
		map[string]any{jsonNameKey: releaseTagPattern, jsonTypeKey: policyTypeTag},
	)
}

// workflowEnvironments reads the default branch's workflows through the
// contents API and returns, per environment a job deploys to, the secrets
// those jobs read; a job reading a signing credential needs release whatever
// it declares. An unreadable workflow is an error: the environments it names
// cannot be judged.
func (a *auditor) workflowEnvironments() (map[string]environmentNeeds, error) {
	var entries []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}

	listing := a.client.getJSON(a.ctx, workflowsListing, &entries)
	if listing.notFound {
		return map[string]environmentNeeds{}, nil
	}

	if listing.err != nil {
		return nil, listing.err
	}

	needs := map[string]environmentNeeds{}

	for _, entry := range entries {
		if entry.Type != "file" || (!strings.HasSuffix(entry.Name, ".yaml") && !strings.HasSuffix(entry.Name, ".yml")) {
			continue
		}

		content, err := a.fileContent("/contents/" + entry.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}

		// A workflow that runs only when called runs in its callers'
		// repositories, never in the one carrying it: its environments are
		// theirs to hold.
		read := workflow.Parse(content)
		if read.CalledOnly() {
			continue
		}

		for _, job := range read.Jobs {
			record(needs, job)
		}
	}

	return needs, nil
}

// record adds a job's secrets to its environment's needs. A job calling
// limen's Apple release lane deploys to release through the called sign job,
// which reads the identity there (the lane's own workflow names the rest as
// optional).
func record(needs map[string]environmentNeeds, job workflow.Job) {
	if callsAppleLane(job) {
		job = workflow.Job{Environment: releaseEnvironment, Secrets: appleLaneSecrets()}
	}

	if !job.HoldsSecrets() {
		return
	}

	name := job.Environment
	if name == "" && len(job.SigningSecrets()) == 0 {
		return
	}

	if name == "" {
		name = releaseEnvironment
	}

	need := needs[name]
	need.all = need.all || job.AllSecrets

	for _, secret := range job.Secrets {
		if !slices.Contains(need.secrets, secret) {
			need.secrets = append(need.secrets, secret)
		}
	}

	needs[name] = need
}

// appleLaneWorkflow is limen's Apple release lane, which a project's
// release.yaml calls at a commit of farcloser/limen.
const appleLaneWorkflow = "/.github/workflows/limen-release-apple.yaml@"

// callsAppleLane reports whether the job calls limen's Apple release lane.
func callsAppleLane(job workflow.Job) bool { return strings.Contains(job.Uses, appleLaneWorkflow) }

// appleLaneSecrets are the secrets the lane's sign job requires of the
// release environment: the Developer ID identity. The provisioning profile
// and the notarization key are the project's policy, optional there.
func appleLaneSecrets() []string {
	return []string{"APPLE_DEVELOPER_ID_P12", "APPLE_DEVELOPER_ID_P12_PASSWORD"}
}

// fileContent reads one file through the contents API.
func (a *auditor) fileContent(path string) ([]byte, error) {
	var file struct {
		Content string `json:"content"`
	}

	outcome := a.client.getJSON(a.ctx, path, &file)
	if outcome.err != nil || outcome.notFound {
		return nil, orNotFound(outcome)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}

	return decoded, nil
}

package github

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The checksum workflow commits onto Renovate's branches as the organisation's
// update App, whose private key it reads from a secret. A repository or
// organisation secret is handed to any run in the repository's own context
// that references it, so a write account could push a branch whose workflow
// prints the key. Two objects close that, both canonical and reconciled like
// the other rulesets: the renovate environment, which holds the key and
// admits renovate/* branches only, and the limen:renovate ruleset, which lets
// only the Renovate App and the update App create or move such a branch.
const (
	checkRenovateEnvironment = "renovate-environment"
	checkRulesetRenovate     = "ruleset-renovate-branches"
	rulesetRenovateName      = "limen:renovate"
	renovateEnvironment      = "renovate"
	// renovateBranchPolicy is the environment's deployment branch pattern; a
	// policy's wildcard does not cross a slash, and Renovate's branch names
	// have one.
	renovateBranchPolicy = "renovate/*"
	// renovateBranchRef is the ruleset's condition; a ruleset's ** does cross.
	renovateBranchRef = "refs/heads/renovate/**"
	// renovateAppID is the GitHub App id of Renovate (GET /apps/renovate).
	renovateAppID         = 2740
	pathWorkflowChecksum  = "/contents/.github/workflows/update-aqua-checksum.yaml"
	bypassModeAlways      = "always"
	actorTypeIntegration  = "Integration"
	actorTypeRole         = "RepositoryRole"
	deploymentPolicyType  = "branch"
	environmentPath       = "/environments/" + renovateEnvironment
	branchPoliciesPath    = environmentPath + "/deployment-branch-policies"
	environmentSecretPath = environmentPath + "/secrets/" + updateAppSecret
)

var errUpdateAppNotRegistered = errors.New("the update App is not registered")

// renovateGuardsApply reports whether the repository runs the checksum
// workflow on its default branch; without it there is no key to guard. An
// unreadable default branch is an error, for the caller to report as
// unverifiable. Read once per audit: the rulesets and the environment both
// ask.
func (a *auditor) renovateGuardsApply() (bool, error) {
	if a.checksumWorkflow == nil {
		var file struct {
			Content string `json:"content"`
		}

		outcome := a.client.getJSON(a.ctx, pathWorkflowChecksum, &file)
		if outcome.err != nil {
			return false, fmt.Errorf("reading %s: %w", pathWorkflowChecksum, outcome.err)
		}

		present := !outcome.notFound
		a.checksumWorkflow = &present
	}

	return *a.checksumWorkflow, nil
}

// updateAppID is the registry id of the organisation's update App, looked up
// by the name limen registers it under: the one fact the ruleset's bypass
// list needs that the repository does not carry.
func updateAppID(ctx context.Context, owner string) (int64, error) {
	var app struct {
		ID int64 `json:"id"`
	}

	outcome := client{base: "apps/" + updateAppName(owner)}.getJSON(ctx, "", &app)
	if outcome.notFound {
		return 0, fmt.Errorf(
			"%w: no GitHub App %q (limen bootstrap registers it)",
			errUpdateAppNotRegistered,
			updateAppName(owner),
		)
	}

	if outcome.err != nil {
		return 0, outcome.err
	}

	return app.ID, nil
}

// renovateBypass is who may create or move a renovate/** branch: the
// repository admins, Renovate, and the update App whose id is given.
func renovateBypass(appID int64) []bypassActor {
	return []bypassActor{
		{ActorType: actorTypeRole, BypassMode: bypassModeAlways, ActorID: repositoryAdminRoleID},
		{ActorType: actorTypeIntegration, BypassMode: bypassModeAlways, ActorID: renovateAppID},
		{ActorType: actorTypeIntegration, BypassMode: bypassModeAlways, ActorID: appID},
	}
}

// canonicalRenovateRuleset restricts the creation and update of renovate/**
// branches to the bypass list: a branch the checksum workflow will push to,
// holding the update App's key in its environment, is made by Renovate alone.
func canonicalRenovateRuleset(bypass []bypassActor) map[string]any {
	return rulesetObject(rulesetRenovateName, "branch", renovateBranchRef, bypass,
		[]map[string]any{
			ruleOf("creation"),
			ruleOf("update"),
		})
}

// environmentDetail is GET /environments/{name}: the branch policy mode and
// the protection rules (reviewers, wait timers), which the renovate
// environment must not carry, since its runs are automation.
type environmentDetail struct {
	DeploymentBranchPolicy *deploymentBranchPolicy `json:"deployment_branch_policy"`
	ProtectionRules        []protectionRule        `json:"protection_rules"`
}

// deploymentBranchPolicy is the environment's branch mode; null in the API
// means every branch may deploy.
type deploymentBranchPolicy struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

// protectionRule is one of an environment's protection rules, by kind
// (required_reviewers, wait_timer, branch_policy).
type protectionRule struct {
	Type string `json:"type"`
}

// branchPolicy is one entry of the environment's deployment branch policies.
type branchPolicy struct {
	Name string `json:"name"`
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

// auditRenovateEnvironment reconciles the renovate environment and reports on
// the key it must hold. The environment and its branch policy are limen's to
// create; the secret's value is the human's, so a missing secret fails with
// the command and no change.
func (a *auditor) auditRenovateEnvironment() {
	guarded, err := a.renovateGuardsApply()
	if err != nil {
		a.unverifiable(err, checkRenovateEnvironment)

		return
	}

	if !guarded {
		a.flag(checkRenovateEnvironment, StatusOK, "", "",
			"no update-aqua-checksum workflow on the default branch: no key to guard", nil)

		return
	}

	var detail environmentDetail

	outcome := a.client.getJSON(a.ctx, environmentPath, &detail)
	if outcome.err != nil {
		a.unverifiable(outcome.err, checkRenovateEnvironment)

		return
	}

	if outcome.notFound {
		a.flag(checkRenovateEnvironment, StatusFail, "(absent)", renovateEnvironment,
			"the "+renovateEnvironment+" environment does not exist: the checksum workflow reads the update App's key "+
				"from it, admitted to "+renovateBranchPolicy+" branches alone",
			&Change{
				Check:   checkRenovateEnvironment,
				Summary: "create the " + renovateEnvironment + " environment, admitting " + renovateBranchPolicy,
				apply:   ensureRenovateEnvironment,
			})

		return
	}

	policies, policiesOutcome := listPages[branchPolicy](
		a.ctx,
		a.client,
		branchPoliciesPath+"?per_page=100",
		"branch_policies",
	)
	if policiesOutcome.err != nil || policiesOutcome.notFound {
		a.unverifiable(orNotFound(policiesOutcome), checkRenovateEnvironment)

		return
	}

	if problem := environmentDrift(detail, policies); problem != "" {
		a.flag(
			checkRenovateEnvironment,
			StatusFail,
			problem,
			renovateBranchPolicy+" branches only, no protection rules",
			"the "+renovateEnvironment+" environment "+problem,
			&Change{
				Check:   checkRenovateEnvironment,
				Summary: "reconcile the " + renovateEnvironment + " environment to " + renovateBranchPolicy + " alone",
				apply:   ensureRenovateEnvironment,
			},
		)

		return
	}

	secretOutcome := a.client.api(a.ctx, "GET", environmentSecretPath, nil)
	if secretOutcome.err != nil {
		a.unverifiable(fmt.Errorf("the %s secret: %w", updateAppSecret, secretOutcome.err), checkRenovateEnvironment)

		return
	}

	if secretOutcome.notFound {
		a.flag(checkRenovateEnvironment, StatusFail, "(no "+updateAppSecret+")", updateAppSecret,
			"the "+renovateEnvironment+" environment holds no "+updateAppSecret+": the value is the human's — "+
				"gh secret set "+updateAppSecret+" --env "+renovateEnvironment+" --repo "+a.client.slug()+
				" < <the App's private key>", nil)

		return
	}

	a.flag(checkRenovateEnvironment, StatusOK, "", "",
		"the "+renovateEnvironment+" environment admits "+renovateBranchPolicy+" alone and holds "+updateAppSecret, nil)
}

// environmentDrift names what differs from the canonical environment, or "".
func environmentDrift(detail environmentDetail, policies []branchPolicy) string {
	if detail.DeploymentBranchPolicy == nil || !detail.DeploymentBranchPolicy.CustomBranchPolicies {
		return "admits every branch: a run on any branch could read the key"
	}

	if len(detail.ProtectionRules) > 0 {
		kinds := make([]string, 0, len(detail.ProtectionRules))
		for _, rule := range detail.ProtectionRules {
			kinds = append(kinds, rule.Type)
		}

		return "carries protection rules (" + strings.Join(
			kinds,
			listSeparator,
		) + "): its runs are automation, nobody reviews them"
	}

	names := make([]string, 0, len(policies))
	for _, policy := range policies {
		names = append(names, policy.Type+" "+policy.Name)
	}

	if len(names) != 1 || names[0] != deploymentPolicyType+" "+renovateBranchPolicy {
		current := strings.Join(names, listSeparator)
		if current == "" {
			current = "(none)"
		}

		return "admits " + current + " instead of " + renovateBranchPolicy + " alone"
	}

	return ""
}

// ensureRenovateEnvironment creates or reconciles the renovate environment:
// custom branch policies, no reviewers, no wait, and exactly the renovate/*
// policy. The PUT replaces the protection rules; the policies are listed and
// moved to the one.
func ensureRenovateEnvironment(ctx context.Context, apiClient client) error {
	if err := apiClient.writeJSON(ctx, "PUT", environmentPath, map[string]any{
		"wait_timer":          0,
		"prevent_self_review": false,
		"reviewers":           nil,
		"deployment_branch_policy": map[string]any{
			"protected_branches":     false,
			"custom_branch_policies": true,
		},
	}); err != nil {
		return err
	}

	policies, outcome := listPages[branchPolicy](ctx, apiClient, branchPoliciesPath+"?per_page=100", "branch_policies")
	if outcome.err != nil {
		return outcome.err
	}

	present := false

	for _, policy := range policies {
		if policy.Type == deploymentPolicyType && policy.Name == renovateBranchPolicy {
			present = true

			continue
		}

		if err := apiClient.deleteResource(
			ctx,
			branchPoliciesPath+"/"+strconv.FormatInt(policy.ID, decimalBase),
		); err != nil {
			return err
		}
	}

	if present {
		return nil
	}

	return apiClient.writeJSON(ctx, "POST", branchPoliciesPath, map[string]any{
		jsonNameKey: renovateBranchPolicy, jsonTypeKey: deploymentPolicyType,
	})
}

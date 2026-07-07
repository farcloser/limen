package github

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Org-level check identifiers (design/LIMEN-GITHUB.md, O1–O7). Same naming
// doctrine as the repository checks: explicit, org-prefixed, readable without
// a syllabus. They are also override-file keys.
const (
	checkOrgTwoFactor           = "org-two-factor-requirement"
	checkOrgDefaultRepoPerm     = "org-default-repository-permission"
	checkOrgCreatePublicRepos   = "org-members-create-public-repositories"
	checkOrgForkPrivateRepos    = "org-members-fork-private-repositories"
	checkOrgChangeVisibility    = "org-members-change-repository-visibility"
	checkOrgDeleteRepos         = "org-members-delete-repositories"
	checkOrgCreatePublicPages   = "org-members-create-public-pages"
	checkOrgWebCommitSignoff    = "org-web-commit-signoff"
	checkOrgAdmins              = "org-admins"
	checkOrgActionsEnabledRepos = "org-actions-enabled-repositories"
	checkOrgActionsAllowed      = "org-actions-allowed"
	checkOrgActionsShaPinning   = "org-actions-sha-pinning"
	checkOrgActionsWorkflow     = "org-actions-workflow-permissions"
	checkOrgActionsApprovePRs   = "org-actions-approve-pull-requests"
	checkOrgForkPRApproval      = "org-actions-fork-pr-approval"
	checkOrgSelfHostedRunners   = "org-actions-self-hosted-runners"
	checkOrgSecurityConfig      = "org-code-security-configuration"
	checkOrgInstalledApps       = "org-installed-apps"
	checkOrgWebhooks            = "org-webhooks"
	checkOrgActionsSecrets      = "org-actions-secrets" //nolint:gosec // G101: a check identifier, not a credential.
	checkOrgTeams               = "org-teams"
	checkOrgPATGrants           = "org-personal-access-tokens"
	checkOrgProfileDescription  = "org-profile-description"
	checkOrgCommunityHealthRepo = "org-community-health-repo"
	checkOrgCommunityHealthSet  = "org-community-health-content"
)

// knownOrgChecks is every org-level identifier, merged into knownChecks for
// override-file validation.
func knownOrgChecks() map[string]bool {
	return map[string]bool{
		checkOrgTwoFactor:           true,
		checkOrgDefaultRepoPerm:     true,
		checkOrgCreatePublicRepos:   true,
		checkOrgForkPrivateRepos:    true,
		checkOrgChangeVisibility:    true,
		checkOrgDeleteRepos:         true,
		checkOrgCreatePublicPages:   true,
		checkOrgWebCommitSignoff:    true,
		checkOrgAdmins:              true,
		checkOrgActionsEnabledRepos: true,
		checkOrgActionsAllowed:      true,
		checkOrgActionsShaPinning:   true,
		checkOrgActionsWorkflow:     true,
		checkOrgActionsApprovePRs:   true,
		checkOrgForkPRApproval:      true,
		checkOrgSelfHostedRunners:   true,
		checkOrgSecurityConfig:      true,
		checkOrgInstalledApps:       true,
		checkOrgWebhooks:            true,
		checkOrgActionsSecrets:      true,
		checkOrgTeams:               true,
		checkOrgPATGrants:           true,
		checkOrgProfileDescription:  true,
		checkOrgCommunityHealthRepo: true,
		checkOrgCommunityHealthSet:  true,
	}
}

// orgSettings is the subset of GET /orgs/{org} the audit reads. Every
// governed field is a pointer, deliberately: the endpoint answers even
// anonymously, with the admin-only fields simply ABSENT — a zero value would
// read as compliant. nil means "the token cannot see this", which is
// unverifiable, never a pass.
type orgSettings struct {
	TwoFactorRequirementEnabled        *bool   `json:"two_factor_requirement_enabled"`
	DefaultRepositoryPermission        *string `json:"default_repository_permission"`
	MembersCanCreatePublicRepositories *bool   `json:"members_can_create_public_repositories"`
	MembersCanForkPrivateRepositories  *bool   `json:"members_can_fork_private_repositories"`
	MembersCanChangeRepoVisibility     *bool   `json:"members_can_change_repo_visibility"`
	MembersCanDeleteRepositories       *bool   `json:"members_can_delete_repositories"`
	MembersCanCreatePublicPages        *bool   `json:"members_can_create_public_pages"`
	WebCommitSignoffRequired           *bool   `json:"web_commit_signoff_required"`
	Description                        *string `json:"description"`
}

// Endpoint-specific unverifiable hints (static errors per err113).
var (
	errOrgHookScope = errors.New(
		"the org webhooks API needs the admin:org_hook scope (gh auth refresh -h github.com -s admin:org_hook)",
	)
	errPATPolicyUnconfigured = errors.New(
		"the fine-grained PAT API answers 404 until the org's PAT policies are configured" +
			" (Settings → Third-party Access → Personal access tokens); configure them, then re-audit",
	)
)

// The org-level baselines (floors).
const (
	orgPermissionRead = "read"
	orgPermissionNone = "none"
	enabledReposNone  = "none"
	enabledReposAll   = "all"
	methodPut         = "PUT"
)

// Named shapes for the inventory decoders (revive: no nested anonymous
// structs).
type orgNamed struct {
	Name string `json:"name"`
}

type orgLogin struct {
	Login string `json:"login"`
}

type orgAppInstallation struct {
	AppSlug string `json:"app_slug"`
}

type orgSecurityDefault struct {
	DefaultForNewRepos string   `json:"default_for_new_repos"`
	Configuration      orgNamed `json:"configuration"`
}

// AuditOrg checks the organization's settings against the baseline (O1–O7 of
// design/LIMEN-GITHUB.md). overrides maps exempted check identifiers to reasons,
// exactly as for Audit — org runs read the same override file, from wherever
// the command runs (canonically the org's .github repository). It returns
// the findings and the changes a fix run would apply.
//
// Deliberate v1 scopes, named: the code-security configuration and the
// community-health repository report advisories rather than auto-fixes
// (creating configurations and repositories is a human act), and org rulesets
// are absent entirely — their migration from the per-repo rulesets is phase 4
// of the design.
func AuditOrg(org string, overrides map[string]string) ([]Finding, []Change) {
	aud := &auditor{
		client:        orgClient(org),
		overrides:     overrides,
		settingsPatch: map[string]any{},
	}

	aud.auditOrgObject()
	aud.auditOrgAdmins()
	aud.auditOrgActions()
	aud.auditOrgSecurityConfiguration()
	aud.auditOrgSurface()
	aud.auditOrgCommunityHealth(org)
	aud.flushSettingsPatch()

	return aud.findings, aud.changes
}

// auditOrgObject covers every check answered by GET /orgs/{org}: the O1
// membership floor, the O6 profile, and the org-wide DCO switch. Fixable
// fields stage into the consolidated PATCH /orgs/{org}.
func (a *auditor) auditOrgObject() {
	var settings orgSettings

	outcome := a.client.getJSON("", &settings)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome),
			checkOrgTwoFactor, checkOrgDefaultRepoPerm, checkOrgCreatePublicRepos,
			checkOrgForkPrivateRepos, checkOrgChangeVisibility, checkOrgDeleteRepos,
			checkOrgCreatePublicPages, checkOrgWebCommitSignoff, checkOrgProfileDescription)

		return
	}

	a.auditOrgMembershipFloor(settings)
	a.auditOrgProfile(settings)
}

// auditOrgMembershipFloor is O1: authentication and what members can do
// unilaterally. Repos are bootstrapped, not clicked — creation of anything
// public, visibility changes, and deletions are owner acts.
func (a *auditor) auditOrgMembershipFloor(settings orgSettings) {
	switch {
	case settings.TwoFactorRequirementEnabled == nil:
		a.unverifiable(errFieldNotVisible, checkOrgTwoFactor)
	case *settings.TwoFactorRequirementEnabled:
		a.flag(checkOrgTwoFactor, StatusOK, "", "", "two-factor authentication is required", nil)
	default:
		// Advisory, never a fix: enabling it evicts non-compliant members —
		// a human decision (and the field is not writable via the API).
		a.flag(checkOrgTwoFactor, StatusAdvisory, boolFalse, boolTrue,
			"two-factor authentication is not required — enable it by hand (members without 2FA are removed)", nil)
	}

	switch {
	case settings.DefaultRepositoryPermission == nil:
		a.unverifiable(errFieldNotVisible, checkOrgDefaultRepoPerm)
	case *settings.DefaultRepositoryPermission == orgPermissionRead,
		*settings.DefaultRepositoryPermission == orgPermissionNone:
		a.flag(checkOrgDefaultRepoPerm, StatusOK, "", "",
			"members' default repository permission is "+*settings.DefaultRepositoryPermission, nil)
	default:
		a.flag(checkOrgDefaultRepoPerm, StatusFail,
			*settings.DefaultRepositoryPermission, orgPermissionRead,
			"members' default repository permission must be read (or none — stricter passes)",
			a.patchSettings(checkOrgDefaultRepoPerm,
				"default repository permission: "+*settings.DefaultRepositoryPermission+" → read",
				map[string]any{"default_repository_permission": orgPermissionRead}))
	}

	// The CHECK fails only on public creation (a paid-plan org allowing
	// private-only creation passes — stricter than the floor). The FIX turns
	// member repo creation off entirely: "public off, private on" is a
	// paid-plan-only state GitHub rejects on free orgs ("Private-only
	// repository creation policy is not allowed"), and the doctrine is that
	// repos are bootstrapped, not clicked — owners create regardless.
	switch {
	case settings.MembersCanCreatePublicRepositories == nil:
		a.unverifiable(errFieldNotVisible, checkOrgCreatePublicRepos)
	case !*settings.MembersCanCreatePublicRepositories:
		a.flag(checkOrgCreatePublicRepos, StatusOK, "", "", "members cannot create public repositories", nil)
	default:
		a.flag(checkOrgCreatePublicRepos, StatusFail, boolTrue, boolFalse,
			"members must not be able to create public repositories",
			a.patchSettings(
				checkOrgCreatePublicRepos,
				"members_can_create_repositories: → false (creation off entirely — the plan-portable floor; owners still create)",
				map[string]any{"members_can_create_repositories": false},
			))
	}

	a.orgBoolFloor(boolFloor{
		check:       checkOrgForkPrivateRepos,
		value:       settings.MembersCanForkPrivateRepositories,
		compliantIs: false,
		field:       "members_can_fork_private_repositories",
		okMessage:   "members cannot fork private repositories",
		failMessage: "members must not be able to fork private repositories",
	})
	a.orgBoolFloor(boolFloor{
		check:       checkOrgCreatePublicPages,
		value:       settings.MembersCanCreatePublicPages,
		compliantIs: false,
		field:       "members_can_create_public_pages",
		okMessage:   "members cannot create public GitHub Pages sites",
		failMessage: "members must not be able to publish public GitHub Pages sites",
	})
	a.orgBoolFloor(boolFloor{
		check:       checkOrgWebCommitSignoff,
		value:       settings.WebCommitSignoffRequired,
		compliantIs: true,
		field:       "web_commit_signoff_required",
		okMessage:   "web commits require sign-off org-wide",
		failMessage: "web commits must require sign-off org-wide (DCO — the repo-level twin inherits this)",
	})

	// Read-only via the REST API (present on GET, absent from PATCH) — the
	// design marked these fixable; reality says advisory. Named deviation.
	a.orgReadOnlyFloor(checkOrgChangeVisibility, settings.MembersCanChangeRepoVisibility,
		"members cannot change repository visibility",
		"members can change repository visibility — restrict to admins by hand (not writable via the API)")
	a.orgReadOnlyFloor(checkOrgDeleteRepos, settings.MembersCanDeleteRepositories,
		"members cannot delete or transfer repositories",
		"members can delete or transfer repositories — restrict to admins by hand (not writable via the API)")
}

// boolFloor is one PATCH-able boolean org setting checked against its
// compliant value (a struct, not positional bools — the toggle doctrine).
type boolFloor struct {
	value       *bool
	check       string
	field       string
	okMessage   string
	failMessage string
	compliantIs bool
}

// orgBoolFloor flags one PATCH-able boolean floor and stages its fix.
func (a *auditor) orgBoolFloor(floor boolFloor) {
	switch {
	case floor.value == nil:
		a.unverifiable(errFieldNotVisible, floor.check)
	case *floor.value == floor.compliantIs:
		a.flag(floor.check, StatusOK, "", "", floor.okMessage, nil)
	default:
		current := strconv.FormatBool(!floor.compliantIs)
		desired := strconv.FormatBool(floor.compliantIs)
		a.flag(floor.check, StatusFail, current, desired, floor.failMessage,
			a.patchSettings(floor.check,
				floor.field+": "+current+" → "+desired,
				map[string]any{floor.field: floor.compliantIs}))
	}
}

// orgReadOnlyFloor flags one boolean floor the API can read but not write:
// compliant passes, non-compliant is an advisory with by-hand guidance.
func (a *auditor) orgReadOnlyFloor(check string, value *bool, okMessage, adviceMessage string) {
	switch {
	case value == nil:
		a.unverifiable(errFieldNotVisible, check)
	case !*value:
		a.flag(check, StatusOK, "", "", okMessage, nil)
	default:
		a.flag(check, StatusAdvisory, boolTrue, boolFalse, adviceMessage, nil)
	}
}

// auditOrgProfile is O6 — low stakes, advisory prose only.
func (a *auditor) auditOrgProfile(settings orgSettings) {
	if settings.Description != nil && strings.TrimSpace(*settings.Description) != "" {
		a.flag(checkOrgProfileDescription, StatusOK, "", "", "the organization has a description", nil)

		return
	}

	a.flag(checkOrgProfileDescription, StatusAdvisory, "(empty)", "a one-line description",
		"the organization has no description — one line of what it is, for people who land on the profile", nil)
}

// auditOrgAdmins inventories the owner roster (O1) — advisory by design:
// membership is people, never auto-fixed. The roster is declared in the
// override file (peribolos-lite), and the declaration is LOAD-BEARING, not a
// blanket exemption: every actual owner login must appear in the declared
// reason, so an owner nobody declared turns the audit red again. Handled
// here rather than through flag()'s generic exemption, which cannot compare
// the declaration against reality.
func (a *auditor) auditOrgAdmins() {
	var admins []struct {
		Login string `json:"login"`
	}

	// One page of 100 is beyond any realistic owner roster; per_page still
	// beats the API's default page of 30.
	outcome := a.client.getJSON("/members?role=admin&per_page=100", &admins)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgAdmins)

		return
	}

	logins := make([]string, 0, len(admins))
	for _, admin := range admins {
		logins = append(logins, admin.Login)
	}

	roster := strings.Join(logins, listSeparator)

	declaration, declared := a.overrides[checkOrgAdmins]
	if !declared {
		a.findings = append(a.findings, Finding{
			Check:   checkOrgAdmins,
			Status:  StatusAdvisory,
			Current: roster,
			Desired: "a declared roster",
			Message: "organization owners: " + roster +
				" — declare the expected roster by exempting this check in the override file",
		})

		return
	}

	var undeclared []string

	for _, login := range logins {
		if !declarationNamesLogin(declaration, login) {
			undeclared = append(undeclared, login)
		}
	}

	if len(undeclared) > 0 {
		a.findings = append(a.findings, Finding{
			Check:   checkOrgAdmins,
			Status:  StatusAdvisory,
			Current: roster,
			Desired: "declared: " + declaration,
			Message: "owner(s) not in the declared roster: " + strings.Join(undeclared, listSeparator) +
				" — an owner was added since the declaration; update it or investigate",
		})

		return
	}

	a.findings = append(a.findings, Finding{
		Check:   checkOrgAdmins,
		Status:  StatusOK,
		Current: roster,
		Message: "owners match the declared roster: " + declaration,
	})
}

// auditOrgActions is O2: the org-wide Actions policy — the org twin of the
// repository R2 checks, so new repositories are born hardened.
func (a *auditor) auditOrgActions() {
	a.auditOrgActionsPermissions()
	a.auditOrgActionsWorkflowDefaults()
	a.auditOrgForkPRApproval()
	a.auditOrgSelfHostedRunners()
}

// orgActionsPermissions is the GET/PUT /orgs/{org}/actions/permissions shape.
type orgActionsPermissions struct {
	EnabledRepositories string `json:"enabled_repositories"`
	AllowedActions      string `json:"allowed_actions"`
	ShaPinningRequired  *bool  `json:"sha_pinning_required"`
}

// auditOrgActionsPermissions covers the enabled-repositories policy, the
// allowed-actions policy, and org-wide SHA pinning. The PUT replaces the
// whole object, so a fix must send every field — but the target is built from
// the CURRENT state with only the FAILING fields moved to the baseline: a
// compliant policy (a "selected" allowlist someone curated, or "local_only")
// must survive a fix that only tightens another field. Sending the canonical
// target for passing fields once wiped an org's pattern allowlist when only
// SHA pinning was being fixed.
func (a *auditor) auditOrgActionsPermissions() {
	var permissions orgActionsPermissions

	outcome := a.client.getJSON("/actions/permissions", &permissions)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome),
			checkOrgActionsEnabledRepos, checkOrgActionsAllowed, checkOrgActionsShaPinning)

		return
	}

	// A field moves to the baseline only when its own check both fails AND is
	// not exempted — flag() below suppresses an exempted check's plan, so its
	// field keeping the current value here is what keeps plan and PUT equal.
	enabledFixes := permissions.EnabledRepositories == enabledReposNone &&
		!a.exempted(checkOrgActionsEnabledRepos)
	allowedFixes := permissions.AllowedActions == allowedActionsAll &&
		!a.exempted(checkOrgActionsAllowed)
	shaFixes := permissions.ShaPinningRequired != nil && !*permissions.ShaPinningRequired &&
		!a.exempted(checkOrgActionsShaPinning)

	targetScope := permissions.EnabledRepositories
	if enabledFixes {
		// "none" would keep CI dead org-wide; the floor is "all".
		targetScope = enabledReposAll
	}

	targetAllowed := permissions.AllowedActions
	if allowedFixes {
		targetAllowed = "selected"
	}

	fixPermissions := func(apiClient client) error {
		payload := map[string]any{
			"enabled_repositories": targetScope,
			"allowed_actions":      targetAllowed,
		}
		// Written back only when the token could read it (nil means the field
		// was invisible and its check went unverifiable — never guess a value
		// into a whole-object PUT).
		if permissions.ShaPinningRequired != nil {
			payload["sha_pinning_required"] = shaFixes || *permissions.ShaPinningRequired
		}

		if err := apiClient.writeJSON(methodPut, "/actions/permissions", payload); err != nil {
			return err
		}

		// The selected-actions allowlist is defined only when THIS fix is the
		// one restricting the policy to "selected": an already-restricted
		// policy keeps whatever allowlist (or local_only semantics) it has.
		if !allowedFixes {
			return nil
		}

		return apiClient.writeJSON(methodPut, "/actions/permissions/selected-actions", map[string]any{
			"github_owned_allowed": true,
			"verified_allowed":     false,
			"patterns_allowed":     []string{},
		})
	}

	if permissions.EnabledRepositories == enabledReposNone {
		a.flag(checkOrgActionsEnabledRepos, StatusFail, enabledReposNone, enabledReposAll,
			"Actions are disabled org-wide — every repository's CI is dead; enable all (or a selected list)",
			&Change{
				Check:   checkOrgActionsEnabledRepos,
				Summary: "Actions enabled repositories: none → all",
				apply:   fixPermissions,
			})
	} else {
		a.flag(checkOrgActionsEnabledRepos, StatusOK, "", "",
			"Actions are enabled ("+permissions.EnabledRepositories+" repositories)", nil)
	}

	if permissions.AllowedActions == allowedActionsAll {
		a.flag(
			checkOrgActionsAllowed,
			StatusFail,
			allowedActionsAll,
			"selected",
			"the org-wide allowed-actions policy must not be \"all\" — restrict to GitHub-owned plus a pinned allowlist",
			&Change{
				Check:   checkOrgActionsAllowed,
				Summary: "org allowed actions: all → selected (GitHub-owned only)",
				apply:   fixPermissions,
			},
		)
	} else {
		a.flag(checkOrgActionsAllowed, StatusOK, "", "",
			"the org-wide allowed-actions policy is restricted", nil)
	}

	switch {
	case permissions.ShaPinningRequired == nil:
		a.unverifiable(errFieldNotVisible, checkOrgActionsShaPinning)
	case *permissions.ShaPinningRequired:
		a.flag(checkOrgActionsShaPinning, StatusOK, "", "",
			"actions must be pinned to a full commit SHA org-wide", nil)
	default:
		a.flag(
			checkOrgActionsShaPinning,
			StatusFail,
			boolFalse,
			boolTrue,
			"org-wide SHA pinning for actions must be required — tags are movable, SHAs are not (the canonical workflows already comply)",
			&Change{
				Check:   checkOrgActionsShaPinning,
				Summary: "require SHA-pinned actions org-wide: false → true",
				apply:   fixPermissions,
			},
		)
	}
}

// auditOrgActionsWorkflowDefaults is the org twin of the repository workflow
// token checks: read-only GITHUB_TOKEN, no PR approvals from workflows.
func (a *auditor) auditOrgActionsWorkflowDefaults() {
	var workflow struct {
		DefaultWorkflowPermissions   string `json:"default_workflow_permissions"`
		CanApprovePullRequestReviews bool   `json:"can_approve_pull_request_reviews"`
	}

	outcome := a.client.getJSON("/actions/permissions/workflow", &workflow)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgActionsWorkflow, checkOrgActionsApprovePRs)

		return
	}

	// The PUT replaces both fields, so the target starts from the current
	// state and moves only the fields whose checks fail unexempted — same
	// doctrine as auditOrgActionsPermissions.
	targetPerms := workflow.DefaultWorkflowPermissions
	if targetPerms != baselineWorkflowPerms && !a.exempted(checkOrgActionsWorkflow) {
		targetPerms = baselineWorkflowPerms
	}

	targetApprove := workflow.CanApprovePullRequestReviews && a.exempted(checkOrgActionsApprovePRs)

	fixWorkflow := func(apiClient client) error {
		return apiClient.writeJSON(methodPut, "/actions/permissions/workflow", map[string]any{
			"default_workflow_permissions":     targetPerms,
			"can_approve_pull_request_reviews": targetApprove,
		})
	}

	if workflow.DefaultWorkflowPermissions == baselineWorkflowPerms {
		a.flag(checkOrgActionsWorkflow, StatusOK, "", "",
			"the org default workflow token is read-only", nil)
	} else {
		a.flag(checkOrgActionsWorkflow, StatusFail,
			workflow.DefaultWorkflowPermissions, baselineWorkflowPerms,
			"the org default workflow token must be read-only",
			&Change{
				Check:   checkOrgActionsWorkflow,
				Summary: "org workflow token permissions: " + workflow.DefaultWorkflowPermissions + " → read",
				apply:   fixWorkflow,
			})
	}

	if !workflow.CanApprovePullRequestReviews {
		a.flag(checkOrgActionsApprovePRs, StatusOK, "", "",
			"workflows cannot approve pull requests org-wide", nil)
	} else {
		a.flag(checkOrgActionsApprovePRs, StatusFail, boolTrue, boolFalse,
			"workflows must not be able to approve pull requests org-wide",
			&Change{
				Check:   checkOrgActionsApprovePRs,
				Summary: "org workflows can approve pull requests: true → false",
				apply:   fixWorkflow,
			})
	}
}

// auditOrgForkPRApproval is the org twin of the repository fork-PR approval
// floor: approval only for accounts new to GitHub is below the floor.
func (a *auditor) auditOrgForkPRApproval() {
	var approval struct {
		ApprovalPolicy string `json:"approval_policy"`
	}

	outcome := a.client.getJSON("/actions/permissions/fork-pr-contributor-approval", &approval)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkOrgForkPRApproval)
	case approval.ApprovalPolicy == approvalWeakest:
		a.flag(checkOrgForkPRApproval, StatusFail, approvalWeakest, approvalBaseline+" (or stricter)",
			"fork pull requests must require approval for all first-time contributors org-wide",
			&Change{
				Check:   checkOrgForkPRApproval,
				Summary: "org fork PR approval: " + approvalWeakest + " → " + approvalBaseline,
				apply: func(apiClient client) error {
					return apiClient.writeJSON(methodPut, "/actions/permissions/fork-pr-contributor-approval",
						map[string]any{"approval_policy": approvalBaseline})
				},
			})
	default:
		a.flag(checkOrgForkPRApproval, StatusOK, "", "",
			"fork pull requests require contributor approval org-wide ("+approval.ApprovalPolicy+")", nil)
	}
}

// auditOrgSelfHostedRunners: the baseline has no self-hosted runners — an
// advisory inventory when any exist. Deliberately runners, not runner
// GROUPS: GitHub's built-in "Default" group always exists, so counting
// groups flags every org forever. Plan-gated (404) counts as none.
func (a *auditor) auditOrgSelfHostedRunners() {
	var runners struct {
		Runners    []orgNamed `json:"runners"`
		TotalCount int        `json:"total_count"`
	}

	outcome := a.client.getJSON("/actions/runners", &runners)

	switch {
	case outcome.notFound:
		a.flag(checkOrgSelfHostedRunners, StatusOK, "", "",
			"no self-hosted runners (unavailable on this plan)", nil)
	case outcome.err != nil:
		a.unverifiable(outcome.err, checkOrgSelfHostedRunners)
	case runners.TotalCount == 0:
		a.flag(checkOrgSelfHostedRunners, StatusOK, "", "", "no self-hosted runners", nil)
	default:
		names := make([]string, 0, len(runners.Runners))
		for _, runner := range runners.Runners {
			names = append(names, runner.Name)
		}

		a.flag(checkOrgSelfHostedRunners, StatusAdvisory, strings.Join(names, listSeparator), "(none)",
			"self-hosted runner(s) exist — a standing execution surface; review by hand", nil)
	}
}

// auditOrgSecurityConfiguration is O3, advisory in v1 (a named deviation
// from the design's fixable ✓): GitHub is closing the legacy per-org
// security fields down in favor of code security configurations, and
// creating a canonical configuration is a payload-heavy human decision —
// v1 verifies one is set as the default for new repositories.
func (a *auditor) auditOrgSecurityConfiguration() {
	var defaults []orgSecurityDefault

	outcome := a.client.getJSON("/code-security/configurations/defaults", &defaults)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkOrgSecurityConfig)
	case len(defaults) == 0:
		a.flag(
			checkOrgSecurityConfig,
			StatusAdvisory,
			"(none)",
			"one canonical configuration, default for new repositories",
			"no default code security configuration — new repositories are born unconfigured; create one and set it as default",
			nil,
		)
	default:
		var described []string
		for _, entry := range defaults {
			described = append(described, entry.Configuration.Name+" ("+entry.DefaultForNewRepos+")")
		}

		a.flag(checkOrgSecurityConfig, StatusOK, "", "",
			"default code security configuration: "+strings.Join(described, listSeparator), nil)
	}
}

// auditOrgSurface is O4: standing inventories, reviewable rather than
// enforceable — apps, webhooks, secrets, teams, and fine-grained PAT grants.
func (a *auditor) auditOrgSurface() {
	a.auditOrgInstalledApps()
	a.auditOrgWebhooks()
	a.auditOrgActionsSecrets()
	a.auditOrgTeams()
	a.auditOrgPATGrants()
}

// auditOrgInstalledApps inventories GitHub App installations. Informational
// (ok with the list): the grant already happened through a human; the value
// is that the roster is visible on every audit.
func (a *auditor) auditOrgInstalledApps() {
	var installations struct {
		Installations []orgAppInstallation `json:"installations"`
		TotalCount    int                  `json:"total_count"`
	}

	outcome := a.client.getJSON("/installations", &installations)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgInstalledApps)

		return
	}

	if installations.TotalCount == 0 {
		a.flag(checkOrgInstalledApps, StatusOK, "", "", "no GitHub Apps installed", nil)

		return
	}

	slugs := make([]string, 0, len(installations.Installations))
	for _, installation := range installations.Installations {
		slugs = append(slugs, installation.AppSlug)
	}

	a.flag(checkOrgInstalledApps, StatusOK, "", "",
		"installed GitHub Apps: "+strings.Join(slugs, listSeparator), nil)
}

// auditOrgWebhooks is the org twin of the repository webhook hygiene check.
func (a *auditor) auditOrgWebhooks() {
	var hooks []webhook

	outcome := a.client.getJSON("/hooks?per_page=100", &hooks)
	if outcome.err != nil || outcome.notFound {
		// Org webhooks live behind their own classic scope, which admin:org
		// does not cover — and GitHub answers 404, not 403, without it.
		if outcome.notFound {
			a.unverifiable(errOrgHookScope, checkOrgWebhooks)

			return
		}

		a.unverifiable(outcome.err, checkOrgWebhooks)

		return
	}

	var offenders []string

	for _, hook := range hooks {
		insecure := fmt.Sprintf("%v", hook.Config.InsecureSSL) != "0"
		if !strings.HasPrefix(hook.Config.URL, "https://") || hook.Config.Secret == "" || insecure {
			offenders = append(offenders, hook.Config.URL)
		}
	}

	if len(offenders) > 0 {
		a.flag(checkOrgWebhooks, StatusAdvisory,
			strings.Join(offenders, listSeparator), "https + secret + TLS verification",
			"org webhook(s) without HTTPS, a secret, or TLS verification — review and fix by hand"+
				pageFullCaveat(len(hooks)), nil)

		return
	}

	a.flag(checkOrgWebhooks, StatusOK, "", "",
		"org webhooks are compliant (or none exist)"+pageFullCaveat(len(hooks)), nil)
}

// auditOrgActionsSecrets inventories org-level Actions secrets (names only —
// the API never exposes values).
func (a *auditor) auditOrgActionsSecrets() {
	var secrets struct {
		Secrets    []orgNamed `json:"secrets"`
		TotalCount int        `json:"total_count"`
	}

	outcome := a.client.getJSON("/actions/secrets", &secrets)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgActionsSecrets)

		return
	}

	if secrets.TotalCount == 0 {
		a.flag(checkOrgActionsSecrets, StatusOK, "", "", "no org-level Actions secrets", nil)

		return
	}

	names := make([]string, 0, len(secrets.Secrets))
	for _, secret := range secrets.Secrets {
		names = append(names, secret.Name)
	}

	a.flag(checkOrgActionsSecrets, StatusOK, "", "",
		"org-level Actions secrets: "+strings.Join(names, listSeparator), nil)
}

// auditOrgTeams inventories teams — management is deliberately out of scope
// while the org is one person (peribolos territory, per the design).
func (a *auditor) auditOrgTeams() {
	var teams []struct {
		Slug string `json:"slug"`
	}

	outcome := a.client.getJSON("/teams?per_page=100", &teams)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgTeams)

		return
	}

	if len(teams) == 0 {
		a.flag(checkOrgTeams, StatusOK, "", "", "no teams", nil)

		return
	}

	slugs := make([]string, 0, len(teams))
	for _, team := range teams {
		slugs = append(slugs, team.Slug)
	}

	a.flag(checkOrgTeams, StatusOK, "", "",
		"teams: "+strings.Join(slugs, listSeparator)+pageFullCaveat(len(teams)), nil)
}

// auditOrgPATGrants inventories fine-grained personal-access-token grants
// into the org. The API is availability-gated; gated reads report
// unverifiable, per the design.
func (a *auditor) auditOrgPATGrants() {
	var grants []struct {
		Owner orgLogin `json:"owner"`
	}

	outcome := a.client.getJSON("/personal-access-tokens", &grants)
	if outcome.err != nil || outcome.notFound {
		if outcome.notFound {
			a.unverifiable(errPATPolicyUnconfigured, checkOrgPATGrants)

			return
		}

		a.unverifiable(outcome.err, checkOrgPATGrants)

		return
	}

	if len(grants) == 0 {
		a.flag(checkOrgPATGrants, StatusOK, "", "", "no fine-grained personal-access-token grants", nil)

		return
	}

	owners := make([]string, 0, len(grants))
	for _, grant := range grants {
		owners = append(owners, grant.Owner.Login)
	}

	a.flag(checkOrgPATGrants, StatusOK, "", "",
		"fine-grained personal-access-token grants by: "+strings.Join(owners, listSeparator), nil)
}

// declarationNamesLogin reports whether the roster declaration names the
// login as a whole token. Token-wise, never substring containment: a login
// that merely appears INSIDE the prose ("post" inside "apostasie") must not
// count as declared — this check exists to catch surprise owners. Tokens are
// runs of GitHub's login alphabet (alphanumerics and hyphens), compared
// case-insensitively (logins are case-insensitive). Residual limit, accepted:
// an owner whose login equals an English word used in the prose ("owner")
// still matches — write declarations with the logins verbatim.
func declarationNamesLogin(declaration, login string) bool {
	notLoginChar := func(char rune) bool {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
			return false
		case char >= '0' && char <= '9', char == '-':
			return false
		default:
			return true
		}
	}

	for _, token := range strings.FieldsFunc(declaration, notLoginChar) {
		if strings.EqualFold(token, login) {
			return true
		}
	}

	return false
}

// communityHealthFiles is the canonical fallback set the org .github
// repository must carry (O7): the security policy (pairs with the repos'
// private vulnerability reporting), the contribution terms (DCO — where
// contributors actually look), and the org profile README.
func communityHealthFiles() []string {
	return []string{"SECURITY.md", "CONTRIBUTING.md", "profile/README.md"}
}

// communityHealthLocations is everywhere GitHub resolves a community health
// file from within a repository: the root, .github/, and docs/. The org
// profile README is the exception — GitHub only reads it at profile/README.md.
func communityHealthLocations(file string) []string {
	if strings.Contains(file, "/") {
		return []string{file}
	}

	return []string{file, ".github/" + file, "docs/" + file}
}

// auditOrgCommunityHealth is O7: the org .github repository — the fallback
// mechanism GitHub resolves community health files from. Advisory verdicts
// throughout: creating repositories and authoring policy documents are human
// acts, and the repository's own compliance is enforced where it always is —
// by `limen check` inside that repository.
func (a *auditor) auditOrgCommunityHealth(org string) {
	healthRepo := repoClient(org + "/.github")

	var settings struct {
		Private bool `json:"private"`
	}

	outcome := healthRepo.getJSON("", &settings)

	switch {
	case outcome.notFound:
		a.flag(
			checkOrgCommunityHealthRepo,
			StatusAdvisory,
			"(missing)",
			org+"/.github, public",
			"no org .github repository — bootstrap one with limen (it carries SECURITY.md, CONTRIBUTING.md, and the org profile README for every repo that lacks its own)",
			nil,
		)
		a.flag(checkOrgCommunityHealthSet, StatusAdvisory, "(missing)",
			strings.Join(communityHealthFiles(), listSeparator),
			"the canonical community-health set does not exist without the org .github repository", nil)

		return
	case outcome.err != nil:
		a.unverifiable(outcome.err, checkOrgCommunityHealthRepo, checkOrgCommunityHealthSet)

		return
	case settings.Private:
		// GitHub only resolves community-health fallbacks from a PUBLIC
		// .github repository; a private one silently does nothing.
		// Advisory, not a fix: flipping visibility exposes history.
		a.flag(
			checkOrgCommunityHealthRepo,
			StatusAdvisory,
			"private",
			"public",
			"the org .github repository is private — GitHub ignores it as a fallback; make it public by hand (review the history first)",
			nil,
		)
	default:
		a.flag(checkOrgCommunityHealthRepo, StatusOK, "", "", "the org .github repository exists and is public", nil)
	}

	var missing []string

	for _, file := range communityHealthFiles() {
		found := false

		for _, location := range communityHealthLocations(file) {
			fileOutcome := healthRepo.api("GET", "/contents/"+location, nil)

			switch {
			case fileOutcome.notFound:
				continue
			case fileOutcome.err != nil:
				a.unverifiable(fileOutcome.err, checkOrgCommunityHealthSet)

				return
			default:
				found = true
			}

			break
		}

		if !found {
			missing = append(missing, file)
		}
	}

	if len(missing) > 0 {
		a.flag(
			checkOrgCommunityHealthSet,
			StatusAdvisory,
			"missing: "+strings.Join(missing, listSeparator),
			strings.Join(communityHealthFiles(), listSeparator),
			"the org .github repository is missing: "+strings.Join(
				missing,
				listSeparator,
			)+" (looked in the root, .github/, and docs/)",
			nil,
		)

		return
	}

	a.flag(checkOrgCommunityHealthSet, StatusOK, "", "",
		"the org .github repository carries the canonical community-health set", nil)
}

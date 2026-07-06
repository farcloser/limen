package github

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// Static errors (err113: no dynamic error comparisons).
var (
	errEndpointNotFound  = errors.New("endpoint not found")
	errNoRepo            = errors.New("no repository given and none could be inferred from git")
	errVisibilityUnknown = errors.New(
		"repository visibility could not be determined (the repository object was unreadable)",
	)
	errFieldNotVisible = errors.New(
		"the field is absent from the response (owner-scoped — the token cannot see it)",
	)
)

// Check identifiers — one per audited setting, spelled out (the naming
// doctrine: explicit names readable without a syllabus). They are also the
// keys the override file exempts.
const (
	checkSecretScanning       = "secret-scanning"
	checkPushProtection       = "secret-scanning-push-protection"
	checkDependabotAlerts     = "dependabot-alerts"
	checkDependabotFixes      = "dependabot-security-updates"
	checkPrivateVulnReporting = "private-vulnerability-reporting"
	checkActionsWorkflowPerms = "actions-workflow-permissions"
	checkActionsApprovePRs    = "actions-approve-pull-requests"
	checkActionsAllowed       = "actions-allowed"
	checkMergeMethods         = "merge-methods"
	checkSquashDefaults       = "squash-commit-defaults"
	checkDeleteBranchOnMerge  = "delete-branch-on-merge"
	checkAutoMerge            = "auto-merge"
	checkUpdateBranch         = "update-branch-suggestions"
	checkDefaultBranch        = "default-branch"
	checkWebCommitSignoff     = "web-commit-signoff"
	checkDescription          = "description"
	checkTopics               = "topics"
	checkWiki                 = "wiki"
	checkProjects             = "projects"
	checkDiscussions          = "discussions"
	checkForking              = "forking"
	checkPages                = "pages"
	checkForkPRApproval       = "actions-fork-pr-approval"
	checkActionsAccessLevel   = "actions-access-level"
	checkOutsideCollaborators = "outside-collaborators"
	checkCodeScanning         = "code-scanning"
	checkRulesetDefaultBranch = "ruleset-default-branch"
	checkRulesetVersionTags   = "ruleset-version-tags"
	checkWebhooks             = "webhooks"
	checkDeployKeys           = "deploy-keys"
)

// knownChecks is every check identifier — repository and organization level
// both — for override-file validation.
func knownChecks() map[string]bool {
	checks := knownOrgChecks()
	maps.Copy(checks, map[string]bool{
		checkSecretScanning:       true,
		checkPushProtection:       true,
		checkDependabotAlerts:     true,
		checkDependabotFixes:      true,
		checkPrivateVulnReporting: true,
		checkActionsWorkflowPerms: true,
		checkActionsApprovePRs:    true,
		checkActionsAllowed:       true,
		checkMergeMethods:         true,
		checkSquashDefaults:       true,
		checkDeleteBranchOnMerge:  true,
		checkAutoMerge:            true,
		checkUpdateBranch:         true,
		checkDefaultBranch:        true,
		checkWebCommitSignoff:     true,
		checkDescription:          true,
		checkTopics:               true,
		checkWiki:                 true,
		checkProjects:             true,
		checkDiscussions:          true,
		checkForking:              true,
		checkPages:                true,
		checkForkPRApproval:       true,
		checkActionsAccessLevel:   true,
		checkOutsideCollaborators: true,
		checkCodeScanning:         true,
		checkRulesetDefaultBranch: true,
		checkRulesetVersionTags:   true,
		checkWebhooks:             true,
		checkDeployKeys:           true,
	})

	return checks
}

// Baseline values (the floor).
const (
	baselineDefaultBranch = "main"
	baselineWorkflowPerms = "read"
	baselineSquashTitle   = "PR_TITLE"
	baselineSquashMessage = "PR_BODY"
	enabledValue          = "enabled"
	disabledValue         = "disabled"
	rulesetMainName       = "limen:main"
	rulesetTagsName       = "limen:tags"
	enforcementActive     = "active"
	allowedActionsAll     = "all"
	approvalWeakest       = "first_time_contributors_new_to_github"
	approvalBaseline      = "first_time_contributors"
	accessLevelNone       = "none"
	scanStateConfigured   = "configured"
	decimalBase           = 10
	jsonTypeKey           = "type"
	ruleRequiredChecks    = "required_status_checks"
	listSeparator         = ", "
	repositoryAdminRoleID = 5 // GitHub's fixed id for the repository "admin" role, used in ruleset bypass lists.
)

// repoSettings is the subset of the repository object the audit reads.
type repoSettings struct {
	SecurityAndAnalysis      *securityAndAnalysis `json:"security_and_analysis"`
	DefaultBranch            string               `json:"default_branch"`
	Description              string               `json:"description"`
	SquashMergeCommitTitle   string               `json:"squash_merge_commit_title"`
	SquashMergeCommitMessage string               `json:"squash_merge_commit_message"`
	Topics                   []string             `json:"topics"`
	Private                  bool                 `json:"private"`
	HasWiki                  bool                 `json:"has_wiki"`
	HasProjects              bool                 `json:"has_projects"`
	HasDiscussions           bool                 `json:"has_discussions"`
	AllowMergeCommit         bool                 `json:"allow_merge_commit"`
	AllowSquashMerge         bool                 `json:"allow_squash_merge"`
	AllowRebaseMerge         bool                 `json:"allow_rebase_merge"`
	AllowAutoMerge           bool                 `json:"allow_auto_merge"`
	AllowUpdateBranch        bool                 `json:"allow_update_branch"`
	AllowForking             bool                 `json:"allow_forking"`
	DeleteBranchOnMerge      bool                 `json:"delete_branch_on_merge"`
	WebCommitSignoffRequired bool                 `json:"web_commit_signoff_required"`
}

type securityAndAnalysis struct {
	SecretScanning               *featureStatus `json:"secret_scanning"`
	SecretScanningPushProtection *featureStatus `json:"secret_scanning_push_protection"`
}

type featureStatus struct {
	Status string `json:"status"`
}

func (s *featureStatus) enabled() bool { return s != nil && s.Status == enabledValue }

// optInChecks are baseline-optional: their presence in the override file is a
// project opting INTO the requirement (a stricter floor), never an exemption —
// flag() must not convert their failures into exempted-ok.
func optInChecks() map[string]bool {
	return map[string]bool{checkCodeScanning: true}
}

// auditor accumulates findings and the planned changes that would repair the
// failing ones.
type auditor struct {
	client        client
	overrides     map[string]string
	settingsPatch map[string]any
	findings      []Finding
	changes       []Change
	// private/privateKnown carry the repository's visibility from
	// auditRepoObject to the checks that only apply to private repositories.
	private      bool
	privateKnown bool
}

// Audit checks the repository's settings against the baseline. overrides maps
// exempted check identifiers to their reasons (see LoadOverrides). It returns
// the findings and the changes a fix run would apply.
func Audit(repo string, overrides map[string]string) ([]Finding, []Change) {
	aud := &auditor{
		client:        repoClient(repo),
		overrides:     overrides,
		settingsPatch: map[string]any{},
	}

	aud.auditRepoObject()
	aud.auditSecurityToggles()
	aud.auditActions()
	aud.auditCodeScanning()
	aud.auditRulesets()
	aud.auditSurface()
	aud.flushSettingsPatch()

	return aud.findings, aud.changes
}

// flag records one verdict. A fail or advisory whose check is exempted by the
// override file becomes ok (with the reason in the message) and plans no
// change; otherwise a failing check with a non-nil fix plans it.
func (a *auditor) flag(check string, status Status, current, desired, message string, fix *Change) {
	if (status == StatusFail || status == StatusAdvisory) && !optInChecks()[check] {
		if reason, exempted := a.overrides[check]; exempted {
			a.findings = append(a.findings, Finding{
				Check:   check,
				Status:  StatusOK,
				Current: current,
				Message: "exempted by .github/limen-github.yaml: " + reason,
			})

			return
		}
	}

	a.findings = append(a.findings, Finding{
		Check:   check,
		Status:  status,
		Current: current,
		Desired: desired,
		Message: message,
	})

	if status == StatusFail && fix != nil {
		// The change applies against whatever this audit targets — capture it
		// here, the single point every planned change passes through.
		fix.client = a.client
		a.changes = append(a.changes, *fix)
	}
}

// unverifiable records every given check as unanswerable under this token.
func (a *auditor) unverifiable(err error, checks ...string) {
	for _, check := range checks {
		a.findings = append(a.findings, Finding{
			Check:   check,
			Status:  StatusUnverifiable,
			Message: "cannot verify: " + err.Error(),
		})
	}
}

// patchSettings stages one field of the consolidated PATCH against the
// audited target object (the repository or the organization) and returns the
// change entry describing it.
func (a *auditor) patchSettings(check, summary string, fields map[string]any) *Change {
	maps.Copy(a.settingsPatch, fields)

	// The apply is a no-op: flushSettingsPatch emits one consolidated change for
	// every staged field, so per-field entries only carry the summary.
	return &Change{Check: check, Summary: summary, apply: nil}
}

// flushSettingsPatch replaces the per-field no-op applies with one consolidated
// PATCH covering every staged repository field.
func (a *auditor) flushSettingsPatch() {
	if len(a.settingsPatch) == 0 {
		return
	}

	payload := a.settingsPatch
	for i := range a.changes {
		if a.changes[i].apply == nil {
			a.changes[i].apply = func(apiClient client) error {
				if payload == nil {
					return nil
				}
				// The first consolidated change applies every staged field;
				// the rest become no-ops.
				fields := payload
				payload = nil

				return apiClient.writeJSON("PATCH", "", fields)
			}
		}
	}
}

const boolTrue, boolFalse = "true", "false"

// auditRepoObject covers every check answered by GET /repos/{owner}/{repo}:
// merge and branch workflow (R3), features and metadata (R5), and the
// security_and_analysis block of R1.
func (a *auditor) auditRepoObject() { //nolint:funlen,gocognit // a linear catalog of independent checks, one block each.
	var settings repoSettings

	outcome := a.client.getJSON("", &settings)
	if outcome.err != nil || outcome.notFound {
		err := outcome.err
		if err == nil {
			err = errEndpointNotFound
		}

		a.unverifiable(err,
			checkMergeMethods, checkSquashDefaults, checkDeleteBranchOnMerge, checkAutoMerge,
			checkUpdateBranch, checkDefaultBranch, checkWebCommitSignoff, checkDescription,
			checkTopics, checkWiki, checkProjects, checkDiscussions, checkForking,
			checkSecretScanning, checkPushProtection)

		return
	}

	a.private = settings.Private
	a.privateKnown = true

	// R3 — merge & branch workflow. The doctrine: rebase + squash, merge
	// commits disallowed (linear history), squash messages from the PR.
	if settings.AllowMergeCommit || (!settings.AllowSquashMerge && !settings.AllowRebaseMerge) {
		current := fmt.Sprintf("merge=%s squash=%s rebase=%s",
			strconv.FormatBool(settings.AllowMergeCommit), strconv.FormatBool(settings.AllowSquashMerge),
			strconv.FormatBool(settings.AllowRebaseMerge))
		a.flag(checkMergeMethods, StatusFail, current, "merge=false squash=true rebase=true",
			"merge commits must be disallowed (linear history); squash and rebase allowed",
			a.patchSettings(checkMergeMethods, "merge methods: "+current+" → merge=false squash=true rebase=true",
				map[string]any{
					"allow_merge_commit": false,
					"allow_squash_merge": true,
					"allow_rebase_merge": true,
				}))
	} else {
		a.flag(checkMergeMethods, StatusOK, "", "", "merge commits disallowed; linear history", nil)
	}

	if settings.SquashMergeCommitTitle != baselineSquashTitle ||
		settings.SquashMergeCommitMessage != baselineSquashMessage {
		current := settings.SquashMergeCommitTitle + "/" + settings.SquashMergeCommitMessage
		a.flag(checkSquashDefaults, StatusFail, current, baselineSquashTitle+"/"+baselineSquashMessage,
			"squash commits must default to the pull request title and body",
			a.patchSettings(checkSquashDefaults, "squash commit defaults: "+current+" → PR title/body",
				map[string]any{
					"squash_merge_commit_title":   baselineSquashTitle,
					"squash_merge_commit_message": baselineSquashMessage,
				}))
	} else {
		a.flag(checkSquashDefaults, StatusOK, "", "", "squash commits default to the pull request title and body", nil)
	}

	a.flagToggle(toggle{
		check:       checkDeleteBranchOnMerge,
		compliant:   settings.DeleteBranchOnMerge,
		failMessage: "merged branches must be deleted automatically",
		okMessage:   "merged branches are deleted automatically",
		fields:      map[string]any{"delete_branch_on_merge": true},
	})

	a.flagToggle(toggle{
		check:       checkAutoMerge,
		compliant:   settings.AllowAutoMerge,
		failMessage: "auto-merge must be allowed (Renovate merges green PRs)",
		okMessage:   "auto-merge is allowed",
		fields:      map[string]any{"allow_auto_merge": true},
	})

	a.flagToggle(toggle{
		check:       checkUpdateBranch,
		compliant:   settings.AllowUpdateBranch,
		failMessage: "update-branch suggestions must be enabled",
		okMessage:   "update-branch suggestions are enabled",
		fields:      map[string]any{"allow_update_branch": true},
	})

	if settings.DefaultBranch != baselineDefaultBranch {
		a.flag(checkDefaultBranch, StatusAdvisory, settings.DefaultBranch, baselineDefaultBranch,
			"the default branch is not "+baselineDefaultBranch+" — renaming is disruptive, do it deliberately", nil)
	} else {
		a.flag(checkDefaultBranch, StatusOK, "", "", "default branch is "+baselineDefaultBranch, nil)
	}

	a.flagToggle(toggle{
		check:       checkWebCommitSignoff,
		compliant:   settings.WebCommitSignoffRequired,
		failMessage: "web commits must require sign-off (DCO holds for UI edits too)",
		okMessage:   "web commits require sign-off",
		fields:      map[string]any{"web_commit_signoff_required": true},
	})

	// R5 — features & metadata.
	if strings.TrimSpace(settings.Description) == "" {
		a.flag(checkDescription, StatusAdvisory, "(empty)", "non-empty",
			"the repository has no description — write one (content is human work)", nil)
	} else {
		a.flag(checkDescription, StatusOK, "", "", "description present", nil)
	}

	if !settings.Private && len(settings.Topics) == 0 {
		a.flag(checkTopics, StatusAdvisory, "(none)", "at least one",
			"public repositories should carry topics for discoverability", nil)
	} else {
		a.flag(checkTopics, StatusOK, "", "", "topics present (or repository is private)", nil)
	}

	a.flagToggle(toggle{
		check:       checkWiki,
		compliant:   !settings.HasWiki,
		failMessage: "the wiki must be off (documentation lives in the repository)",
		okMessage:   "wiki is off",
		fields:      map[string]any{"has_wiki": false},
	})

	a.flagToggle(toggle{
		check:       checkProjects,
		compliant:   !settings.HasProjects,
		failMessage: "projects must be off unless deliberately used",
		okMessage:   "projects are off",
		fields:      map[string]any{"has_projects": false},
	})

	a.flagToggle(toggle{
		check:       checkDiscussions,
		compliant:   !settings.HasDiscussions,
		failMessage: "discussions must be off (issues are the tracker)",
		okMessage:   "discussions are off",
		fields:      map[string]any{"has_discussions": false},
	})

	if settings.Private && settings.AllowForking {
		a.flag(checkForking, StatusFail, boolTrue, boolFalse,
			"private repositories must not allow forking",
			a.patchSettings(checkForking, "forking: allowed → disallowed",
				map[string]any{"allow_forking": false}))
	} else {
		a.flag(checkForking, StatusOK, "", "", "forking policy compliant", nil)
	}

	// R1 — the security_and_analysis block (nil for tokens without the
	// necessary read access, which must not read as compliance).
	a.auditSecretScanning(settings)
}

// flagToggle records the common boolean shape: a compliant toggle reports
// okMessage; a non-compliant one fails with failMessage and stages the
// repository PATCH fields. A struct rather than a boolean parameter: revive's
// flag-parameter rule proved environment-nondeterministic under the per-GOOS
// lint legs, so no suppressible boolean control parameter may exist here.
type toggle struct {
	fields      map[string]any
	check       string
	failMessage string
	okMessage   string
	compliant   bool
}

func (a *auditor) flagToggle(item toggle) {
	if item.compliant {
		a.flag(item.check, StatusOK, "", "", item.okMessage, nil)

		return
	}

	a.flag(item.check, StatusFail, boolFalse, boolTrue, item.failMessage,
		a.patchSettings(item.check, item.check+": → compliant", item.fields))
}

func (a *auditor) auditSecretScanning(settings repoSettings) {
	analysis := settings.SecurityAndAnalysis
	if analysis == nil {
		a.unverifiable(errEndpointNotFound, checkSecretScanning, checkPushProtection)

		return
	}

	if analysis.SecretScanning.enabled() {
		a.flag(checkSecretScanning, StatusOK, "", "", "secret scanning is enabled", nil)
	} else {
		a.flag(checkSecretScanning, StatusFail, disabledValue, enabledValue,
			"secret scanning must be enabled",
			a.patchSettings(checkSecretScanning, "secret scanning: disabled → enabled",
				map[string]any{"security_and_analysis": map[string]any{
					"secret_scanning": map[string]any{"status": enabledValue},
				}}))
	}

	if analysis.SecretScanningPushProtection.enabled() {
		a.flag(checkPushProtection, StatusOK, "", "", "secret scanning push protection is enabled", nil)
	} else {
		a.flag(checkPushProtection, StatusFail, disabledValue, enabledValue,
			"secret scanning push protection must be enabled",
			&Change{
				Check:   checkPushProtection,
				Summary: "secret scanning push protection: disabled → enabled",
				apply: func(apiClient client) error {
					return apiClient.writeJSON("PATCH", "", map[string]any{
						"security_and_analysis": map[string]any{
							"secret_scanning":                 map[string]any{"status": enabledValue},
							"secret_scanning_push_protection": map[string]any{"status": enabledValue},
						},
					})
				},
			})
	}
}

// auditSecurityToggles covers the R1 endpoints that answer through dedicated
// URLs: Dependabot alerts (status-code endpoint), Dependabot security updates,
// and private vulnerability reporting (public repositories only).
func (a *auditor) auditSecurityToggles() {
	// Dependabot alerts: 204 = enabled, 404 = disabled.
	alertsOutcome := a.client.api("GET", "/vulnerability-alerts", nil)

	switch {
	case alertsOutcome.err != nil:
		a.unverifiable(alertsOutcome.err, checkDependabotAlerts)
	case alertsOutcome.notFound:
		a.flag(checkDependabotAlerts, StatusFail, disabledValue, enabledValue,
			"Dependabot alerts must be enabled",
			&Change{
				Check:   checkDependabotAlerts,
				Summary: "dependabot alerts: disabled → enabled",
				apply:   func(c client) error { return c.writeJSON("PUT", "/vulnerability-alerts", nil) },
			})
	default:
		a.flag(checkDependabotAlerts, StatusOK, "", "", "Dependabot alerts are enabled", nil)
	}

	var fixes struct {
		Enabled bool `json:"enabled"`
	}

	fixesOutcome := a.client.getJSON("/automated-security-fixes", &fixes)

	switch {
	case fixesOutcome.err != nil || fixesOutcome.notFound:
		a.unverifiable(orNotFound(fixesOutcome), checkDependabotFixes)
	case fixes.Enabled:
		a.flag(checkDependabotFixes, StatusOK, "", "", "Dependabot security updates are enabled", nil)
	default:
		a.flag(checkDependabotFixes, StatusFail, disabledValue, enabledValue,
			"Dependabot security updates must be enabled",
			&Change{
				Check:   checkDependabotFixes,
				Summary: "dependabot security updates: disabled → enabled",
				apply:   func(c client) error { return c.writeJSON("PUT", "/automated-security-fixes", nil) },
			})
	}

	a.auditPrivateVulnerabilityReporting()
}

func (a *auditor) auditPrivateVulnerabilityReporting() {
	var reporting struct {
		Enabled bool `json:"enabled"`
	}

	outcome := a.client.getJSON("/private-vulnerability-reporting", &reporting)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkPrivateVulnReporting)
	case reporting.Enabled:
		a.flag(checkPrivateVulnReporting, StatusOK, "", "", "private vulnerability reporting is enabled", nil)
	default:
		a.flag(checkPrivateVulnReporting, StatusFail, disabledValue, enabledValue,
			"private vulnerability reporting must be enabled",
			&Change{
				Check:   checkPrivateVulnReporting,
				Summary: "private vulnerability reporting: disabled → enabled",
				apply:   func(c client) error { return c.writeJSON("PUT", "/private-vulnerability-reporting", nil) },
			})
	}
}

// auditActions covers R2: the workflow token defaults and the allowed-actions
// policy.
func (a *auditor) auditActions() {
	var workflow struct {
		DefaultWorkflowPermissions   string `json:"default_workflow_permissions"`
		CanApprovePullRequestReviews bool   `json:"can_approve_pull_request_reviews"`
	}

	workflowOutcome := a.client.getJSON("/actions/permissions/workflow", &workflow)
	if workflowOutcome.err != nil || workflowOutcome.notFound {
		a.unverifiable(orNotFound(workflowOutcome), checkActionsWorkflowPerms, checkActionsApprovePRs)
	} else {
		fixWorkflow := func(c client) error {
			return c.writeJSON("PUT", "/actions/permissions/workflow", map[string]any{
				"default_workflow_permissions":     baselineWorkflowPerms,
				"can_approve_pull_request_reviews": false,
			})
		}

		if workflow.DefaultWorkflowPermissions == baselineWorkflowPerms {
			a.flag(checkActionsWorkflowPerms, StatusOK, "", "", "workflow token defaults to read-only", nil)
		} else {
			a.flag(checkActionsWorkflowPerms, StatusFail,
				workflow.DefaultWorkflowPermissions, baselineWorkflowPerms,
				"the default workflow token must be read-only",
				&Change{
					Check:   checkActionsWorkflowPerms,
					Summary: "workflow token permissions: " + workflow.DefaultWorkflowPermissions + " → read",
					apply:   fixWorkflow,
				})
		}

		if !workflow.CanApprovePullRequestReviews {
			a.flag(checkActionsApprovePRs, StatusOK, "", "", "workflows cannot approve pull requests", nil)
		} else {
			a.flag(checkActionsApprovePRs, StatusFail, boolTrue, boolFalse,
				"workflows must not be able to approve pull requests",
				&Change{
					Check:   checkActionsApprovePRs,
					Summary: "workflows can approve pull requests: true → false",
					apply:   fixWorkflow,
				})
		}
	}

	var permissions struct {
		Enabled        bool   `json:"enabled"`
		AllowedActions string `json:"allowed_actions"`
	}

	permissionsOutcome := a.client.getJSON("/actions/permissions", &permissions)

	switch {
	case permissionsOutcome.err != nil || permissionsOutcome.notFound:
		a.unverifiable(orNotFound(permissionsOutcome), checkActionsAllowed)
	case permissions.Enabled && permissions.AllowedActions == allowedActionsAll:
		a.flag(checkActionsAllowed, StatusFail, allowedActionsAll, "selected",
			"the allowed-actions policy must not be \"all\" — restrict to GitHub-owned plus a pinned allowlist",
			&Change{
				Check:   checkActionsAllowed,
				Summary: "allowed actions: all → selected (GitHub-owned only)",
				apply: func(apiClient client) error {
					if err := apiClient.writeJSON("PUT", "/actions/permissions", map[string]any{
						"enabled":         true,
						"allowed_actions": "selected",
					}); err != nil {
						return err
					}

					return apiClient.writeJSON("PUT", "/actions/permissions/selected-actions", map[string]any{
						"github_owned_allowed": true,
						"verified_allowed":     false,
						"patterns_allowed":     []string{},
					})
				},
			})
	default:
		a.flag(checkActionsAllowed, StatusOK, "", "",
			"allowed-actions policy is restricted (or Actions disabled entirely)", nil)
	}

	a.auditForkPRApproval()
	a.auditActionsAccess()
}

// auditForkPRApproval covers the fork-pull-request approval policy. The
// weakest setting — approval only for contributors new to GitHub — is below
// the floor: any account older than a few days bypasses it. Requiring
// approval for all first-time contributors (or all external contributors —
// stricter) passes.
func (a *auditor) auditForkPRApproval() {
	var approval struct {
		ApprovalPolicy string `json:"approval_policy"`
	}

	outcome := a.client.getJSON("/actions/permissions/fork-pr-contributor-approval", &approval)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkForkPRApproval)
	case approval.ApprovalPolicy == approvalWeakest:
		a.flag(checkForkPRApproval, StatusFail, approvalWeakest, approvalBaseline+" (or stricter)",
			"fork pull requests must require approval for all first-time contributors, not only accounts new to GitHub",
			&Change{
				Check:   checkForkPRApproval,
				Summary: "fork PR approval: " + approvalWeakest + " → " + approvalBaseline,
				apply: func(apiClient client) error {
					return apiClient.writeJSON("PUT", "/actions/permissions/fork-pr-contributor-approval",
						map[string]any{"approval_policy": approvalBaseline})
				},
			})
	default:
		a.flag(checkForkPRApproval, StatusOK, "", "",
			"fork pull requests require contributor approval ("+approval.ApprovalPolicy+")", nil)
	}
}

// auditActionsAccess covers which outside repositories may use this one's
// actions and reusable workflows — a private-repository concept (the API
// only applies there; public repositories pass as not-applicable). The floor
// is "none".
func (a *auditor) auditActionsAccess() {
	if !a.privateKnown {
		a.unverifiable(errVisibilityUnknown, checkActionsAccessLevel)

		return
	}

	if !a.private {
		a.flag(checkActionsAccessLevel, StatusOK, "", "", "not applicable (public repository)", nil)

		return
	}

	var access struct {
		AccessLevel string `json:"access_level"`
	}

	outcome := a.client.getJSON("/actions/permissions/access", &access)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkActionsAccessLevel)
	case access.AccessLevel == accessLevelNone:
		a.flag(checkActionsAccessLevel, StatusOK, "", "",
			"no outside repository can use this repository's workflows", nil)
	default:
		a.flag(checkActionsAccessLevel, StatusFail, access.AccessLevel, accessLevelNone,
			"outside repositories must not have access to this repository's actions and workflows",
			&Change{
				Check:   checkActionsAccessLevel,
				Summary: "actions access level: " + access.AccessLevel + " → none",
				apply: func(apiClient client) error {
					return apiClient.writeJSON("PUT", "/actions/permissions/access",
						map[string]any{"access_level": accessLevelNone})
				},
			})
	}
}

// auditCodeScanning is opt-in, per the decided baseline (the SAST posture is
// golangci's gosec/staticcheck plus govulncheck, per GOOS): listing
// "code-scanning" in the override file REQUIRES CodeQL default setup rather
// than exempting anything (see optInChecks). Repositories that have not
// opted in are not even queried.
func (a *auditor) auditCodeScanning() {
	if _, optedIn := a.overrides[checkCodeScanning]; !optedIn {
		a.flag(checkCodeScanning, StatusOK, "", "",
			"not required by the baseline (opt in via "+OverridePath+")", nil)

		return
	}

	var setup struct {
		State string `json:"state"`
	}

	outcome := a.client.getJSON("/code-scanning/default-setup", &setup)

	switch {
	case outcome.err != nil || outcome.notFound:
		a.unverifiable(orNotFound(outcome), checkCodeScanning)
	case setup.State == scanStateConfigured:
		a.flag(checkCodeScanning, StatusOK, "", "", "code scanning default setup is configured (opted in)", nil)
	default:
		a.flag(checkCodeScanning, StatusFail, setup.State, scanStateConfigured,
			"this repository opted into code scanning, but default setup is not configured",
			&Change{
				Check:   checkCodeScanning,
				Summary: "code scanning default setup: " + setup.State + " → configured",
				apply: func(apiClient client) error {
					return apiClient.writeJSON("PATCH", "/code-scanning/default-setup",
						map[string]any{"state": scanStateConfigured})
				},
			})
	}
}

// orNotFound normalizes an apiOutcome into a reportable error.
func orNotFound(outcome apiOutcome) error {
	if outcome.err != nil {
		return outcome.err
	}

	return errEndpointNotFound
}

// rulesetSummary is one entry of GET /repos/{owner}/{repo}/rulesets.
type rulesetSummary struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	ID          int64  `json:"id"`
}

type rulesetDetail struct {
	Rules []rulesetRule `json:"rules"`
}

type rulesetRule struct {
	Parameters *rulesetRuleParameters `json:"parameters,omitempty"`
	Type       string                 `json:"type"`
}

type rulesetRuleParameters struct {
	RequiredStatusChecks []requiredStatusCheck `json:"required_status_checks"`
}

type requiredStatusCheck struct {
	Context string `json:"context"`
}

// statusCheckContexts returns the contexts of the required_status_checks
// rule, if the ruleset carries one.
func (d rulesetDetail) statusCheckContexts() []string {
	var contexts []string

	for _, rule := range d.Rules {
		if rule.Type != ruleRequiredChecks || rule.Parameters == nil {
			continue
		}

		for _, check := range rule.Parameters.RequiredStatusChecks {
			contexts = append(contexts, check.Context)
		}
	}

	return contexts
}

// auditRulesets covers R4: the canonical default-branch and version-tag
// rulesets, created if missing and reconciled if drifted.
func (a *auditor) auditRulesets() {
	var summaries []rulesetSummary

	outcome := a.client.getJSON("/rulesets", &summaries)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkRulesetDefaultBranch, checkRulesetVersionTags)

		return
	}

	byName := map[string]rulesetSummary{}
	for _, summary := range summaries {
		byName[summary.Name] = summary
	}

	a.auditRuleset(checkRulesetDefaultBranch, byName, rulesetMainName, canonicalMainRuleset,
		[]string{"pull_request", "deletion", "non_fast_forward", "required_linear_history", ruleRequiredChecks})
	a.auditRuleset(checkRulesetVersionTags, byName, rulesetTagsName,
		func([]string) map[string]any { return canonicalTagsRuleset() },
		[]string{"creation", "update", "deletion"})
}

func (a *auditor) auditRuleset(
	check string,
	byName map[string]rulesetSummary,
	name string,
	build func(existingContexts []string) map[string]any,
	requiredRules []string,
) {
	existing, present := byName[name]
	if !present {
		canonical := build(nil)

		a.flag(check, StatusFail, "(absent)", name,
			"the canonical ruleset "+name+" does not exist",
			&Change{
				Check:   check,
				Summary: "create ruleset " + name,
				apply:   func(c client) error { return c.writeJSON("POST", "/rulesets", canonical) },
			})

		return
	}

	rulesetPath := "/rulesets/" + strconv.FormatInt(existing.ID, decimalBase)

	var detail rulesetDetail

	detailOutcome := a.client.getJSON(rulesetPath, &detail)
	if detailOutcome.err != nil || detailOutcome.notFound {
		a.unverifiable(orNotFound(detailOutcome), check)

		return
	}

	// The status-check contexts are project-owned — their names follow the
	// project's CI shape — so a reconcile preserves them, exactly like the
	// standard-registry ref inside the otherwise-pinned aqua sections. An
	// empty set falls back to the canonical defaults inside the builder.
	payload := build(detail.statusCheckContexts())
	reconcile := &Change{
		Check:   check,
		Summary: "reconcile ruleset " + name + " to the canonical definition",
		apply:   func(c client) error { return c.writeJSON("PUT", rulesetPath, payload) },
	}

	if existing.Enforcement != enforcementActive {
		a.flag(check, StatusFail, existing.Enforcement, enforcementActive,
			"ruleset "+name+" exists but is not active", reconcile)

		return
	}

	have := map[string]bool{}
	for _, rule := range detail.Rules {
		have[rule.Type] = true
	}

	var missing []string

	for _, required := range requiredRules {
		if !have[required] {
			missing = append(missing, required)
		}
	}

	if len(missing) > 0 {
		a.flag(check, StatusFail, "missing rule(s): "+strings.Join(missing, listSeparator), "all required rules",
			"ruleset "+name+" is missing required rules", reconcile)

		return
	}

	if have[ruleRequiredChecks] && len(detail.statusCheckContexts()) == 0 {
		a.flag(check, StatusFail, "required status checks name no contexts", "at least one required check",
			"ruleset "+name+" requires status checks but names none — merges would not wait for CI", reconcile)

		return
	}

	a.flag(check, StatusOK, "", "", "ruleset "+name+" is active with the required rules", nil)
}

// ruleOf renders a parameterless ruleset rule of the given kind.
func ruleOf(kind string) map[string]any {
	return map[string]any{jsonTypeKey: kind}
}

// defaultRequiredChecks mirrors the job matrix of the canonical ci.yaml (the
// "verify" job across its runner matrix — those are the check-run names
// GitHub produces). Changing the matrix there means changing this list too,
// in the same release; the two are cross-linked by comments.
func defaultRequiredChecks() []string {
	return []string{
		"verify (ubuntu-24.04)",
		"verify (ubuntu-24.04-arm)",
		"verify (macos-15)",
		"verify (windows-2025)",
	}
}

// canonicalMainRuleset is the default-branch protection: pull requests always
// (0 required approvals — the PR is the audit trail and the CI gate), linear
// history, no force pushes, no deletion, and required status checks so a
// merge (auto-merge included — Renovate depends on this) waits for green CI.
// existingContexts preserves a project's own check names on reconcile; empty
// falls back to the canonical ci.yaml matrix.
func canonicalMainRuleset(existingContexts []string) map[string]any {
	contexts := existingContexts
	if len(contexts) == 0 {
		contexts = defaultRequiredChecks()
	}

	checkEntries := make([]map[string]any, 0, len(contexts))
	for _, context := range contexts {
		checkEntries = append(checkEntries, map[string]any{"context": context})
	}

	return map[string]any{
		"name":        rulesetMainName,
		"target":      "branch",
		"enforcement": enforcementActive,
		"conditions": map[string]any{
			"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}, "exclude": []string{}},
		},
		"rules": []map[string]any{
			ruleOf("deletion"),
			ruleOf("non_fast_forward"),
			ruleOf("required_linear_history"),
			{jsonTypeKey: "pull_request", "parameters": map[string]any{
				"required_approving_review_count":   0,
				"dismiss_stale_reviews_on_push":     false,
				"require_code_owner_review":         false,
				"require_last_push_approval":        false,
				"required_review_thread_resolution": false,
				"allowed_merge_methods":             []string{"squash", "rebase"},
			}},
			{jsonTypeKey: ruleRequiredChecks, "parameters": map[string]any{
				// Not strict (branches need not be up to date before merge):
				// linear history plus update-branch suggestions cover it
				// without serializing every merge behind a rebase.
				"strict_required_status_checks_policy": false,
				"required_status_checks":               checkEntries,
			}},
		},
	}
}

// canonicalTagsRuleset restricts v* tag creation/update/deletion to repository
// admins — the tag push is the release button, and this names who may press it.
func canonicalTagsRuleset() map[string]any {
	return map[string]any{
		"name":        rulesetTagsName,
		"target":      "tag",
		"enforcement": enforcementActive,
		"conditions": map[string]any{
			"ref_name": map[string]any{"include": []string{"refs/tags/v*"}, "exclude": []string{}},
		},
		"bypass_actors": []map[string]any{
			{"actor_type": "RepositoryRole", "actor_id": repositoryAdminRoleID, "bypass_mode": "always"},
		},
		"rules": []map[string]any{
			ruleOf("creation"),
			ruleOf("update"),
			ruleOf("deletion"),
		},
	}
}

// webhook and deployKey are the credential-surface objects of R6.
type webhook struct {
	Config webhookConfig `json:"config"`
	ID     int64         `json:"id"`
}

type webhookConfig struct {
	URL         string `json:"url"`
	Secret      string `json:"secret"`
	InsecureSSL any    `json:"insecure_ssl"`
}

type deployKey struct {
	Title    string `json:"title"`
	ID       int64  `json:"id"`
	ReadOnly bool   `json:"read_only"`
}

// outsideCollaborator is one entry of the collaborators listing (outside
// affiliation): who they are and what they can do.
type outsideCollaborator struct {
	Login       string                  `json:"login"`
	RoleName    string                  `json:"role_name"`
	Permissions collaboratorPermissions `json:"permissions"`
}

type collaboratorPermissions struct {
	Push     bool `json:"push"`
	Maintain bool `json:"maintain"`
	Admin    bool `json:"admin"`
}

// auditOutsideCollaborators inventories outside collaborators holding
// elevated access — only organization members should (Allstar's check).
// People are never auto-fixed: always advisory. One page of 100, stated in
// the finding when hit — an org with more outside collaborators than that
// has outgrown this audit.
func (a *auditor) auditOutsideCollaborators() {
	var collaborators []outsideCollaborator

	const pageSize = 100

	outcome := a.client.getJSON("/collaborators?affiliation=outside&per_page=100", &collaborators)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOutsideCollaborators)

		return
	}

	var elevated []string

	for _, person := range collaborators {
		if person.Permissions.Admin || person.Permissions.Maintain || person.Permissions.Push {
			elevated = append(elevated, person.Login+" ("+person.RoleName+")")
		}
	}

	if len(elevated) == 0 {
		a.flag(checkOutsideCollaborators, StatusOK, "", "", "no outside collaborators with elevated access", nil)

		return
	}

	message := "outside collaborator(s) with elevated access — people are never auto-fixed, review by hand"
	if len(collaborators) == pageSize {
		message += " (first " + strconv.Itoa(pageSize) + " inspected)"
	}

	a.flag(checkOutsideCollaborators, StatusAdvisory,
		strings.Join(elevated, listSeparator), "read-only or organization members", message, nil)
}

// auditSurface covers the advisory-only credential surface of R6 (webhooks,
// deploy keys) and the pages check of R5. All of it is inventory: nothing
// here is ever auto-fixed.
func (a *auditor) auditSurface() {
	var hooks []webhook

	hooksOutcome := a.client.getJSON("/hooks", &hooks)

	switch {
	case hooksOutcome.err != nil || hooksOutcome.notFound:
		a.unverifiable(orNotFound(hooksOutcome), checkWebhooks)
	default:
		var offenders []string

		for _, hook := range hooks {
			insecure := fmt.Sprintf("%v", hook.Config.InsecureSSL) != "0"
			if !strings.HasPrefix(hook.Config.URL, "https://") || hook.Config.Secret == "" || insecure {
				offenders = append(offenders, hook.Config.URL)
			}
		}

		if len(offenders) > 0 {
			a.flag(
				checkWebhooks,
				StatusAdvisory,
				strings.Join(offenders, listSeparator),
				"https + secret + TLS verification",
				"webhook(s) without HTTPS, a secret, or TLS verification — review and fix by hand",
				nil,
			)
		} else {
			a.flag(checkWebhooks, StatusOK, "", "", "webhooks are compliant (or none exist)", nil)
		}
	}

	var keys []deployKey

	keysOutcome := a.client.getJSON("/keys", &keys)

	switch {
	case keysOutcome.err != nil || keysOutcome.notFound:
		a.unverifiable(orNotFound(keysOutcome), checkDeployKeys)
	case len(keys) == 0:
		a.flag(checkDeployKeys, StatusOK, "", "", "no deploy keys", nil)
	default:
		var titles []string
		for _, key := range keys {
			titles = append(titles, fmt.Sprintf("%s (read_only=%s)", key.Title, strconv.FormatBool(key.ReadOnly)))
		}

		a.flag(
			checkDeployKeys,
			StatusAdvisory,
			strings.Join(titles, listSeparator),
			"(none, or named in the override file)",
			"deploy key(s) present — credentials are never auto-fixed, review by hand",
			nil,
		)
	}

	pagesOutcome := a.client.api("GET", "/pages", nil)

	switch {
	case pagesOutcome.notFound:
		a.flag(checkPages, StatusOK, "", "", "GitHub Pages is off", nil)
	case pagesOutcome.err != nil:
		a.unverifiable(pagesOutcome.err, checkPages)
	default:
		a.flag(checkPages, StatusAdvisory, enabledValue, "off unless deliberate",
			"GitHub Pages is enabled — confirm it is deliberate (exempt it in the override file if so)", nil)
	}

	a.auditOutsideCollaborators()
}

// agents.go — the agents team: the one team grant the baseline asserts.
//
// Coding agents contribute through a dedicated account that is an
// organization member and holds write access through the organization's
// `agents` team (book/agents.md). A repository created without that grant is
// one the agents can read but not push to — which is what every repository
// created by hand looked like until this check existed: the bot's work
// failed at push time, on a 403 nobody had arranged.
//
// The grant is baseline apparatus, not a person: the audit asserts it and fix
// applies it — the one narrow exception to "team grants are never auto-fixed"
// (design/LIMEN-GITHUB.md), and only ever additive: write for the canonical
// team, never a removal, never a human. The team itself and its members stay
// human work: a missing team is a failing finding with the commands printed,
// never a planned change.

package github

import (
	"errors"
	"slices"
	"strings"
)

const (
	// agentsTeamSlug is the canonical team through which coding agents hold
	// write access on every governed repository.
	agentsTeamSlug = "agents"
	// teamPermissionPush is GitHub's API name for the "Write" team role.
	teamPermissionPush = "push"
	// teamsListPath lists an organization's teams, or a repository's team
	// grants; teamPath is the prefix of one team's resources.
	teamsListPath = "/teams?per_page=100"
	teamPath      = "/teams/"
)

var (
	errTeamGrantsScope = errors.New(
		"reading a repository's team grants needs admin on it (GitHub answers 404 without)",
	)
	errOrgTeamsScope = errors.New(
		"reading the organization's teams needs an org-scoped token (GitHub answers 404 without)",
	)
)

// teamSummary is the subset of a team object the audit reads.
type teamSummary struct {
	Slug string `json:"slug"`
}

// repoTeamGrant is one entry of GET /repos/{o}/{r}/teams: a team and the
// permission it holds on the repository.
type repoTeamGrant struct {
	Slug       string `json:"slug"`
	Permission string `json:"permission"`
}

// teamRepoGrant is one entry of GET /orgs/{o}/teams/{t}/repos.
type teamRepoGrant struct {
	Name        string                  `json:"name"`
	Permissions collaboratorPermissions `json:"permissions"`
}

// orgRepo is the subset of GET /orgs/{o}/repos the audit reads.
type orgRepo struct {
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

// teamMember is one entry of GET /orgs/{o}/teams/{t}/members.
type teamMember struct {
	Login string `json:"login"`
}

// grantsWrite reports whether a team-repository permission name includes
// push: GitHub's roles nest, so maintain and admin carry write as well.
func grantsWrite(permission string) bool {
	switch permission {
	case teamPermissionPush, "maintain", "admin":
		return true
	default:
		return false
	}
}

// teamGrantPath is the team-repository grant resource under an organization.
func teamGrantPath(owner, name string) string {
	return teamPath + agentsTeamSlug + "/repos/" + owner + "/" + name
}

// grantWriteCommand is the exact gh invocation that grants the agents team
// write on one repository — printed on the finding, and what fix runs.
func grantWriteCommand(owner, name string) string {
	return "gh api -X PUT orgs/" + owner + teamGrantPath(owner, name) + " -f permission=" + teamPermissionPush
}

// teamMissingMessage explains a missing agents team and how to create it.
// Teams and their members are people management, never auto-fixed.
func teamMissingMessage(owner string) string {
	return "the organization has no " + agentsTeamSlug + " team, so no repository can grant coding agents" +
		" write access through it: create it (gh api -X POST orgs/" + owner + "/teams -f name=" +
		agentsTeamSlug + " -f privacy=closed) and add the agents' account (gh api -X PUT orgs/" + owner +
		teamPath + agentsTeamSlug + "/memberships/<login>) — a team and its members are people," +
		" never auto-fixed"
}

// findAgentsTeam looks the canonical team up in an organization's team list.
// The list, not the team endpoint: both answer 404 to a token that cannot see
// teams at all, and only the list separates "no such team" from "cannot tell"
// — an unreadable list is unverifiable, a readable one without the team is a
// finding. found is meaningful only when outcome carries neither error nor
// notFound.
func findAgentsTeam(org client) (found bool, outcome apiOutcome) {
	var teams []teamSummary

	outcome = org.getJSONAllPages(teamsListPath, &teams)
	if outcome.err != nil || outcome.notFound {
		return false, outcome
	}

	for _, team := range teams {
		if team.Slug == agentsTeamSlug {
			return true, outcome
		}
	}

	return false, outcome
}

// auditAgentsTeam asserts the agents team holds write on the repository. An
// owner that is a user account has no teams: not applicable, ok.
func (a *auditor) auditAgentsTeam(owner, name string) {
	org := orgClient(owner)

	probe := org.api("GET", "", nil)

	switch {
	case probe.notFound:
		a.flag(checkAgentsTeam, StatusOK, "", "",
			"not applicable: the owner is a user account, which has no teams", nil)

		return
	case probe.err != nil:
		a.unverifiable(probe.err, checkAgentsTeam)

		return
	}

	found, outcome := findAgentsTeam(org)

	switch {
	case outcome.err != nil:
		a.unverifiable(outcome.err, checkAgentsTeam)

		return
	case outcome.notFound:
		a.unverifiable(errOrgTeamsScope, checkAgentsTeam)

		return
	case !found:
		a.flag(checkAgentsTeam, StatusFail, "(no such team)", agentsTeamSlug+" with "+teamPermissionPush,
			teamMissingMessage(owner), nil)

		return
	}

	var grants []repoTeamGrant

	outcome = a.client.getJSONAllPages(teamsListPath, &grants)

	switch {
	case outcome.err != nil:
		a.unverifiable(outcome.err, checkAgentsTeam)

		return
	case outcome.notFound:
		a.unverifiable(errTeamGrantsScope, checkAgentsTeam)

		return
	}

	current := "(no access)"

	for _, grant := range grants {
		if grant.Slug != agentsTeamSlug {
			continue
		}

		if grantsWrite(grant.Permission) {
			a.flag(checkAgentsTeam, StatusOK, grant.Permission, teamPermissionPush,
				"the "+agentsTeamSlug+" team has write access", nil)

			return
		}

		current = grant.Permission
	}

	a.flag(checkAgentsTeam, StatusFail, current, teamPermissionPush,
		"the "+agentsTeamSlug+" team cannot push: coding agents can read this repository but not contribute"+
			" to it ("+grantWriteCommand(owner, name)+")",
		&Change{
			Check:   checkAgentsTeam,
			Summary: agentsTeamSlug + " team: " + current + " → " + teamPermissionPush,
			apply: func(_ client) error {
				return org.writeJSON("PUT", teamGrantPath(owner, name),
					map[string]string{"permission": teamPermissionPush})
			},
		})
}

// auditOrgAgentsTeam asserts the agents team exists, has members, and holds
// write on every repository that is not archived — the organization-wide
// view of auditAgentsTeam, so one audit lists every repository created
// without the grant. Archived repositories are skipped: GitHub refuses
// permission changes on them, and nothing is contributed to them anyway.
func (a *auditor) auditOrgAgentsTeam(org string) {
	found, outcome := findAgentsTeam(a.client)

	switch {
	case outcome.err != nil:
		a.unverifiable(outcome.err, checkOrgAgentsTeam)

		return
	case outcome.notFound:
		a.unverifiable(errOrgTeamsScope, checkOrgAgentsTeam)

		return
	case !found:
		a.flag(checkOrgAgentsTeam, StatusFail, "(no such team)", agentsTeamSlug+" with "+teamPermissionPush,
			teamMissingMessage(org), nil)

		return
	}

	var members []teamMember

	outcome = a.client.getJSONAllPages(teamPath+agentsTeamSlug+"/members?per_page=100", &members)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgAgentsTeam)

		return
	}

	var granted []teamRepoGrant

	outcome = a.client.getJSONAllPages(teamPath+agentsTeamSlug+"/repos?per_page=100", &granted)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgAgentsTeam)

		return
	}

	var repos []orgRepo

	outcome = a.client.getJSONAllPages("/repos?per_page=100&type=all", &repos)
	if outcome.err != nil || outcome.notFound {
		a.unverifiable(orNotFound(outcome), checkOrgAgentsTeam)

		return
	}

	writable := map[string]bool{}

	for _, grant := range granted {
		if grant.Permissions.Push || grant.Permissions.Maintain || grant.Permissions.Admin {
			writable[grant.Name] = true
		}
	}

	var missing []string

	for _, repo := range repos {
		if !repo.Archived && !writable[repo.Name] {
			missing = append(missing, repo.Name)
		}
	}

	slices.Sort(missing)

	noMembers := ""

	if len(members) == 0 {
		noMembers = " — and the team has no members: add the agents' account (gh api -X PUT orgs/" + org +
			teamPath + agentsTeamSlug + "/memberships/<login>)"
	}

	switch {
	case len(missing) > 0:
		a.flag(checkOrgAgentsTeam, StatusFail, strings.Join(missing, listSeparator),
			teamPermissionPush+" on every repository",
			"repositories the "+agentsTeamSlug+" team cannot push to"+noMembers,
			&Change{
				Check: checkOrgAgentsTeam,
				Summary: agentsTeamSlug + " team: grant " + teamPermissionPush + " on " + strings.Join(
					missing,
					listSeparator,
				),
				apply: func(c client) error {
					for _, name := range missing {
						if err := c.writeJSON("PUT", teamGrantPath(org, name),
							map[string]string{"permission": teamPermissionPush}); err != nil {
							return err
						}
					}

					return nil
				},
			})
	case len(members) == 0:
		a.flag(checkOrgAgentsTeam, StatusAdvisory, "(no members)", "the agents' account",
			"the "+agentsTeamSlug+" team has write on every repository but no members"+noMembers, nil)
	default:
		a.flag(checkOrgAgentsTeam, StatusOK, "", "",
			"the "+agentsTeamSlug+" team has write on every repository", nil)
	}
}

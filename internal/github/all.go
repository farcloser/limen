package github

import (
	"fmt"
	"slices"
)

// OrgRepos lists the non-archived repositories of an organization as
// "owner/name" slugs, sorted. Archived repositories are skipped: their
// settings are frozen by GitHub, so every fixable check would fail forever
// with a write the API refuses.
//
// One page of 100, like every other inventory in this package. A full page
// means there may be more, which OrgRepos reports rather than silently
// auditing a prefix of the organization — a sweep that quietly stopped at
// 100 would read as "every repository is compliant".
func OrgRepos(org string) ([]string, error) {
	var repos []orgRepo

	outcome := orgClient(org).getJSON("/repos?per_page=100&type=all", &repos)
	if outcome.err != nil {
		return nil, fmt.Errorf("listing the repositories of %s: %w", org, outcome.err)
	}

	if outcome.notFound {
		return nil, fmt.Errorf("listing the repositories of %s: %w", org, errEndpointNotFound)
	}

	if caveat := pageFullCaveat(len(repos)); caveat != "" {
		return nil, fmt.Errorf("%w: %s has at least 100 repositories%s — sweep them in smaller sets",
			errTooManyRepos, org, caveat)
	}

	slugs := make([]string, 0, len(repos))

	for _, repo := range repos {
		if repo.Archived {
			continue
		}

		slugs = append(slugs, org+"/"+repo.Name)
	}

	slices.Sort(slugs)

	return slugs, nil
}

// AuditMany audits the organization itself and then each named repository,
// tagging every finding and change with the target it came from. The order
// is stable — the org first, then the repositories as given — because the
// text report groups on consecutive targets.
//
// The repository list is a parameter rather than something this re-derives:
// fix audits twice (plan, then the post-apply re-audit) and both passes must
// judge the same set, or a repository created mid-run would appear in the
// report as a fix that was never planned.
func AuditMany(org string, repos []string, overrides map[string]string) ([]Finding, []Change) {
	findings, changes := AuditOrg(org, overrides)
	target := "org " + org

	for index := range findings {
		findings[index].Target = target
	}

	for index := range changes {
		changes[index].Target = target
	}

	for _, slug := range repos {
		repoFindings, repoChanges := Audit(slug, overrides)

		for index := range repoFindings {
			repoFindings[index].Target = slug
		}

		for index := range repoChanges {
			repoChanges[index].Target = slug
		}

		findings = append(findings, repoFindings...)
		changes = append(changes, repoChanges...)
	}

	return findings, changes
}

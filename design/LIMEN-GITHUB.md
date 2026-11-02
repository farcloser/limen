# LIMEN-GH — GitHub organization & repository configuration as a limen rule set

Status: **built, through the organisation level.** `limen github check` and `limen github
fix` audit a repository (R1–R7) and an organisation (O1–O7) against the embedded floor,
with the override file, the four verdict classes and plan-then-apply consent; the
human-facing statement is the book's [GitHub chapter](../book/github.md), which wins
where the two differ. This document is kept for the catalog: the R and O numbering is
the key the code's check identifiers and the open issues use. The issues: what the
catalog names and the code does not have,
https://github.com/farcloser/limen/issues/508; the scheduled drift audit and the org
rulesets migration, https://github.com/farcloser/limen/issues/507; the exact fine-grained
permission set, https://github.com/farcloser/limen/issues/509.

## Why

Repository and organization *settings* are the most ignored half of engineering hygiene:
they live behind a web UI, drift when humans click, gate real security properties
(who can push a `v*` tag; whether a workflow token can write; whether a leaked secret
gets blocked), and are enforced today by tribal memory. That is exactly the gap the
limen doctrine exists for: **every rule written, verifiable, enforceable** — true for
files in the tree, and with this command for the platform that hosts them.

`limen github check` and `limen github fix` do for GitHub state what `limen check` and
`limen fix` do for files — judge live settings against a canonical, embedded baseline;
repair what is safe to repair; report the rest as advisories.

## Design

### Command surface

```
limen github check [--repo owner/name | --org name [--all-repos]] [-json]
limen github fix   [--repo owner/name | --org name [--all-repos]] [-json] [--yes]
```

- Defaults: `--repo` inferred from the current checkout's `origin`. `--org` audits the
  organization itself; `--org X --all-repos` sweeps — the organization AND every
  non-archived repository in it, each finding tagged with its target. The sweep is
  what keeps a repository correct after the day someone last pointed limen at it:
  settings drift when humans click, and a baseline rollout can hand a compliant
  repository dependencies it did not have before (farcloser/godolint, 2026-09-08 —
  the tools/go.mod rollout gave it vulnerable pins and its repo-level Dependabot
  security updates, on since creation and until then harmless, opened PRs).
  Every list the package reads — the sweep's repositories, and every inventory
  and lookup behind a check — is read whole (the Link header followed to the last
  page, the object-wrapped lists collected by field): a page is not an inventory, and a ruleset, owner,
  team, or App on a later page is exactly as real as one on the first.
- `check` is read-only and needs only read scopes. `fix` prints the full plan (current →
  desired, one line per change) and applies only with `--yes` or interactive consent.
- Output reuses the existing `Finding`/`Outcome` machinery and exit codes (0/1/2), plus
  one new verdict class — see below.

### Transport: HTTP in-process, identity from `gh`

limen calls the REST API itself (`net/http`, one connection pool, Link-header pagination,
one bounded retry on a rate limit). `gh auth` owns identity: the token is `GH_TOKEN` or
`GITHUB_TOKEN` from the environment, else `gh auth token` from the `gh` on the hermetic
PATH, asked once; limen stores no credential, and the same run works on a laptop and in CI.
`gh` keeps the attestation verification (`gh attestation verify`, `gh release verify-asset`).
Decided after measuring the first transport, `gh api` per call: 34 processes and TLS
handshakes for one repository, 0.44 s each, 16 s an audit; in-process the same audit is a
couple of seconds. The tests run against an in-package HTTP server (`GITHUB_API_URL`), so the
rule set is tested against canned responses without a network.

### The baseline: floor semantics, embedded, arguable

Desired state is an embedded canonical baseline (Go data, like the embedded files — the
source of truth reviewed in this repo), interpreted as a **floor**: a repository may be
*stricter* than the baseline, never looser. Example: baseline says "secret scanning on"
— off fails, on-plus-push-protection passes even if the baseline is silent on push
protection. Settings the baseline does not name are not
judged (a repo's description is its own business; whether *some* description exists is
checkable).

Per-repo deviations that must be legal (a repo that genuinely needs wikis) are an
explicit, committed override file, `.lint-github.yaml` at the repository root, named
for its lane like `.lint-go.yaml`: a **delta, exceptions only**, never a full settings
copy. Each entry is a check identifier and its required reason, one line:

```yaml
wiki: hosts the operations runbook
```

`limen github check` reports a listed deviation as compliant-with-exception (visible,
not failed); anything not listed is judged against the floor; unknown keys fail the
file itself (schema-validated). The escape hatch lives in review, never in a UI click.

### Verdict classes

The file rules have ok/fail; settings need four:

| Verdict | Meaning |
|---|---|
| ok | matches or exceeds the floor |
| fail | below the floor; `fix` can repair it |
| advisory | below the floor but never auto-fixed (people, keys, hooks — anything whose removal could lock someone out or break production) |
| unverifiable | the API cannot answer under this token/plan (fine-grained gaps, Enterprise-gated endpoints) — reported distinctly, never counted as ok |

`unverifiable` is non-negotiable: "what cannot be verified does not pass" is the aqua
rule's phrasing, and silently skipping unqueryable settings would fake compliance.

### Fix safety model

- Plan-then-apply, always; `--yes` for unattended use.
- **Never auto-fix people or credentials**: collaborators, deploy keys, webhooks, app
  installations are advisory-only, with the exact `gh` command printed. Team grants
  likewise, with one carve-out: the canonical `agents` team's write grant is baseline
  apparatus, not a person — `fix` adds it, and only adds it (never a removal, never a
  human; the team and its members stay human work). Decided 2026-09-06, after every
  hand-created repository failed the bot's first push.
- Fixes are minimal PATCHes (only the non-compliant fields), idempotent, and re-checked
  after application (same pattern as `remediateAqua`'s post-check).
- Drift direction matters: settings drift *back* when humans click. The companion is a
  scheduled audit workflow (`limen github check` on a cadence, failing loudly), not built:
  https://github.com/farcloser/limen/issues/507.

---

## Repo-level catalog

Legend: **Fix** ✓ = auto-fixable · adv = advisory-only · — = check-only by nature.
Scope column = minimal token capability (classic scope names; fine-grained equivalents
exist for all). Endpoints verified reachable 2026-07-03 (`/rulesets` public-read,
`/actions/permissions` auth-gated, `security_and_analysis` on the repo object).

Each section ends with what the code has, by check identifier (`internal/github`), and
what it does not, which https://github.com/farcloser/limen/issues/508 decides row by row.

### R1 — Security & analysis

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Secret scanning | enabled | ✓ | `PATCH /repos/{o}/{r}` `security_and_analysis` |
| Secret scanning push protection | enabled | ✓ | same |
| Dependabot alerts | enabled | ✓ | `PUT /repos/{o}/{r}/vulnerability-alerts` |
| Dependabot security updates | **disabled** (Renovate is the one dependency bot; alerts stay on as its signal, and the seeded renovate.json enables `vulnerabilityAlerts` so fix PRs cover indirect Go modules too) | ✓ | `DELETE /repos/{o}/{r}/automated-security-fixes` |
| Private vulnerability reporting | enabled (public repos) | ✓ | `PUT /repos/{o}/{r}/private-vulnerability-reporting` |
| Code scanning default setup | **not required** (decided: our SAST posture is gosec + staticcheck via golangci and govulncheck, per platform; CodeQL's marginal catch is taint-flow analysis, which matters for network/parser-heavy services — revisit if we ship one; repos may opt in via the override file) | ✓ when opted in | `PATCH /repos/{o}/{r}/code-scanning/default-setup` |

Built: `secret-scanning`, `secret-scanning-push-protection`, `dependabot-alerts`,
`dependabot-security-updates`, `private-vulnerability-reporting`, `code-scanning`.

### R2 — Actions hardening

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Default workflow token permissions | `read` | ✓ | `PUT /repos/{o}/{r}/actions/permissions/workflow` |
| Workflows can approve PRs | false | ✓ | same (`can_approve_pull_request_reviews`) |
| Allowed actions policy | `selected`: GitHub-owned + our SHA-pinned list (matches the "one first-party action" doctrine) | ✓ | `PUT /repos/{o}/{r}/actions/permissions` + `/selected-actions` |
| Fork PR approval requirement | first-time contributors (or stricter) | ✓ | `PUT /repos/{o}/{r}/actions/permissions/fork-pr-contributor-approval` |
| Actions access by other repos | `none` | ✓ | `PUT /repos/{o}/{r}/actions/permissions/access` |
| Artifact/log retention | ≤ 90 days (org default usually fine) | ✓ | part of org/repo actions settings |
| Workflow content sanity (extra, scorecard-inspired) | no `pull_request_target` + checkout-of-head, no unpinned third-party `uses:`, no `permissions:` absent at workflow level | — (**decided**: these are tree checks — they go into the existing file-rule engine, not limen github) | tree scan |

Built: `actions-workflow-permissions`, `actions-approve-pull-requests`, `actions-allowed`,
`actions-fork-pr-approval`, `actions-access-level`. Not built: retention; the
workflow-content checks are tree checks for the file-rule engine, not this command.

### R3 — Merge & branch workflow

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Merge methods | merge commits allowed, linear history not required (book/github.md: GitHub's rebase merge recreates commits unsigned, squash collapses a pull request, so the merge commit is the one method that keeps a reviewed history) | ✓ | `PATCH /repos/{o}/{r}` |
| Squash commit message default | PR title + body | ✓ | same |
| Delete branch on merge | true | ✓ | same |
| Auto-merge allowed | true (pairs with Renovate) | ✓ | same |
| Renovate processing | an open issue titled `Dependency Dashboard` authored by the `renovate` app — the execution-layer twin of O4's "Renovate installed": the app skips forks by default, before any preset resolves, and every configuration check stays green while it does. Fail, no fix: no API starts a job; the message says to trigger one in the Mend portal and, for a fork, to confirm `forkProcessing` | — | `GET /repos/{o}/{r}/issues?state=open&creator=app%2Frenovate` |
| Always suggest updating branches | true | ✓ | same |
| Default branch name | `main` | adv (rename is disruptive) | `PATCH /repos/{o}/{r}` / rename endpoint |
| Web commit sign-off required | true (DCO enforcement for UI edits — complements `lint commits`) | ✓ | `PATCH /repos/{o}/{r}` (`web_commit_signoff_required`) |

Built: every row (`merge-methods`, `squash-commit-defaults`, `delete-branch-on-merge`,
`auto-merge`, `renovate-processing`, `update-branch-suggestions`, `default-branch`,
`web-commit-signoff`).

### R4 — Rulesets

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Default-branch ruleset exists & active | PRs required, always, no exceptions (0 required approvals is acceptable while solo — the PR is the audit trail and the CI gate); required status checks = `gate` (and `security` where the workflow exists); signatures required; block force pushes & deletion; no linear history (per the merge doctrine) | ✓ (`limen:main`, reconciled by name) | `POST/PUT /repos/{o}/{r}/rulesets` |
| `v*` tag ruleset | creation/update/deletion restricted to maintainers + the release flow (the tag push is the release button — the once-manual ruleset chore, mechanized) | ✓ (`limen:tags`) | same |
| Legacy branch-protection absent | rulesets are the one mechanism (no drift between two systems) | adv | `GET /repos/{o}/{r}/branches/{b}/protection` |
| Bypass lists | empty or named-in-baseline only | ✓ | ruleset payload |

Built: `ruleset-default-branch`, `ruleset-version-tags`, bypass actors included. Not
built: legacy branch protection absent.

### R5 — Features & metadata

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Description set | non-empty | adv (content is human) | `PATCH /repos/{o}/{r}` |
| Topics | non-empty for public repos | adv | `PUT /repos/{o}/{r}/topics` |
| Issues | on unless override (the tracker: SUPPORT.md and the org-wide issue forms assume it) | ✓ | `PATCH /repos/{o}/{r}` |
| Wiki / Projects / Discussions | off unless override (docs live in-repo; issues are the tracker) | ✓ | `PATCH /repos/{o}/{r}` |
| Template repo flag, archived, visibility | as declared in override file only — visibility changes are never auto-fixed | adv | same |
| Pages | off unless override | adv | `GET /repos/{o}/{r}/pages` |
| Forking allowed (private repos) | false | ✓ | `PATCH /repos/{o}/{r}` (`allow_forking`) |

Built: `description`, `topics`, `issues`, `wiki`, `projects`, `discussions`, `pages`,
`forking`. Not built: template flag, archived state, visibility as declared.

### R6 — Access & credential surface (audit-only)

All advisory: these are people and credentials.

| Check | Baseline | API |
|---|---|---|
| Direct collaborators | none outside the baseline's named set; no outside collaborator with admin/push (Allstar's check) | `GET /repos/{o}/{r}/collaborators?affiliation=…` |
| Deploy keys | none, or read-only + named in override | `GET /repos/{o}/{r}/keys` |
| Webhooks | HTTPS only, secret set (scorecard's check), named in override | `GET /repos/{o}/{r}/hooks` |
| Environments | every env the default branch's workflows deploy to exists, has a deployment branch policy and holds what its jobs read; `release` = the one `v*` tag policy + a required reviewer (fix creates or reconciles it, owners as reviewers; values never) | `GET /repos/{o}/{r}/environments`, `…/deployment-branch-policies`, `…/secrets`; `PUT /repos/{o}/{r}/environments/release` |
| Repo secrets | none: a repository secret is read from any branch a write account pushes; advisory naming the environment move (existence, never values) | `GET /repos/{o}/{r}/actions/secrets` |
| App installations touching this repo | subset of org-approved list | `GET /repos/{o}/{r}/installation` (app-scoped) / org view |

Built: `outside-collaborators`, `deploy-keys`, `webhooks`, `actions-secrets`,
`environments`. Not built: app installations.

### R7 — Community health (mostly existing limen file rules)

CODEOWNERS, SECURITY.md, issue/PR templates, CONTRIBUTING: these are *files*, so they
belong to the existing rule engine (candidate new mandatory-file rules), not the gh
API — with one twist: GitHub falls back to the **org `.github` repository** for all of
them. Decided (O7): per-repo `README`/`LICENSE` stay mandatory, SECURITY.md,
CONTRIBUTING and the templates live in the org `.github` fallback once. Built at the org
(`org-community-health-repo`, `org-community-health-content`). Not built: the
*effective* value per repository through `GET /repos/{o}/{r}/community/profile`.

---

## Org-level catalog

### O1 — Authentication & membership floor

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| 2FA required | true | adv (flips members out of the org — human decision) | `PATCH /orgs/{o}` (`two_factor_requirement_enabled`) |
| Base member permission | `read` | ✓ | `PATCH /orgs/{o}` (`default_repository_permission`) |
| Members can create repos | private only, or false (repos are bootstrapped, not clicked) | ✓ | `PATCH /orgs/{o}` (`members_can_create_*`) |
| Members can fork private repos | false | ✓ | same |
| Members can change repo visibility / delete repos | admins only | ✓ | same-family fields |
| Pages creation | restricted | ✓ | same |
| Default repo permission audit: owners list | named set (peribolos-style roster, inventory only) | adv | `GET /orgs/{o}/members?role=admin` |

Built: every row (`org-two-factor-requirement`, `org-default-repository-permission`,
`org-members-create-public-repositories`, `org-members-fork-private-repositories`,
`org-members-change-repository-visibility`, `org-members-delete-repositories`,
`org-members-create-public-pages`, `org-admins`), the visibility and deletion floors as
advisories since the REST API reads them and does not write them.

### O2 — Org-wide Actions policy

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Enabled repositories | all (or selected list) | ✓ | `PUT /orgs/{o}/actions/permissions` |
| Allowed actions | `selected`: GitHub-owned + pinned allowlist — the org-wide version of R2, so new repos are born hardened | ✓ | `PUT /orgs/{o}/actions/permissions/selected-actions` |
| Default workflow token permissions | `read` | ✓ | `PUT /orgs/{o}/actions/permissions/workflow` |
| Workflows can approve PRs | false | ✓ | same |
| Fork PR approval | required | ✓ | org actions settings |
| Self-hosted runners / runner groups | none (inventory advisory if any appear) | adv | `GET /orgs/{o}/actions/runner-groups` |

Built: every row (`org-actions-enabled-repositories`, `org-actions-allowed`,
`org-actions-sha-pinning`, `org-actions-workflow-permissions`,
`org-actions-approve-pull-requests`, `org-actions-fork-pr-approval`,
`org-actions-self-hosted-runners`).

### O3 — Org security defaults

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Security configurations (the new mechanism: named bundles of dependabot/secret-scanning/code-scanning settings, attachable to repos incl. by default for new ones) | one canonical config, enforced + default for new repos | ✓ | `/orgs/{o}/code-security/configurations` |
| Dependabot alerts default for new repos | enabled | ✓ | `PATCH /orgs/{o}` (legacy fields) or security configurations |
| Dependabot security updates in every org-owned configuration | **disabled** — the org half of R1: while a configuration says `enabled`, the repository-level `DELETE /automated-security-fixes` answers 422, so the repo check cannot reach the floor on its own. Disabled at the org, not `not_set`: unsetting merely unlocks the repo toggle and every repository created later is born enabled again. Configurations this org does not own (`target_type` `global`/`enterprise`) are advisory — detaching is a human act | ✓ | `PATCH /orgs/{o}/code-security/configurations/{id}` |
| Security manager team | defined if org grows beyond one human | adv | `PUT /orgs/{o}/security-managers/teams/{t}` |

Built: `org-code-security-configuration` (advisory: creating one is a human act),
`org-dependabot-security-updates` (with its fix). Not built: the security-manager team.

### O4 — Org surface audits (advisory)

| Check | Baseline | API |
|---|---|---|
| Installed GitHub Apps | subset of a named allowlist (Renovate just joined — this check makes such grants reviewable) with scopes recorded | `GET /orgs/{o}/installations` |
| Renovate installed | the `renovate` app is among the installations — the seeded `renovate.json` and the checksum-refresh workflow depend on it. Fail, no fix: app installation has no API. Self-hosted Renovate = exemption | `GET /orgs/{o}/installations` |
| Org webhooks | HTTPS + secret + named | `GET /orgs/{o}/hooks` |
| Org-level secrets | none: an org secret is read from any branch a write account pushes in a covered repository; advisory naming the environment move | `GET /orgs/{o}/actions/secrets` |
| Fine-grained PAT approvals | inventory (API is partial; mark unverifiable where gated) | `/orgs/{o}/personal-access-token*` |
| Teams & grants | inventory (peribolos territory; management out of scope while the org is one person) — plus the one asserted grant: the `agents` team exists, has members, and holds write on every non-archived repository; fix grants the missing ones | `GET /orgs/{o}/teams`, `/teams/agents/repos`, `/teams/agents/members`, `PUT /orgs/{o}/teams/agents/repos/{o}/{r}` |
| Custom properties schema | reserved for future repo classification (safe-settings uses these well) | `/orgs/{o}/properties/schema` |

Built: `org-installed-apps`, `org-renovate-installed`, `org-webhooks`,
`org-actions-secrets`, `org-personal-access-tokens`, `org-teams`, `org-agents-team`. Not
built: the custom properties schema.

### O5 — Org rulesets

Org-level rulesets apply protections across repos by name/property patterns — the
org-wide successor to per-repo R4. Once trusted, migrate `limen:main`/`limen:tags` from
per-repo reconciliation to **one org ruleset each**, and the per-repo check reduces to
"not weakened locally, no local bypass added." API: `/orgs/{o}/rulesets`. (Availability
on free-plan private repos is limited — public repos and paid plans fine; the check
must classify accordingly, `unverifiable`/plan-gated rather than fail.)

Not built: https://github.com/farcloser/limen/issues/507, with the scheduled audit.

### O6 — Org profile & metadata

Display name, description, email, URL, verified domains (adv), sponsor settings (adv) —
`PATCH /orgs/{o}` for the fixable subset; mostly advisory prose.

Built: `org-profile-description`, `org-web-commit-signoff`. Not built: email, URL,
verified domains, sponsorship.

### O7 — The org `.github` repository (the fallback mechanism, explicitly in scope)

GitHub resolves several files from an org-level `.github` repo when a repo lacks its
own: community health files (`SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
`SUPPORT.md`, `FUNDING.yml`, issue/PR templates, `GOVERNANCE`-adjacent docs), the org
**profile README** (`profile/README.md`), and **workflow templates**
(`workflow-templates/*.yaml` + metadata — starter workflows offered in the Actions UI).

Proposed use — high leverage, pure limen-shape:

1. `farcloser/.github` becomes a limen-managed repo like any other (it must itself pass
   `limen check`), whose *content* is part of the canonical baseline: SECURITY.md
   (vulnerability reporting policy — pairs with R1's private-vulnerability-reporting),
   CONTRIBUTING.md (DCO! — the sign-off requirement `lint commits` enforces, finally
   written where contributors look), issue/PR templates, org profile README.
2. Per-repo duplication is then *removed* from the mandatory set: repos carry only what
   must be repo-specific (README, LICENSE, the tree files limen already pins);
   `limen github check --repo` verifies the **effective** community profile via
   `GET /repos/{o}/{r}/community/profile` (which accounts for the fallback), so a repo
   that shadows the org file with a *weaker* one is caught.
3. Workflow templates: publish `ci.yaml`/`release.yaml`/`update-aqua-checksum.yaml` as
   org workflow templates too. This does **not** replace content-pinning (templates are
   copy-on-create, drift-prone) — pinning stays the enforcement; templates are the
   discoverability layer for repos not yet under limen.

`limen github check --org` gains: `.github` repo exists, is public, passes `limen check`,
carries the canonical fallback set.

Built: points 1 and 2 (`org-community-health-repo`, `org-community-health-content`;
`farcloser/.github` and `forkcloser/.github` exist and pass `limen check`). Not built:
point 3, the workflow templates.

---

## Auth & scopes

| Mode | Token | Notes |
|---|---|---|
| `check --repo` (public) | none-to-minimal | much of R3/R4/R5 is public-readable; R1/R2 need repository administration read — below that, findings degrade to `unverifiable`, never `ok` |
| `check --org` | `read:org` + admin-read equivalents | 2FA/member-privilege fields require org owner tokens |
| `fix --repo` | fine-grained: administration write on the repository | the exact per-check permission set is https://github.com/farcloser/limen/issues/509 |
| `fix --org` | org owner | rare, deliberate, never in CI |

The scheduled audit (issue 507) cannot run on the default `GITHUB_TOKEN`, which reads
little of R1/R2; the identity it runs under is that issue's to decide (the update App of
decision 8 no longer exists).

## Decisions

1. **Merge doctrine**: merge commits allowed, linear history not required. The first
   version of this document decided the reverse (rebase + squash, linear history), which
   lost to a GitHub constraint: a rebase merge recreates commits unsigned, so a branch
   requiring signatures loses that button, and squash collapses a multi-commit pull
   request; the merge commit is the one method that keeps a reviewed history. Stated in
   [book/github.md](../book/github.md), which the code enforces (`allow_merge_commit`
   required, `required_linear_history` forbidden).
2. **Code scanning**: not required. Our SAST posture is gosec + staticcheck (golangci,
   per platform) + govulncheck; CodeQL's marginal value (taint-flow) applies to
   network/parser-heavy services we do not currently ship. Opt-in via the override file;
   revisit when that changes.
3. **PRs on main**: mandatory, always, no exceptions — with 0 required approvals
   acceptable while the org is solo. The PR is the audit trail and the CI gate.
4. **Override file**: `.lint-github.yaml` at the repository root, flat `check: reason`
   lines, exceptions only. It moved twice (`.github/limen-github.yaml`, a sectioned root
   `limen.yaml`) before the other judgments grew their own files (`.lint-go.yaml`,
   `.lint-links.toml`) and this one took the same shape.
5. **Command name**: `limen github`, never `limen gh` — per the naming doctrine (no
   shorthand, no abbreviations; explicit qualified names readable without a syllabus),
   recorded in book/recipes.md. `gh` remains only the *transport binary's* name.
6. **Workflow-content checks** (token permissions, dangerous triggers, unpinned uses):
   tree checks for the file-rule engine, not API checks.
7. **The org `.github` repository**: a limen-managed repository like any other, carrying
   the community health fallback; its org-side checks are O7.
8. **Bump-PR push credential**: none. The `update-aqua-checksum` workflow pushes its fix-up
   with the run's own token, which starts no run, and then approves the pull request's runs
   awaiting approval with the same token (`actions: write`); the checks report on the new
   head. The first version was a per-org GitHub App (`limen-ci-<org>`, registered by `limen
   bootstrap` through the app-manifest flow) whose private key was an organisation secret,
   readable by any run on any branch of a covered repository; the approval route was
   verified on a test pull request; limen no longer registers or uses the App, and the human
   deletes it once every repository has merged its migration bump. A dispatch of the CI does
   not do: GitHub does not count a dispatched run's checks for the pull request.

# LIMEN-GH — GitHub organization & repository configuration as a limen rule set

Status: phases 1 + 2 (repo level, check + fix) implemented in full —
`limen github check` / `limen github fix` cover R1, R2, R3, R4, R5, and R6's
advisory surface, with the override file (exemptions *and* opt-ins), the four
verdict classes, plan-then-apply consent, and the gh transport behind a test
seam. The former v1 omissions were closed 2026-07-04 against the verified
OpenAPI shapes: fork-PR approval (floor: all first-time contributors),
Actions access level (private repositories; floor "none"), the outside-
collaborators advisory (elevated access, first-100 stated), code scanning as
a true opt-in (requiring, never exempting), and required status checks in
`limen:main` — contexts are project-owned and preserved on reconcile, with
the canonical CI matrix as the fresh-create default. Phases 3 (org) and 4
(scheduled audit) remain unimplemented.

## Why

Repository and organization *settings* are the most ignored half of engineering hygiene:
they live behind a web UI, drift when humans click, gate real security properties
(who can push a `v*` tag; whether a workflow token can write; whether a leaked secret
gets blocked), and are enforced today by tribal memory. That is exactly the gap the
limen doctrine exists for: **every rule written, verifiable, enforceable** — currently
true for files in the tree, false for the platform that hosts them.

The end state: `limen github check` and `limen github fix` do for GitHub state what
`limen check`/`limen fix` do for files — judge live settings against a canonical,
embedded baseline; repair what is safe to repair; report the rest as advisories.

## Design

### Command surface

```
limen github check [--repo owner/name | --org name] [-json]
limen github fix   [--repo owner/name | --org name] [-json] [--yes]
```

- Defaults: `--repo` inferred from the current checkout's `origin`. `--org` audits the
  organization itself (not every repo in it — iterating repos is the caller's loop or a
  later `--all-repos` flag).
- `check` is read-only and needs only read scopes. `fix` prints the full plan (current →
  desired, one line per change) and applies only with `--yes` or interactive consent.
- Output reuses the existing `Finding`/`Outcome` machinery and exit codes (0/1/2), plus
  one new verdict class — see below.

### Transport: `gh`, not tokens

limen shells out to the aqua-pinned `gh` (`gh api …`) exactly like it shells to `aqua`
and `git` today. Rationale: `gh auth` owns identity (keychain, SSO, fine-grained
tokens), limen never stores or even sees a credential, and the same invocation works on
a laptop and in CI (`GH_TOKEN` env). The seam is one package-level `ghBin` variable —
the `aquaBin` test-stub pattern — so the entire rule set is testable against recorded
JSON fixtures without a network.

### The baseline: floor semantics, embedded, arguable

Desired state is an embedded canonical baseline (Go data, like the embedded files — the
source of truth reviewed in this repo), interpreted as a **floor**: a repository may be
*stricter* than the baseline, never looser. Examples: baseline says "squash merges
allowed, merge commits disallowed" — a repo that disallows squash too is compliant;
baseline says "secret scanning on" — off fails, on-plus-push-protection passes even if
the baseline is silent on push protection. Settings the baseline does not name are not
judged (a repo's description is its own business; whether *some* description exists is
checkable).

Per-repo deviations that must be legal (a repo that genuinely needs wikis) get the same
treatment as `LINT_GO_LICENSES_FLAGS`: an explicit, committed override file —
`limen.yaml` at the repository root (`github:` section; relocated from
`.github/limen-github.yaml` on 2026-07-06 — one project-owned declarations
file, sectioned by concern) — that is a **delta, exceptions only**, never a full
settings copy. Each entry names the setting and carries a required reason:

```yaml
wiki:
  enabled: true
  reason: hosts the operations runbook
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
- **Never auto-fix people or credentials**: collaborators, team grants, deploy keys,
  webhooks, app installations are advisory-only, with the exact `gh` command printed.
- Fixes are minimal PATCHes (only the non-compliant fields), idempotent, and re-checked
  after application (same pattern as `remediateAqua`'s post-check).
- Drift direction matters: settings drift *back* when humans click. The companion is a
  scheduled audit workflow (weekly `limen github check` in CI, failing loudly) — phase 4.

## Competition review

Read for this plan (READMEs/docs fetched 2026-07-03): `github/safe-settings`,
`ossf/allstar`, `ossf/scorecard` checks, the Terraform GitHub provider's resource list;
peribolos from prior knowledge (its README no longer ships in-tree).

**safe-settings** (GitHub app, org-hosted). Settings-as-code with org → suborg → repo
inheritance; covers repo settings, default branch, topics, custom properties, teams,
collaborators, labels, milestones, branch protections, autolinks, rulesets,
environments, repo-name validation; can force-create repos from templates; syncs on
schedule and on PR ("dry-run in PR" comparing proposed config to live state).
*Take:* the coverage list itself (environments and autolinks were not on my radar);
inheritance precedence ideas for org-vs-repo baselines; the PR-time dry-run as a future
nicety. *Reject:* hosting a Probot app and handing it org-admin — limen's model is a
binary a human (or CI) runs; and safe-settings *replaces* state ("the config is truth"),
where limen wants floor semantics with explicit overrides.

**Allstar** (OpenSSF app). Continuous policy enforcement with opt-in/opt-out strategy;
policies: branch protection (with fix), binary artifacts, CODEOWNERS presence, outside
collaborators with admin/push, SECURITY.md presence, dangerous workflow patterns,
generic scorecard threshold, GitHub Actions allowlist, repository administrators; acts
by filing issues (or fixing). *Take:* the policy set maps almost 1:1 onto our repo
catalog; "action on violation = file an issue" is a good CI-adjacent alternative to
failing a build; opt-out-with-reason mirrors our override file. *Reject:* app hosting;
issue-spam as the primary UX.

**Scorecard** (OpenSSF CLI). Heuristic *audit* (0–10 scores), not enforcement:
branch protection, token permissions in workflows, dangerous workflows, pinned
dependencies, binary artifacts, security policy, signed releases, packaging, SAST,
fuzzing, maintained, code review, contributors, vulnerabilities, webhooks (secret set),
SBOM, license, CI tests, dependency-update tool. *Take:* check ideas that read the
*content* of workflows (token-permissions, dangerous-workflow, unpinned actions) — our
workflow files are content-pinned so we get this for free where the baseline ships them,
but the checks matter for repos with extra workflows; webhook-secret and signed-release
checks. *Reject:* scoring — limen rules are booleans with reasons, not weighted grades.

**peribolos** (Kubernetes). Org config-as-code: org metadata, member/admin rosters,
teams (members, maintainers, privacy), team repo permissions; deliberately nothing at
the repo-settings level. *Take:* the org-level catalog shape, and its hard-won lesson
that people-management needs explicit "confirm before removing humans" semantics (it
requires opting into destructive membership changes). *Reject:* rosters-in-YAML as v1
scope — inventory/audit first, management maybe never (small org).

**Terraform provider** (88 resources). The completeness map — its resource list is
effectively GitHub's writable-settings API enumerated: actions permissions (org/repo),
workflow permissions, runner groups, rulesets (org/repo), custom properties,
dependabot/actions/codespaces secrets at all scopes, environments, deploy keys,
webhooks, custom roles, security managers, organization settings, autolinks…
*Take:* used as the checklist to make the catalogs below exhaustive. *Reject:* the tool
itself — Terraform state + HCL + apply lifecycle is the heavyweight IaC posture limen
exists to avoid; and it has no notion of "floor" (it owns what it manages).

**Positioning:** limen-gh = Allstar's policy set + safe-settings' coverage, delivered as
scorecard-style tooling (a CLI you run), with limen's floor semantics and check/fix
UX, no hosted app, no state file, `gh` as the only credential surface.

---

## Repo-level catalog

Legend: **Fix** ✓ = auto-fixable · adv = advisory-only · — = check-only by nature.
Scope column = minimal token capability (classic scope names; fine-grained equivalents
exist for all). Endpoints verified reachable 2026-07-03 (`/rulesets` public-read,
`/actions/permissions` auth-gated, `security_and_analysis` on the repo object).

### R1 — Security & analysis (highest value, v1)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Secret scanning | enabled | ✓ | `PATCH /repos/{o}/{r}` `security_and_analysis` |
| Secret scanning push protection | enabled | ✓ | same |
| Dependabot alerts | enabled | ✓ | `PUT /repos/{o}/{r}/vulnerability-alerts` |
| Dependabot security updates | enabled | ✓ | `PUT /repos/{o}/{r}/automated-security-fixes` |
| Private vulnerability reporting | enabled (public repos) | ✓ | `PUT /repos/{o}/{r}/private-vulnerability-reporting` |
| Code scanning default setup | **not required** (decided: our SAST posture is gosec + staticcheck via golangci and govulncheck, per-GOOS; CodeQL's marginal catch is taint-flow analysis, which matters for network/parser-heavy services — revisit if we ship one; repos may opt in via the override file) | ✓ when opted in | `PATCH /repos/{o}/{r}/code-scanning/default-setup` |

### R2 — Actions hardening (v1)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Default workflow token permissions | `read` | ✓ | `PUT /repos/{o}/{r}/actions/permissions/workflow` |
| Workflows can approve PRs | false | ✓ | same (`can_approve_pull_request_reviews`) |
| Allowed actions policy | `selected`: GitHub-owned + our SHA-pinned list (matches the "one first-party action" doctrine) | ✓ | `PUT /repos/{o}/{r}/actions/permissions` + `/selected-actions` |
| Fork PR approval requirement | first-time contributors (or stricter) | ✓ | `PUT /repos/{o}/{r}/actions/permissions/fork-pr-contributor-approval` |
| Actions access by other repos | `none` | ✓ | `PUT /repos/{o}/{r}/actions/permissions/access` |
| Artifact/log retention | ≤ 90 days (org default usually fine) | ✓ | part of org/repo actions settings |
| Workflow content sanity (extra, scorecard-inspired) | no `pull_request_target` + checkout-of-head, no unpinned third-party `uses:`, no `permissions:` absent at workflow level | — (**decided**: these are tree checks — they go into the existing file-rule engine, not limen github) | tree scan |

### R3 — Merge & branch workflow (v1)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Merge methods | **decided**: rebase + squash allowed, merge commits disallowed — linear history: every main commit bisectable and revertable as a unit, DCO survives rebase (goes in the book with implementation) | ✓ | `PATCH /repos/{o}/{r}` |
| Squash commit message default | PR title + body | ✓ | same |
| Delete branch on merge | true | ✓ | same |
| Auto-merge allowed | true (pairs with Renovate) | ✓ | same |
| Always suggest updating branches | true | ✓ | same |
| Default branch name | `main` | adv (rename is disruptive) | `PATCH /repos/{o}/{r}` / rename endpoint |
| Web commit sign-off required | true (DCO enforcement for UI edits — complements `lint commits`) | ✓ | `PATCH /repos/{o}/{r}` (`web_commit_signoff_required`) |

### R4 — Rulesets (v2: the write model is richer)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Default-branch ruleset exists & active | **decided**: PRs required, always, no exceptions (0 required approvals is acceptable while solo — the PR is the audit trail and the CI gate); required status checks = the ci.yaml jobs; block force pushes & deletion; linear history (per the merge doctrine) | ✓ (create/reconcile a canonical ruleset by name, e.g. `limen:main`) | `POST/PUT /repos/{o}/{r}/rulesets` |
| `v*` tag ruleset | creation/update/deletion restricted to maintainers + the release flow (the tag push is the release button — the once-manual ruleset chore, mechanized) | ✓ (`limen:tags`) | same |
| Legacy branch-protection absent | rulesets are the one mechanism (no drift between two systems) | adv | `GET /repos/{o}/{r}/branches/{b}/protection` |
| Bypass lists | empty or named-in-baseline only | ✓ | ruleset payload |

### R5 — Features & metadata (v1, low stakes)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Description set | non-empty | adv (content is human) | `PATCH /repos/{o}/{r}` |
| Topics | non-empty for public repos | adv | `PUT /repos/{o}/{r}/topics` |
| Wiki / Projects / Discussions | off unless override (docs live in-repo; issues are the tracker) | ✓ | `PATCH /repos/{o}/{r}` |
| Template repo flag, archived, visibility | as declared in override file only — visibility changes are never auto-fixed | adv | same |
| Pages | off unless override | adv | `GET /repos/{o}/{r}/pages` |
| Forking allowed (private repos) | false | ✓ | `PATCH /repos/{o}/{r}` (`allow_forking`) |

### R6 — Access & credential surface (audit-only, v2)

All advisory: these are people and credentials.

| Check | Baseline | API |
|---|---|---|
| Direct collaborators | none outside the baseline's named set; no outside collaborator with admin/push (Allstar's check) | `GET /repos/{o}/{r}/collaborators?affiliation=…` |
| Deploy keys | none, or read-only + named in override | `GET /repos/{o}/{r}/keys` |
| Webhooks | HTTPS only, secret set (scorecard's check), named in override | `GET /repos/{o}/{r}/hooks` |
| Environments | protection rules on any env that holds secrets; no self-review | `GET /repos/{o}/{r}/environments` |
| Repo secrets inventory | named in override (existence, never values) | `GET /repos/{o}/{r}/actions/secrets` |
| App installations touching this repo | subset of org-approved list | `GET /repos/{o}/{r}/installation` (app-scoped) / org view |

### R7 — Community health (mostly existing limen file rules)

CODEOWNERS, SECURITY.md, issue/PR templates, CONTRIBUTING: these are *files*, so they
belong to the existing rule engine (candidate new mandatory-file rules), not the gh
API — with one twist: GitHub falls back to the **org `.github` repository** for all of
them. Decision proposed below (O7): keep per-repo `README`/`LICENSE` mandatory as today,
put SECURITY.md, CONTRIBUTING, and templates in the org `.github` fallback once, and
have `limen github check --repo` verify the *effective* value (repo file or org fallback)
via `GET /repos/{o}/{r}/community/profile` (which resolves fallbacks server-side).

---

## Org-level catalog

### O1 — Authentication & membership floor (v3)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| 2FA required | true | adv (flips members out of the org — human decision) | `PATCH /orgs/{o}` (`two_factor_requirement_enabled`) |
| Base member permission | `read` | ✓ | `PATCH /orgs/{o}` (`default_repository_permission`) |
| Members can create repos | private only, or false (repos are bootstrapped, not clicked) | ✓ | `PATCH /orgs/{o}` (`members_can_create_*`) |
| Members can fork private repos | false | ✓ | same |
| Members can change repo visibility / delete repos | admins only | ✓ | same-family fields |
| Pages creation | restricted | ✓ | same |
| Default repo permission audit: owners list | named set (peribolos-style roster, inventory only) | adv | `GET /orgs/{o}/members?role=admin` |

### O2 — Org-wide Actions policy (v3)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Enabled repositories | all (or selected list) | ✓ | `PUT /orgs/{o}/actions/permissions` |
| Allowed actions | `selected`: GitHub-owned + pinned allowlist — the org-wide version of R2, so new repos are born hardened | ✓ | `PUT /orgs/{o}/actions/permissions/selected-actions` |
| Default workflow token permissions | `read` | ✓ | `PUT /orgs/{o}/actions/permissions/workflow` |
| Workflows can approve PRs | false | ✓ | same |
| Fork PR approval | required | ✓ | org actions settings |
| Self-hosted runners / runner groups | none (inventory advisory if any appear) | adv | `GET /orgs/{o}/actions/runner-groups` |

### O3 — Org security defaults (v3)

| Check | Baseline (floor) | Fix | API |
|---|---|---|---|
| Security configurations (the new mechanism: named bundles of dependabot/secret-scanning/code-scanning settings, attachable to repos incl. by default for new ones) | one canonical config, enforced + default for new repos | ✓ | `/orgs/{o}/code-security/configurations` |
| Dependabot alerts default for new repos | enabled | ✓ | `PATCH /orgs/{o}` (legacy fields) or security configurations |
| Security manager team | defined if org grows beyond one human | adv | `PUT /orgs/{o}/security-managers/teams/{t}` |

### O4 — Org surface audits (advisory, v3)

| Check | Baseline | API |
|---|---|---|
| Installed GitHub Apps | subset of a named allowlist (Renovate just joined — this check makes such grants reviewable) with scopes recorded | `GET /orgs/{o}/installations` |
| Org webhooks | HTTPS + secret + named | `GET /orgs/{o}/hooks` |
| Org-level secrets | named inventory | `GET /orgs/{o}/actions/secrets` |
| Fine-grained PAT approvals | inventory (API is partial; mark unverifiable where gated) | `/orgs/{o}/personal-access-token*` |
| Teams & grants | inventory only in v1 of org support (peribolos territory; management deliberately out of scope while the org is one person) | `GET /orgs/{o}/teams` |
| Custom properties schema | reserved for future repo classification (safe-settings uses these well) | `/orgs/{o}/properties/schema` |

### O5 — Org rulesets (v3, powerful)

Org-level rulesets apply protections across repos by name/property patterns — the
org-wide successor to per-repo R4. Once trusted, migrate `limen:main`/`limen:tags` from
per-repo reconciliation to **one org ruleset each**, and the per-repo check reduces to
"not weakened locally, no local bypass added." API: `/orgs/{o}/rulesets`. (Availability
on free-plan private repos is limited — public repos and paid plans fine; the check
must classify accordingly, `unverifiable`/plan-gated rather than fail.)

### O6 — Org profile & metadata (v3, low stakes)

Display name, description, email, URL, verified domains (adv), sponsor settings (adv) —
`PATCH /orgs/{o}` for the fixable subset; mostly advisory prose.

### O7 — The org `.github` repository (v3 — the fallback mechanism, explicitly in scope)

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

Timing (decided): the repo itself is created **opportunistically, now-ish** — it is just
a repo limen already knows how to bootstrap, and the SECURITY.md / profile-README value
is immediate. Only the *automated checks* of it wait for phase 3.

---

## Phasing

| Phase | Scope | Ships when |
|---|---|---|
| 1 | `limen github check --repo`: R1 + R2 + R3 + R5 (read-only), verdict classes, `gh` transport + fixtures | first |
| 2 | `limen github fix --repo` (plan/apply) for R1/R2/R3/R5 ✓-items; R4 rulesets check+fix; R6 audits | after 1 settles |
| 3 | `-org`: O1–O7, incl. the `.github` repo work and community-profile resolution | **shipped 2026-07-06** (O3 advisory-only in v1 — the legacy org security fields are closing down, so the check verifies a default code-security configuration exists; O5 stays in phase 4 per this table; the visibility/deletion member floors are read-only in the REST API and report as advisories) |
| 4 | Scheduled drift audit (weekly workflow running `limen github check`, failing loudly); org rulesets migration (O5); PR-time dry-run à la safe-settings | last |

## Auth & scopes

| Mode | Token | Notes |
|---|---|---|
| `check --repo` (public) | none-to-minimal | much of R3/R4/R5 is public-readable; R1/R2 need `repo`/fine-grained Administration:read — below that, findings degrade to `unverifiable`, never `ok` |
| `check --org` | `read:org` + admin-read equivalents | 2FA/member-privilege fields require org owner tokens |
| `fix --repo` | fine-grained: Administration:write on the repo | narrowest that works; document the exact fine-grained permission set in the book chapter |
| `fix --org` | org owner | rare, deliberate, never in CI initially |

CI usage note: the default `GITHUB_TOKEN` cannot read most of R1/R2 — the scheduled
audit workflow needs a fine-grained PAT or GitHub App, which is the same decision
already pending for `UPDATE_AQUA_CHECKSUM_TOKEN`; solve once, reuse.

## Decisions (2026-07-03)

1. **Merge doctrine**: rebase + squash, merge commits disallowed, linear history —
   confirmed as the 2026 posture for this org (bisectable/revertable main, DCO survives
   rebase); to be written into the book alongside implementation.
2. **Code scanning**: not required. Our SAST posture is gosec + staticcheck (golangci,
   per-GOOS) + govulncheck; CodeQL's marginal value (taint-flow) applies to
   network/parser-heavy services we do not currently ship. Opt-in via the override file;
   revisit when that changes.
3. **PRs on main**: mandatory, always, no exceptions — with 0 required approvals
   acceptable while the org is solo. The PR is the audit trail and the CI gate.
4. **Override file**: delta-only with required reasons, as specified above.
   Amended 2026-07-06: consolidated to root `limen.yaml` (`github:` section) — the
   single project-owned declarations file for everything limen judges.
5. **Command name**: `limen github`, never `limen gh` — per the naming doctrine (no
   shorthand, no abbreviations; explicit qualified names readable without a syllabus),
   now recorded in book/recipes.md. `gh` remains only the *transport binary's* name.
6. **Workflow-content checks** (token permissions, dangerous triggers, unpinned uses):
   folded into the existing file-rule engine — they are tree checks, not API checks.
7. **`farcloser/.github`**: created opportunistically now; automated org-side checks of
   it remain phase 3.
8. **Bump-PR push credential** (added 2026-07-07): GitHub App over fine-grained PAT for
   the `update-aqua-checksum` push — per-run one-hour repo-scoped tokens, org-owned,
   nothing expires on a calendar. Workflow token preference: App
   (`UPDATE_AQUA_CHECKSUM_APP_ID` variable + `UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY`
   secret) → PAT (`UPDATE_AQUA_CHECKSUM_TOKEN`) → default token. Each org registers its
   own App — private keys don't travel, so a shared App is structurally impossible;
   the App is named `limen-ci-<org>` (globally-unique App names force the org suffix),
   so farcloser's is `limen-ci-farcloser`. This resolves the "solve once, reuse" note
   above: when the scheduled audit workflow lands, extend the same App with the
   admin-read permissions instead of minting a second identity. Adopter setup is four
   UI steps (documented in book/tooling.md). Amended 2026-07-07: automated in
   `limen bootstrap` (`-org`, or inferred from origin) via the app-manifest flow —
   pre-filled manifest form on localhost, one browser approval, then
   `POST /app-manifests/{code}/conversions` yields id + private key, stored as the org
   variable/secret; installation is the one remaining click, polled for. Idempotent;
   every state it cannot create or verify (no org admin, headless, half-configured)
   degrades to a warning, never a failed bootstrap (internal/github/app.go).

## Open questions (decide at review)

1. **Merge doctrine** (R3): squash+rebase/no-merge-commits + linear history is the
   proposal — confirm, it's currently unwritten anywhere.
2. **Code scanning** (R1): adopt CodeQL default-setup, or declare golangci+govulncheck
   our SAST posture and set the baseline to "not required"?
3. **PR requirement on main** (R4) for a one-person org: PRs required with 0 approvals
   (audit trail, CI gate) vs direct pushes allowed until contributors arrive?
4. **Override file** name/shape: `.github/limen-github.yaml`? And is it subset-pinned
   (exceptions only) — proposed yes.
5. **Command name**: `limen github` (implies the transport) vs `limen github` (implies the
   platform). Mild preference: `gh`, it is honest about the dependency.
6. **R2 workflow-content checks**: fold into the existing file-rule engine now (they are
   tree checks, not API checks) or keep with limen-gh for cohesion?
7. **O7 timing**: the `.github` repo could be created much earlier than phase 3 by hand
   (it is just a repo limen already knows how to bootstrap) — do it opportunistically?

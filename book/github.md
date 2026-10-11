# GitHub settings

Repository settings are configuration like any other — they gate real security properties
(who can push a release tag, whether a workflow token can write, whether a leaked secret
gets blocked) — yet they live behind a web UI, drift when humans click, and are enforced by
tribal memory. The operating principle applies unchanged: **every rule is written,
verifiable, and enforceable.** This chapter is the written part; `limen github check`
verifies, `limen github fix` enforces.

```bash
limen github check            # audit the repo's settings (slug inferred from origin)
limen github fix              # plan → consent → apply → re-audit
limen github check -repo owner/name -json
```

## The floor model

The baseline is a **floor**: a repository may be *stricter* than it, never looser. A repo
that disables a merge method the baseline allows is compliant; one that enables a method the
baseline forbids is not. Settings the baseline does not name are not judged.

Every check yields one of four verdicts:

| Verdict | Meaning |
|---|---|
| ok | matches or exceeds the floor |
| fail | below the floor — `limen github fix` can repair it |
| advisory | below the floor but **never auto-fixed**: people, credentials and content (collaborators, deploy keys, webhooks, descriptions) are a human's to change |
| unverifiable | the API cannot answer under the current token — reported distinctly, and it does **not** count as passing |

The catalog of checks is the tool itself: run `limen github check` and read the findings;
each names its check identifier. The book carries the reasoning, not a copy of the list.

## Exceptions — `.lint-github.yaml`

A repository that needs to deviate declares it, in a committed file at the repository root,
with a reason — the escape hatch lives in review, never in a UI click:

```yaml
# .lint-github.yaml — where and why this repository deviates from what
# `limen github check` enforces. Delta only, one reason each.
wiki: hosts the operations runbook
org-admins: apostasie is the sole owner
```

The file is a **delta**: exceptions only, never a full settings copy. Each entry is
`check-identifier: reason`. An exempted check reports ok, visibly carrying the reason; an
unknown identifier or a missing reason fails the file itself. It is named for its lane like
the other project-owned carve-outs (`.lint-go.yaml`, `.lint-links.toml`, `.lint-signers`):
one file per check, read by that check alone. Its former name, `limen.yaml`, is refused: the
audit does not run while it is present, rather than read no exceptions, and `limen fix`
converts it.

Every declarable identifier, grouped (a test pins this list to the live check catalog):

```yaml
# .lint-github.yaml — every declarable identifier
# --- Repository: security posture ---
# secret-scanning: <reason>
# secret-scanning-push-protection: <reason>
# dependabot-alerts: <reason>
# dependabot-security-updates: <reason>  # baseline is OFF (Renovate is the one bot); exempt to keep Dependabot's own PRs
# private-vulnerability-reporting: <reason>
# code-scanning: <reason>  # OPT-IN: listing this REQUIRES CodeQL default setup (a stricter floor), never exempts
# --- Repository: Actions hardening ---
# actions-workflow-permissions: <reason>
# actions-approve-pull-requests: <reason>
# actions-allowed: <reason>
# actions-fork-pr-approval: <reason>
# actions-access-level: <reason>
# --- Repository: merge & branch workflow ---
# merge-methods: <reason>
# squash-commit-defaults: <reason>
# delete-branch-on-merge: <reason>
# auto-merge: <reason>
# update-branch-suggestions: <reason>
# default-branch: <reason>
# web-commit-signoff: <reason>
# --- Repository: rulesets ---
# ruleset-default-branch: <reason>
# ruleset-version-tags: <reason>
# --- Repository: hygiene & surface ---
# description: <reason>
# topics: <reason>
# issues: <reason>
# wiki: <reason>
# projects: <reason>
# discussions: <reason>
# forking: <reason>
# pages: <reason>
# outside-collaborators: <reason>
# webhooks: <reason>
# deploy-keys: <reason>
# agents-team: <reason>
# renovate-processing: <reason>  # self-hosted Renovate without a dashboard, say
# --- Organization (only read by `limen github … -org <name>`) ---
# org-two-factor-requirement: <reason>
# org-default-repository-permission: <reason>
# org-members-create-public-repositories: <reason>
# org-members-fork-private-repositories: <reason>
# org-members-change-repository-visibility: <reason>
# org-members-delete-repositories: <reason>
# org-members-create-public-pages: <reason>
# org-web-commit-signoff: <reason>
# org-admins: <owner logins>  # the roster declaration: every actual owner login must appear here
# org-actions-enabled-repositories: <reason>
# org-actions-allowed: <reason>
# org-actions-sha-pinning: <reason>
# org-actions-workflow-permissions: <reason>
# org-actions-approve-pull-requests: <reason>
# org-actions-fork-pr-approval: <reason>
# org-actions-self-hosted-runners: <reason>
# org-code-security-configuration: <reason>
# org-dependabot-security-updates: <reason>  # Dependabot raises the fix PRs here, not Renovate, say
# org-installed-apps: <reason>
# org-renovate-installed: <reason>  # Renovate is self-hosted from <repo>, say
# org-webhooks: <reason>
# org-actions-secrets: <reason>
# org-teams: <reason>
# org-agents-team: <reason>
# org-personal-access-tokens: <reason>
# org-profile-description: <reason>
# org-community-health-repo: <reason>
# org-community-health-content: <reason>
```

A few checks are **opt-in**: listing them declares a *stricter* floor, never an exemption.
Today that is `code-scanning`: the baseline does not require CodeQL (the SAST posture is
golangci plus govulncheck), but a repo that parses untrusted input can require it of itself,
and `limen github fix` then configures default setup.

## What the baseline asserts

- **Security features on.** Secret scanning with push protection, Dependabot alerts, private
  vulnerability reporting: GitHub's own defenses, and no repository for which "off" is right.
  Code scanning is deliberately not required ([linting Go](./linting-go.md)); a repo may opt
  in through the exceptions file.
- **One dependency bot.** Dependabot *alerts* stay on, as the vulnerability signal Renovate
  consumes; Dependabot *security updates*, the toggle that has Dependabot open PRs, is
  **off**. Two bots do not coordinate (one advisory, two PRs), and Dependabot ignores every
  convention the repository sets: no release cooldown, no aqua preset, its own branch and
  commit shape. The shared Renovate preset enables `vulnerabilityAlerts` explicitly, which
  makes Renovate's fix PRs reach `// indirect` Go modules too, so nothing Dependabot would
  have caught goes unraised. A repository that wants Dependabot's PRs anyway declares
  `dependabot-security-updates`.
- **Renovate is processing the repository**, not merely installed on the organization. Every
  configuration check can be green while Renovate skips a repository: it skips forks by
  default under an all-repositories installation, decided from `renovate.json` before any
  preset resolves — four forks once sat silent for three weeks with every audit passing. The
  proof is the open **Dependency Dashboard** issue the app keeps on every repository it
  manages, present whether or not anything is pending; the shared preset turns the dashboard
  on explicitly. A failing verdict with no fix: nothing in the API starts a Renovate job, so
  the message carries the remedy (trigger one from the Mend Developer Portal; for a fork,
  confirm `forkProcessing` and that the job is not disabled). A repository whose first run
  has not happened yet fails too: not yet true and false deserve the same red. A self-hosted
  Renovate that keeps no dashboard declares `renovate-processing`.
- **Actions hardened, and on.** The default workflow token is read-only, workflows cannot
  approve pull requests, and the allowed-actions policy is restricted: GitHub-owned actions
  plus an explicitly pinned allowlist, never "all". Actions switched off at the repository
  fails the same check: the required checks can then never run, and a pull request waits on
  an empty check suite with nothing naming the cause (forkcloser/xz, two hours of it). This
  mirrors the canonical workflows' own construction: one SHA-pinned first-party action,
  everything else through aqua and `just`.
- **Features off unless used, issues on.** Wiki, projects, discussions off: documentation
  lives in the repository, issues are the tracker — so issues themselves must be on, or
  `SUPPORT.md` and the org-wide issue forms point at nothing. An exception either way is
  declared.
- **The agents team has write.** Coding agents contribute through a dedicated account that
  holds write on every repository through the organization's `agents` team
  ([identity and keys](./identity.md#per-repository)). A repository created without the
  grant is one the agents can read but not push to, and nothing says so until a push fails
  on a 403. `check` reports it; `fix` grants it — the one team grant the baseline applies
  itself, additive only: write for the canonical team, never a removal, never a person. The
  team and its members stay human work: a missing team is a failing verdict with the
  commands printed, never a fix.
- **The merge doctrine and the rulesets**, below.

## Mainline doctrine: pull requests always

The decided merge model, enforced by the repository settings and the `limen:main` ruleset:

- **Merge commits are allowed, and linear history is not required.** This reverses the
  original rule ("merge commits are disallowed, history reads as a sequence of reviewed
  changes"), which lost the argument to a GitHub constraint: GitHub's rebase merge
  *recreates* commits, rewriting committer and SHA, so it always produces unsigned commits,
  and GitHub disables that button on a branch requiring signatures. Linear history forbids
  merge commits. Squash was all that remained, and it collapses every multi-commit pull
  request to one commit. Of the three methods, exactly one preserves a pull request's
  history:

  | Method | Commits | Signatures |
  |---|---|---|
  | Squash | destroyed | one new commit, GitHub-signed |
  | Rebase | preserved | **destroyed** — rewritten, unsigned |
  | Merge | **preserved verbatim** | **preserved** — nothing is rewritten |

  A merge rewrites nothing: the branch's commits land with their own SHAs and their author's
  signature, under a merge commit GitHub signs itself. Braided history is the price, and the
  cheaper one: bisect still works, and the claim that every mainline commit was individually
  CI-tested was never quite true under rebase merges either. `rebase` stays in the allowed
  list, inert while signatures are required. If the bubbles become unreadable, the mitigation
  is `strict_required_status_checks_policy` (branches up to date before merging:
  semi-linear history at the cost of serializing every merge behind a rebase), off by default
  for exactly that reason.
- **Pull requests, always, and one required approval.** The approval is not about review
  quality on a solo project; it is what makes "a bot cannot land code on `main`" true rather
  than written down. Every write-level identity — the agent account, and every GitHub App,
  since `contents: write` is the permission GitHub's merge endpoint takes — can otherwise
  open a pull request, wait for its own green gate, and merge it with no second party.
  GitHub refuses to let an author approve their own pull request, so one required approval
  means no single identity both proposes and lands. The repository-admin bypass keeps the
  cost off the human, who cannot get their own pull request approved on a solo project;
  admin is a role no App and no write-only account holds. **An approval covers the head it
  was given on**: the ruleset dismisses it on the next push, since an approved pull request
  merges itself and a push after the human looked would otherwise land unread; the author
  asks again with the new head. The same holds for Renovate's branches, which the preset
  rebases on every `main` commit: each rebase drops the reviewing session's approval, and
  the sweep gives it again at the rebased head. Force pushes and branch deletion on the
  default branch are blocked.
- **Merges wait for green CI.** The `limen:main` ruleset carries required status checks,
  without which auto-merge (and a hasty human) would merge on red. A fresh ruleset requires
  `gate` — the job in the canonical `ci.yaml` that `needs` the shared lanes (the `limen`
  call to the pinned `limen-verify.yaml`: every verify leg, fuzz and tools) and any job the
  project adds, and fails unless all succeeded — and, where the default branch carries
  `security.yaml`, the `security` check beside it (**Security**, below): what the canonical
  workflows report, read from the default branch, never assumed from the seed. The check
  names stay project-owned, so reconciliation preserves what a repository declared, with one
  exception: canonical names follow the workflows. A ruleset still naming the matrix legs
  (`verify (…)`) on a repository whose `ci.yaml` carries the gate job is drift, and
  `limen github fix` moves it onto `gate`: a leg the matrix dropped is a check nothing
  reports, and the pull request waits on it forever. `gate` alone on a repository whose
  default branch carries the security lane is moved onto `gate` and `security`, and
  `security` required where no lane reports it is moved back. A repository without the gate
  job keeps its legs, and gets no fresh `limen:main` until it has one: the fixer never
  creates a ruleset that requires a check nothing reports (a pre-gate repository reached by
  the `-all-repos` sweep was exactly that, and every pull request on it waited forever).

  The single gate is deliberate. Branch protection names contexts as *strings*, so requiring
  the matrix legs directly would bake one repository's runner list into every ruleset; a
  project with a different matrix then waits on checks nothing will report, and the symptom
  is the worst kind: "Expected — Waiting for status to be reported" with nothing red to fix.
  One stable name decouples them. The gate job is written with `if: always()` and asserts
  `needs.limen.result == 'success'`, and each project job's, explicitly: without `always()`
  a failed dependency *skips* the gate rather than failing it, and a skipped required check
  does not block a merge.

  <a id="what-the-matrix-proves"></a>
  **What the matrix proves.** CI verifies that everything works on every platform, which is
  two things. The *code* is verified for every platform by the per-platform analysis legs,
  which cross-compile every GOOS/GOARCH pair from whichever host runs them; one leg would
  cover that half. The *commands* that do the verifying work on every platform — `just lint`
  and `just test`, whole, under git-bash on windows, on arm64 and x64, with the pinned tools
  resolving natively — and that half is what the matrix exists for. So a lane is never
  skipped on a host where its tool is slow, emulated or awkward: a windows leg that runs
  shellcheck emulated at six times linux's time is proving the developer environment works
  there; one that skips it has stopped proving anything about windows. Speed comes from
  caches, never from proving less. The runner images are part of what is proven, so they are
  not dependencies to bump: the shared Renovate preset turns off the runner-image proposals
  (`ubuntu-24.04` to `26.04`, `macos-15` to `26`); the matrix moves when a platform target
  moves, as a reviewed change to the canonical seed, while the actions the workflows call
  keep being bumped like any dependency.

  <a id="fuzz"></a>
  **Fuzz.** The shared lanes carry a `fuzz` job: one linux leg running `just do test go
  fuzz`, a short coverage-guided fuzz of every `Fuzz*` target ([recipes](./recipes.md)).
  One leg, not the matrix: fuzzing explores the same code from the same corpus wherever it
  runs. The job caches the generated corpus between runs (`GOCACHE/fuzz`, keyed on the fuzz
  sources) so a short budget compounds into depth, uploads any crasher under `testdata/fuzz/`
  as an artifact, and feeds `gate`, so a crasher blocks a merge like a failing test. Safe
  everywhere: the recipe reports "no Fuzz* targets" and passes where there are none.

  <a id="security"></a>
  **Security.** The verdicts that move without the tree are not in `ci.yaml` at all: the
  vulnerability scans, and the link check, whose verdict is the web's. A canonical
  `security.yaml`, content-pinned like `limen-verify.yaml`, runs the project's `just
  security` (the shared `do::security::default` plus whatever scans the project adds) on one
  linux leg, on every push and pull request and once a day. Apart on purpose: a linter's
  verdict is a function of the tree, a scan's of a database that moves without it, so inside
  `ci.yaml` a new advisory would turn `gate` red on a pull request that changed nothing near
  it. In its own lane the red is one check that says what it is, the schedule is how `main`
  learns of an advisory between pushes, and the answer is the dependency bump Renovate
  opens, never a code change on whichever pull request was open. The check is required in
  its own name where the lane exists: a vulnerability reachable from a pull request blocks
  its merge, and a red inherited from `main` is explained on the pull request until the bump
  lands. Safe everywhere: the recipe says "no go.mod" and passes where there is no Go module.

  <a id="tools"></a>
  **Tools.** A `tools` job, one linux leg, runs a real `aqua install` where every other job
  only links. Links verify nothing, so without it an asset missing from a release, or failing
  its checksum, merges green and fails months later at first use. The job caches aqua's
  package store keyed on the exact pins, so an unchanged pin set costs seconds and a changed
  one is always a real install; it feeds `gate`. (The Go-built tools are `tools/go.mod`
  directives; the verify legs build them.)

  **Caches on the verify legs.** Each leg restores the same package store, per architecture
  since it holds native binaries, and the Go build, module and linter caches keyed on the
  pinned go's version and every `go.sum`; the per-platform analysis legs otherwise compile
  the whole module graph five times, cold, on every run. Fallback keys are always taken: the
  build cache is content-addressed, the module cache checksum-verified, and a pin a bump
  moved still downloads and verifies at first use, so a stale restore only costs what it
  cannot reuse.

  **Migration.** `ci.yaml` is seeded once and the project's own afterwards, so repositories
  created before the gate, fuzz and tools jobs existed do not have them. Their rulesets keep
  working (reconciliation preserves existing contexts), but the gate job is added to
  `ci.yaml` *before* the ruleset is moved onto `gate`: the wrong order reproduces the very
  failure this design removes. The fuzz job is adopted the same way, by hand, from the
  canonical workflow: a repository with `Fuzz*` targets and no fuzz job is running its
  fuzzers nowhere.
- **Every commit is signed.** The `limen:main` ruleset requires signatures, so an unsigned
  commit cannot land on the default branch. Not the same guarantee as the DCO:
  `git-validation` checks that a `Signed-off-by` trailer is present, and a trailer is a line
  anyone can type under any name; a signature binds the commit to a key. Both are required —
  the trailer is the legal assertion, the signature the proof — and we sign with SSH keys
  backed by hardware tokens.
- **Every author is listed.** `.lint-signers` names everyone who authors commits in the
  repository, email and SSH key, in git's allowed-signers format, and `lint commits` fails a
  commit in its range not signed by a key listed for its committer. Not a security boundary
  (a pull request can edit the file) but a forcing function: an author lists themselves in
  their first pull request. Merges author nothing and are skipped, as are GitHub Apps'
  commits (Renovate, the checksum-update App), which GitHub signs. A commit made in the web
  UI is signed by GitHub too, and fails: editing in a browser is not a way to contribute.
- **Squash commits default to the pull request title and body**, merged branches are
  deleted automatically, auto-merge is allowed (the reviewing session arms it with its
  verdict, [coding agents as contributors](./agents.md)), and web-UI commits require
  sign-off — belt and braces for a path `lint commits` already refuses.

Requiring signatures has two sharp edges:

- **GitHub refuses a squash merge of a pull request you did not author** into a
  signature-required branch: it signs the squash commit on the author's behalf, and only for
  the author. A bot-authored pull request is merged by that bot (Renovate merges its own
  through the API, which is why auto-merge keeps working).
- **Every commit on a pull request branch must itself be signed.** The merge commit lands
  the branch's commits *verbatim*, so the signatures rule judges each; rebase is disabled
  while signatures are required, and squash is refused to everyone but the author. One
  unsigned commit — a workflow's plain `git commit` fix-up on a bot branch — leaves a pull
  request with no merge path at all. Renovate's commits are safe (GitHub's API signs), and
  the canonical `update-aqua-checksum` workflow commits through the GraphQL
  `createCommitOnBranch` mutation for exactly this reason: GitHub signs the mutation's
  commits, where a token-authenticated `git push` signs nothing.

The `limen:tags` ruleset restricts `v*` tag creation, update and deletion to repository
admins: the tag push is the release button ([releasing](./releasing.md)), and the ruleset
names who may press it.

Both rulesets are canonical objects owned by limen: created when missing, reconciled when
drifted, recognized by name. Local weakening is drift and gets reset by `limen github fix`.
The bypass list is compared too, and must be exactly the repository admins: with none, the
admins' own release tag push and merges are refused; with another actor, that actor can
press the button. GitHub shows the list only to an admin's token, so under any other the
check reports it unverifiable rather than guessing.

## Fix semantics

`limen github fix` prints the full plan first — one line per change, current → desired — and
applies only on consent (interactively, or `-yes` for unattended use). Repairs are minimal
writes; the advisory class is never touched: nothing that could lock a person out or break a
credential is changed by a tool. After applying, it re-audits and reports the
**post-state**, not the intent. A change that applied without error and still fails on the
re-audit is a write GitHub accepted and ignored — a feature it gates by plan on that
repository, such as auto-merge on a private repository in a Free organization — and the
finding says so, with the ways out (visibility, plan, a declared exception). It stays a
failure. When the gate is known before the write (an organization whose plan the token can
read, and it is Free), the fixer does not plan the write at all: the check fails up front
and says why, and the plan never promises "→ compliant" for it; thirteen private
repositories in one Free organization once got that promise and the same thirteen failures
a second later.

## Authentication

All GitHub access goes through the aqua-pinned `gh` CLI: `gh auth` owns identity, limen
never sees a credential, and the same invocation works on a laptop and in CI (`GH_TOKEN`).
Reading most of the security settings needs a token with repository administration read
access; below that, findings degrade to `unverifiable`, which fails the check rather than
faking compliance.

One call is the exception, on purpose: the lookup of the update-App's bot user id, which
`limen check` needs to verify `gitIgnoredAuthors` in `renovate.json`. It reads the public
users endpoint directly, with no credential: `gh` refuses to run without a login, and the
verify legs carry none, so a lookup through `gh` would resolve on a laptop and not in CI —
the same tree, red in one place and green in the other, the one thing `check` must never
do. The endpoint honors `GITHUB_API_URL`, the variable Actions exports and an Enterprise
host sets.

## Organization level

`limen github check -org <name>` (and `fix -org <name>`) audits the organization's own
settings, the layer that decides what every *new* repository is born with. The same floor
semantics, verdict classes and exceptions file apply (org runs read the exceptions from the
working directory, canonically the org's `.github` repository). The catalog:

- **Membership floor**: members' default repository permission capped at read, no
  member-created public repositories or public Pages sites, no forking of private
  repositories, org-wide web-commit sign-off (the DCO switch every repository inherits). All
  fixable in one consolidated update. Two floors the API can read but not write — members
  changing repository visibility or deleting repositories — report as advisories, and the
  2FA requirement is advisory by nature: enabling it evicts members without 2FA.
- **The owner roster** is a standing advisory until you declare who the owners are meant to
  be, in `.lint-github.yaml`:

  ```yaml
  org-admins: apostasie is the sole owner
  ```

  Despite living among the exceptions, this one is **not** an escape hatch: the reason is
  *parsed* — every login-shaped token in it is matched against the live roster on every run —
  so the declaration keeps enforcing after it is written. An owner not named in it re-raises
  the finding, which is the point: someone becoming an org owner is exactly the event to hear
  about. A reason that names nobody silences nothing. Removals only leave a stale name
  behind, which review catches on the next edit.
- **Org-wide Actions policy**: the org twin of the per-repository hardening, so new
  repositories are born hardened: Actions restricted to GitHub-owned (never "all"),
  SHA-pinned `uses:` required org-wide, read-only default workflow token, no PR approvals
  from workflows, fork-PR approval for all first-time contributors, a self-hosted-runner
  inventory (baseline: none).
- **Security configuration**: a default code security configuration exists for new
  repositories (the mechanism GitHub replaced the legacy per-org security fields with).
  Advisory: creating the canonical configuration is a human act.
- **Standing inventories**: installed GitHub Apps, org webhooks (HTTPS + secret + TLS
  verification), org-level Actions secrets (names only), teams, fine-grained PAT grants —
  visible on every audit, so a grant nobody remembers making has nowhere to hide.
- **The agents team, organization-wide**: the team exists, has members, and holds write on
  every repository that is not archived; the org view of the per-repository `agents-team`
  check, so one audit lists every repository created without the grant, and `fix -org`
  grants them in one run. A missing or empty team is reported, never created or filled.
- **Renovate installed**: the one GitHub App the baseline depends on. Every governed
  repository carries a seeded `renovate.json` and the content-pinned checksum-refresh
  workflow; without the app on the organization none of it runs, and every pin silently
  stops moving. A failing verdict, never auto-fixed: installing a GitHub App is a
  browser-only consent flow. A self-hosted Renovate is declared in `.lint-github.yaml`.
- **The org `.github` repository**: exists, public (GitHub silently ignores a private one as
  a fallback source), and carries the canonical community-health set: `SECURITY.md`,
  `CONTRIBUTING.md` (the DCO terms, where contributors look) and the org profile README.
  Advisory verdicts: creating repositories and authoring policy is human work, and the
  repository's own compliance is `limen check` inside it.

One deliberate absence: **org rulesets**. The per-repository `limen:main` / `limen:tags`
rulesets remain authoritative; migrating them to org-level rulesets is phase 4 of
[`design/LIMEN-GITHUB.md`](../design/LIMEN-GITHUB.md), with the scheduled drift audit. Org
reads need an owner-scoped token: the governed fields are absent from anonymous responses,
and absent classifies as `unverifiable`, never as passing.

## Enforcement

`limen github check [-repo owner/name]` / `check -org <name>` — or, through the recipes,
`just do lint github [args]` and `just do fix github [args]` — verifies all of the above
against the live target and exits non-zero on any failure, advisory or unverifiable finding.
The same command on a laptop and in CI. Settings drift *back* when humans click, so the end
state (in the design plan) is a scheduled audit. See [`../cmd/limen/`](../cmd/limen).

<a id="creating-a-repository"></a>
## Creating a repository

The order matters: two of the rules above are traps for whoever goes first, both hit on the
two repositories created in one day.

1. **Create it empty.** `gh repo create <org>/<name> --private` (or public), no README, no
   license: the files come from `limen bootstrap`, not GitHub's templates.
2. **Run the fixer before anything is pushed:** `limen github fix -repo <org>/<name>`, as
   the human. A fresh repository grants the `agents` team nothing — the bot's effective
   permission is *pull*, and its first push is refused with "correct access rights" — and
   the fixer grants it, with the feature toggles, vulnerability reporting and the
   `limen:tags` ruleset. `limen:main` is the one thing it defers: the ruleset requires the
   `gate` check, an empty repository has no workflow to report it, and the fixer never
   requires a check nothing reports.
3. **The human pushes the initial `main`**, signed, from the bootstrap tree. The first
   commit is the human's, and whoever pushes first sets the default branch. Before the fixer
   deferred `limen:main`, a ruleset created on the empty repository refused this very push
   as a pull request into a `main` that did not exist; only the bypass actor could create
   `main`.
4. **Run the fixer again.** With the canonical `ci.yaml` on `main`, it now creates
   `limen:main`, and from then on everything is pull requests. Until this run `main` is
   unprotected: do it right after the first push, before any branch is opened against it.
5. **Push `main` before any other branch.** Whatever branch reaches GitHub first becomes the
   default; a bot branch pushed first has to be moved off with `gh repo edit
   --default-branch main`, and cannot be deleted until it is.
6. **Description and topics are the human's.** `gh repo edit` answers 404 to the bot (no
   admin), and the fixer reports them as advisories. Set them right after the first push.
7. **`.lint-signers` is the human's enrollment commit.** `limen bootstrap` does not seed it:
   the human's key is theirs to publish. It lands with the first push, with the bot's key
   beside it ([identity and keys](./identity.md)), or is copied from a sibling repository at
   the owner's instruction.

Then the bot works as everywhere: its own branches, pull requests, the fixer's rulesets in
force from the first one.

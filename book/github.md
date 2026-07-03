# GitHub settings

Repository settings are configuration like any other — they gate real security
properties (who can push a release tag, whether a workflow token can write,
whether a leaked secret gets blocked) — yet they live behind a web UI, drift
when humans click, and are traditionally enforced by tribal memory. The
operating principle applies to them unchanged: **every rule is written,
verifiable, and enforceable.** This chapter is the written part;
`limen github check` verifies, `limen github fix` enforces.

```bash
limen github check            # audit the repo's settings (slug inferred from origin)
limen github fix              # plan → consent → apply → re-audit
limen github check -repo owner/name -json
```

## The floor model

The baseline is a **floor**: a repository may be *stricter* than it, never
looser. A repo that disables a merge method the baseline allows is compliant; a
repo that enables one the baseline forbids is not. Settings the baseline does
not name are not judged.

Every check yields one of four verdicts:

| Verdict | Meaning |
|---|---|
| ok | matches or exceeds the floor |
| fail | below the floor — `limen github fix` can repair it |
| advisory | below the floor but **never auto-fixed**: people, credentials, and content (collaborators, deploy keys, webhooks, descriptions) are a human's to change |
| unverifiable | the API cannot answer under the current token — reported distinctly, and it does **not** count as passing: what cannot be verified does not pass |

The authoritative catalog of checks is the tool itself — run
`limen github check` and read the findings; each names its check identifier.
The book carries the reasoning, not a copy of the list.

## Exceptions — `.github/limen-github.yaml`

A repository that genuinely needs to deviate declares it, in a committed file,
with a reason — the escape hatch lives in review, never in a UI click:

```yaml
# Exceptions to the limen github baseline: delta only, one reason each.
wiki: hosts the operations runbook
```

The file is a **delta**: exceptions only, never a full settings copy. Each line
is `check-identifier: reason`. An exempted check reports ok, visibly carrying
the reason; an unknown identifier or a missing reason fails the file itself.

A small set of checks works in the opposite direction — **opt-in**: listing
them declares a *stricter* floor for this repository, never an exemption.
Today that is `code-scanning` (the baseline does not require CodeQL — the SAST
posture is golangci plus govulncheck — but a repo that parses untrusted input
can require it of itself, and `limen github fix` will then configure default
setup).

## What the baseline asserts

- **Security features on.** Secret scanning with push protection, Dependabot
  alerts and security updates, private vulnerability reporting. These are
  GitHub's own defenses; there is no repository for which "off" is the right
  setting. (Code scanning is deliberately *not* required: the SAST posture is
  the per-GOOS golangci run plus govulncheck — see
  [per-language](./per-language.md) tooling; a repo may opt in via the
  exceptions file.)
- **Actions hardened.** The default workflow token is read-only, workflows
  cannot approve pull requests, and the allowed-actions policy is restricted —
  GitHub-owned actions plus an explicitly pinned allowlist, never "all". This
  mirrors the construction rules of the canonical workflows themselves (one
  SHA-pinned first-party action, everything else through aqua and `just`).
- **Features off unless used.** Wiki, projects, discussions: documentation
  lives in the repository, issues are the tracker. A repo that wants one
  declares the exception.
- **The merge doctrine and the rulesets** — below.

## Mainline doctrine: linear history, pull requests always

The decided merge model, enforced by both the repository settings and the
`limen:main` ruleset:

- **Merge commits are disallowed; squash and rebase are allowed.** Every commit
  on the default branch is buildable, bisectable, and revertable as a unit;
  DCO sign-offs survive rebase; history reads as a sequence of reviewed
  changes, not a braid.
- **Pull requests, always — no exceptions.** Zero required approvals is
  acceptable while a project is solo: the pull request is the audit trail and
  the CI gate, not (only) the review venue. Force pushes and branch deletion
  on the default branch are blocked.
- **Merges wait for green CI.** The `limen:main` ruleset carries required
  status checks, without which auto-merge (and a hasty human) would merge on
  red. The check *names* are project-owned — they follow the project's CI
  shape — so reconciliation preserves them, exactly like the standard-registry
  ref inside the pinned aqua sections; a fresh ruleset starts from the
  canonical CI matrix.
- **Squash commits default to the pull request title and body**, merged
  branches are deleted automatically, auto-merge is allowed (Renovate merges
  green PRs), and web-UI commits require sign-off — DCO holds even for edits
  made in a browser.

The `limen:tags` ruleset restricts `v*` tag creation, update, and deletion to
repository admins: the tag push is the release button (see the release lanes
in [the recipes chapter](./recipes.md)), and the ruleset names who may press
it.

Both rulesets are canonical objects owned by limen — created when missing,
reconciled when drifted, recognized by name. Local weakening is drift and gets
reset by `limen github fix`.

## Fix semantics

`limen github fix` prints the full plan first — one line per change, current →
desired — and applies only on consent (interactively, or `-yes` for unattended
use). Repairs are minimal writes; the advisory class is never touched: nothing
that could lock a person out or break a credential is ever changed by a tool.
After applying, it re-audits and reports the **post-state**, not the intent.

## Authentication

All GitHub access goes through the aqua-pinned `gh` CLI: `gh auth` owns
identity, limen never sees a credential, and the same invocation works on a
laptop and in CI (`GH_TOKEN`). Reading most of the security settings needs a
token with repository administration read access; below that, findings degrade
to `unverifiable` — which fails the check rather than faking compliance.

## Organization level

Not yet implemented. The org-side plan — member privileges, org-wide Actions
policy, security configurations, app-installation audits, org rulesets, and
the org `.github` fallback repository — lives in
[`design/LIMEN-GH.md`](../design/LIMEN-GH.md).

## Enforcement

`limen github check [-repo owner/name]` verifies all of the above against the
live repository and exits non-zero on any failure, advisory, or unverifiable
finding. It is the same command on a laptop and in CI. Settings drift *back*
when humans click, so the end state (also in the design plan) is a scheduled
audit. See [`../cmd/limen/`](../cmd/limen).

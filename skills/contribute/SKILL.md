---
name: contribute
description: How a coding agent contributes to a farcloser repository — its own branch in its own worktree, signed commits, the full lint and test before pushing, a pull request it owns until the checks are green, and a local main kept fresh. Use whenever the task ends in a commit, a push, or a pull request.
---

# Contribute

The doctrine is `book/agents.md` in limen ("The workflow"); this is the
procedure. The human's branches — `work`, and anything not named after the
bot — are the human's: never commit there unless pairing interactively at the
human's request, and never commit on `main`.

## 1. Start from a fresh main, in a worktree

```
git fetch --prune origin main:main
git worktree add -b claudio/<topic> ../<repo>-<topic> main   # or the harness's worktree tool
cd ../<repo>-<topic>
aqua policy allow aqua-policy.yaml && aqua install --only-link
```

One topic per branch and per pull request. The bot's login is the branch
prefix.

## 2. Commit

- Signed as the bot, with a `Signed-off-by:` trailer as the bot. When the
  change is the human's own work, `--author` the human; the bot stays the
  committer.
- Subject line, blank line, the reasoning. The commit message is the record;
  the pull request description is derived from it.
- Never commit scratchpads (`AUDIT.md` and the like) or generated files the
  repository ignores.

## 3. Green before pushing

Run the whole `just lint` and `just test` — the aggregates, not a single lane.
A lane on one machine misses what CI runs elsewhere: a linux-only package
under a CGO project, a windows leg, a guest build. Fix what fails before
pushing.

Fix a finding; do not silence it. When silencing is the honest answer, silence
the rule, never the linter: `//revive:disable-next-line:<rule>`,
`// #nosec G### -- reason`, `//nolint:staticcheck // SA####: reason`. The lint
recipe rejects `//nolint:revive`, `//nolint:gosec`, a bare `#nosec`, and a bare
`//nolint` (`book/per-language.md`, "silencing a finding").

## 4. Push, open the pull request, own it

```
git c push origin claudio/<topic>             # `git c` drops the sandbox's GIT_SSH_COMMAND
gh pr create --base main --head claudio/<topic> --title "…" --body "…"
gh pr checks claudio/<topic> --watch
```

No `-u` on the push — recording the upstream writes `.git/config`, which the
sandbox denies; name the remote and branch instead.

Read the failed logs (`gh run view --job <id> --log-failed`), fix, push
again. A red check is yours to turn green or to explain in the pull request —
never to leave for the human to find.

Then, and only then — checks green, pull request ready — request the
owner's review; that is how the human learns the work exists, and it is
said once, never on a red pull request. Never on a stacked one either: a
branch cut from an unmerged branch is not mergeable on its own, so it
waits, unrequested, until its base merges and it is rebased down to its
own commits. A requested pull request that turns red has its request
withdrawn (`gh api -X DELETE repos/<org>/<repo>/pulls/<n>/requested_reviewers
-f 'reviewers[]=<owner>'`) until it is green again. The human's comments
on the pull request are the review: address them when pointed there or
when next checking open pull requests.

```
owner="$(gh api "orgs/<org>/members?role=admin" -q '.[0].login')"   # a CODEOWNERS entry, when the repo has one
gh pr edit claudio/<topic> --add-reviewer "$owner"
```

## 5. After a merge

```
git fetch --prune origin main:main
git worktree remove ../<repo>-<topic> && git branch -d claudio/<topic>
```

Rebase every open branch of yours onto the fresh `main`, so the human and you
can both rebase often and cheaply.

## Stacking

Cut a branch from the human's unmerged branch only when nothing else can be
green — the repository's CI is broken on `main` until that branch lands. Say
so in the pull request body ("stacked on #N"), never request review on it,
and once #N merges, rebase it onto `main` so it shows only its own commits —
then it is ready, and then the review request goes out.

## Scope

- Deliver the ask, whole; mention the unrelated, do not act on it.
- A drive-by fix on the way through is fine (a pin, a stale suppression, a
  workflow bug). A campaign — onboarding a legacy repository onto limen, a
  wholesale cleanup of a broken one — needs the human's explicit green light
  first. Measure before moving.
- A red inherited from `main` is explained on the pull request, not fixed in
  it; a file the human is editing on `work` is left alone.
- Keep a queue of the human's instructions across the session; none is
  dropped because another arrived.

## Never

Merge, push to `main`, force-push a shared branch, tag a release. The rulesets
and the release lane enforce these; the skill just says them out loud.

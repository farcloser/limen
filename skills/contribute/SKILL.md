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

## 4. Push, open the pull request, own it

```
git c push -u origin claudio/<topic>          # `git c` drops the sandbox's GIT_SSH_COMMAND
gh pr create --base main --head claudio/<topic> --title "…" --body "…"
gh pr checks claudio/<topic> --watch
```

Read the failed logs (`gh run view --job <id> --log-failed`), fix, push
again. A red check is yours to turn green or to explain in the pull request —
never to leave for the human to find.

## 5. After a merge

```
git fetch --prune origin main:main
git worktree remove ../<repo>-<topic> && git branch -d claudio/<topic>
```

Rebase every open branch of yours onto the fresh `main`, so the human and you
can both rebase often and cheaply.

## Never

Merge, push to `main`, force-push a shared branch, tag a release. The rulesets
and the release lane enforce these; the skill just says them out loud.

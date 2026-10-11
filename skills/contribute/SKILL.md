---
name: contribute
description: How a coding agent contributes to a farcloser repository — its own branch in its own worktree, signed commits, the full lint and test, a local review before anything is pushed, a pull request opened at the reviewed head and owned until the checks are green, and a local main kept fresh. Use whenever the task ends in a commit, a push, or a pull request.
---

# Contribute

The doctrine is `book/agents.md` in limen ("The workflow"); this is the
procedure. The human's branches — `work`, and anything not named after the
bot — are the human's: never commit there unless pairing interactively at the
human's request, and never commit on `main`.

## 1. Start from a fresh main, in a worktree

```
git -C <clone> c fetch --prune origin   # `git c` drops the sandbox's GIT_SSH_COMMAND, which outranks the rig's core.sshCommand
git -C <clone> worktree add --no-track -b claudio/$(date +%Y%m%d)-<topic> .claude/worktrees/<date>-<topic> origin/main
test -d <clone>/.claude/worktrees/<date>-<topic>   # its own command, before anything else runs
cd <clone>/.claude/worktrees/<date>-<topic> && aqua policy allow .aqua/aqua-policy.yaml && aqua install --only-link
```

One topic per branch and per pull request. The bot's login is the branch
prefix, the day it was cut comes next (`claudio/20260906-<topic>`), so a
branch listing reads in date order and a stale one shows its age.

The worktree lives under `.claude/worktrees/` in the clone, where the
harness's own worktree tool puts its worktrees, so the harness can switch the
session into it by path. `.claude` is a required `.gitignore` pattern
(`book/mandatory-files.md`), so the nested checkout stays out of the human's
`git status`.

`--no-track`, because a remote-tracking start point records an upstream, and
that writes `.git/config`, which the sandbox denies (the same denial as `-u`
on a push): the ref lands, the command fails, and the worktree is never
created. Name the branch right at creation for the same reason — `git branch
-m` rewrites the config section and fails the same way. Run the `worktree
add` on its own, with nothing piped after it: a `| tail` makes the pipeline's
status the pipe's, and a `cd` chained on that once fell through into the
human's clone, where the next command rebased `main`. The `test -d` is what
stops the chain when the add did not land.

Inside a worktree the harness isolates the session there, and it refuses any
git command aimed at the clone, `git -C <clone> status` included. A session
already in a worktree, because it started in one or entered one, cannot run
the block above. It cuts the next branch in its own worktree instead, once
the previous branch is pushed: `git c fetch origin`, then `git checkout
--no-track -b claudio/<date>-<topic> origin/main`.

## 2. Commit

The commit message is the record; the pull request description is derived
from it. Sloppy trailers have been caught by the human before; the steps below
are what prevents it.

1. **Name the tree on every git command**: `git -C <absolute worktree path> …`,
   or `cd <absolute path> && …` in the same command. Never rely on the shell's
   current directory — it resets between commands, and a worktree cut from the
   wrong clone once pushed a branch with no shared history to another
   repository.
2. **Write the subject, the body and the co-author trailer; let `-s` add the
   sign-off.** Subject line, blank line, the reasoning in prose, blank line,
   then the one trailer you write, naming the model:
   ```
   Co-Authored-By: <Model> <noreply@vendor.example>
   ```
   Commit with `git -C <path> commit -s -F -` and a heredoc. `-s` appends
   `Signed-off-by:` last, from the configured identity, so it cannot be
   mistyped; never write `Signed-off-by:` by hand — `-s` on top of a
   hand-written one that is not the last line produces two. When the change is
   the human's own work, `--author` the human; the bot stays the committer and
   the sign-off is still the bot's.
3. **Verify in the same command as the commit, before anything is pushed:**
   ```
   git -C <path> log -1 --format=%B | grep -c '^Signed-off-by:'   # 1
   git -C <path> log -1 --format=%B | grep -ciE 'https?://[^[:space:]]*(anthropic|claude)|generated with|-Session:'   # 0 (AGENTS.md, "No links to your tooling"): a link, a banner, a session footer. Every domain the tooling links to is listed — the vendor's and its products' differ — and never the bare domain alone, which the mandated trailer's address carries
   git -C <path> -c gpg.ssh.allowedSignersFile=.lint-signers log -1 --format='%G?'  # G — signed; anything else means the identity or key is wrong: stop and say so
   just do lint commits                                            # passes
   ```
   A wrong answer is fixed with `git -C <path> commit --amend -F -` before the
   push. Amending after a push means `--force-with-lease=<branch>:<old sha>` to
   the bot's own branch, then the checks again.
4. **Never commit scratchpads** (`AUDIT.md` and the like) or generated files
   the repository ignores. A scratchpad lives under `_scratch/` at the
   repository root, which `.gitignore` ignores (`/_scratch`), and nowhere
   else in the tree.
5. **One commit per thing.** Different things, different commits. Iteration
   on the same thing — a review round on the lines you just added, a fix to
   your own commit — is squashed into the commit it amends and the message
   rewritten to tell the whole story once, before the pull request is opened:
   ```
   git -C <path> reset --soft origin/main && git -C <path> commit -s -F -   # one commit, whole message
   git -C <path> c push --force-with-lease=<branch>:<old sha> origin <branch>
   ```
   Your own branch only; a shared branch is never rewritten. A stack of
   fix-ups leaves a message a later commit contradicts.

## 3. Green before the review

Run the whole `just lint` and `just test` — the aggregates, not a single lane.
A lane on one machine misses what CI runs elsewhere: a linux-only package
under a CGO project, a windows leg, a guest build. Fix what fails before
anyone reads the change.

Fix a finding; do not silence it. When silencing is the honest answer, silence
the rule, never the linter: `//revive:disable-next-line:<rule>`,
`// #nosec G### -- reason`, `//nolint:staticcheck // SA####: reason`. The lint
recipe rejects `//nolint:revive`, `//nolint:gosec`, a bare `#nosec`, and a bare
`//nolint` (`book/linting-go.md`, "silencing a finding").

Then commit, and lint the commit itself: `just do lint commits` judges a
range, and a `just lint` run before the commit existed has not seen it. This
is what catches a missing sign-off, a subject over the limit, or a trailer
eaten by a shell variable — in your tree, not in CI.

## 4. The review, in the worktree

Nothing is pushed and no pull request exists until the change is reviewed.
Message the reviewing session three things and end the turn:

```
<absolute worktree path>
base: $(git -C <path> rev-parse origin/main)
head: $(git -C <path> rev-parse HEAD)
```

The reviewing session reads `git diff <base>..<head>` there and answers by
message, in the shape of a review (`book/review.md`): ✅ with the head it
read, or 🛑 and ⚠️ to settle. Answer each finding by message — fix it and
amend (one commit per thing, step 2.5), or argue it with the evidence; a
new head is a new review, say its sha. When the reviewing session has sent
the project manager its line, `reviewed: <org/repo> <branch> <head sha>`,
the head is the one to open; any other head is refused downstream.

## 5. Push, open the pull request at the reviewed head, own it

```
git c push origin claudio/<date>-<topic>             # `git c` drops the sandbox's GIT_SSH_COMMAND
gh pr create --base main --head claudio/<date>-<topic> --title "…" --body "…"
```

Then bind the pull request in your own PR pane and own it to green. You
never arm auto-merge: on a public repository the project manager arms it
once it has matched the head against the review and confirmed green
(below), and an approved pull request merges itself from there; a private
repository is never armed, and the human merges by hand after approving.
You never click merge, never bypass.

No `-u` on the push — recording the upstream writes `.git/config`, which the
sandbox denies; name the remote and branch instead.

The body follows the book's shape (`book/agents.md`, "The shape of a pull
request"), why first and detail last, one fixed marker per section, only the
sections that have content:

```
🎯 Why: <what fails or is missing, or who asked for what; one or two sentences>
🛠️ What: <what changes, by file or component>
🧪 How: <how it fixes or satisfies, and how that was verified; mark each claim V or U>
  1. <commit subject>: <what this commit does for the why>. V: <where or how>
  2. <commit subject>: <…>. U: <what could not be verified, and why>
💡 Follow-ups: <left for later, non-blocking, with the reason>
📝 Notes: <stacked on #N, a deliberate hold, a red inherited from main>
📎 Annex: <measurements, tables, logs, history; last, so the rest fits one screen>
```

The description is drawn from the commit messages and does not repeat the
diff. With several commits, 🧪 is one numbered line per commit, in history
order and keyed by subject (a sha goes stale on every amend), so the
description and the history are the same list. A one-line
change needs 🎯 and 🧪 and nothing else; never write a section to look
complete.

The title says what the pull request lands, and stays true: a version
bump names the version it pins, and when a later push changes what the
pull request lands (a newer version, a narrower scope), the title and the
body are updated with it (`gh pr edit --title … --body-file …`).

The PR pane says when the checks finish; a session does not poll CI. On
red, read the failed logs (`gh run view --job <id> --log-failed`) and sort
the red: one that needs a redesign goes back to step 4, a new review; an
environmental one (a links `503`, a flake whose cause is known) is said to
the reviewing session, which sends the project manager `bypass-safe:
<org/repo> <branch> <sha> <run URL>`. A red check is yours to turn
green or to have named — never to leave for the human to find.

Message nobody about the pull request. The project manager finds it on
GitHub itself, matches the branch and head against its ledger of
`reviewed:` lines, confirms the checks, arms auto-merge (a public
repository only), and binds the pull request in its own PR pane, which is
the human's approval queue; a head it has no line for is refused as
`UNREVIEWED:` for the human to decide, and a bound head that moves is
disarmed the same way. The human approves on GitHub, and auto-merge
merges: nothing more to do but the after-merge step. A push after the
approval drops it (the ruleset dismisses stale reviews) and the whole flow
runs again from step 4. No review request, no review comment, no message
at open: review discussion lives in the sessions. The human's comments on a
pull request, when there are any, are addressed when pointed there or when
next checking open pull requests.

A pull request that is green but deliberately held is said to be held, and
why, so it is not taken for forgotten.

In anything public on GitHub — a pull request title or body, a comment, a
commit message — the human is `@apostasie`, by handle, never "the owner" or
"the human". Those are session words; GitHub has a name for the person.

## 6. After a merge

These commands aim at the clone, and from inside a worktree the harness
refuses every one of them (step 1). A session that entered its worktree with
the harness's EnterWorktree leaves it first, with ExitWorktree keeping the
worktree. ExitWorktree only leaves a worktree EnterWorktree made in the same
session, so a session that started in its worktree stays there. It fetches,
rebases its open branches, and says the clone's `main`, the merged branch and
the worktree are left for the human to prune.

```
git -C <clone> c fetch --prune origin
git -C <clone> merge --ff-only origin/main   # the clone has main checked out, so a main:main refspec is refused there; on any other branch, `c fetch origin main:main`
git -C <clone> worktree remove .claude/worktrees/<date>-<topic> && git -C <clone> branch -d claudio/<date>-<topic>
```

Rebase every open branch of yours onto the fresh `main`, so the human and you
can both rebase often and cheaply.

## Stacking

Every pull request targets `main`; never open one against another pull
request's branch, and never merge one into it. A change that cannot be green
without an unmerged one is either a layer of a GitHub native stack, or a
branch with no pull request yet, opened against `main` once the other has
merged. Say what it stacks on in the body ("stacked on #N").

A stack is reviewed and merged whole. Every layer is held to `main`'s
ruleset at once: one unsigned commit, or one layer without its approval,
refuses the whole merge and nothing lands. So:

- Each layer goes through steps 4 and 5 on its own: reviewed in the
  worktree, opened at its reviewed head. The human approves each layer; GitHub refuses auto-merge on a stack, so the
  project manager merges it once, from the top, through GitHub's
  asynchronous merge API, when every layer is green and approved; the
  layers land as one merge commit on `main`.
- If a lower layer merges alone, the layer above ends in one of two
  states. The merge deleted the branch (the baseline's setting): GitHub
  retargeted the layer onto `main` and rebased it as commits it committed
  and signed itself, which `just do lint commits` rejects. The branch
  stayed: the layer still has it as its base, and deleting it by hand
  closes the layer. Recover the same way from either, retargeting first
  where GitHub did not (`gh pr edit <n> --base main`): rebase your own
  branches onto `main` locally, re-signing them, and force-push with lease.
- On a layer, `lint commits` checks everything since `main`, the layers
  below included. A red low in the stack reddens every layer above it; fix
  it in the layer that owns the commit.

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
- No GitHub issues unless the human asks for one. What belongs to another
  repository goes to the session that owns it, as a message (what, why,
  where, the run or pull request URL); it fixes and opens the pull request.
  A limen defect goes to the limen session the same way.

## Never

Merge by hand or bypass a ruleset, push to `main`, force-push a shared
branch, tag a release, comment a review onto a pull request, open a pull
request at a head nobody reviewed. The rulesets, the release lane and the
queue enforce these; the skill just says them out loud.

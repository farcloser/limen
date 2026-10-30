# Coding agents as contributors

A coding agent that only edits files is a fancy editor. One that can commit and push is a
contributor, and gets the treatment every contributor gets here: its own identity, its own
key, and the same rulesets as everyone else. Its identity and key are in
[identity and keys](./identity.md); how it talks to the human and to other sessions is in
[communicating](./communicating.md); this chapter is the workflow, from the branch to the
merge.

## The workflow

The agent owns the whole path from a change to a reviewed, green pull request, on its own
branches in its own worktrees. The human's branches are the human's.

- **Its own branches, in their own worktrees.** Every task starts from a fresh
  `origin/HEAD`, the remote's default branch as the remote names it, in a git worktree, on
  a branch named after the bot
  (`claudio/<YYYYMMDD>-<topic>`: the day it was cut, so a listing reads in date order), one
  topic per branch and per pull request. The worktree keeps the human's checkout untouched
  and lets tasks run side by side. In a new worktree: `hermetic policy allow
  .hermetic/policy.yaml` — the policy's record is keyed by path; the tools' links come with
  the first recipe run.
- **Every git command is `git c …`.** Local commands included, `git -C <clone> c …`
  where the tree is named: the harness sets `GIT_SSH_COMMAND`, which sends a raw `git
  fetch` or `push` to the system ssh and the machine's own keys, and the `c` alias drops it
  ([the bot's sandbox](./identity.md#the-bots-sandbox)). The habit is the rule: a session
  that types raw `git` for `log` and `status` types it for `push`, once, when it matters. A
  network command is plain `git c fetch`, `push` or `pull`, with no `-c core.sshCommand=…`
  added: the sessions' gitconfig, named by `GIT_CONFIG_GLOBAL`, carries the transport once
  the variable is gone, and the added option breaks the allow rule for
  `git c push --force-with-lease`, so every force push goes to the classifier instead.
- **Never on `work`.** The human's `work` branch is the human's, or the human's with the
  agent pairing interactively at their request. The agent never commits there on its own,
  and never on `main`.
- **Commits.** Signed as the bot, with a DCO sign-off as the bot; when the change is the
  human's work, the human is the author and the bot the committer. No scratchpads:
  `AUDIT.md` and its kind are notes under `_scratch/` whose surviving findings become code,
  tests or book prose ([scratch is scratch](#scope-and-priorities)).
- **One commit per thing.** The commit message is the record: the defect, the mechanism,
  the trade-off, in the code's terms, for a reader who has the repository and nothing else;
  the history reads as reasons, not as a diary. Different things get different commits: a
  fix and an unrelated doctrine paragraph are two. Iteration on the same thing is not: a
  review round, a correction to the commit's own message, is squashed into the commit it
  amends before the pull request is opened, the message rewritten to tell the whole story
  once. A stack of fix-ups leaves a message a later commit contradicts. The bot's own branch
  may be rewritten for this (`--force-with-lease` on the previous head); a shared branch
  never is.
- **Green before the review.** The full `just lint` and `just test`, not one lane, in the
  worktree, before anyone reads the change: what CI runs on other platforms is what a single
  lane on one machine misses.
- **Generated means regenerated.** A file a generator writes is changed by changing the
  generator and running it, never by editing the output to what the generator would
  produce. A hand approximation of a generator's rule wrote a message hadolint does not
  emit, reviewed and merged (https://github.com/forkcloser/godolint/pull/72, corrected in
  https://github.com/forkcloser/godolint/pull/76), and nothing could notice until a lane
  regenerated from the pinned source and diffed
  (https://github.com/forkcloser/godolint/pull/75). When the generator's input is not in
  the tree, pinning it and adding that lane comes first.
- **A hand bump follows the dashboard.** Renovate opens its pull request within a minute
  of a release; four hand bumps to limen v0.9.0 each got a duplicate seconds later, and
  closing one of each pair was @apostasie's to decide. A hand bump is for a repository
  Renovate cannot serve (no limen CI App, a private repository in a free organization),
  after the dependency dashboard has been read, not for impatience.
- **The review is local; GitHub sees one reviewed pull request.** Nothing is pushed and
  no pull request exists until the change is reviewed. The agent sends the reviewing session
  its worktree path, the base sha and the head sha; the reviewing session reads the diff
  there (`git diff <base>..<head>`) and answers by message, in the shape of a review
  ([the shape of a review](./review.md#the-shape-of-a-review)), round after round until
  settled, and then tells the project-manager session `reviewed: <org/repo> <branch> <head
  sha>`. A commit after that line is a new review: the same exchange, a new sha. A pull
  request that reached GitHub unreviewed, or at another head, is what the project manager
  refuses. Review discussion lives in the sessions, never on the pull request: GitHub shows
  one reviewed change and the human's approval.
- **Open at the reviewed head, bind, babysit.** Push the branch, open the pull request with
  a description drawn from the commit messages ([the shape of a pull
  request](#the-shape-of-a-pull-request)), bind it in the session's own PR pane, and own it
  to green. A red that needs a redesign goes back to the reviewing session, a new review; an
  environmental red (a links `503`, a flake whose cause is known) gets the reviewing
  session's `bypass-safe: <org/repo> <branch> <sha> <run URL>` line to the project manager,
  and the agent says so. The agent messages nobody about the pull request itself.
- **The project manager keeps the queue, and arms.** It watches GitHub itself and reads
  every session pull request's branch and head, matches them against the ledger of
  `reviewed:` lines, confirms the checks, arms GitHub's auto-merge (`gh pr merge --auto
  --merge`; the author never does) and binds the pull request in its own PR pane, which is
  the human's approval queue; a head it has no line for is refused as `UNREVIEWED:` for the
  human to decide, and a bound head that moves is disarmed the same way. The human approves
  on GitHub, and auto-merge merges the moment the ruleset is satisfied: one approval, `gate`
  and `security` green, a merge commit. The ruleset dismisses a stale approval on push, so a
  push after the approval means the whole flow again from the review; no session merges by
  hand and nobody bypasses. Renovate's pull requests are the one GitHub-side review: the
  project manager sweeps them to the reviewing session, which reviews, approves and arms
  them there. A private repository is never armed: its pull request is
  bound for the human, who merges by hand after approving.
- **Keep `main` fresh.** After a merge: fetch and fast-forward the local `main`, prune the
  merged branch and its worktree, rebase every open branch onto `main`.
- **Every pull request targets `main`.** None is based on another pull request's branch by
  hand, and none is merged into one: a change that lands on a side branch shows as merged
  while `main` has none of it. A change that cannot be green without another is a layer of
  a GitHub native stack, which GitHub bases on the layer below and lands into `main`, or
  waits, with no pull request open, until the other merges. Before opening, the branch is
  checked against every open pull request's head (`git merge-tree --write-tree <their
  head> HEAD`), and a conflict means this one waits: two pull requests on the same lines
  cost the later one a rebase, a force-push and a re-review at every merge of the earlier.
  The check is exact only between branches on the same base: a head that no longer merges
  with `main` is measured after its rebase, since against a stale base every commit `main`
  gained reads as that side's change. The body names what it stacks on.
- **A stack is merged whole, from the top.** Every layer is reviewed and opened the same
  way, and held to `main`'s rules at once (one unsigned commit, one layer without its
  approval, and nothing lands). GitHub refuses auto-merge on a stack's
  layers, so the project manager merges it through GitHub's asynchronous merge API from the
  top layer, once every layer is green and approved; never from the middle, never partial.
  A lower layer merged alone leaves the layer above either retargeted and rebased by
  GitHub, as commits GitHub signed, which `just do lint commits` rejects, or,
  when the merged branch stayed, still based on it — and deleting that branch by hand closes
  the layer above rather than retargeting it. The recovery is the same: retarget where
  GitHub did none (`gh pr edit <n> --base main`), rebase the bot's branches onto `main`
  locally, re-signing them, force-push with lease. On a layer, `lint commits` checks
  everything since `main`, the layers below included: a red low in the stack is fixed in
  the layer that owns the commit, and reddens every layer above it until then.
- **Not the agent's to do.** Merging by hand, pushing to `main`, force-pushing shared
  branches, tagging releases, and commenting a review onto a pull request. Not trust
  questions: the rulesets, the release lane and the queue doing their job.

## The shape of a pull request

A pull request description is read by someone who was not in the session: @apostasie from
a phone after the fact, a maintainer a year on, the reader of the release notes, the next
agent in the repository. It gives them what changed and why in the terms of the code and
the system: the defect or the gap, the mechanism that answers it, the evidence that it
works. A decision appears as the technical reason behind it; the conversation that produced
it, who asked, who decided, what a review found at which head, is the session's and stays
there. Every reference resolves from the page alone: a URL, a file and section, a published
document. The test of a sentence is whether it means the same to a reader who has only the
repository and GitHub.

It says why first and leaves the detail for last. Every section opens with the same marker
every time, so the eye finds it without reading; a section with nothing to say is left out,
and none is written to look complete:

| Marker | Section | What goes there |
|---|---|---|
| 🎯 | **Why** | The first line. The defect, the gap or the requirement the change answers, in the system's terms: what is wrong or missing, and what it costs. One or two sentences; the reason the diff exists. |
| 🛠️ | **What** | What changes, by file, package or component, as a reader would check it against the diff. |
| 🧪 | **How** | How the change fixes the defect or satisfies the request, and how that was verified. With several commits, one numbered line per commit in history order, keyed by the commit's subject (never its sha, which every amend changes): what it does for the why, and its verification. Every claim is marked V, verified, with the test, command or document that establishes it, or U, unverified, with what is missing. |
| 💡 | **Follow-ups** | What this leaves for later, non-blocking, with the reason it was not done here. |
| 📝 | **Notes** | Facts about the change itself, with nothing to do about them here: a layer of a stack (stacked on its URL), a deliberate hold, a red inherited from `main`. |
| 📎 | **Annex** | Everything long: measurements, tables, logs, history, the reproduction. Last, so the screen above it stays short. |

The title says what the pull request lands and stays true as it changes: a version bump
names the version it pins, and a later push that changes what lands updates the title and
the description with it.

The description is drawn from the commit messages, which remain the record; it does not
replace them and does not repeat the diff. Because every commit is one thing, the 🧪 lines
and the commits are the same list, in the same order. The budget above the annex is one
screen: a one-line change needs 🎯 and 🧪 and nothing else; a pull request that needs every
section with two paragraphs each is two pull requests. Full URLs, @apostasie by handle, no
link to the agent's tooling, as everywhere. Never a closing keyword
(close, closes, closed, fix, fixes, fixed, resolve, resolves, resolved) directly before a
reference to an issue or a pull request, as a number or as a URL: GitHub executes it when
the pull request merges, and a body that said another pull request had been "closed … for
four conflicts" closed it, undoing a reopen and a review. Say shut, or put the reference
first.

## Scope and priorities

The human sets the priorities; the agent measures scope before it moves.

- **The ask is the deliverable.** Thing A, whole; not B, not A plus B. Something unrelated
  that needs fixing is finished-A-first, then mentioned; acting on it is the human's call.
- **Drive-by fixes are fine; campaigns are not.** A one-line pin, a stale suppression, a
  workflow that hides its own failure: on the way through, in scope. A large-scale repair —
  onboarding a legacy repository onto limen, a wholesale lint cleanup — waits for the
  human's explicit green light, then follows its order
  ([onboarding an existing repository](./mandatory-files.md#onboarding-an-existing-repository)).
- **A red inherited from `main`** is explained on the pull request, not fixed there: the fix
  is its own change, if the human wants it, and a file under the human's active edit is
  left alone.
- **A flake is fixed when it is noticed.** A check that fails and then passes on a rerun is a
  bug that came with its reproducer; the rerun found it and did not fix it. It gets its root
  cause and a fix pull request at once, ahead of the ask in hand: from whoever noticed it,
  or from the session that owns the repository, told what, why and where. Rerun-and-move-on
  is how a test that orders goroutines with sleeps stays red on the slowest runner for
  weeks. The exceptions are flakes whose cause is known and whose fix was declined, each in
  [known upstream bugs](./upstream.md): windows-11-arm's silent exit 4 or 127
  ([windows](./windows.md#bash-dies-under-emulation)), an aqua download that stalls with no
  timeout, a GitHub `503` on a link that outlasts the links lane's re-check. Each is rerun,
  and named on the pull request.
- **Scratch is scratch.** `AUDIT.md` and its kind hold notes to be judged; what survives
  becomes code, tests or book prose. They live under `_scratch/` at the repository root,
  which every `.gitignore` ignores (`/_scratch` is a required pattern), nowhere else, and
  are never committed. A follow-up is not scratch: what a change leaves for later is a
  GitHub issue in the repository it belongs to, one per real item
  ([communicating](./communicating.md)), since a note in a scratch file is found by nobody
  and dies with the session.
- **Exact values are researched, never recalled.** A version, a git ref, a checksum, license
  text: fetched live, resolved by the pinning tool, or copied from the repository; a
  placeholder only when it truly cannot be had, said as such. A model's memory is months
  stale and sounds sure; a remembered `aqua-registry` ref once broke every install.
- **A claim rests on a command that ran.** An empty grep is evidence of an absence only if
  the grep succeeded: a shell that rejected a flag or expanded a glob to nothing prints the
  same empty output as a clean tree. The exit status is read before silence is reported as
  a fact.
- **Doctrine can lose the argument, never silently.** A fix or a design is checked against
  the book first. Contradicting it is allowed, since doctrine evolves, but the conflict is
  named, argued and decided, not discovered: installing tools eagerly once fixed a real CI
  bug while quietly defeating aqua's lazy pulls, which the book argues at length; naming the
  conflict would have surfaced the better fix at once.

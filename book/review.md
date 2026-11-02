# Reviewing code

A review judges a change against this book first and against itself second. The
contributor rules are in [coding agents as contributors](./agents.md); this chapter is the
reviewer's side of the same workflow. The review is local: the reviewing session reads the
change in the author's worktree, `git diff <base>..<head>`, before anything is pushed, and
answers by message; GitHub sees one reviewed pull request and the human's approval.

## The order of the questions

1. **Does the mechanism belong?** Before reading the implementation, name the pattern the
   change introduces or relies on and check it against the doctrine: pinned by digest,
   nothing at `latest`, no `replace`, the consumer gets the contract, tests black-box. A
   correct implementation of a mechanism the book rejects is a wrong change, and the review
   says so at the mechanism, not at the lines. A one-tool exception to a rule that should
   hold for every tool is the usual shape of this miss: the exception is right, the rule it
   patches is wrong.
2. **Is it right?** The implementation: what it claims, verified as below.
3. **Is it clean?** One commit per thing, trailers, comments that name a trap, a commit
   message and a description a reader outside the session can follow, no scratchpads, no
   tooling links.

A finding at step 1 outranks a clean pass at steps 2 and 3, and is raised with the
baseline's owner when the mechanism is the baseline's.

## Reading the change

- **Try to break it.** Every guard, check and claim is attacked before it is believed: the
  input that bypasses it, the caller that never reaches it, the platform it behaves
  differently on. A check with a known bypass, or a document that contradicts the rule it
  ships with, is a 🛑 whatever its size. 🔍 lists what was tried, not what was read.
- **A test that passes before and after proves nothing about the change.** A format test
  that checked only the `digraph` prefix stayed green while `-Tdot` started emitting xdot's
  drawing operations; the review that caught it built the binary and counted `_draw_`
  lines. A claim that a behaviour is kept is verified by an assertion on the behaviour, both
  ways where the change names two outputs apart, and the pull request says which test.
- **A binary in the diff is a 🛑, and the fix is the ignore.** `go build` in a repository
  whose root is a `main` package leaves the executable beside the sources, and `git add -A`
  sweeps it into the next commit: a 12 MB binary reached two pull requests. The repository
  ignores its own binary name from its first commit, or builds into `.build/`; a review of
  the file list, not only the diff, catches it.
- **Follow the value to its consumer.** A changed value, error or path is traced to where it
  is used, in this repository and in the ones that import it.
- **A re-push is a new change.** After a redesign the whole pull request is read again as if
  for the first time: what can now go, and why each piece is where it is. A design question
  is judged against the [generic principles](./principles.md), not against the previous
  round.
- **Shape blocks before the first release.** A flaw in an exported API — mutable state, a
  value the type should not admit, state kept twice — is a ⚠️ or a 🛑 until it ships, not a
  💡. A 💡 repeated across rounds was a ⚠️ in the first one.
- **Fix the type before the call sites.** When a value should not exist, the recommendation
  is the type that cannot hold it; a guard is the fallback.

## Reviewing a package

A package review is the owner's whole read of one package, on @apostasie's ask, and ends in
pull requests. The questions, in this order: is it useful, is it well designed, is the code
correct, are the comments lean and meaningful, is the testing appropriate. Design and
surface first; tests are where discipline slips, not where quality comes from. Each step
below is done, or named as skipped with its reason; "done" with an unnamed gap is not done.

- **Read all of it.** Every production file, every test, the package doc, the subpackages.
- **Consumers.** Every exported symbol grepped across the fleet's clones. An unused export,
  gratuitous surface and a shape that makes callers awkward are findings; a consumer gets
  the contract, and only the contract.
- **Dependencies.** Each third-party module pinned, maintained and needed, or the standard
  library would do; said per module.
- **Correctness.** Every edge reasoned about; a suspected defect reproduced with a
  throwaway probe under `_scratch/` before it is fixed; a package that mirrors another
  compared with it, call for call.
- **Comments.** Each names a trap, not a story; a doc comment states the contract
  ([principles](./principles.md)).
- **Tests.** The full pass of [what a package's tests establish](./testing.md#what-a-packages-tests-establish).
- **Performance.** Where the package exists for speed, a benchmark kept as a guard and the
  hot path read for the regression a green suite does not show.
- **A finding is a decision, not a fix.** A design finding is walked with @apostasie one at
  a time and decided, keep, accept or fix, before any fix is written; a lock order that
  reads as a hazard may be the fix for a race, and a panic on an invalid root may be the
  design. Of sixteen findings in one sweep, four were kept as designed, two accepted, ten
  fixed.
- **Fixes.** One commit per thing, one pull request per topic, through the local review.
- **Doctrine.** A lesson that holds beyond the package is a book change of its own, with
  its evidence; decided per package, book entry or not, and said.
- **The report.** What changed; what was found and left as is, named; what was verified and
  how; which steps were skipped and why.

## Verify live, never from memory

Every statement in a review was checked at the time of writing, against the source of
truth, and the review says where:

- A pinned checksum or digest, against what upstream publishes: the release asset digest,
  `sum.golang.org` for a Go module, the publisher's checksum file. A match establishes
  identity, not fitness.
- A pseudo-version, against the owner's default branch: the commit is on it, and the
  timestamp in the version is the commit's.
- A content-pinned file, byte for byte against the canonical copy at the version the
  repository pins — the release, not the baseline's `main`.
- A hand-made dependency bump, against what the automated path produced elsewhere: the
  resulting module set, line for line.
- The head under review, against the worktree: the sha the author named is the one the
  diff was read at, and the `reviewed:` line carries that sha and no other. The state of a
  pull request — merged, closed, head, checks — re-read before it is reported, never
  carried over from an earlier look.

A local clone is a source of truth once it has been fetched *and the fetch succeeded*; a
fetch whose error went to `/dev/null` has audited the past. Use the rig's transport
(`git fetch`) or the API, and say which. What could not be verified is stated as
unverified, not inferred.

A search that found nothing has verified the search, not the tree, until the same search
finds something known to be there: `git grep -E` with `\b` matches nothing on macOS, whose
ERE has no word boundary, and a claim that no file consumed a type two files switch on went
out marked V. A negative result is V with its positive control named, or it is U.

## Dependency bumps

A green run and a pin that matches upstream are the bump's preconditions, not its review:
CI is the merge gate, and a digest compared with the release the bot read it from checks
the source against itself. Neither says whether the new version is right for this
consumer. A bump, bot- or hand-made, is approved on evidence about that, and the approval
names it:

- **What changed upstream between the two versions**: release notes, changelog, the
  advisories the bump claims to fix and the ones it does not, new platform or toolchain
  requirements. A bump that leaves a published advisory open says so.
- **What the bump touches that CI does not exercise**, and how that was covered. Where CI
  cannot run it (a kernel build, a formula no lane builds, a binary that boots a VM), the
  approval waits for the owning session's result on the pull request.
- **Where the dependency lands**: shipped code, a test, a generator, a tool module. The
  depth of the check follows the exposure, and the approval says which it is.

A routine patch bump of a tool module, with nothing in its upstream changes, is approved in
one line that says so. A bump nobody has evidence for is not approved. A red bump is the
owning session's to unblock, never the project manager's to fix: it reads the failing log
and forwards what it found.

**Red by construction is a baseline defect.** A bump that cannot be green without a hand
edit the bot will never make — a seeded workflow calling a recipe the project does not
define, a managed file the bot and a workflow both rewrite — is one finding, to the
baseline's owner, with the URL of every repository it reddens.

**A branch that moves while its base does not is a question.** Count force-pushes and
workflow runs over time: a head that differs at each look, on a base that has not moved, is
two automations disagreeing about the same file, and every round costs a full CI run.

**Two green pull requests are not a green `main`.** Each was green on a base without the
other; what the second one's checks never saw is the first one's merge. A content-checked
file is where this bites: a hand pull request made `ci.yaml` limen's seed byte for byte
while a limen bump, converged on the base before it, left that seed alone; each was green,
and `main` was red on every leg once both merged (farcloser/homebrew-brews#80 and #81,
fixed by #85). An approval on a pull request that touches a file another open pull request
also touches says which merges first; the second is rebased and re-run after that merge,
before it is opened.

## Who approves what

The reviewing session reviews a session's change in its worktree and settles it by message,
round after round; when settled, it sends the project-manager session one line,
`reviewed: <org/repo> <branch> <head sha>`, and that line is the whole of its verdict on
GitHub's side: the project manager binds the pull request the author opens at that head as
the human's approval queue, and refuses any other head. A commit after the line is a new
review, the same exchange, a new sha. The reviewing session leaves nothing on a session's
pull request — no comment, no approval (the shared account cannot approve itself, every
participant knows it, and nothing says so), no arming, which is the project manager's — and
a red the author cannot fix is
either a redesign, back in the worktree, or environmental, which the reviewing session says
to the project manager as `bypass-safe: <org/repo> <branch> <sha> <run URL>`.

A trivial documentation-only pull request does not go to the reviewing session: it goes to
the project manager, which reviews it, logs it and arms it. Trivial is prose alone and
nothing decided in it: a typo, a wrap, a stale link or name, a sentence restating a fact the
book already holds. A change that touches code, configuration, a workflow or a test goes to
the reviewing session, a dependency bump excepted (below), and so does a documentation
change that is not trivial: a new rule, a changed meaning, a chapter rewritten, a claim a
reader would act on. A line that states a fact about an artifact or a mechanism, a release,
a signature, a command's behaviour, is a claim, and when the project manager cannot verify
it itself, that is doubt. In doubt, the reviewing session; the cost of a needless review is
a turn, the cost of a skipped one is a wrong rule in the book.

A pull request that only bumps dependencies, Renovate's or a session's, goes to the project
manager, not the reviewing session. The project manager reviews it ([dependency
bumps](#dependency-bumps)); a session's it logs, arms and binds for the human as it does a
trivial documentation change; Renovate's, the one review on GitHub, it approves and arms
(`gh pr merge --auto --merge`). The preset rebases a bump on every `main` commit and each
rebase drops the approval, which the sweep gives again at the rebased head. Renovate re-arms
after every rebase, so what holds a bump is the approval the project manager withholds.
GitHub does not always re-evaluate an armed merge when the approval lands: an approved,
green, armed pull request sat seven minutes, and a disarm and re-arm merged it within half a
minute; so an approved, armed pull request that sits is re-armed by the project manager. A
native stack is merged by the project manager, from the top layer through the asynchronous
merge API (`PUT /repos/{owner}/{repo}/pulls/{top}/merge-async`), once every layer is green
and approved; never from the middle, never partially ([a stack is merged
whole](./agents.md)). Closing, tagging and dismissing alerts are @apostasie's, and a review
that needs one of them says which and why, in one line, and ends.

**A 🛑 is argued, not obeyed.** The reviewer reads every repository and may miss what the
owner knows of its own code: a guard in another file, a setting the rule keeps elsewhere, a
constraint the platform imposes. The owner who disagrees says so, with the evidence (the
file and line, the measurement, the document), and the reviewer answers in kind: the finding
is withdrawn, as a miss named as such, or held with the reason the evidence does not cover.
Most rounds end there; a withdrawn finding costs the reviewer nothing. The round that does
not end is escalated to @apostasie, by either side, with both positions in one message, and
a review that reaches it says so rather than repeating itself.

## The shape of a review

A review is a message to the author, read from a terminal, often on a phone, by someone who
already knows the change. It is short, and it says the verdict first. Every section opens
with the same marker every time, so the eye finds it without reading:

| Marker | Section | Rule |
|---|---|---|
| ✅ / ❌ | **Verdict**, the first line | `✅ Ready` or `❌ Not ready`, the head it was read at, and the one-line reason. Nothing above it. An open 🛑 or ⚠️ makes it ❌ until fixed or decided; ✅ is what the `reviewed:` line to the project manager follows. |
| 🛑 | **Blocking** | What must change before merge: the defect, the evidence, the fix. One bullet per item. |
| ⚠️ | **Decision** | Not a defect: a call the owner or @apostasie has to make, with the options and the recommendation. |
| 💡 | **Non-blocking** | A suggestion or a follow-up. Never requests a change on its own. |
| 📝 | **Note** | A fact the reader should know and nothing to do about it here. |
| 🔍 | **Verified** | What was checked and where, one line each, last. Not the diff restated, not the reasoning narrated. |

A section with nothing in it is left out, markers are never reused for anything else, and a
review that is only `✅ Ready` plus 🔍 is complete. One screen is the budget; a review that
needs more is two findings that should have been one, or a design discussion that belongs in
the conversation with the owner, raised here as one ⚠️ and settled there.

Every section obeys: a finding names the defect, the evidence and the owner; full URLs for
every pull request, run, commit or advisory; @apostasie by handle in anything public;
nothing the author already knows — a constraint shared by everyone, a rule the commit
message states, a repeat of an earlier round. The same finding in several repositories is
one review, to the owner of what they share.

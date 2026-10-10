# Reviewing code

A review judges a change against this book first and against itself second. The
contributor rules are in [coding agents as contributors](./agents.md); this chapter
is the reviewer's side of the same workflow, whoever the reviewer is: a session
assigned the role, or a person.

## The order of the questions

1. **Does the mechanism belong?** Before reading the implementation, name the
   pattern the change introduces or relies on and check it against the doctrine:
   pinned by digest, nothing at `latest`, no `replace`, the consumer gets the
   contract, tests black-box. A correct implementation of a mechanism the book
   rejects is a wrong change, and the review says so at the mechanism, not at the
   lines. A one-tool exception to a rule that should hold for every tool is the
   usual shape of this miss: the exception is right, the rule it patches is wrong.
2. **Is it right?** Then the implementation: what it claims, verified as below.
3. **Is it clean?** Last: one commit per thing, trailers, comments that name a
   trap, no scratchpads, no tooling links.

A finding at step 1 outranks a clean pass at steps 2 and 3, and is raised with the
baseline's owner when the mechanism is the baseline's.

## Reading the change

- **Try to break it.** Every guard, check and claim in the change is attacked
  before it is believed: the input that bypasses it, the caller that never reaches
  it, the platform it behaves differently on. A check with a known bypass, or a
  document that contradicts the rule it ships with, is a 🛑 whatever its size. 🔍
  lists what was tried, not what was read.
- **A test that passes before and after proves nothing about the change.** A
  format test that checked only the `digraph` prefix stayed green while `-Tdot`
  started emitting xdot's drawing operations; the review that caught it built
  the binary and counted `_draw_` lines. A claim that a behaviour is kept is
  verified by an assertion on the behaviour, both ways where the change names
  two outputs apart, and the pull request says which test.
- **A binary in the diff is a 🛑, and the fix is the ignore.** `go build` in a
  repository whose root is a `main` package leaves the executable beside the
  sources, and `git add -A` sweeps it into the next commit: a 12 MB binary
  reached two pull requests of the same repository. The repository ignores
  its own binary name from its first commit, or builds into `build/`; a
  review of the file list, not only the diff, is what catches it.
- **Follow the value to its consumer.** A changed value, error or path is traced to
  where it is used, in this repository and in the ones that import it.
- **A re-push is a new change.** After a redesign the whole pull request is read
  again as if for the first time, and two questions are asked of it: what can now
  go, and why each piece is where it is. A design question is judged against the
  [generic principles](./principles.md), not against the previous
  round.
- **Shape blocks before the first release.** A flaw in an exported API — mutable
  state, a value the type should not admit, state kept twice — is a ⚠️ or a 🛑
  until it ships, not a 💡. A 💡 repeated across rounds was a ⚠️ in the first one.
- **Fix the type before the call sites.** When a value should not exist, the
  recommendation is the type that cannot hold it; a guard is the fallback option.

## Verify live, never from memory

Every statement in a review was checked at the time of writing, against the
source of truth, and the review says where:

- A pinned checksum or digest, against what upstream publishes: the GitHub release
  asset digest, `sum.golang.org` for a Go module, the publisher's checksum file. A
  match establishes identity (this is the artifact upstream published), not fitness.
- A pseudo-version, against the owner's default branch: the commit is on it, and
  the timestamp in the version is the commit's.
- A content-pinned file, byte for byte against the canonical copy at the version
  the repository pins — the release, not the baseline's `main`.
- A hand-made dependency bump, against what the automated path produced elsewhere:
  the resulting module set, line for line.
- The state of a pull request — merged, closed, head, checks — re-read before it is
  reported, never carried over from an earlier look.

A local clone is not a source of truth until it has been fetched *and the fetch
succeeded*; a fetch whose error went to `/dev/null` has audited the past. Use the
rig's transport (`git c fetch`) or the API, and say which. What could not be
verified is stated as unverified, not inferred.

A search that found nothing has verified the search, not the tree, until the same
search finds something known to be there. `git grep -E` with `\b` matches nothing on
macOS, whose ERE has no word boundary (`-P` or an explicit class does), and a claim
that no file consumed a type two files switch on went out marked V. A negative result
is V with its positive control named, or it is U.

## Dependency bumps

A green run and a pin that matches upstream are the bump's preconditions, not its
review: CI is the merge gate and the reader sees it without being told, and a digest
compared with the release the bot read it from checks the source against itself.
Neither says whether the new version is right for this consumer. A dependency bump,
bot- or hand-made, is approved on evidence about that, and the approval names the
evidence:

- **What changed upstream between the two versions**: release notes, changelog, the
  advisories the bump claims to fix and the ones it does not, new platform or
  toolchain requirements. A "security" bump is checked against the advisory list: a
  bump that leaves a published advisory open says so.
- **What the bump touches that CI does not exercise**, and how that was covered.
  Where CI cannot run it (a kernel build, a formula no install lane builds, a binary
  that boots a VM), the approval waits for the owning session's result on the pull request; it
  is not given on green.
- **Where the dependency lands**: shipped code, a test, a generator, a tool module.
  The depth of the check follows the exposure, and the approval says which it is.

A routine patch bump of a tool module, with nothing in its upstream changes, is
approved in one line that says so. A bump nobody has evidence for is not approved.

A red bump is the owning session's to unblock, never the reviewer's to fix: the
reviewer reads the failing log and forwards what it found.

**Red by construction is a baseline defect.** A bump that cannot be green without a
hand edit the bot will never make — a seeded workflow calling a recipe the project
does not define, a managed file the bot and a workflow both rewrite — is not a
problem with that pull request. It is one finding, to the baseline's owner, with
the URL of every repository it reddens.

**A branch that moves while its base does not is a question.** Count force-pushes
and workflow runs over time, not the head at a glance: a head that differs at each
look, on a base that has not moved, is two automations disagreeing about the same
file, and every round costs a full CI run.

**Two green pull requests are not a green `main`.** Each was green on a base without
the other; what the second one's checks never saw is the first one's merge. A
content-checked file is where this bites: a hand pull request made `ci.yaml` limen's
seed byte for byte while a limen bump, converged on the base before it, left that seed
alone. Each was green, and `main` was red on every leg once both merged
(farcloser/homebrew-brews#80 and #81, fixed by #85). An approval on a pull request that
touches a file another open pull request also touches says which merges first; the
second is rebased and re-run after that merge, before its review request stands.

## Who approves what

The reviewing session approves bot-authored pull requests once verified. On a
session-authored pull request it comments and stops: the shared account cannot
approve itself, every participant knows it, and the review never says so. Merging is
the ruleset's, on the approval ([an approved pull request merges itself](./agents.md)),
and arming it is the reviewer's: a ✅ at a head on a session's pull request, or an
approval on a bot's, comes with `gh pr merge --auto --merge` on that pull request, and
a later 🛑 or ⚠️ at a newer head comes with `gh pr merge --disable-auto`. On a bot's
pull request the disarm is a signal, not the gate: Renovate re-arms auto-merge after
every rebase (its `platformAutomerge`), so what holds a Renovate bump is the approval
the reviewer withholds; on a session's pull request the disarm holds. The arming is
the verdict made executable, so it is never the author's; the ruleset keeps the human's
approval as the gate, and dismisses it on a push, so the author asks again with the new
head. GitHub does not always re-evaluate an armed merge when the approval lands: a pull
request approved, green, clean and armed sat seven minutes, and a disarm and re-arm
(`gh pr merge --disable-auto`, then `--auto --merge`) merged it within half a minute. So
an approved, armed pull request that sits is re-armed by the reviewer; nobody presses
merge. A native stack is the one merge the reviewer makes itself: GitHub refuses
auto-merge on a stack's layers, so once every layer is green, ✅ at its head and
approved, the reviewer merges the stack from its top layer through the asynchronous
merge API (`PUT /repos/{owner}/{repo}/pulls/{top}/merge-async`), never from the middle
and never partially ([every pull request targets `main`](./agents.md)). Closing,
tagging and dismissing alerts are @apostasie's, and a review that needs one
of them says which and why, in one line, and ends.

**A 🛑 is argued, not obeyed.** The reviewer reads every repository and may miss
what the owner knows of its own code: a guard that lives in another file, a
setting the rule keeps elsewhere, a constraint the platform imposes. The owner
who disagrees with a finding says so on the pull request, with the evidence
(the file and line, the measurement, the document), and the reviewer answers in
kind: the finding is withdrawn, as a miss named as such, or held with the reason
the evidence does not cover. Most such rounds end there, in one exchange, and a
withdrawn finding costs the reviewer nothing: a miss corrected is the review
working. The round that does not end is escalated to @apostasie, by either side,
with both positions in one message; that is the exception, and a review that
reaches it says so rather than repeating itself.

## The shape of a review

A review is read from a terminal, often on a phone, usually by someone who already
knows the change. It is short, and it says the verdict first. Every section opens
with the same marker every time, so the eye finds it without reading:

| Marker | Section | Rule |
|---|---|---|
| ✅ / ❌ | **Verdict**, the first line | `✅ Ready` or `❌ Not ready`, the head it was read at, and the one-line reason. Nothing above it. An open 🛑 or an open ⚠️ makes it ❌ until it is fixed or decided. |
| 🛑 | **Blocking** | What must change before merge: the defect, the evidence, the fix. One bullet per item. |
| ⚠️ | **Decision** | Not a defect: a call the owner or @apostasie has to make, with the options and the recommendation. |
| 💡 | **Non-blocking** | A suggestion or a follow-up. Never requests a change on its own. |
| 📝 | **Note** | A fact the reader should know and nothing to do about it here. |
| 🔍 | **Verified** | What was checked and where, one line each, last. Not the diff restated, not the reasoning narrated. |

A section with nothing in it is left out, markers are never reused for anything else,
and a review that is only `✅ Ready` plus 🔍 is a complete review. The verdict carries
the bottom line; the rest is evidence for the reader who wants it. One screen is the
budget; a review that needs more is two findings that should have been one, or a
design discussion that belongs in the conversation with the owner, raised here as
one ⚠️ and settled there.

What every section obeys:

- A finding names the defect, the evidence, and the owner.
- Full URLs for every pull request, run, commit or advisory. The reader clicks.
- @apostasie by handle, in anything public.
- Nothing the author already knows: a constraint shared by everyone, a rule the
  pull request body already states, a repeat of an earlier round.

The same finding in several repositories is one review, to the owner of what
they share, not one comment per repository.

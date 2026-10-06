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
  Where CI cannot run it (a kernel build, a source-built formula, a binary that boots
  a VM), the approval waits for the owning session's result on the pull request; it
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

## Who approves what

The reviewing session approves bot-authored pull requests once verified. On a
session-authored pull request it comments and stops: the shared account cannot
approve itself, every participant knows it, and the review never says so. Merging,
closing, tagging and dismissing alerts are @apostasie's; a review that needs one of
them says which and why, in one line, and ends.

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

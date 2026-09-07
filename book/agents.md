# Coding agents as contributors

A coding agent that only edits files is a fancy editor. One that can commit and
push is a contributor, and gets the treatment every contributor gets here: its
own identity, its own key, and the same rulesets as everyone else. This chapter
is the doctrine; the mechanics are one script,
[`limen-install-agent`](https://github.com/farcloser/limen-install), in the
machine-bootstrap repository.

## The identity model

**A separate machine-user account, never the human's key.** An SSH key on a
personal account is account-wide: everything the human can push to, the agent
could too, and every commit the agent made would be indistinguishable from the
human's. A dedicated account (`closer-claudio` at farcloser) fixes both at once:

- **Blast radius** is exactly what the organization's `agents` team is granted.
  The bot is an organization member whose only access comes through that
  team, on an organization whose members' base permission is read — the
  `org-default-repository-permission` floor `limen github check -org` keeps.
  Every governed repository grants the team write: the `agents-team` check
  asserts it and `limen github fix` grants it, so a repository created by hand
  is one fix run away from contributable. Revocation is one team membership.
- **Attribution** is honest: commits are authored and signed by the bot, and
  the audit trail says so forever.

Alternatives, and why not: a key on the human's account (full access, weak
attribution); per-repository deploy keys (cannot be signing keys, so every
commit shows *Unverified* and the `limen:main` ruleset refuses the merge); a
GitHub App (the tightest scoping on paper, but its private key is a file on
disk, which is the thing to avoid).

## The key model

**Hardware-bound, non-exportable, no touch.** The bot's key lives in the Secure
Enclave, served by [Secretive](https://github.com/maxgoedjen/secretive)'s agent.
It cannot leak; it can only be used by a process that can reach the agent's
socket on that one machine. The "requires authentication" (Touch ID) option is
deliberately **off**: a gate the human taps reflexively, dozens of times a day,
is theater — worse than no gate, because it feels like control. The human's own
YubiKey keeps its touch, where a touch is rare and means something.

What is the veto, then? **The rulesets.** `limen:main` requires a pull request
with signed commits; the bot can push branches and open pull requests, and
cannot land anything on `main`. Merging stays a human act — one deliberate
gesture per pull request, with the diff in front of the human.

The key is ECDSA P-256 because that is the only curve the Secure Enclave
implements. Ed25519 is the better algorithm (deterministic, transparent curve);
P-256 in hardware with an RNG the enclave controls is an acceptable trade for a
bot whose damage is bounded by rulesets and revocable in a click.

## The sandbox model

The agent runs in a sandbox that denies `~/.ssh` and every unix socket it was
not told about. The setup opens exactly one thing: the Secretive socket. The
agent can ask the enclave to sign; it can never read a key. Three consequences
the script handles:

- **Host keys** — `~/.ssh/known_hosts` is unreadable, so GitHub's host keys are
  pinned into a file the sandbox can read, from GitHub's published list, and
  ssh runs with strict checking against it. No trust-on-first-use.
- **The ssh client** — the Homebrew OpenSSH is built without OpenSSL and cannot
  use an ECDSA key at all; Apple's `/usr/bin/ssh` and `ssh-keygen` are used
  for the bot, via `core.sshCommand` and `gpg.ssh.program`.
- **The egress proxy** — every outbound connection goes through the sandbox's
  per-session proxy, which requires credentials. The sandbox's own ssh wiring
  offers none (an unauthenticated `nc`), so a small ProxyCommand helper speaks
  authenticated HTTP CONNECT to the same proxy instead. The proxy's domain
  allowlist still applies; nothing is bypassed.

The bot's identity — name, email, signing key — is injected as session-scoped
git configuration (`GIT_CONFIG_*` in the agent's environment), so the human's
own git configuration is never modified and a shell the human opens never sees
the bot.

## What stays manual

Three things, all one-time, all deliberately human:

1. Creating the bot's GitHub account, inviting it to the organization, and
   adding it to the `agents` team.
2. Creating its key in Secretive and registering the public key on the account
   as both an authentication key and a signing key.
3. Running `limen-install-agent`, which does everything else and verifies.

The working agreement itself needs no machine step: it travels with every
repository as the content-pinned `AGENTS.md` (imported by a seeded
`CLAUDE.md`), one of the [mandatory files](./mandatory-files.md#canonical-agentsmd),
so it loads whichever repository a session starts in.

## Per repository

The bot's public key belongs in `.allowed_signers`, next to the human's, so
`just lint`'s commit check verifies its signatures locally exactly as it
verifies the human's. That file is the same in every repository and hand-copied
today; it is the shape of a canonical, content-pinned file, and making it one is
the planned next step — until then, the script prints the line to add.

## The workflow

The agent is a contributor with its own branches and its own worktrees, and it
owns the whole path from a change to a green pull request. The human's
branches are the human's.

- **Its own branches, in their own worktrees.** Every task starts from a fresh
  `origin/main` in a git worktree, on a branch named after the bot
  (`claudio/<YYYYMMDD>-<topic>`, the day it was cut so a listing reads in
  date order), one topic per branch and per pull request. The
  worktree keeps the human's checkout untouched and lets several tasks run
  side by side. Inside a new worktree: `aqua policy allow aqua-policy.yaml`
  then `aqua install --only-link` — aqua's policy is keyed by path.
- **Never on `work`.** The human's `work` branch is the human's, or the
  human's with the agent pairing interactively at the human's request. The
  agent does not commit there on its own, and never on `main`.
- **Commits.** Signed as the bot, with a DCO sign-off as the bot; when the
  change is the human's work, the human is the author and the bot the
  committer. No scratchpads: `AUDIT.md` and its kind are transient notes
  whose surviving findings become code, tests, or book prose.
- **Green before pushing.** The full `just lint` and `just test`, not one
  lane: what CI runs on other platforms (a linux-only package, a windows leg)
  is what a single lane on one machine misses.
- **Push, open, own.** Push the branch, open the pull request with a
  description drawn from the commit messages, then own the checks: watch
  them, read the failed logs, fix, push again. A red check is the agent's to
  turn green or to explain — never to leave for the human to discover.
- **Green, then the reviewer.** Once the checks are green and the pull
  request is ready, request the repository owner's review — the human is
  told, not left to notice, and told once: a review request on a red pull
  request is a request to watch the agent work. Ready also means
  *mergeable on its own*: a pull request stacked on another — branched
  from an unmerged branch, showing that branch's commits until it lands —
  waits, unrequested, until its base has merged and it has been rebased
  down to its own commits. The owner is whoever the repository says: a
  `CODEOWNERS` entry when there is one, else the organization's owner.
- **Keep `main` fresh.** After a merge: fetch and fast-forward the local
  `main`, prune the merged branch and its worktree, and rebase every open
  branch onto `main` — so that both the human and the agent can rebase often
  and cheaply.
- **Stacking, rarely and said out loud.** A branch is cut from the human's
  unmerged branch only when the change cannot be green without it — a
  repository whose CI is broken on `main` until the human's pending
  migration lands. The pull request body names what it stacks on; once the
  base merges, the branch is rebased down to its own commits. A stacked
  pull request is never sent for review.
- **Not the agent's to do.** Merging, pushing to `main`, force-pushing shared
  branches, and tagging releases. Those are not trust questions; they are the
  rulesets and the release lane doing their job.

## Scope and priorities

The human sets the priorities; the agent measures scope before it moves.

- **The ask is the deliverable.** Thing A, whole, not thing B and not A plus
  B. Something unrelated that genuinely needs fixing is finished-A-first,
  then mentioned; acting on it is the human's call.
- **Drive-by fixes are fine; campaigns are not.** A one-line pin, a stale
  suppression, a workflow that hides its own failure — casual, on the way
  through, in scope. Several repositories are in a broken state and their
  large-scale repair — onboarding onto limen, wholesale lint cleanups —
  waits for the human's explicit green light, however tempting.
- **A red inherited from `main`** is explained on the pull request, not
  fixed there: the fix is its own change, if the human wants it, and a
  file under the human's active edit is left alone.
- **Scratch is scratch.** `AUDIT.md` and its kind hold notes to be judged;
  what survives judgment becomes code, tests, or book prose. They are never
  committed.

## Where the conversation happens

- **Instructions come in the conversation**; the agent keeps a running queue
  of them and does not lose one to another. A short list from the human is
  enough — structure is welcome, not required.
- **Review happens on GitHub.** A review request is the signal that a pull
  request is green and ready: it lands in the human's review inbox and is
  sent once. The human's comments on the pull request are the review; the
  agent addresses them when the human points it there, or when it next
  checks its open pull requests. If a requested pull request turns red, the
  request is withdrawn until it is green again.
- **Answers, not menus.** Lead with the conclusion; give one recommendation,
  not a survey; a fix that cuts against recorded doctrine is named as such
  and argued, never slipped in. When something is either the right call or
  not, say which.

These rules ship in two forms: compressed into the content-pinned `AGENTS.md`
every repository carries — the harness-neutral file any coding agent reads —
and as a skill (`skills/contribute`, the step-by-step procedure with the
commands) for harnesses that load skills. An agent applies them without being
told each time.

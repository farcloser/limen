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

- **Blast radius** is exactly the repositories it is invited to, as an outside
  collaborator with write access — never as an organization member, so it
  inherits no org-wide defaults. Revocation is one account.
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

1. Creating the bot's GitHub account and inviting it per repository.
2. Creating its key in Secretive and registering the public key on the account
   as both an authentication key and a signing key.
3. Running `limen-install-agent`, which does everything else and verifies.

## Per repository

The bot's public key belongs in `.allowed_signers`, next to the human's, so
`just lint`'s commit check verifies its signatures locally exactly as it
verifies the human's. That file is the same in every repository and hand-copied
today; it is the shape of a canonical, content-pinned file, and making it one is
the planned next step — until then, the script prints the line to add.

## Rules of engagement

The agent commits with a DCO sign-off as itself, pushes branches, and opens
pull requests. It does not merge, does not push to `main`, does not force-push,
and does not tag releases — those are not trust questions, they are the
rulesets and the release lane doing their job.

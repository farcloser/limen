# Identity and keys

Who commits, with what key, and what the key can and cannot do. One identity per
contributor, human or bot; one hardware-bound key each, for authentication and for signing;
nothing shared. The human's key comes first, then the bot's identity, key and sandbox, and
what stays the human's to do.

## The human's key: one hardware-bound SSH key, for both uses

**One `sk-` key, on a hardware token, for authentication and for signing.** A person's key is
generated on a FIDO2 token (a YubiKey), with a passphrase, and registered on GitHub twice:
as an authentication key and as a signing key. The same key pushes and signs, so there is
one identity to reason about, one thing to register, and one thing to revoke.

```bash
ssh-keygen -o -a 100 -t ed25519-sk -f ~/.ssh/id_ed25519_sk -C "you@farcloser.world"
```

What that buys, and why each part is there:

- **The private key cannot be copied.** It never leaves the token: a stolen laptop, a
  leaked backup, a compromised process on the machine, none of them yield the key. This is
  the property the whole model rests on, and it is why a key *file*, however well
  protected, is not acceptable for a person.
- **The passphrase is a second factor against the token itself.** A stolen token is a
  physical event the owner notices; until then, using it needs the passphrase too. The
  passphrase is asked once per plug-in, and again when the agent's cache expires.
- **Touch is presence.** Every use needs a touch on the token, so malware on the machine
  cannot sign or push quietly in the background: each signature is one deliberate
  gesture. This is the opposite choice from the bot's key, whose touch requirement is off
  because a gate tapped reflexively dozens of times a day is theater
  ([the bot's key](#the-bots-key)); a person's touch is rare and means
  something.
- **`-o -a 100`** is the OpenSSH key format with a slow KDF on the passphrase (the format
  is implied for ed25519; the rounds are not). Ed25519 because it is the better
  algorithm; the enclave constraint that puts the bot on P-256 does not apply to a token.

**The agent that serves it.** macOS's system `ssh-agent` does not support `sk-` keys, so a
machine runs OpenSSH's agent as a user launch agent instead
([farcloser/ssh-agent](https://github.com/farcloser/ssh-agent)), on its own socket; the
system agent is disabled, not killed, which on current macOS is the only lever SIP
leaves. A cached passphrase lives in that agent, and dies with it.

**Git signs every commit with it**, through `gpg.format ssh` and `commit.gpgsign`: the
configuration is in [CONTRIBUTING.md](https://github.com/farcloser/.github/blob/main/.github/CONTRIBUTING.md),
and the public key goes into each repository's `.lint-signers` with the first
contribution, so `git log --show-signature` and `git tag -v` resolve offline. Signing and
the DCO sign-off are different things and both are required; the git moves are in
[GIT.md](https://github.com/farcloser/.github/blob/main/.github/GIT.md).

**If the token is lost or stolen.** Revoke the key on GitHub at once, as an authentication
key *and* as a signing key: the two registrations are independent and either one left
behind is a door. Then generate a new key on a new token and register it. Commits the old
key signed keep their badge: GitHub records a verification at push time and never
re-evaluates it when a key is later removed, revoked or expired
([persistent commit signature verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification#persistent-commit-signature-verification));
only a commit signed with the removed key and pushed *after* the removal shows as
unverified, its key unknown. Locally, verification reads `.lint-signers`, so the old key's
entry stays there as the record, with the new key added beside it.

**Changing the passphrase** rewrites the key file's protection, not the key:
`ssh-keygen -p -f ~/.ssh/id_ed25519_sk`.

Non-resident keys, by design: the key handle lives in `~/.ssh`, the secret in the token,
and a FIDO PIN does not apply to them. Resident keys and PIV are the alternatives when a
key must move between machines with the token alone; neither is in use today.

## The bot's identity

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
  the audit trail says so forever. It is also the *whole* of the attribution:
  a `Co-Authored-By:` trailer naming the model, and nothing that points at the
  tooling — no vendor or product link, no "generated with" banner, no session
  URL or identifier, in any commit message, pull request, comment, issue or
  release note. A session URL is a leak of the human's private session; the
  rest is advertising on someone else's repository. Harness defaults have
  asked for exactly these, so `AGENTS.md` states the rule and the agent greps
  for it before pushing.

Alternatives, and why not: a key on the human's account (full access, weak
attribution); per-repository deploy keys (cannot be signing keys, so every
commit shows *Unverified* and the `limen:main` ruleset refuses the merge); a
GitHub App (the tightest scoping on paper, but its private key is a file on
disk, which is the thing to avoid).

## The bot's key

**Hardware-bound, non-exportable, no touch.** The bot's key lives in the Secure
Enclave, served by [Secretive](https://github.com/maxgoedjen/secretive)'s agent.
It cannot leak; it can only be used by a process that can reach the agent's
socket on that one machine. The "requires authentication" (Touch ID) option is
deliberately **off**: a gate the human taps reflexively, dozens of times a day,
is theater — worse than no gate, because it feels like control. The human's own
YubiKey keeps its touch, where a touch is rare and means something.

**The key only works while the human's session is unlocked.** Secretive creates
every key as `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`, whatever its
authentication setting, so once the screen locks the agent refuses every request:
no signed commit, no push, until the human is back. That is the enclave's
guarantee working, not a fault, and the agent reads it as such: a signing or push
failure that follows earlier successes means a locked screen, so the agent
finishes the work in its worktree — green, its commit message written — and
leaves it ready to commit and push, instead of reporting a broken rig. Its
worktree stays until then. A per-key opt-in to stay usable while locked is
proposed upstream
([maxgoedjen/secretive#819](https://github.com/maxgoedjen/secretive/pull/819)).

What is the veto, then? **The rulesets.** `limen:main` requires a pull request
with signed commits *and one approving review*; the bot can push branches and
open pull requests, and cannot land anything on `main`. Merging stays a human
act — one deliberate gesture per pull request, with the diff in front of the
human.

The approval is what makes that a capability and not a convention, and it was
missing for a long time. Signed commits, no force pushes, no direct pushes to
`main` — the rulesets always enforced those against everyone. Merging was
different: the bot holds write, a write-level identity can call GitHub's merge
endpoint, and nothing but `AGENTS.md` said not to. The same was true of every
installed App, which is the sharper case: a GitHub App's permissions are taken
wholesale, `contents: write` covers both pushing a branch and merging a pull
request, and an app that raises dependency pull requests necessarily has it.
Requiring an approval closes it for all of them at once, because GitHub will
not let an author approve their own pull request.

The key is ECDSA P-256 because that is the only curve the Secure Enclave
implements. Ed25519 is the better algorithm (deterministic, transparent curve);
P-256 in hardware with an RNG the enclave controls is an acceptable trade for a
bot whose damage is bounded by rulesets and revocable in a click.

## The bot's sandbox

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

**A sandbox exclusion matches the bare command.** A command the human took out of
the sandbox — `just kernel`, which boots a VM that the sandbox cannot — is excluded only as
written, alone on its line. Wrapped in a pipe, a `$(…)`, an `&&` chain or an environment
prefix, it runs sandboxed, and the capability is simply absent: Virtualization.framework
reports no hardware, and the build that was meant to prove the change never happens. Run it
on its own, read its result, and never report a build or a boot you did not watch finish.
A toolchain bump went out marked ready on a sandboxed attempt that had not built, and the
release failed on it ([farcloser/ossein-kernel#79](https://github.com/farcloser/ossein-kernel/pull/79),
fixed in [#109](https://github.com/farcloser/ossein-kernel/pull/109)).

**Breakage is reported, not routed around.** An agent that finds the rig not working —
the installed ssh refused, signing unable to reach the agent, a recipe failing — says so
first and stops there; a private workaround that keeps the work moving hides the defect
from the one person who can fix it, and once hid a broken installer for a whole session
while pushes quietly took another path. A workaround needs the human's agreement and is
named as one each time it is used. A key that stops answering after it worked is the
exception: a locked screen, not a broken rig ([the bot's key](#the-bots-key)).

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

The bot's public key belongs in `.lint-signers`, next to the human's, so
`just lint`'s commit check verifies its signatures exactly as it verifies the
human's, and fails a commit neither key signed. That file is the same in every
repository and hand-copied
today; it is the shape of a canonical, content-pinned file, and making it one is
the planned next step — until then, the script prints the line to add.

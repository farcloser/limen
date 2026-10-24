# Identity and keys

Who commits, with what key, and what the key can and cannot do. One identity per
contributor, human or bot; one hardware-bound key each, for authentication and for signing;
nothing shared. The human's key first, then the bot's identity, key and sandbox, and what
stays the human's to do.

## The human's key: one hardware-bound SSH key, for both uses

**One `sk-` key, on a hardware token, for authentication and for signing.** A person's key
is generated on a FIDO2 token (a YubiKey), with a passphrase, and registered on GitHub twice:
as an authentication key and as a signing key. One identity to reason about, one thing to
register, one thing to revoke.

```bash
ssh-keygen -o -a 100 -t ed25519-sk -f ~/.ssh/id_ed25519_sk -C "you@farcloser.world"
```

- **The private key cannot be copied.** It never leaves the token: a stolen laptop, a leaked
  backup, a compromised process yield nothing. This is the property the model rests on, and
  why a key *file*, however well protected, is not acceptable for a person.
- **The passphrase is a second factor against the token itself.** A stolen token is a
  physical event the owner notices; until then, using it needs the passphrase too. It is
  asked once per plug-in, and again when the agent's cache expires.
- **Touch is presence.** Every use needs a touch, so malware on the machine cannot sign or
  push in the background: each signature is one deliberate gesture. The opposite choice from
  the bot's key, whose touch is off because a gate tapped reflexively dozens of times a day
  is theater ([the bot's key](#the-bots-key)); a person's touch is rare and means something.
- **`-o -a 100`** is the OpenSSH key format with a slow KDF on the passphrase (the format is
  implied for ed25519; the rounds are not). Ed25519 because it is the better algorithm; the
  enclave constraint that puts the bot on P-256 does not apply to a token.

**The agent that serves it.** macOS's system `ssh-agent` does not support `sk-` keys, so a
machine runs OpenSSH's agent as a user launch agent
([farcloser/ssh-agent](https://github.com/farcloser/ssh-agent)), on its own socket; the
system agent is disabled, not killed, the only lever SIP leaves. A cached passphrase lives
in that agent and dies with it.

**Git signs every commit with it**, through `gpg.format ssh` and `commit.gpgsign`
([CONTRIBUTING.md](https://github.com/farcloser/.github/blob/main/.github/CONTRIBUTING.md));
the public key goes into each repository's `.lint-signers` with the first contribution, so
`git log --show-signature` and `git tag -v` resolve offline. Signing and the DCO sign-off
are different things and both are required
([GIT.md](https://github.com/farcloser/.github/blob/main/.github/GIT.md)).

**If the token is lost or stolen.** Revoke the key on GitHub at once, as an authentication
key *and* as a signing key: the two registrations are independent, and either one left
behind is a door. Then generate a new key on a new token and register it. Commits the old
key signed keep their badge: GitHub records a verification at push time and never
re-evaluates it
([persistent commit signature verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification#persistent-commit-signature-verification));
only a commit signed with the removed key and pushed *after* the removal shows as
unverified. Locally, verification reads `.lint-signers`, so the old key's entry stays as the
record, with the new key beside it.

**Changing the passphrase** rewrites the key file's protection, not the key:
`ssh-keygen -p -f ~/.ssh/id_ed25519_sk`.

Non-resident keys, by design: the key handle lives in `~/.ssh`, the secret in the token,
and a FIDO PIN does not apply. Resident keys and PIV are the alternatives when a key must
move between machines with the token alone; neither is in use.

## The bot's identity

**A separate machine-user account, never the human's key.** An SSH key on a personal
account is account-wide: everything the human can push to, the agent could too, and every
commit the agent made would be indistinguishable from the human's. A dedicated account
(`closer-claudio` at farcloser) fixes both:

- **Blast radius** is exactly what the organization's `agents` team is granted. The bot is
  a member whose only access comes through that team, on an organization whose members'
  base permission is read — the floor `limen github check -org` keeps. Every governed
  repository grants the team write: the `agents-team` check asserts it and `limen github
  fix` grants it. Revocation is one team membership.
- **Attribution** is honest: commits are authored and signed by the bot, forever. It is
  also the *whole* of the attribution: a `Co-Authored-By:` trailer naming the model, and
  nothing that points at the tooling — no vendor or product link, no "generated with"
  banner, no session URL or identifier, in any commit message, pull request, comment,
  issue or release note. A session URL is a leak of the human's private session; the rest
  is advertising on someone else's repository. Harness defaults ask for exactly these, so
  `AGENTS.md` states the rule and the agent greps for it before pushing.

Not chosen: a key on the human's account (full access, weak attribution); per-repository
deploy keys (cannot be signing keys, so every commit shows *Unverified* and the
`limen:main` ruleset refuses the merge); a GitHub App (the tightest scoping on paper, but
its private key is a file on disk, the thing to avoid).

## The bot's key

**Hardware-bound, non-exportable, no touch.** The bot's key lives in the Secure Enclave,
served by [taciturn](https://github.com/forkcloser/taciturn)'s agent (a machine still on
[Secretive](https://github.com/maxgoedjen/secretive) names that agent's socket to the
installer). It cannot leak; it can only be used by a process that reaches the agent's
socket on that one machine. Touch ID
is deliberately **off**: a gate the human taps reflexively, dozens of times a day, is
theater — worse than no gate, because it feels like control. The human's own YubiKey keeps
its touch, where a touch is rare and means something.

**The key answers while the screen is locked, until the next boot.** taciturn creates the
bot's key as `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` (`taciturn key create …
--use locked`): from the human's first unlock after boot it signs with the screen locked,
which is exactly when an agent works unattended. So a signing or push failure that follows
earlier successes in a session is a defect — the agent gone, its socket closed, the key's
policy refusing the peer — and is reported as broken tooling, never waited out. Only a key
still in Secretive, which creates every key `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`,
stops once the screen locks; there the agent finishes the work in its worktree — green, its
commit message written — and leaves it ready for the human's return (a per-key opt-in to
stay usable while locked is proposed upstream,
[maxgoedjen/secretive#819](https://github.com/maxgoedjen/secretive/pull/819)).

**The veto is the rulesets.** `limen:main` requires a pull request with signed commits *and
one approving review*: the bot can push branches and open pull requests, and cannot land
anything on `main`. The approval is what makes that a capability and not a convention: a
write-level identity can call GitHub's merge endpoint, and so can every installed App whose
`contents: write` covers both pushing a branch and merging a pull request, which an app that
raises dependency pull requests necessarily has. Requiring an approval closes it for all of
them at once, because GitHub will not let an author approve their own pull request.

The key is ECDSA P-256 because that is the only curve the Secure Enclave implements.
Ed25519 is the better algorithm; P-256 in hardware with an RNG the enclave controls is an
acceptable trade for a bot whose damage is bounded by rulesets and revocable in a click.

## The bot's sandbox

The agent runs in a sandbox that denies `~/.ssh` and every unix socket it was not told
about. The setup opens exactly one thing: the signing agent's socket (taciturn's, or
Secretive's on a machine still there). The agent can ask the enclave to sign; it can never
read a key. Three consequences the script handles:

- **Host keys.** `~/.ssh/known_hosts` is unreadable, so GitHub's host keys are pinned into
  a file the sandbox can read, from GitHub's published list, and ssh runs with strict
  checking against it. No trust-on-first-use.
- **The ssh client.** The toolset's own OpenSSH, the fork with security keys and ECDSA
  built in, serves the bot by its real path: `core.sshCommand` runs its `ssh` with the bot
  key's algorithm as the one accepted (the fork's default list carries no ECDSA, so the key
  would never be offered), and `gpg.ssh.program` a wrapper around its `ssh-keygen`. The
  real path, because a session's hermetic root carries no openssh for the toolset's link to
  resolve, and git execs the signer with no shell in between; the installer resolves it
  from the machine toolset and runs again after a bump moves it.
- **The egress proxy.** Every outbound connection goes through the sandbox's per-session
  proxy, which requires credentials. The sandbox's own ssh wiring offers none, so a small
  ProxyCommand helper speaks authenticated HTTP CONNECT to the same proxy. The proxy's
  domain allowlist still applies; nothing is bypassed.

The bot's git configuration is one file, `~/.claude/gitconfig`, written whole by
`limen-install-agent` and named by `GIT_CONFIG_GLOBAL` in the sessions' environment: the
identity, the signing wrapper, the ssh command, the `c` alias, an empty excludes file,
`push.autosetupremote` off, the gh credential helper. The `c` alias is `env -u
GIT_SSH_COMMAND git`, and every git command a session runs goes through it, local ones
included: the harness sets `GIT_SSH_COMMAND` in the sessions' environment, which outranks
`core.sshCommand`, so a raw `git fetch` reaches the system ssh and the machine's own keys
instead of the rig's ssh and the bot's key, and a raw local command is the habit that
produces the raw network one. A network command names the transport as well, `git c -c
core.sshCommand="$(git c config -f ~/.claude/gitconfig core.sshCommand)" fetch`, so the
rig's ssh is used whatever the environment carries. A session's git reads neither the
human's `~/.gitconfig` nor `~/.config/git`, so neither needs to be readable, and a shell
the human opens never sees the bot. It replaced a numbered `GIT_CONFIG_*` block in the
environment on 2026-10-12, the day a read deny on the directory the human's `.gitconfig`
links into took git down in every session
([farcloser/limen-install#97](https://github.com/farcloser/limen-install/pull/97)).

**The sessions' tool root.** Everything a session installs or caches lives under one
directory of its own, `~/.local/share/limen-agent`, created by `limen-install-agent`: a
hermetic root of their own (`HERMETIC_ROOT_DIR`, holding the toolset's pins copied from the
machine's root, a `bin/hermetic` that is a symlink to the machine's binary, which every tool
link hermetic plants in that `bin` points at and which follows the machine's nightly bump
where a copy would not, and a store of its own), a home for the verifiers (`LIMEN_AGENT_HOME`:
slsa-verifier keeps its TUF cache under `$HOME/.sigstore` and reads no other variable,
[known upstream bugs](./upstream.md)), the Go module and build caches, cargo and the XDG
cache. The sandbox's write list is the directories the installer was given plus that root;
the human's tool roots, Go caches, cargo and sigstore cache are not writable to a session,
so nothing a sandboxed command writes is ever executed outside the sandbox. The installer
puts the pins in the root and nothing per repository: a session approves a checkout's
policy and installs its pins itself, and every real install runs with `HOME` at the agent
home, which the `tools` recipes and the contribute skill do by themselves. For that first
command to run at all, the harness's own `env`, resolved through PATH outside the sandbox
with the checkout's manifest, must not be a toolset link: limen's registry links no `env`.

**A session's `PATH` is the harness's, and no rc file changes it.** Before every Bash call
the harness sources a shell snapshot it wrote at process start, and that snapshot's
`export PATH=` is a literal copied from the harness's own process environment, not read back
from the shell that sourced the rc files. A line in `~/.zshrc` putting the agent root's
`bin` first when `CLAUDECODE` is set never changed a session's `PATH`: on 2026-10-12 a
snapshot written at 11:13, eighteen minutes after that line landed, still led with the
human's tool `bin`. The installer no longer writes the line
([farcloser/limen-install#87](https://github.com/farcloser/limen-install/pull/87)). What a
session runs: the machine root's `bin` first, read-only to it, whose one binary dispatches
into the sessions' root through `HERMETIC_ROOT_DIR`; a tool linked only in the sessions'
root is reached through `just`, whose `PATH` is hermetic.

**A link in the root's `bin` runs only what the checkout declares.** The root's `bin`
carries a link for every tool any repository ever pinned there, so `xz` is found on
`PATH` from any directory; but the link resolves its name against the checkout's
`.hermetic/hermetic.yaml`, found by hermetic's walk upward from the current directory, then
the root's own pins, the toolset's, and a tool neither declares fails with the library's
error 004, "command is not found", whatever the host has installed. On 2026-10-14
forkcloser/compress's interop tests looked `xz` and
`zstd` up through a `just` recipe's hermetic `PATH` and were red in every session, while
CI, on a runner with both tools, stayed green. A tool a repository runs, tests included,
is pinned in that repository's `.hermetic/hermetic.yaml`; one no registry carries (xz and zstd, as of
aqua's standard registry v4.574.0) is probed first, and the test that cannot find it skips
by name. What the host happens to have is not a dependency.

**A `Write` deny rule binds Bash too.** `permissions.deny` rules for the file tools are
also compiled into the sandbox: a `Read` deny becomes a read denial for every command, and
a `Write` deny becomes a write denial, even inside a directory the sandbox's write list
allows. On 2026-10-12 the rules `Write(//~/.*)` and `Write(//~/.*/**)`, meant to keep the
file tools out of the dotfiles, made the agent root under `~/.local/share` unwritable to
Bash at once: aqua could not write its timestamps and `touch` in the root failed. Name the
paths the file tools must not write one by one, and never with a pattern that covers a
directory sessions write through the shell.

**A repository's worktree registry is closed to a session holding a live worktree in it.**
`git worktree remove` and `git worktree prune` fail with "failed to delete
'.git/worktrees/<name>': Operation not permitted" for every entry of that repository,
whatever the session's working directory, while the session has a worktree of its own
there; the same prune from a session without one succeeds (2026-10-13, saprobe-dsd and
limen). The harness manages `.claude/worktrees` and keeps its hand on the registry it
depends on. Prune from a session that holds no worktree in the repository, or leave the
stale entries to the human; do not retry from the one that does.

**A fixed list of paths beneath the working directory is never writable.** The sandbox
runtime appends a mandatory deny to every write profile it builds: the files `.gitconfig`,
`.gitmodules`, `.bashrc`, `.bash_profile`, `.zshrc`, `.zprofile`, `.profile`, `.ripgreprc`
and `.mcp.json`, the directories `.vscode`, `.idea`, `.claude/commands` and
`.claude/agents`, and `.git/hooks`, each as a literal under the working directory and as a
glob at any depth beneath it; `.git/config` is on the list too unless the harness's
`allowGitConfig` setting is on (sandbox-runtime `src/sandbox/sandbox-utils.ts`,
`DANGEROUS_FILES` and `getDangerousDirectories`, applied by `macGetMandatoryDenyEntries` in
`macos-sandbox-utils.ts`). No entry in the write configuration lifts them; only a session
with no write configuration at all is without them. For a bot that runs git, `.git/config`
is the one that bites first: `git push -u`, `git remote add` and `git config` in a
repository under the working directory fail with "could not write config file …: Operation
not permitted" while the push itself succeeds, so a branch is pushed and never tracked,
and nothing else is wrong. `git worktree add` of a repository that tracks one of those
paths fails at checkout, "unable to create file … Operation not permitted" then "Could not
reset index file" (farcloser/onboarding, which tracks
`home/.zshrc` and three siblings, 2026-10-18); a repository with submodules fails the same
way on `.gitmodules`; a build that unpacks an archive carrying `.vscode/settings.json` fails
at that file (forkcloser/openssh). A sparse worktree that leaves those paths out is the
workaround for a checkout; the rule itself is upstream's, and not a setting.

**The sandbox's `.` is wherever the session's shell last ended up.** The write list's `.`
entry is the session's primary working directory, and where the harness lets a `cd` persist
across calls (a setting, not a constant: another harness resets the shell to the primary
directory after every call), the `cd` moves it: what the session may write changes with it,
a command that ran from one worktree is refused from another, and a relative path lands in
whichever tree the shell is in (an amend meant for one fork's worktree rewrote the branch
of another, 2026-10-16). Every git command and every write names its tree by absolute path,
under the session's root or an explicit write entry; a persisted `cd` is state, not an
address.

**A sandbox exclusion matches the bare command.** A command the human took out of the
sandbox — `just kernel`, which boots a VM the sandbox cannot — is excluded only as written,
alone on its line. Wrapped in a pipe, a `$(…)`, an `&&` chain or an environment prefix, it
runs sandboxed and the capability is simply absent: Virtualization.framework reports no
hardware, and the build meant to prove the change never happens. Run it on its own, read
its result, and never report a build or a boot you did not watch finish: a toolchain bump
went out marked ready on a sandboxed attempt that had not built, and the release failed on
it ([farcloser/ossein-kernel#79](https://github.com/farcloser/ossein-kernel/pull/79), fixed
in [#109](https://github.com/farcloser/ossein-kernel/pull/109)).

**A read denial on the keychain blinds every keychain client.** The login keychain's
contents are encrypted, but a client still has to open the file: with
`Read(~/Library/Keychains)` in the deny list, `security find-generic-password` reports the
item missing, `gh auth token` prints nothing, and every `gh` call fails with 401 as if the
bot's token had expired. gh keeps its token in the keychain, not in `hosts.yml`. On
2026-10-12 the rule, added to a sweep of credential stores, took the bot offline for half an
hour and was first read as an expired token. Deny the stores a session must never read; the
keychain is one it uses.

**The human's per-user temp and cache directories stay the human's.** macOS gives each user
a scratch directory, `getconf DARWIN_USER_TEMP_DIR` under `/var/folders`, and a cache
beside it; the human's installers and apps extract into them and read back from them, so a
sandboxed write there is a planted file waiting for the next process, the hole the agent root
closed for tools. Both left the write list with the agent root
([farcloser/limen-install#85](https://github.com/farcloser/limen-install/pull/85)), and two
base tools showed it on 2026-10-12: Apple's `mktemp -d` ignores `TMPDIR` and dies there, and
the `clang` shim runs `xcrun`, whose cache lives there. The fix is on the tool side, not the
list: `mktemp -d "$TMPDIR/x.XXXXXX"` works sandboxed, and cgo builds pin `CC` and `SDKROOT`
from `xcode-select -p`, which removes `xcrun` from the path.

**A file in `$TMPDIR` that nothing touches for four days is deleted at midnight.** The
sandbox's `$TMPDIR` is a directory under `/tmp`, and macOS's `tmp_cleaner` (since 14.0)
runs there daily at 00:00, removing every file whose access, modification and change times
are all more than three days old. A clone kept there loses its untouched files one night,
and git then shows each as a deletion that `commit -a` or `add -A` would record: on
2026-10-14 the limen clone this book is written from woke with 111 tracked files gone, all
last touched 2026-10-09, while everything touched since survived. The sweep does not stop
at the working tree: loose objects under `.git/objects` go the same night, and the clone is
then corrupt, not merely bare: `git status` reports "error: invalid object 100644 … for
'.lint-signers'", and `git checkout -- .` cannot restore what the object store no longer
holds (2026-10-19, on a clone made 2026-10-12). A clone cannot move into the session root,
where the harness refuses to write `.git/config` ([known upstream bugs](./upstream.md)), so
a clone in `$TMPDIR` is good for the day it is made: anything older than three days is
thrown away and cloned afresh, never repaired, and nothing in it is ever staged with `-a` or
`-A`.

**Bypass mode cannot be refused from the user's settings.** `disableBypassPermissionsMode:
"disable"` is honoured in managed settings only; placed in `~/.claude/settings.json` it
changes nothing, and a session switched to bypass mode a minute after it was written
(2026-10-12). So the one sandbox exclusion, UTM's `utmctl` by absolute path, runs
unprompted whenever a session is in bypass mode, and nothing in the rig prevents that mode.
Bypass mode is the human's break-glass and stays one; the rule is to leave it the moment the
emergency is over, and never to run a VM tool from a session in it.

**A read denial follows every symlink into the directory.** `~/.gitconfig` is a link into
`~/Vault`; the day `Read(~/Vault)` joined the deny list, every git command in every session
died with `fatal: unable to access '/Users/…/.gitconfig': Operation not permitted`, for the
twenty minutes it took to see that the rule, not the dot-rules, was the cause (2026-10-12).
The sandbox resolves the link and judges the target. Before denying a directory, list the
home's symlinks and their targets (`for f in ~/.* ~/*; do [ -L "$f" ] && echo "$f -> $(readlink "$f")"; done`)
and keep the target readable or move the file; the alphabet scheme leaves `V*` readable for
this reason alone.

**A default that moves under a running fleet is checked before anything is written.** The
agent installer's default socket moved from Secretive to taciturn the hour taciturn was
released ([farcloser/limen-install#95](https://github.com/farcloser/limen-install/pull/95)),
and the next run, on a machine where taciturn was not installed yet, pinned the dead path
into the signer wrapper, the ssh command and the sandbox's socket list: signing stopped in
every session until a re-run with `--socket` (2026-10-12). The same run showed a second
defect the old socket had hidden: `core.sshCommand` is parsed by `sh` and then by ssh, so a
path with a space needs ssh_config's double quotes inside the shell's single quotes,
`-o 'IdentityAgent="…/Application Support/taciturn/agent.sock"'`; the bare single quotes
vanish in `sh` and ssh reports "keyword identityagent extra arguments". The installer now
refuses to run when the socket does not exist and writes the doubly quoted form
([#97](https://github.com/farcloser/limen-install/pull/97)). An installer that pins a path
proves the path exists first, and a change of default is tried on a machine that has the
new thing before the fleet's installer learns it.

**A change to the rig is tried in one session before it reaches them all.** Settings
edits apply to every running session at once, and the harness itself runs through the rig:
each Bash call starts as `env`, resolved through the harness's own `PATH` outside the
sandbox, so with the toolset first on `PATH` that `env` is the coreutils link, and an empty
or sick root stops every shell before any command runs. On 2026-10-12 a write-list and
`env` change meant to give sessions a tool root of their own pointed `AQUA_ROOT_DIR` at a
root nobody had populated; every session's shell died at once, and the bootstrap could not
run from inside the sandbox because slsa-verifier keeps its TUF cache under `~/.sigstore`
([known upstream bugs](./upstream.md)), which the same change had just made read-only. A
throwaway project with its own `.claude/settings.local.json` and one fresh session would
have shown both before anyone else was affected. Populate before you point, add before you
remove, and restart for what only a restart applies.

**A word an expansion produces is never an assignment.** The shell recognises `NAME=value`
before it expands anything, so `HOME="$X" hermetic install` typed is an assignment, and
`${X:+HOME="$X"} hermetic install` is a command named `HOME=/…` that does not exist. The
prefix that gave sessions' installs their own home shipped in that second form
([farcloser/limen#367](https://github.com/farcloser/limen/pull/367)) and was never reached
before the recipe died earlier, so nobody saw it fail. Pass the assignment through `env`,
which takes it as an argument after expansion and, with the variable unset, runs the bare
command ([#373](https://github.com/farcloser/limen/pull/373)). A conditional prefix is
proven by running the recipe both ways, variable set and unset.

**An excluded command names a fixed, human-owned binary by absolute path, or nothing.** A
sandbox exclusion is matched on the typed command, but what runs unsandboxed is whatever
that name resolves to. `just kernel` is the `kernel` recipe of the current `.justfile`,
repository content a session edits; `ossein *` resolves through aqua into the agent root,
which sessions write; `./.build/ossein *` is a binary the session just built. Each was an
escape with a prompt in front of it, and the prompt shows the name, not the contents. On
2026-10-12 the list went from nine entries to one,
`/Applications/UTM.app/Contents/MacOS/utmctl *`, after every owner was asked what it types:
the VM boots the others served cannot run sandboxed, because the sandbox denies the
`kern.hv_support` sysctl ([known upstream bugs](./upstream.md)), so a boot is a human-run
step until Anthropic moves. The rule for the next request: a name on `PATH` or a recipe is
refused on sight; a human-owned binary a session cannot replace, by its absolute path, may
be weighed.

**Breakage is reported, not routed around.** An agent that finds the rig not working — the
installed ssh refused, signing unable to reach the agent, a recipe failing — says so first
and stops there: a private workaround that keeps the work moving hides the defect from the
one person who can fix it, and once hid a broken installer for a whole session while pushes
took another path. A workaround needs the human's agreement and is named as one each time.
A key that stops answering after it worked is the exception: a locked screen, not a broken
rig ([the bot's key](#the-bots-key)).

## What stays manual

Three things, all one-time, all deliberately human:

1. Creating the bot's GitHub account, inviting it to the organization, adding it to the
   `agents` team.
2. Creating its key in taciturn (`taciturn key create … --use locked`, the fork's `ssh`
   and `ssh-keygen` as its peers) and registering the public key on the account as both an
   authentication key and a signing key.
3. Running `limen-install-agent`, which does everything else and verifies.

The working agreement itself needs no machine step: it travels with every repository as the
content-pinned `AGENTS.md` (imported by a seeded `CLAUDE.md`), one of the
[mandatory files](./mandatory-files.md#canonical-agentsmd), so it loads whichever repository
a session starts in.

## Per repository

The bot's public key belongs in `.lint-signers`, next to the human's, so `just lint`'s
commit check verifies its signatures exactly as the human's and fails a commit neither key
signed. The file is the same in every repository and hand-copied today; it has the shape of
a canonical, content-pinned file, and making it one is the planned next step. Until then
the script prints the line to add.

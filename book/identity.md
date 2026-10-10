# Identity and keys

Who commits, with what key, and what the key can and cannot do. This chapter is the human
half; the bot's identity and key model are in
[coding agents as contributors](./agents.md#the-identity-model) until they move here.

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
  ([the bot's key model](./agents.md#the-key-model)); a person's touch is rare and means
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

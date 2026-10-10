# Homebrew

A repository that is a Homebrew tap: how its formulas are linted, what they pin, and
where the tap is tested.

## Homebrew formulas

A repository that is a Homebrew tap — formulas under `Formula/` (sharded subdirectories
included), casks under `Casks/`; the **modern layout only**, by decision: brew still reads
the legacy locations (`HomebrewFormula/`, bare `*.rb` at the repository root), the recipes
deliberately do not — lints them with brew's **own** tooling — `just do lint homebrew` runs `brew style`
(Homebrew's vendored RuboCop with the formula cops) and `brew audit --strict`;
`just do fix homebrew` is `brew style --fix`. No Ruby toolchain is ever installed for
this: brew vendors its own Ruby and RuboCop, and plain RuboCop would not know the
formula rules anyway. Both recipes pass vacuously when the repository carries no
formulas.

Two deliberate exceptions, named because they cut against doctrine:

- **brew is not on the hermetic PATH and never will be.** It is a machine-layer package
  manager that aqua cannot pin, and the PATH exclusion exists to stop machine tools
  substituting for pinned ones — but brew substitutes for nothing here; it *is* the
  subject under test. The shared `main.just` captures its location from the **ambient**
  PATH at startup, before the hermetic PATH locks down (`BREW_BIN`, overridable by
  exporting it) — the invoking shell knows where brew lives, whatever the prefix — and
  the recipes fail with guidance when the capture came up empty; the hermetic PATH
  itself is untouched.
- **`brew audit` needs a tap identity.** brew addresses formulas by tap name, never by
  path, so the project declares which tap it is (`export LINT_HOMEBREW_TAP :=
  'user/name'` in the root `.justfile`) and the audit recipe registers the working tree
  under that name for the duration of the run — a symlink, so the audit judges the
  working tree, not a stale clone. Audit flags that only make sense on CI (`--online`
  does network calls) go through `LINT_HOMEBREW_AUDIT_FLAGS`.

**What a formula pins.** Every source a formula fetches is pinned by content, like
everything else: a tarball by its `sha256`, a resource and a backport patch likewise. A
formula for one of our own repositories builds from that repository's signed release,
`tag:` **and** `revision:` together: the tag names the release and Homebrew infers the
version from it, the revision ties the checkout to that exact commit, and one Renovate
`github-tags` manager rewrites both. Never `branch:` under a constant `version "dev"`:
Homebrew compares versions to decide what is outdated, and `dev` equals `dev`, so a bump
of the branch, or of a bare `revision:`, reaches no machine that already has the formula,
and a `post_install` meant to re-run on upgrade never runs again (limen's own formula sat
on a commit pin that way until limen-install had a release). Upstream's `head` line is
stripped by the fork's patch: a build from a moving branch, pinned by nothing. The one
accepted `branch:` is a meta-formula that installs nothing but its dependencies and a
README; a change to its dependency list bumps its `revision`, so installed machines pick
it up.

**Forks of upstream formulas** are refreshed from a **pinned commit** of the upstream tap
(homebrew-core), never from its default branch: the refresh script carries the commit, a
run reproduces the committed formulas byte for byte, and taking upstream's changes means
moving the pin. The fork's modifications live in a patch regenerated with `diff -U1`
against that base (bottle block stripped first, since upstream rewrites it at every
release); applied to the base it must reproduce the committed formula exactly, which is
the check a refresh and its review make.

**Testing the tap is the tap's job.** A consumer repository that installed itself from the
live tap tested whatever release the tap's `main` pointed at, at an unpinned revision, not
its own tree (ssh-agent, until farcloser/ssh-agent#23). The proof beyond linting, `brew
install` (which builds every formula from source, a tap shipping no bottles), `brew test`,
and the service started and stopped, runs in the tap's own CI from the **checkout**: the
working tree is symlinked in as the tap, so a dependency on another formula of the same
tap resolves to the same tree, and `HOMEBREW_NO_AUTO_UPDATE=1` is exported first, because
brew's auto-update rebases every tap it finds, the symlinked checkout included, onto its
remote. The install mutates the machine's live brew, so the recipe runs it on CI's macOS
leg only (a `CI` guard) and skips, not fails, elsewhere. A project recipe that must hand
brew's directory to a script **appends** it to `PATH`, never prepends: the script needs
brew by name, and nothing else of Homebrew's may shadow a pin.

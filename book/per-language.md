# Per-language rules

The [mandatory files](./mandatory-files.md) apply to every repository without exception.
This chapter collects the rules that apply *conditionally* — only when a project uses a
given language or tool. `limen` detects the trigger automatically and enforces the rule only
when it is relevant, so a pure-Go repository is never asked for a shell config, and a
repository that ships shell or YAML cannot quietly skip one.

The shape of every per-language rule is the same: **if the trigger is present, the
requirement is mandatory; if it is absent, the rule does not apply** and `limen` reports
nothing for it.

## Shell — `.limen/.shellcheckrc`

| Trigger | Requirement |
|---------|-------------|
| Always — every repository. | A `.limen/.shellcheckrc` is present and matches the canonical baseline **exactly**. |

Any project that ships shell must lint it, and lint it *the same way everywhere*.
[ShellCheck](https://www.shellcheck.net) is the linter; `.shellcheckrc` is how its
configuration — which checks are disabled, which shell dialect is assumed — travels with the
code.

**Why unconditional**, despite living in the per-language chapter. This rule used to fire
only once a repository actually contained shell, which read as the tidier design and was the
wrong call in practice. `just do lint shell` passes `--rcfile .limen/.shellcheckrc`
unconditionally, so a repository without the file gets `Warning: unable to read --rcfile
.limen/.shellcheckrc` on every run — a message that reads like a broken setup, not like a
rule that does not apply yet. And projects grow shell: the config would appear the day
someone adds a first script, as a surprise edit in an unrelated pull request. A config file
that is simply always there is one less thing to explain, and an unused one costs nothing.

The YAML twin below is still conditional, but only nominally: every repository carries YAML
(the workflows alone guarantee it), so the trigger fires everywhere in practice.

**What `.limen/.shellcheckrc` must be.** The file is **content-pinned**: `limen` requires it to
equal the canonical baseline **byte for byte** — the directives that follow sourced files and
opt into the high-value optional checks ShellCheck ships but does not run by default. The
baseline is defined once and lives in one place: this repository's own
[`.limen/.shellcheckrc`](../.limen/.shellcheckrc), embedded into `limen` and exposed as
`rules.CanonicalShellcheckrc`. **That file is the source of truth.** A repo may not add, remove,
or reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to
the canonical. (This is the same exact-match rule as the `.editorconfig`, the `.justfile`, and
the `.limen/just/*.just` modules — only `.aqua/aqua.yaml` uses the subset, "contains the baseline"
model, and `.gitignore` is merely seeded once when absent.)

**How to accommodate specific projects**

Projects that need overrides can use inline `# shellcheck` disable directives.
Global changes / improvements to the baseline should be submitted to project limen for review,
as the shared file should never be modified locally in a project.

## YAML — `.limen/.yamlfmt`

| Trigger | Requirement |
|---------|-------------|
| The repository contains YAML files. | A `.limen/.yamlfmt` is present and matches the canonical baseline **exactly**. |

YAML is whitespace-significant and easy to format inconsistently — indentation, quoting, and
flow vs block style all drift between authors and editors. Any project that ships YAML must
format it *the same way everywhere*. [yamlfmt](https://github.com/google/yamlfmt) is the
formatter; `.yamlfmt` is how its configuration travels with the code instead of living in
someone's head or CI script. A repo that contains YAML but no `.yamlfmt`, or one whose
`.yamlfmt` differs from the baseline, is unformatted or formatted inconsistently; both are
failures.

**What counts as YAML.** `limen` treats a file as YAML when it is a `*.yaml` or `*.yml` file.
The scan skips `.git` and vendored dependency directories (`node_modules`, `vendor`), so a
dependency's manifests never trigger the rule — only YAML that is genuinely *ours* does.
(`.yamlfmt` itself is not matched: its extension is `.yamlfmt`, not `.yaml`/`.yml`.)

In practice the trigger always fires: every compliant repository carries `.aqua/aqua.yaml`
([tooling is mandatory](./tooling.md)), so the YAML rule is effectively universal. It stays a
per-language rule because the *mechanism* is what limen checks — the trigger, not the mandate
— and the uniform shape keeps the chapter honest if the trigger set ever changes.

**What `.limen/.yamlfmt` must be.** As with `.limen/.shellcheckrc`, the file is **content-pinned**:
`limen` requires it to equal the canonical baseline **byte for byte** — the yamlfmt settings that
keep formatting consistent across repos and match our editorconfig indentation. The baseline is
defined once and lives in one place: this repository's own
[`.limen/.yamlfmt`](../.limen/.yamlfmt), embedded into `limen` and exposed as
`rules.CanonicalYamlfmt`. **That file is the source of truth.** A repo may not add, remove, or
reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to the
canonical.

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

## Rust — cargo is pinned through rustup, never ambient

The Rust modules (`just do lint rust`, `just do fix rust`) call `cargo`, and no repository
exercises them yet; this records the decision ahead of the first one, so it is made rather
than discovered. cargo is not brew: brew is the subject under test and substitutes for
nothing, which is why it alone is captured from the ambient PATH. A rustup-managed cargo in
`~/.cargo/bin` is exactly the machine tool the hermetic PATH exists to hide — and, unlike
brew, it has a pin story. The toolchain is pinned in two halves:

- **rustup itself through aqua.** The standard registry carries `rust-lang/rustup` as an
  `http` package from the project's own static host, checksum-verified against the `.sha256`
  published next to each installer. That is the sourcing ladder's rung 2 without the
  signature — rustup publishes no signature for the installer — so a bump is verified by
  hand like every other checksum-only pin, and the pin lives in the Rust repository's
  `.aqua/aqua.yaml`, not the baseline: no other repository pays for it.
- **The toolchain through `rust-toolchain.toml`.** rustup reads it from the working tree,
  so the channel and components are a committed, reviewed file — the same shape as
  `go.mod`'s `go` line — and rustup verifies what it downloads against the release
  manifest. The recipes reach cargo through the pinned rustup, never through the proxies in
  `~/.cargo/bin`; that wiring lands in the Rust modules the day a repository needs it, and
  until then the modules stay as they are: named, never in a default, and not runnable.

## Go — one lint baseline, per-project carve-outs: `.lint-go.yaml`

Linting Go takes many tools, each configured its own way: golangci-lint reads a long YAML
file, go-licenses takes its allowed list and its ignores as flags, the rest take flags of
their own. And golangci-lint has no overlay — one configuration file, first found wins, no
`extends`, `include` or merge, a request upstream has parked since 2020 — so every Go
repository used to carry its own hand-edited copy of the file, the copies drifted from the
baseline and from each other, and a change to the lint policy could not be applied to any of
them without a manual three-way merge, so it was not applied. The per-project ignores for
go-licenses lived in an exported variable in the root `.justfile`, a hidden override.

The shape now is one baseline, one overlay, rendered at lint time:

- **The baseline is limen's**: `.limen/lint-go.yaml`, content-pinned like the other files
  under `.limen/`, never edited by a project, and read from the tree by `limen-lint-go`, the
  second binary of limen's release. Pinned rather than built into the driver so that a change
  to the policy is the diff of the pull request that bumps limen, where `limen fix` rewrites
  the file, the same review every other baseline gets. It carries
  the golangci-lint configuration every repository starts from, the go-licenses allowed list,
  and the oldest golangci-lint release every linter name in it exists in. Two placeholders are
  filled from the project's `go.mod` at render time: the module path (depguard's allow list)
  and its first two elements, host and owner, the sibling modules `gci` groups together.
- **The overlay is the project's**: a root `.lint-go.yaml`, seeded once as commented examples
  and never rewritten. It uses the baseline's sections and golangci's own vocabulary, so it
  reads as a fragment of the file it patches, and holds only what the project adds, changes
  or takes out. The rules, one per kind of key:

  | Overlay key | Effect on the baseline |
  |---|---|
  | `golangci.linters.disable`, `…enable` | moves the linter between the two sets (the baseline runs `default: all`, so this is golangci's own meaning too); a linter already where it is going is a note, not a carve-out |
  | `golangci.formatters.enable` | adds a formatter (golangci's schema has no `formatters.disable`) |
  | `golangci.linters.exclusions.paths`, `…rules`, `…presets`; `golangci.formatters.exclusions.paths` | append; nothing in the baseline's exclusions can be removed — if a project needs that, the baseline is wrong and it is a limen change. The baseline excludes nothing in tests: what a project tolerates in `_test.go` is its own, and the seed shows the reviewed rule to tailor from |
  | `golangci.linters.settings.<linter>`, `golangci.formatters.settings.<formatter>` | merge key by key: a scalar overrides, a mapping recurses, a list appends (a value the baseline already lists is a note); in a list of named mappings — revive's rules — a name the baseline has overrides that entry's keys, so a project changes one rule's arguments without restating the list, and a new name appends |
  | `golangci.formatters.settings.gci.sections` | inserted just before the baseline's `localmodule` section rather than appended after it, in the overlay's order: the baseline groups imports as standard, third party, the project's own organization (`prefix(${MODULE_PREFIX})`, derived from `go.mod`), then the module itself, so a project whose siblings live under another organization adds `prefix(<that organization>)` and those imports group beside its own instead of among third parties |
  | `licenses.allowed` | replaces the allowed list |
  | `licenses.ignore` | appends the modules go-licenses skips (typically the false positives of google/go-licenses#186) |
  | `nilaway.blocking` | overrides: `false` makes NilAway's findings print without failing the lane, for a project working off a backlog |
  | `nilaway.exclude-pkgs`, `nilaway.exclude-errors-in-files` | append: package and file prefixes NilAway leaves out, its only suppression |
  | anything else — `run`, `issues`, `output`, `severity`, `linters.default`, `exclusions.generated`, an unknown key | rejected with the key named: that is policy the baseline owns, never merged |

- **The rendered configuration is a build artifact.** `just do lint go` has `limen-lint-go`
  check the pinned golangci-lint against the baseline's floor, render the baseline with the
  overlay into `build/golangci.yml`, and print how many carve-outs it applied; golangci-lint
  then runs with `-c` on that file, which also makes it ignore any configuration at the root.
  The licenses lane splices in the flags the driver prints for it. Nothing is committed, and
  nothing at the root invites a hand edit: a root `.golangci.yml` is a stray that `limen check`
  reports, since nothing reads it. Editor auto-discovery of a root file is deliberately given
  up — the workflow is `just lint`.
- **The user story is one command.** Edit `.lint-go.yaml`, run `just do lint go`: the render
  is the recipe's first step, and the change is live.
- **The baseline and the golangci-lint pin are checked against each other.** The baseline names
  linters; a name unknown to an older golangci-lint fails the run. The baseline moves with
  limen releases, the golangci-lint pin per repository with Renovate, so either can run ahead:
  the driver refuses to run when the pinned golangci-lint is older than the release the
  baseline declares, naming both and the recipe that moves the pin. The other direction is
  safe — a newer golangci-lint warns on a retired name rather than failing on an unknown one.

**NilAway is a lane of its own.** Uber's nil-panic analyzer is not a golangci-lint linter
and will not become one: golangci-lint declined it because its false positives are by
design, and its README labels it under active development with breaking changes. So it is
`just do lint go nilaway`, in the lane's default set, built from `tools/nilaway` (a module of
its own, pinned by pseudo-version since upstream tags no releases, moved by Renovate inside
the "go modules" group like every other digest update) and run once per supported platform.
The driver carries its flags from the baseline: `-include-pkgs` bounds the analysis to the
module, without which NilAway walks the whole dependency graph. It has no per-line
suppression, which is why the overlay carries two exclude lists and a `blocking` switch: the
baseline says blocking, and a project with a backlog declares `nilaway.blocking: false` while
it works the findings off, the findings printing all the while. `limen-lint-go mode nilaway`
is how the lane learns which.

**Stale revive directives are reported.** nolintlint reads golangci's `//nolint` syntax, not
revive's, so a `//revive:disable-next-line:<rule>` naming a rule the baseline has since turned
off would silence nothing and be reported by nothing. The suppression check in the lint
recipe asks `limen-lint-go disabled revive` for the rules the rendered configuration turns off
and fails on a directive naming one: remove it, or move the finding to the linter that owns
the check now.

**What limen enforces** (the `lintgo` rule, Go modules only): `.limen/lint-go.yaml` is
content-pinned, created when missing and overwritten when drifted; a missing root `.lint-go.yaml`
is seeded; a root golangci-lint configuration fails the check and is an advisory on fix, since
its carve-outs are a human's to move. The driver itself needs no rule: it arrives with the limen
pin, in the same archive, verified by the same checksum and signature, and the aqua registry
entry for limen lists both binaries.

**Why a driver of our own, and why it ships with limen.** limen is zero-dependency and has no
YAML parser, and a module of its own keeps the parser out of limen's `go.mod`; the driver owns
configuration only, never execution — each analyzer keeps its own module, built by `_go-tool`,
and the just recipes stay the one orchestrator — and it does not import golangci-lint's
packages, whose API stability upstream does not state; it reads the binary's build information
from the standard library instead. It is a second binary of limen's release rather than a tool
directive or a repository of its own because it is not an independent tool: its subcommands are
called by limen's content-pinned recipes, its baseline is limen's policy, and its overlay
contract is what limen's rule seeds. Those three move on one clock this way. It is the one
pure-Go tool beside limen itself that a repository takes prebuilt rather than compiled by the
pinned toolchain ([tooling](./tooling.md)): the release workflow builds both with the same
pinned toolchain in the same job, so the provenance argument for source builds does not apply,
and the driver loads no Go source, so the correctness argument does not either.

## Go — judging a finding

A linter reports a pattern; whether the pattern is wrong *here* is a judgment, and the
judgment is the contributor's. Every finding gets one of three answers, never a fourth:

- **Fix it**, when the change the linter asks for makes the code better on its own terms:
  clearer, safer, more correct. An unchecked error, a leaked file, a test that cannot fail,
  a magic number with a name waiting for it — these are what the baseline exists to catch.
- **Silence it inline**, when the code is right as it is: by the rule (see below), with a
  reason that says why — a linear boot sequence, a hot path measured in a benchmark, an
  API inherited from upstream, a format's own field names. The silence is the record that
  the finding was read and the code chosen over the rule.
- **Raise the rule**, when it is wrong for a whole class of code: with limen, for the
  baseline, or in the project's `.lint-go.yaml` for what is genuinely the project's own —
  with the evidence, the findings and why they are noise. The baseline is a proposal the
  enrolled repositories converge on, and it changes when the evidence says so.

What is never an answer: **restructuring working code only to get under a linter.** A
function split into helpers that carry its state in a new type, a linear sequence broken
at arbitrary seams, an idiomatic name lengthened, a literal hoisted into a constant that
means nothing — each makes the count go down and the code worse, and the next reader pays
for it. A split is right when its parts have a name and a meaning of their own; if the
only reason for it is the threshold, the answer is the inline silence. Fixes a linter
proposes mechanically (`just do fix go`) are held to the same bar: its allowlist holds only
the fixers that cannot change behavior.

Decide a class-wide answer **before** silencing anything in that class. An inline silence
written first and an exemption added after (in the baseline or the overlay) leaves every
silence dead, and dead silences fail lint: a directive that silences nothing is reported
(by nolintlint for a linter that runs, and by `just do lint go` for a disabled linter, a
disabled revive rule, and a `#nosec` in a test file, which gosec does not judge).

One warning is not a verdict: golangci-lint's "Skipped 0 issues by rules" for a
*formatter* exclusion (gci, gofumpt, golines, goimports on a generated file) says only
that the lint pass found nothing there, since formatters report through the separate
format pass. The exclusion is live when removing it makes `just do lint go` rewrite the
file; a generated file in a nested module, which the lint pass never enters, shows the
same warning for the same reason. Test by removal before calling one dead (seen on
go-graphviz's `bind.go` and `nori.pb.go`).

## Go — silencing a finding

A finding is silenced by its **rule**, never by its **linter**. `//nolint:<linter>` is
golangci-lint's idiom, and for a linter that is a bag of independent rules — revive, gosec,
staticcheck — it switches the whole bag off for that line: the rule the author meant, and
every rule the line grows into later. The rule-level directive keeps the rest of the bag
armed, and it is *checked*: a directive naming the wrong rule still fails, so the
suppression stays an honest record of one accepted finding.

The forms, per linter:

- **revive**: `//revive:disable-next-line:<rule>`, or a `//revive:disable:<rule>` …
  `//revive:enable:<rule>` block around a region. `//nolint:revive` is banned outright — it
  also proved environment-nondeterministic across the per-platform legs, where the
  suppression then flakes as "unused"; revive's own directives are invisible to
  nolintlint and stable.
- **gosec**: `// #nosec G### -- reason`, gosec's own directive, which golangci-lint honors
  on the line itself or on the line above. gosec does not look inside a `//nolint:`
  comment, so a line that needs both gets two comment lines: the `#nosec` first, the
  `//nolint:<other>` second. A bare `#nosec` silences every rule and is banned, as is
  `//nolint:gosec`.
- **staticcheck**: golangci-lint ignores staticcheck's own `//lint:ignore` directive, so
  `//nolint:staticcheck` is the only form that works — and the reason must then name the
  check (`//nolint:staticcheck // ST1003: …`), so the suppression still records one rule
  and the ids are already in place for the day the selective form is honored.
- **single-purpose linters** (wrapcheck, noctx, gochecknoglobals, …): `//nolint:<linter>
  // reason` is already rule-level. Bare `//nolint` and `//nolint:all` are banned.

A `#nosec G115` is the common case, and its reason is held to a shape: the bound, and
where it was established ("readInfo bounded the nid through checkNid", "phys was checked
against MaxInt64 >> BlkSizeBits just above", "the filter count is checked to be one to
four"), never "safe" or "fits". Where nothing bounds the value, the finding is a bug and
the answer is the check that refuses it with the module's sentinel (erofs `mkfs_stat.go`
refuses a device number a 32-bit field would truncate; xz `format.go` rejects a size that
wraps negative as corrupt), and the silence names that check. Never a file-level or
module-level exclusion, however many findings: erofs answers 91 one by one, and answering
them found four real truncations in a reader that had been fuzzed
(https://github.com/forkcloser/erofs/pull/95, https://github.com/forkcloser/erofs/pull/96).
Each finding is a place where an untrusted number meets a fixed width, which is where a
format reader breaks.

`just do lint go` enforces this after golangci-lint runs: nolintlint polices the *shape*
of a directive, not which linter it names, so the recipe greps the tracked Go files for
every banned form and fails with the fix spelled out.

## Go — fixing a shadowed error

govet's `shadow` reports an inner declaration hiding an outer variable that is read again,
and in practice that is almost always an `err`. The fix keeps the `if` and assigns instead
of declaring, so the function's own `err` carries the value:

```go
if err = f(); err != nil {           // was: if err := f(); err != nil {
if _, err = w.Write(b); err != nil { // was: if _, err := w.Write(b); err != nil {
```

Reuse the variable; do not invent new ones. A fix that renames (`closeErr`, `parseErr`,
`err2`, one per site) fills a function with near-identical names for one value, so the
reader has to check which one each check reads. Not a split into `err = f()` and a separate
`if err != nil {` either: that doubles the diff, and wsl_v5 then wants blank lines around the
pair. When the `if` also declares a new value (`if ok, err := …`), declare that value on the
line above and keep the assignment. A new name only where writing the outer variable would
itself be a bug, typically a goroutine that must not write the enclosing `err`: then one
plain name, and the reason in the commit message. gocritic's `sloppyReassign` reports
exactly the assigning form, which is why it is off in the baseline: the two cannot both
pass.

## Go — the baseline version

A module's `go` directive is the earliest Go release still supported upstream, written as
that release's first version, `go 1.N.0`, unless a dependency requires a later patch of the
same release. Go supports its two most recent major releases, so the baseline is the older
of the two, and it moves when a new major release retires it. A module on an earlier
version moves up; none requires a newer release.

- **`.0`, unless a dependency needs a later patch.** The directive is the minimum a builder
  and every consumer must run: a module saying `go 1.N.8` refuses to build with `1.N.4`
  under `GOTOOLCHAIN=local`, and forces every importer up with it. Patch fixes reach a build
  through the toolchain the repository pins (the aqua `golang/go` pin, kept at the latest
  release), not through the directive. A module cannot sit below its dependencies, though:
  with a dependency on `go 1.N.8`, a module on `go 1.N.0` refuses to build, and
  `go mod tidy` raises it to `1.N.8`. So the directive is the higher of `1.N.0` and the
  highest any dependency requires; `go mod edit -go=1.N.0` followed by `go mod tidy` lands
  on it. `go mod init` writes whatever patch level the toolchain that ran it had, which is
  how repositories drift across a line; a new module's directive is settled the same way.
- **Not the bare `go 1.N`.** Go orders `1.N` before `1.N`'s release candidates, so it
  admits a toolchain that is not a release; `1.N.0` is the release.
- **A dependency that needs a newer release waits.** Taking a dependency on `go 1.(N+1)`
  would raise the module past the baseline: the module stays on the dependency's last
  release that supports the baseline until the baseline moves.
- **The tools modules are the exception.** `tools/go.mod` and `tools/<name>/go.mod` are
  built only by the repository's pinned toolchain and never imported, and `go mod tidy`
  raises their directive to whatever the tools themselves require. Their directive is left
  to that.

## Go — no replace, ever

A `replace` directive is never committed: not in `go.mod`, not in a nested module, not
through a relative path to a sibling checkout, and no configuration that permits one is
committed either (`gomoddirectives`' `replace-local` or `replace-allow-list`, in a
`.lint-go.yaml` or anywhere else). There is no case where it is right.

A local replace has one legitimate use: working on two modules at once, on one machine,
while a change travels from one to the other. It lives in the working tree for as long as
that takes, and is gone before anything is committed. What is committed builds from
published versions only, so that the build a reviewer, CI, or a consumer runs is the one
the author ran — a replace makes the module build from whatever happens to sit at a path,
which nobody else has, and which the checksum database never saw.

When the change a module needs is not tagged yet, require the commit that carries it: a
pseudo-version (`go get <module>@<commit>`) is published like a tag — the module proxy
serves it, the checksum database records it. The commit must already be on the owner's
default branch: one from an unmerged branch can be rewritten or vanish, and the pin then
names code that never landed. When a tag is wanted instead, ask the owning session for a
release. A tool module nested in a repository requires its parent's published version like
any other consumer, or becomes part of the parent module.

The baseline refuses `replace` by default (gomoddirectives), and `exclude` with it, being a
replace by other means.

## Go — paths are validated where they enter

A path the user hands the program — a flag, an argument, a volume source, a cache or log
location — is made absolute and validated at the input boundary, in the command layer
that parses it, and nowhere deeper: `filepath.Abs`, then primordium's
`pathcheck.Validate`; `pathcheck.ValidateComponent` for a bare name that becomes a
directory (an instance id); `pathcheck.ValidateSocket` on the final absolute string, at
the one place that assembles a socket path, because the `sun_path` limit (104 bytes on
macOS, 108 on linux) binds the whole path and only that place has it. The check runs once,
where the whole input exists, and the rest of the program takes a validated absolute path
as a fact. A command-line flag shared by several commands carries its check once, as a
`Validate` hook on a type the commands embed by name — an anonymous embedding promotes
the method and runs it twice.

Two scopes stay out, on purpose. A path that is only read is not checked: the rules are
the filesystem's for what can be *created*, and a failed open says so itself. A path that
belongs to another system — a guest's working directory, a container's mount destination,
anything the host never resolves — is never checked with the host's rules: macOS limits
applied to a linux guest path refuse valid input and catch nothing the guest would refuse.

The trap the rule prevents is validation that wanders into the library, where host and
guest paths meet in one function and a test of an unrelated feature ends up asserting it.
ossein's first version did exactly that — the guest `Cwd` checked with macOS rules, the
checks asserted from the cache tests, the Windows legs red in `internal/cli` — and was
rebuilt as three checks in `cmd/ossein` plus the socket check where the path is joined,
each with its own test ([farcloser/ossein#103](https://github.com/farcloser/ossein/pull/103)).

## Go — what golangci-lint cannot see

golangci-lint's `govet` runs the same analyzers as `go vet`, with one structural gap: its
package loader never hands an analyzer the package's *non-Go* files. Two analyzers work on
exactly those — `asmdecl`, which checks an assembly function's frame and argument offsets
against the Go declaration it implements, and `buildtag`, which checks `//go:build`
constraints in non-Go files — so through golangci-lint they report nothing, whatever the
configuration says. A hash library's generated amd64 assembly failed `go vet` on every
amd64 build while `just do lint go` stayed green on the amd64 CI legs; nothing in the
pipeline could observe it, because `go test` runs a fixed vet subset that excludes both.

`just do lint go vet` closes the gap by running `go vet -asmdecl -buildtag` — those two
analyzers and **no others** — over every supported GOOS/GOARCH pair. The scoping is the
point: `go vet` honours neither the rendered lint configuration nor `//nolint`, so any analyzer that
overlaps with golangci-lint's `govet` would re-report a finding the project deliberately
silenced and force the exception to be written twice, in two syntaxes. Restricted to the
analyzers golangci-lint physically cannot run, the step can never contradict a project's
lint configuration, and a finding from it is silenced the same way as any other: by
fixing the assembly or the constraint, or by regenerating it. The platform loop matters as
much as the analyzer: assembly files are selected by their `_amd64.s` suffix, so on an
arm64 host the file is not in the package at all; every Go analysis step iterates the full
GOOS/GOARCH matrix for that reason (see [recipes](./recipes.md)).

## Go — the tests are one contract, checked within a bound

A package's tests state its contract once, as a comment at the top of
`contract_test.go`, from the sources that define it — the specification, the man page,
the platform's own documentation, the package's own docs — each rule cited or quoted.
The files beside it hold the code to that comment and to nothing else:

- `bounded_test.go` walks every input in a bound drawn from the contract's own edges:
  for each class the contract names, its boundary, one step past it, and a middle; for
  a stateful package, every sequence of calls up to a small length, held to a model after
  each. The bound is what makes the check exhaustive; the contract is what keeps it small.
- `fuzz_test.go` runs the same checks past the bound, seeded from it.
- a hand-written test remains only for a case no bounded walk reaches — a device path, a
  path past `MAX_PATH` — and its comment says which.
- a drop-in for a standard-library package (`xos` for `os`) is checked beside it, call
  for call, on the same filesystem state: what the caller saw and what was left on disk
  must match, on every platform.

An older suite goes only under a mutation score: single-point mutants of the package (a
flipped comparison, a dropped `!`, an off-by-one, a deleted statement, an `error` made
`nil`), each run against both suites, and the old suite is removed when the new one
catches every mutant the old one did. The score is the evidence; the line count is not.

Writing the contract down is where the bugs are found: stating a rule from its source
and holding the code to it is what turned up seven in primordium in a week, six in its
filesystem packages and one in a store, each a rule the code did not meet once written
(https://github.com/mycophonic/primordium/pull/138,
https://github.com/mycophonic/primordium/pull/139,
https://github.com/mycophonic/primordium/pull/140,
https://github.com/mycophonic/primordium/pull/143,
https://github.com/mycophonic/primordium/pull/146,
https://github.com/mycophonic/primordium/pull/147,
https://github.com/mycophonic/primordium/pull/153). The shape, package by package:
https://github.com/mycophonic/primordium/pull/141 (a path validator),
https://github.com/mycophonic/primordium/pull/144 (an `io.ReadSeeker` wrapper),
https://github.com/mycophonic/primordium/pull/145 (advisory locks, with blocking),
https://github.com/mycophonic/primordium/pull/151 (the `os` drop-in, differential),
https://github.com/mycophonic/primordium/pull/157 (XDG and platform directories), and
https://github.com/mycophonic/primordium/pull/165, where a 1,789-line port of Go's own
`os` tests went under the score.

## Go — errors: a sentinel per fault class, wrapped at every site

The baseline enforces the floor (err113, errorlint, wrapcheck): an error value is a
package-level sentinel or wraps one with `%w`, and a caller compares with `errors.Is` or
`errors.As`, never by text. The doctrine is what the sentinels *mean*.

**One sentinel per fault class, not per message.** A caller acts on the class, and the
classes a module has are few: the input is not valid (reject it), the caller's argument is
wrong (fix the call), the transport underneath failed (retry, or report it as the
transport's own), the format is valid but the module does not implement it. Three to five
exported `Err*` per module, each documented by what a caller should do on it; the message
carries the specifics (which field, which offset), the sentinel the class. A module whose
every fault is one sentinel has told its caller nothing: erofs reported every malformed
image and every bad argument as `fs.ErrInvalid`, so a caller could not tell a corrupt image
from a misspelled path (https://github.com/forkcloser/erofs/pull/108 added `ErrCorrupt`).
A module whose faults are bare messages has told its caller nothing either: go-graphviz's
generated binding returned `errors.New("… is not loaded")` and `fmt.Errorf("cannot find
lookup function …")`, matchable only by text
(https://github.com/forkcloser/go-graphviz/pull/89 made them `ErrNotLoaded`,
`ErrMemoryAccess`, `ErrNotRegistered`). The model is xz: `ErrCorrupt`, `ErrUnsupported`,
`ErrClosed` in `errors.go`, the same two in `lzma/errors.go`, every diagnostic of the
decoder matching one of them.

**A transport error is passed through, never reclassified.** An error from the
`io.Reader` or `io.ReaderAt` underneath the module is returned wrapped or as is, and
matches none of the module's sentinels; a short read at a place the format says has bytes
is `io.ErrUnexpectedEOF`, not "corrupt". The caller that owns the transport is the one
that can act on it. The test that pins this is cheap and belongs in every module with a
reader: a `ReaderAt` that fails with its own error, `Open` returning that error and nothing
of the module's (erofs `errcorrupt_test.go`).

**The chain carries the class without carrying its text.** The plain form is
`fmt.Errorf("nid %d is out of range: %w", nid, ErrCorrupt)`, and the sentinel's text is
then a noun phrase that reads at the end of a message ("corrupt image", not "an error
occurred"). Where that text would be noise on every line, a kind type: `Error()` returns
the message, `Unwrap()` the sentinel, built by a one-line constructor (`corruptf` in xz
`errors.go`). Both match with `errors.Is`; neither needs the caller to parse.

**A layer maps the layer below into its own vocabulary at the boundary.** A caller of xz
sees `xz.ErrCorrupt` whether the fault was in the container or in the LZMA2 payload:
`classify` wraps `lzma.ErrCorrupt` in a value whose `Unwrap() []error` returns both the
new sentinel and the original chain, so the lower sentinel stays reachable and the
message stays the decoder's. The alternative, re-exporting the lower module's sentinels,
leaks the layering into the API.

**Widening is additive.** A new sentinel that names a subset of what an old one matched
unwraps to the old one (`ErrCorrupt` in erofs matches `ErrInvalid` too), so every
`errors.Is` a consumer wrote keeps its answer and the change is not `breaking`. The
reverse, moving faults out from under a sentinel consumers test, is.

**Where the lint does not reach, the rule still does.** golangci-lint skips generated
files and the lint lane does not enter a nested module, so a bare `fmt.Errorf` there is
never reported: the binding's errors above were 59 such sites. The fix goes in the
template or the generator, then `just bindings`, and the nested tool gets the same
sentinels by hand. A dynamic error is right in one place only: a test fixture, where the
error is the fake's own and compared by identity; err113 stays on in tests in the
baseline, and a project that wants that carve-out states it in its own `.lint-go.yaml`,
as xz does.

**Not a fault class: a limit.** A refusal that protects the caller from itself, a path
over 4096 bytes, `ReadFile` on a file over the cap, is the caller's argument, not the
input's fault: it stays on the bad-argument sentinel even when the value came off disk.
The package doc lists the limits and the sentinel each reports.

## Go — a chaining API carries its first error

A setter that returns its receiver so that calls chain, `g.SetLabel(…).SetShape(…)`, has
nowhere to return an error, and the three ways out are not equal. Panicking turns a failed
allocation deep in a library into a crash the caller never asked for. Dropping the error,
the usual choice and what go-graphviz did for 210 setters, means the graph silently does
not hold what the program set, and every one of those drops is an errcheck finding carved
out of the lint baseline. The rule is the third way: the receiver records the first error
it meets, `Err()` returns it, and the terminal operation, the one that consumes what the
setters built (`Layout`, and so every render), returns it wrapped before doing anything,
so a dropped error cannot pass silently and a caller that never reads `Err()` still cannot
proceed on a half-built value. Closing the root forgets it. The chaining signature stays;
the carve-out goes. The error is keyed by the root object, so a node's or an edge's failure
is the graph's, which is what the terminal operation sees.

## Go — logging: a library writes nothing; a command speaks once, in main

**A library package imports no logger and writes nothing to standard error.** Not behind
a verbosity flag, not through a package-global logger a command configures: what a
library has to say is a returned error or a returned value, and a consumer that wants a
trace adds it at the call site, where it knows the context. A debug dump in a decoder is
the upstream smell to remove on fork: xz's `reader.go` and `lzma/reader2.go` printed
headers and chunk headers through upstream's own `internal/xlog` under `-vv`, with the
format's `String` methods existing only to feed it
(https://github.com/forkcloser/xz/pull/96 deleted the package, the dumps and the
methods). The survey that preceded it found no other library package of the forks
importing any logger; that is the state to keep.

**A command either logs or prints, and knows which.** A tool with diagnostics at levels
(a linter, a server, anything a `--log-level` flag makes sense for) uses `log/slog`, and
nothing else: the handler and the level are set once in `main`, from the flags, the way
godolint's `cmd/godolint` does it; no third-party logger, no logger set up by a library
on import. A tool whose output to the user is messages (a compressor, a renderer: "file
exists", "format not recognized") has nothing to log. It prints each message to standard
error as `<cmd>: <message>` and sets the exit status, with the quiet levels the tool it
mirrors defines (xz: `-q` hides warnings, `-qq` errors too). No timestamps, no levels, no
logging package for three `Fprintf` calls.

**No logger ends the process.** `Fatal` and `Panic` entry points are what a home-grown
logger grows, and they turn every helper that reports into one that exits. A function
returns its error; `main` prints it and exits. A real programmer error, an invariant the
code itself violated, is a `panic` with its message, not a log line that happens to exit.

## Go — a test that only the standard library can fail is not a test

A test earns its place by a change to the package that would fail it. One that checks
what the standard library guarantees — that `errors.New` returns an error carrying its
message, that `errors.Is` finds a sentinel through `fmt.Errorf`'s `%w` once and twice,
that two sentinels are distinct — cannot fail short of a change to Go, and goes. A
package that is a set of sentinel errors and no logic needs no test file at all; the
coverage gate is on the module, not the package. The trap: such a file reads as coverage
of error handling and covers none of it, and it rots unnoticed — primordium's `fault`
carried 215 lines of it, with a sentinel missing from two of its own lists
(https://github.com/mycophonic/primordium/pull/160).

## Go — a precondition the program must meet at startup panics when missed

A package whose every call needs something set first — an application name before any
directory, a configuration before any client — panics at the first call made without
it, naming what is missing. It does not degrade: no empty-string default, no zero value
standing for "unset" that the code then builds a path or a connection on. This is
[a type admits only its valid values](./index.md#generic-principles) at the one place
a type cannot reach, the order of calls at startup; the check is a run of the test
binary in a process of its own, where that call comes first, and the same shape checks
a value the setter refuses. The trap: the degraded path looks like it works, and nobody
reads it until it has done damage. primordium's `dirs` with no application name handed
out the user's whole base directory — `~/.cache`, `~/Library/Application Support` — as
the application's own, created it private, and returned it for every caller to fill
and clean (https://github.com/mycophonic/primordium/pull/159).

## Go — a library zeroes the process umask once, by name, and says what the children inherit

The umask is process-wide state: the kernel strips its bits from the mode of every file
and directory the process creates, so code asking for `0o644` gets `0o600` under an
operator's `0o077` and never learns. A library that wants the mode asked for to be the
mode the file gets zeroes the umask once, at startup, through a function named for the
write (`Disable`), never as the side effect of a read: a `Get` that zeroes on its first
call is the footgun the package exists to remove, and a `Set` has no place beside a
`Disable`. `Get` returns the mask found, the operator's, and panics before `Disable`,
since a umask cannot be read without being set. The package documents that every child
process inherits the zeroed umask, so a tool following the `0o666`-less-umask convention
then creates world-writable files; giving the mask back is the child's
(`sh -c 'umask 077; exec "$@"'`), never a toggle around the spawn, which would strip what
every other goroutine creates meanwhile. A drop-in for `os.WriteFile` honours the umask
as `os.WriteFile` does, by creating with `perm`, never by re-applying a captured mask
after the fact. The trap, from https://github.com/mycophonic/primordium/pull/158:
`WriteFile`'s first call zeroed the umask as a side effect, and every program calling it
without the library's initializer relied on that without knowing; the fix broke eight
callers across four repositories, each of which now says what it wants.

## Go — a port from the standard library is read beside the toolchain's source when revisited

A function ported from the standard library — `os.CreateTemp` under another share mode,
`syscall.Open` with `FILE_SHARE_DELETE` — drifts as upstream moves. When it is touched,
it is read beside the current toolchain's source, the one aqua pins under
`pkgs/http/golang.org/dl/`, function by function, and each difference is adopted or
named: a divergence kept goes in the package doc with the platforms or versions it
concerns, so the next reader comparing the two does not find it again, and one adopted
gets the differential check beside the original. The source is the reference, never
memory: a review round's "upstream returns EINVAL there" read right and was wrong
against the file. The trap: a difference that looks like a gap may be upstream's own
doing elsewhere. Go's runtime sets the process's long-path bit on Windows from
10.0.15063, so the `\\?\` prefix `os` adds to a long path is dead on every platform
this project runs, and porting it would have been sixty lines nobody could exercise
(https://github.com/mycophonic/primordium/pull/166, which adopted the one real
difference, the FILE_FLAG bits of an open flag, and named the other).

## Enforcement

`limen check [path]` evaluates the applicable per-language rules alongside the mandatory
ones. A rule whose trigger is absent produces no finding; a rule whose trigger is present
must pass like any other. See [`../cmd/limen/`](../cmd/limen).

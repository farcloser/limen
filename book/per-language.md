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
the canonical. (This is the same exact-match rule as the `.editorconfig`, the `Justfile`, and
the `.limen/just/*.just` modules — only `aqua.yaml` uses the subset, "contains the baseline"
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

In practice the trigger always fires: every compliant repository carries `aqua.yaml`
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
  'user/name'` in the root `Justfile`) and the audit recipe registers the working tree
  under that name for the duration of the run — a symlink, so the audit judges the
  working tree, not a stale clone. Audit flags that only make sense on CI (`--online`
  does network calls) go through `LINT_HOMEBREW_AUDIT_FLAGS`.

The proof beyond linting — `brew install --build-from-source` plus `brew test` — mutates
the machine's live brew and therefore belongs to disposable CI runners, not to a shared
recipe; a tap wires that in its own workflow.

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
  `aqua.yaml`, not the baseline: no other repository pays for it.
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
go-licenses lived in an exported variable in the root `Justfile`, a hidden override.

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

`just do lint go` enforces this after golangci-lint runs: nolintlint polices the *shape*
of a directive, not which linter it names, so the recipe greps the tracked Go files for
every banned form and fails with the fix spelled out.

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

## Enforcement

`limen check [path]` evaluates the applicable per-language rules alongside the mandatory
ones. A rule whose trigger is absent produces no finding; a rule whose trigger is present
must pass like any other. See [`../cmd/limen/`](../cmd/limen).

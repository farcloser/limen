# Linting — Go

The Go form of [linting](./linting.md): golangci-lint on a rendered baseline, NilAway,
go-licenses and `go vet`'s two blind spots, each a lane of `just do lint go`.

## One lint baseline, per-project carve-outs: `.lint-go.yaml`

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
  | `licenses.ignore` | appends the modules go-licenses skips (typically the false positives of google/go-licenses#186); the repository's own module is skipped by the lane itself, since its license is the `license` rule's business and go-licenses has no class for the Closed-source notice |
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

## Dead formatter exclusions

One warning is not a verdict: golangci-lint's "Skipped 0 issues by rules" for a
*formatter* exclusion (gci, gofumpt, golines, goimports on a generated file) says only
that the lint pass found nothing there, since formatters report through the separate
format pass. The exclusion is live when removing it makes `just do lint go` rewrite the
file; a generated file in a nested module, which the lint pass never enters, shows the
same warning for the same reason. Test by removal before calling one dead (seen on
go-graphviz's `bind.go` and `nori.pb.go`).

A dead silence is reported by nolintlint for a linter that runs, and by `just do lint go`
for a disabled linter, a disabled revive rule, and a `#nosec` in a test file, which gosec
does not judge.

## Silencing a finding

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

## Fixing a shadowed error

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

## What golangci-lint cannot see

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

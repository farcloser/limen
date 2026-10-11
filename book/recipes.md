# The shared recipes

Every repository carries the same task-runner baseline: a root `.justfile` that mounts the
shared `.limen/just/` modules, which `limen` content-pins (the root `.justfile` is the
project's own; only its import line is required — [mandatory files](./mandatory-files.md#justfile)).
This chapter is what that baseline *is*: the environment it builds and the conventions every
shared recipe obeys, so what `just` does is explainable, not folklore.

**The live inventory is not in this book.** `just --list` enumerates the recipes; each
recipe's comment in its module file is its reference. The book holds the invariants. When
this chapter and a recipe disagree, the recipe is right and this chapter has a bug.

## The execution environment is hermetic

Recipes do not run in your shell's environment; they run in one the `.justfile` constructs:

- **Hermetic `PATH`**: the aqua tool directory plus the base system paths, nothing else (no
  Homebrew). Every tool a recipe invokes resolves to the aqua-pinned version or fails loudly;
  a machine-installed copy can never be silently substituted. Windows/git-bash is the one
  sanctioned exception: with no knowable base-system directory list, the pins are
  *prepended* to the ambient `PATH` — they still shadow everything, and the POSIX legs keep
  full hermeticity enforced. Two machine tools are captured from the ambient `PATH` before it
  is replaced, by absolute path into a variable the recipes name, never onto the `PATH`:
  `BREW_BIN`, for the homebrew recipes (brew has no pin and lives at a machine-chosen
  prefix), and `GPG_BIN`, for git to read the PGP signatures GitHub puts on its merge and
  web-UI commits (the registry has no gnupg, macOS's base system none). Neither decides what
  a pin should: brew is what the homebrew lanes exercise, and no lane's verdict rests on a
  PGP signature.
- **Hermetic Go environment**: Go reads a dozen `GO*` variables that tunnel through the
  pinned `PATH`, so each is emptied or pinned. `GOROOT` (IDEs inject one), so the pinned
  toolchain finds its own stdlib; `GOTOOLCHAIN=local`, so a `go.mod` ahead of the pin fails
  asking for a pin bump instead of fetching an unpinned compiler; `GOFLAGS`, `GOPRIVATE`,
  `GOOS`/`GOARCH` neutralized, so ambient flags or a cross-compile target cannot rewrite a
  build; `GOSUMDB`/`GOPROXY` pinned to their real defaults, so an ambient `GOSUMDB=off`
  cannot defeat the checksum verification the `tools/go.mod` pins rely on. Each stays
  overridable by a project's own `.justfile`: explicit and tracked, never ambient. The one
  exception is `GOWORK`: a `go.work` in the tree (or a parent) deliberately puts recipes into
  workspace mode, at the eyes-open cost that a build under an active workspace can differ
  from CI.
- **The host's C compiler, for diagnostics and tests only.** cgo calls the C compiler it
  finds: Xcode's clang on macOS, `/usr/bin/gcc` on Linux, whatever the ambient `PATH` carries
  on Windows. No C toolchain is pinned, so two kinds of build run on the host's, as a named
  exception: the race lanes (`test go race` and `build go race` force cgo; the detector needs
  it) and the tests of a project that sets `GO_CGO`. Both are diagnostics, never shipped: a
  compiler difference there can change what a test finds, never what a consumer runs. A
  release artifact is not covered: one for macOS that links Apple's SDK
  (Virtualization.framework, an `xcodebuild` bundle) builds with Xcode, which belongs to the
  machine like the OS; on Linux and Windows a shipped cgo build needs a pinned C toolchain
  (an ossein container).

The coreutils are pinned too: `sort`, `date`, `mktemp`, `sha256sum`, `base64` and the rest
on the hermetic PATH are aqua's, never the runner's. On macOS and Linux that is one Rust
multicall binary linked under every name; on Windows it is Git for Windows' MSYS2 coreutils
under the same names (the local registry entries say how), on purpose: recipes run in
git-bash there, and a native coreutils inside it prints Windows paths (`C:\…`) its MSYS2
neighbours misread, so the pin is the kind of program the shell is. The shell itself is not
pinned there: an aqua link named `bash` becomes every `shell: bash` step's shell, which aqua
cannot serve. Write the bare GNU name and let the PATH answer: a `command -v` fallback
between BSD `stat` and GNU `stat` institutionalizes the ambient tool. A third-party build
script that branches on `uname` and expects BSD flags on macOS meets GNU there; patch it
after fetching, never unpin the PATH for it. Call pinned tools from the project tree: an
aqua shim finds its pin by walking up from the working directory, so a recipe that `cd`s
into a temp dir outside the repository has no pinned tool there — hand it the path instead.

The consequence, and the point: a recipe behaves identically on every machine that ran
[machine setup](./tooling.md#machine-setup-limen-install-one-time-per-machine), and
anything *not* pinned is unusable from a recipe by construction. A toolchain that lives
outside aqua — a rustup-managed cargo — needs an explicit, documented decision before its
recipes can work; the Rust one is in [rust](./rust.md).

## Conventions every shared recipe follows

- **Defaults pass vacuously.** Invoking a module bare (`just do lint`, `just do fix`) runs
  its curated `default`. A recipe belongs in a default only when it no-ops harmlessly where
  it does not apply ([mandatory files](./mandatory-files.md#justfile)); anything needing a
  language toolchain is named explicitly (`just do lint go`). `test` and `perf` are the honest limit of that rule: *every* test and
  every report is language-bound, so bare `just do test` and `just do perf` refuse with
  guidance. A project declares its suites as a `test` aggregate in the root `.justfile`
  (mirroring `lint`), and that pair is what CI runs.
- **Project knobs are exported variables, named after the task path.** `TEST_GO_TIMEOUT`
  and `TEST_GO_COVER_MIN` for the test module, `BUILD_GO_FLAGS` and `BUILD_GO_LDFLAGS` for
  the build module. A project sets them once in the root `.justfile` (`export NAME :=
  'value'`; exports propagate into every module recipe), or on the invocation for a one-off.
  Lint *policy* is not a knob: what `just do lint go` accepts is declared in the project's
  `.lint-go.yaml`, a committed file with a defined vocabulary
  ([linting Go](./linting-go.md#one-lint-baseline-per-project-carve-outs-lint-goyaml)),
  never an exported variable. The exceptions carry the name of what they configure rather
  than one task's: `LIMEN_BIN` serves both `lint limen` and `fix limen`; `GO_CGO` declares
  that a project's product cannot link without cgo, which every Go task has to know.
- **A knob is read in the recipe, not resolved into a module variable.** A child module
  cannot see its parent's variables, and `env()` reads `just`'s *process* environment, which
  never holds one — so `export X := env('X', default)` in a shared module silently discards
  what the project set. `${X:-default}` in the recipe body works, because a parent's exports
  *are* in the recipe's environment. `GO_CGO` is the shape: the recipes resolve
  `${CGO_ENABLED:-${GO_CGO:-0}}`, so the project declares its nature once, a single
  invocation can override either way, and unset still means pure Go.
- **A knob composes with the recipe; it does not replace it.** Linker flags cannot ride in
  on `BUILD_GO_FLAGS`: go honours the *last* occurrence of a repeated flag, and the project's
  flags come after the `-ldflags` the recipe computed, so a project passing its own would
  silently drop the version stamp and the CGO linkmode. `BUILD_GO_LDFLAGS` is spliced inside
  the recipe's own `-ldflags` string, last, where it adds to the baseline and still wins an
  explicit conflict. Prefer this shape whenever a knob would otherwise let a project
  overwrite something the baseline is responsible for.
- **Go analysis runs once per supported platform.** The build graph is selected by GOOS
  *and* GOARCH — a file built only on linux is invisible to a darwin run, an `_amd64.go` to
  an arm64 one — so the Go linters, vet, vulnerability scan and license check iterate over
  every supported pair with CGO disabled (the `_per-platform` helper). A project that needs
  cgo exports `CGO_ENABLED=1` and gets a single native run, with the reduced coverage
  announced.
- **Names are spelled out.** `--dry-run`, never `-n`; `just do release`, never `just rel`;
  `limen github`, never `limen gh`. Everything here is written for the reader who does not
  know yet. Established tool names are not respelled: `gh` is `gh`.
- **Recipes announce themselves and stay quiet otherwise.** Modules set `set quiet`; each
  recipe's first act is a `▶ module: name` banner (the shared `_banner`). Output beyond that
  belongs to the tool.
- **Recipe bodies are themselves linted.** Multi-line recipes run under `#!/usr/bin/env
  bash` with `set -euo pipefail`, written to bash 3.2 (macOS's system bash). `just do lint
  shell` extracts every shebang recipe body from the parsed justfile tree and shellchecks it.
- **Discovery respects git; judgment respects the working tree.** Recipes that scan for
  files (`lint just`, `lint shell`, `lint dockerfile`, the homebrew modules) enumerate via
  `git ls-files` — tracked and new-untracked, honoring `.gitignore` — so generated and
  vendored trees never surprise a run. That is git's whole role in a recipe, plus the
  questions that are about history (`lint commits`). What is *judged* is the working tree:
  enumerated paths are existence-checked before reaching a tool (a tracked file deleted but
  not yet staged is a skip), and `lint aqua` compares the checksums file against a
  regeneration from the working tree's manifest, never against HEAD or the index. Commit
  timing never changes a lint verdict.

## The modules, briefly

What each shared module is *for*; mechanics live in the module files:

- **`build`**: the project's `cmd/` binaries into `build/`: an optimized, reproducible
  `release` shape plus `debug`, `race` and (Linux) `static`, version-stamped from
  `git describe`. The release shape is the **twin** of the goreleaser configuration where a
  project ships one: the flag sets are kept aligned, and a change to either lands in both
  (each file's comment names the other). Pure Go by default; CGO is an explicit opt-in that
  adds the hardening flags.
- **`tools`**: the aqua manifest operations (`add`, `set`, `update`, `remove`) that keep
  `.aqua/aqua.yaml` and `.aqua/aqua-checksums.json` changing together
  ([tooling](./tooling.md#day-to-day-changes--the-just-do-tools-recipes)).
- **`lint`**: read-only verifiers. In the default: `limen` (this repository against the
  rules — first, since every other linter trusts the canonical files it verifies), `just`,
  `aqua`, `yaml`, `shell`, `dockerfile`, `commits` (DCO and commit hygiene over a range).
  Named: the `go` submodule (code, vet, mod, licenses, nilaway, the informational deadcode
  report), `rust`, `homebrew` (formula style and audit through brew's own tooling,
  [homebrew](./homebrew.md)) and `github` (the live settings audit, `limen github check`,
  needing network and an authed `gh`, [github](./github.md)). A linter's verdict is a
  function of the tree: the same tree gets the same answer tomorrow. What is not — a scan
  against a database that moves on its own — is not a linter, and lives in `security`.
- **`security`**: scans whose verdict changes with a database, not with the tree, so a green
  turns red with nothing pushed. In the default: `links` (every link in the repository, the
  web's verdict) and the `go` submodule's `vuln` (the module's reachable dependency graph
  against the Go vulnerability database, once per supported platform). Named: `binaries`
  (every Go binary aqua pins, scanned by the symbols it links: a pinned tool carries its
  upstream's standard library, and a fix there reaches us only when that upstream rebuilds;
  named until the pinned tools are built here with the pinned toolchain, since its first run
  found most of them carrying some).
  Its CI lane is its own, `security.yaml`, apart from `ci.yaml`, so a new advisory reddens
  one check that says what it is, answered by a bump, never a code change
  ([github](./github.md#mainline-doctrine-pull-requests-always)).
- **`test`**: the suites, per language. `just do test go`: `unit`; `race`, which asks the
  toolchain whether the host has a race detector and, where it does not (windows/arm64), says
  so and passes rather than fail every push on a Go limitation; `fuzz`, a short smoke of
  every `Fuzz*` target, run by the canonical CI's own `fuzz` job on one leg, not from the
  `test` aggregate; `cover`, with an optional minimum gate. No default: `just test` is the
  project's aggregate.
- **`perf`**: the reports that judge nothing, per language. `just do perf go`: `bce` and
  `escape` by default (every bounds check the compiler could not eliminate, every value it
  moved to the heap, every function it refused to inline; compile-only and quick), and,
  named, `bench` with allocation stats and `profile` with pprof's top entries and rendered
  call graphs, slow and writing under `build/`. None fails and none asserts, which is why
  they are neither `lint` nor `test`, and nothing in CI runs them. No default.
- **`fix`**: the mutating counterparts, separate from `lint`. In the default: `limen`
  (rewrite drifted canonical files), `just`, `yaml`, `aqua` (regenerate
  `.aqua/aqua-checksums.json`). Named: the `go`, `rust` and `homebrew` submodules, and
  `github` (plan shown, applied on consent). Nothing mutates under a lint name, and `fix`
  applies only what cannot change behaviour: for Go, the formatters plus the linter fixes
  that touch layout alone (blank lines, a comment's period, struct-tag alignment, `//nolint`
  directives). Every other linter fix is a pattern rewrite with no promise to preserve
  meaning, and several have broken code (a loop shortened by one iteration, a path losing
  its separator); those findings stay findings, and a person fixes them.

Two roles close the enforcement loop:

- **`just do lint limen` / `just do fix limen`** run the pinned `limen` against the
  repository: the rules in this book, enforced from inside the baseline. The `LIMEN_BIN` knob
  has one consumer, the limen repository, which points it at `go run ./cmd/limen` so its
  working tree is judged by its own enforcer rather than the always-older released pin.
- **`just do release`** is shared; artifacts are opt-in. The recipe, its two lanes and the
  rules a release follows are [releasing](./releasing.md).

## Extending the baseline

A new shared recipe goes into a module in limen's `.limen/just/` (a new concern gets a new
module plus its `mod` line in `do.just`); the content-pin carries it to every repository on
the next `limen fix`. A recipe only one project needs goes in that project's root
`.justfile`: the `do` namespace keeps the shared names off the top level, so a project is
free to define its own `lint`/`test`/`build` there. A project recipe that shares a name
replaces, never extends: the baseline sets `allow-duplicate-recipes`, and the last definition
is the whole recipe. A lane is added by appending it to the one `lint:` line; a second
`lint:` line meant to add one silently dropped every baseline lane, and CI ran the one lane
and reported green. `just --show lint` prints what will run. Global changes are proposed
against limen, never edited locally: the shared files are locked by the content-pin, and
drift is overwritten.

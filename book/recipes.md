# The shared recipes

Every repository carries the same task-runner baseline: a root `.justfile` that mounts the
shared `.limen/just/` modules, which `limen` content-pins (the root `.justfile` itself is the
project's own — only its import line is required; the pinning rule and file layout are in
[mandatory files](./mandatory-files.md#justfile)). This chapter documents what that baseline
*is* — the architecture and the conventions every shared recipe obeys — so the behavior you
see from `just` is explainable, not folklore.

**The live inventory is not in this book.** `just --list` enumerates the recipes; each
recipe's comment in its module file is its reference documentation. The book documents the
invariants — the things that stay true as recipes come and go. When this chapter and a
recipe disagree, the recipe is right and this chapter has a bug.

## The execution environment is hermetic

Recipes do not run in your shell's environment; they run in one the `.justfile` constructs:

- **Hermetic `PATH`** — the aqua tool directory plus base system paths, nothing else (no
  Homebrew). Every tool a recipe invokes resolves to the aqua-pinned version or fails
  loudly; a machine-installed copy can never be silently substituted. (Windows/git-bash is
  the one sanctioned exception: with no knowable base-system directory list, the pinned
  tools are *prepended* to the ambient `PATH` — pins still shadow everything, and the POSIX
  legs of the CI matrix keep full hermeticity enforced.) Two machine tools are captured
  from the ambient `PATH` before it is replaced, by absolute path into a variable the
  recipes name, never onto the `PATH`: `BREW_BIN`, for the homebrew recipes (brew has no
  pin and lives at a machine-chosen prefix), and `GPG_BIN`, for git to read the PGP
  signatures GitHub puts on its merge and web-UI commits (the aqua registry has no gnupg,
  and macOS's base system has none). Neither decides what a pin should: brew is what the
  homebrew lanes exercise, and no lane's verdict rests on a PGP signature.
- **Hermetic Go environment** — Go reads its behavior from a dozen `GO*` variables that
  tunnel through the pinned `PATH`, so each is emptied or pinned: `GOROOT` (IDEs inject
  one) so the pinned toolchain finds its own stdlib; `GOTOOLCHAIN=local` to forbid Go's
  silent toolchain downloads (when `go.mod` outpaces the pin, recipes fail asking for a
  pin bump instead of fetching an unpinned compiler); `GOFLAGS`, `GOPRIVATE`, `GOOS`/`GOARCH`
  neutralized so ambient flags or a cross-compile target can't rewrite a build; and
  `GOSUMDB`/`GOPROXY` pinned to their real defaults so an ambient `GOSUMDB=off` cannot
  defeat the checksum verification the `tools/go.mod` pins rely on. Each stays overridable
  by a project's own `.justfile` — explicit and tracked, never ambient. **The one
  exception is `GOWORK`:** a `go.work` in the tree (or a parent) deliberately puts recipes
  into workspace mode — Go workspaces are a supported way to work here, at the eyes-open
  cost that a build under an active workspace can differ from CI.
- **The host's C compiler, for diagnostics and tests only.** cgo calls the C compiler and
  linker it finds: Xcode's clang on macOS, the base system's `gcc` from `/usr/bin` on Linux,
  whatever the ambient `PATH` carries on Windows. No C toolchain is pinned, so two kinds of
  build run on the host's, as a named exception: the race lanes (`test go race` and `build
  go race` force cgo, the detector needs it) and the tests of a project that sets
  `GO_CGO`. Both are diagnostics, never shipped: a compiler difference there can change
  what a test finds, never what a consumer runs. A release artifact is not covered by this
  exception. One for macOS that links Apple's SDK (Virtualization.framework, an
  `xcodebuild` bundle) builds with Xcode, which belongs to the machine like the OS itself.
  On Linux and Windows, a shipped cgo build needs a pinned C toolchain (an ossein container).

The coreutils are pinned too — `sort`, `date`, `mktemp`, `sha256sum`, `base64` and the rest
on the hermetic PATH are aqua's, never the runner's. On macOS and Linux that is one Rust
multicall binary linked under every name it provides; on Windows it is Git for Windows'
MSYS2 coreutils under the same names (the local registry entries say how). Windows is
different on purpose: recipes run in git-bash there, and a native Windows coreutils inside it
prints Windows paths (`C:\…`) its MSYS2 neighbours misread, so the pin is the kind of program
the shell itself is. The shell is not pinned there: an aqua link named `bash` becomes every
`shell: bash` step's shell, which aqua cannot serve. The ambient tools differ by platform (BSD `stat` and perl's `shasum`
on macOS, GNU on Linux), and a `command -v` fallback between them institutionalizes the
ambient tool instead of pinning one; write the bare GNU name and let the PATH answer. A
third-party build script that branches on `uname` and expects BSD flags on macOS meets GNU
there; patch it after fetching, never unpin the PATH for it. Call it from the project tree: an aqua shim finds its pin by walking up from the
working directory, so a recipe that `cd`s into a temp dir outside the repository has no pinned
tool there — hand it the path instead.

The consequence, and the point: a recipe behaves identically on every machine that ran
[machine setup](./tooling.md#machine-setup-limen-install-one-time-per-machine), and
anything *not* pinned is unusable from a recipe by construction. (This is also why a
language toolchain that lives outside aqua — a rustup-managed cargo, for instance — needs
an explicit, documented decision before its recipes can work; the Rust one is in the
[per-language rules](./rust.md).)

## Conventions every shared recipe follows

- **Defaults pass vacuously.** Invoking a module bare (`just do lint`, `just do fix`) runs its
  curated `default`. The membership rule is in [mandatory
  files](./mandatory-files.md#justfile): a recipe belongs in a default only when it no-ops
  harmlessly where it does not apply; anything needing a language toolchain is named
  explicitly (`just do lint go`). The `test` and `perf` modules are the honest limit of that
  rule: *every* test and every report is language-bound, so bare `just do test` and
  `just do perf` refuse with guidance instead of guessing —
  a project declares its suites as a `test` aggregate in the root `.justfile` (mirroring
  `lint`), and that pair is what CI runs.
- **Project knobs are exported variables, named after the task path.** A recipe that
  needs per-project configuration reads an environment variable named after its task path
  — `TEST_GO_TIMEOUT` and `TEST_GO_COVER_MIN` for the test module, `BUILD_GO_FLAGS` and
  `BUILD_GO_LDFLAGS` for the build module. Lint *policy* is not a knob: what
  `just do lint go` accepts — a linter off, an exclusion, a module go-licenses must ignore
  — is declared in the project's `.lint-go.yaml`, a committed file with a defined vocabulary
  ([per-language](./linting-go.md#one-lint-baseline-per-project-carve-outs-lint-goyaml)),
  never an exported variable. A
  project sets them once in the root `.justfile` (`export NAME := 'value'` — exports propagate
  into every module recipe), or on the invocation for a one-off. The exceptions that
  prove the rule carry the name of what they configure rather than one task's:
  `LIMEN_BIN` serves both `lint limen` and `fix limen`; `GO_CGO` declares that a project's
  product cannot link without cgo, which every Go task — build, test, and the per-platform
  analysis legs — has to know.
- **A knob is read in the recipe, not resolved into a module variable.** A child module
  cannot see its parent's variables, and `env()` reads `just`'s *process* environment, which
  never holds one — so `export X := env('X', default)` in a shared module silently discards
  what the project set and re-exports the default over it. Reading `${X:-default}` in the
  recipe body works instead, because a parent's exports *are* present in the recipe's
  environment. `GO_CGO` is the shape to copy: the recipes resolve
  `${CGO_ENABLED:-${GO_CGO:-0}}`, so the project declares its nature once, a single
  invocation can still override in either direction, and unset still means pure Go.
- **A knob composes with the recipe; it does not replace it.** `BUILD_GO_LDFLAGS` is the
  worked example of why that distinction earns a second variable. Linker flags cannot ride
  in on `BUILD_GO_FLAGS`: go honours the *last* occurrence of a repeated flag, and the
  project's flags are appended after the `-ldflags` the recipe computed, so a project
  passing its own would silently drop the version stamp and the CGO linkmode — a build that
  succeeds while quietly losing what the baseline guarantees. So the knob is spliced inside
  the recipe's own `-ldflags` string instead, last, where it adds to the baseline and still
  wins an explicit conflict. Prefer this shape whenever a knob would otherwise let a project
  overwrite something the baseline is responsible for.
- **Go analysis runs once per supported platform.** The Go build graph is selected by
  GOOS *and* GOARCH — a file built only on linux is invisible to a darwin run, and a
  file built only on amd64 (an `_amd64.go`, an `_amd64.s`) is invisible to an arm64 one —
  so the Go linters, vet, vulnerability scan, and license check iterate over every
  supported GOOS/GOARCH pair with CGO disabled (the `_per-platform` helper). A GOOS-only
  loop at the host's architecture would silently skip half the matrix. A project that
  genuinely needs cgo exports `CGO_ENABLED=1` and gets a single native run, with the
  reduced coverage announced loudly rather than hidden.
- **Names are spelled out.** Recipes, commands, flags, and variables use explicit,
  qualified names that can be read and understood without a syllabus: `--dry-run`, never
  `-n`; `just do release`, never `just rel`; `limen github`, never `limen gh`. Shorthand
  flags and abbreviations optimize for the person who already knows — everything here is
  written for the person (or agent) who does not, yet. (Established tool names are not
  respelled: the `gh` binary is called `gh`.)
- **Recipes announce themselves and stay quiet otherwise.** Modules set `set quiet`; each
  recipe's first act is a `▶ module: name` banner (the shared `_banner`). Output beyond
  that belongs to the underlying tool.
- **Recipe bodies are themselves linted.** Multi-line recipes run under
  `#!/usr/bin/env bash` with `set -euo pipefail`, written to bash 3.2 (macOS's system
  bash). `just do lint shell` extracts every shebang recipe body from the parsed justfile
  tree and shellchecks it — the recipes are held to the same standard as any shell the
  repo ships.
- **Discovery respects git; judgment respects the working tree.** Recipes that scan for
  files (`lint just`, `lint shell`, `lint dockerfile`, the homebrew modules) enumerate via
  `git ls-files` — tracked and new-untracked files, honoring `.gitignore` — so generated
  and vendored trees never surprise a lint run. That is git's whole role in a recipe:
  enumeration, plus the questions that are genuinely about history (`lint commits`). What
  gets *judged* is always the working tree as it stands: enumerated paths are
  existence-checked before reaching a tool (a tracked file deleted but not yet staged is
  a skip, not an error), and `lint aqua` compares the checksums file against a
  regeneration from the working tree's manifest — never against HEAD or the index.
  Commit timing must never change a lint verdict.

## The modules, briefly

What each shared module is *for* — mechanics live in the module files themselves:

- **`build`** — compile the project's `cmd/` binaries into `build/`: an optimized,
  reproducible `release` shape plus `debug`, `race`, and (Linux) `static` variants, with
  version stamping from `git describe`. The release shape is the **twin** of the
  goreleaser configuration where a project ships one: the flag sets are kept aligned, and
  a change to either must land in both (each file's comment names the other). Pure Go by
  default; CGO is an explicit opt-in that adds the hardening flag set.
- **`tools`** — the aqua manifest operations (`add`, `set`, `update`, `remove`) that keep
  `.aqua/aqua.yaml` and `.aqua/aqua-checksums.json` changing together; see
  [tooling](./tooling.md#day-to-day-changes--the-just-do-tools-recipes).
- **`lint`** — read-only verifiers: `limen` (this repository against the rules — the
  first thing the default runs, since every other linter trusts the canonical files it
  verifies), `just`, `aqua`, `links`, `yaml`, `shell`, `dockerfile`, and `commits` (DCO and
  commit hygiene over a range) in the default, plus the explicit `go` submodule (code, vet, mod,
  licenses, nilaway, and the informational deadcode report), `rust`, `homebrew`
  (formula style and audit through brew's own vendored tooling — see
  [per-language rules](./homebrew.md)), and `github` (the live GitHub
  settings audit — `limen github check`, needing network and an authed `gh`; see
  [the github chapter](./github.md)). A linter's verdict is a function of the tree: the
  same tree gets the same answer tomorrow. What is not — a scan against a database that
  moves on its own — is not a linter, and lives in `security`.
- **`security`** — scans whose verdict changes with a database, not with the tree, so a green
  turns red with nothing pushed: the `go` submodule's `vuln` (the module's reachable
  dependency graph against the Go vulnerability database, once per supported platform), in
  the default, and `binaries` (every Go binary aqua pins, scanned by the symbols it links: a
  pinned tool carries its upstream's standard library, and a fix there reaches us only when
  that upstream rebuilds, so this is how a known vulnerability in a tool we did not build is
  seen the day the advisory lands; named until the pinned tools are built here with the
  pinned toolchain, since its first run found most of them carrying some). Its CI lane is
  its own, `security.yaml`, apart from `ci.yaml`, so a new advisory reddens one check that
  says what it is, and a red is answered by a bump, never a code change — see
  [the github chapter](./github.md#mainline-doctrine-pull-requests-always).
- **`test`** — the suites, per language (`just do test go`: `unit`, `race` — which asks the
  toolchain whether the host has a race detector at all and, where it does not (windows/arm64:
  Go vendors LLVM's ThreadSanitizer per platform and there is none there), says so loudly and
  passes rather than fail every push on a Go limitation — `fuzz`
  as a short smoke of every `Fuzz*` target — run by the canonical CI's own `fuzz` job on
  one leg, not from the `test` aggregate, so the matrix does not fuzz five times over —
  and `cover` with an optional minimum gate). No default — see
  above: bare `just do test` refuses, `just test` is the project's aggregate.
- **`perf`** — the reports that judge nothing, per language (`just do perf go`: `bce` and
  `escape` by default — every bounds check the compiler could not eliminate, every value it
  moved to the heap and every function it refused to inline, both compile-only and quick —
  and, named, `bench` with allocation stats and `profile` with pprof's top entries and
  rendered call graphs, slow and writing under `build/`). None fails and none asserts,
  which is why they are neither `lint` nor `test`: a number to read, not a verdict, and
  nothing in CI runs them. No default, for the reason `test` has none: every report is
  language-bound, so bare `just do perf` refuses.
- **`fix`** — the mutating counterparts, deliberately separate from `lint`: `limen`
  (rewrite drifted canonical files), `just`, `yaml`, `aqua` (regenerate `.aqua/aqua-checksums.json`)
  in the default, plus the `go`, `rust`, and `homebrew` submodules and `github` (plan shown,
  applied on consent). Nothing mutates under a lint name, and `fix` applies only what cannot
  change behaviour: for Go, the formatters plus the linter fixes that touch layout alone (blank
  lines, a comment's period, struct-tag alignment, `//nolint` directives). Every other linter
  fix is a pattern rewrite with no promise to preserve meaning, and several have broken code:
  a loop shortened by one iteration, a path losing its separator. Those findings stay
  findings, and a person fixes them.

Two roles deserve emphasis because they close the enforcement loop:

- **`just do lint limen` / `just do fix limen`** run the pinned `limen` binary against the
  repository — the rules in this book, enforced from inside the baseline itself. The
  `LIMEN_BIN` knob exists for exactly one consumer: the limen repository, which points it
  at `go run ./cmd/limen` so its working tree is judged by its own enforcer rather than
  the (always older) released pin.
- **`just do release`** is shared; artifacts are opt-in. The recipe, its two lanes, and
  the rules a release follows are [releasing](./releasing.md).

## Extending the baseline

A new shared recipe goes into a module in limen's `.limen/just/` (a new concern gets a new
module plus its `mod` line in `do.just`); the content-pin then carries it
to every repository on the next `limen fix`. A recipe only one project needs goes in that
project's root `.justfile` — the `do` namespace keeps the shared names off the top level, so a
project is free to define its own `lint`/`test`/`build` there. A project recipe that shares a
name replaces, never extends: the baseline sets `allow-duplicate-recipes` so that a project can
redefine `lint`, and the last definition is the whole recipe. A lane is added by appending it
to the one `lint:` line; a second `lint:` line meant to add one silently dropped every
baseline lane, and CI ran the one lane and reported green. `just --show lint` prints what
will run. Global changes are
proposed against limen itself, never edited locally: the shared files are locked by the
content-pin, and drift is overwritten.

# The shared recipes

Every repository carries the same task-runner baseline: a root `Justfile` that mounts the
shared `.limen/just/` modules, which `limen` content-pins (the root `Justfile` itself is the
project's own — only its import line is required; the pinning rule and file layout are in
[mandatory files](./mandatory-files.md#justfile)). This chapter documents what that baseline
*is* — the architecture and the conventions every shared recipe obeys — so the behavior you
see from `just` is explainable, not folklore.

**The live inventory is not in this book.** `just --list` enumerates the recipes; each
recipe's comment in its module file is its reference documentation. The book documents the
invariants — the things that stay true as recipes come and go. When this chapter and a
recipe disagree, the recipe is right and this chapter has a bug.

## The execution environment is hermetic

Recipes do not run in your shell's environment; they run in one the `Justfile` constructs:

- **Hermetic `PATH`** — the aqua tool directory plus base system paths, nothing else (no
  Homebrew). Every tool a recipe invokes resolves to the aqua-pinned version or fails
  loudly; a machine-installed copy can never be silently substituted. (Windows/git-bash is
  the one sanctioned exception: with no knowable base-system directory list, the pinned
  tools are *prepended* to the ambient `PATH` — pins still shadow everything, and the POSIX
  legs of the CI matrix keep full hermeticity enforced.)
- **Hermetic Go environment** — Go reads its behavior from a dozen `GO*` variables that
  tunnel through the pinned `PATH`, so each is emptied or pinned: `GOROOT` (IDEs inject
  one) so the pinned toolchain finds its own stdlib; `GOTOOLCHAIN=local` to forbid Go's
  silent toolchain downloads (when `go.mod` outpaces the pin, recipes fail asking for a
  pin bump instead of fetching an unpinned compiler); `GOFLAGS`, `GOPRIVATE`, `GOOS`/`GOARCH`
  neutralized so ambient flags or a cross-compile target can't rewrite a build; and
  `GOSUMDB`/`GOPROXY` pinned to their real defaults so an ambient `GOSUMDB=off` cannot
  defeat the checksum verification the `go_install` pins rely on. Each stays overridable
  by a project's own `Justfile` — explicit and tracked, never ambient. **The one
  exception is `GOWORK`:** a `go.work` in the tree (or a parent) deliberately puts recipes
  into workspace mode — Go workspaces are a supported way to work here, at the eyes-open
  cost that a build under an active workspace can differ from CI.

The consequence, and the point: a recipe behaves identically on every machine that ran
[machine setup](./tooling.md#machine-setup-limen-install-one-time-per-machine), and
anything *not* pinned is unusable from a recipe by construction. (This is also why
language toolchains that aqua cannot pin — rustup-managed cargo, for instance — need an
explicit, documented decision before their recipes can work.)

## Conventions every shared recipe follows

- **Defaults pass vacuously.** Invoking a module bare (`just do lint`, `just do fix`) runs its
  curated `default`. The membership rule is in [mandatory
  files](./mandatory-files.md#justfile): a recipe belongs in a default only when it no-ops
  harmlessly where it does not apply; anything needing a language toolchain is named
  explicitly (`just do lint go`). The `test` module is the honest limit of that rule: *every*
  test is language-bound, so bare `just do test` refuses with guidance instead of guessing —
  a project declares its suites as a `test` aggregate in the root `Justfile` (mirroring
  `lint`), and that pair is what CI runs.
- **Project knobs are exported variables, named after the task path.** A recipe that
  needs per-project configuration reads an environment variable named after its task path
  — `LINT_GO_LICENSES_FLAGS` for `just do lint go licenses`, `TEST_GO_TIMEOUT` and
  `TEST_GO_COVER_MIN` for the test module, `BUILD_GO_FLAGS` and `BUILD_GO_LDFLAGS` for the
  build module. A
  project sets them once in the root `Justfile` (`export NAME := 'value'` — exports propagate
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
- **Go analysis runs once per supported platform.** The Go build graph differs per GOOS —
  a file built only on linux is invisible to a darwin-only run — so the Go linters,
  vulnerability scan, and license check iterate over the supported platforms with CGO
  disabled (the `_per-goos` helper). A project that genuinely needs cgo exports
  `CGO_ENABLED=1` and gets a single native run, with the reduced coverage announced
  loudly rather than hidden.
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
  `aqua.yaml` and `aqua-checksums.json` changing together; see
  [tooling](./tooling.md#day-to-day-changes--the-just-do-tools-recipes).
- **`lint`** — read-only verifiers: `limen` (this repository against the rules — the
  first thing the default runs, since every other linter trusts the canonical files it
  verifies), `just`, `aqua`, `links`, `yaml`, `shell`, `dockerfile`, and `commits` (DCO and
  commit hygiene over a range) in the default, plus the explicit `go` submodule (code, mod,
  vuln, licenses, and the informational bce/escape/deadcode reports), `rust`, `homebrew`
  (formula style and audit through brew's own vendored tooling — see
  [per-language rules](./per-language.md#homebrew-formulas)), and `github` (the live GitHub
  settings audit — `limen github check`, needing network and an authed `gh`; see
  [the github chapter](./github.md)).
- **`test`** — the suites, per language (`just do test go`: `unit`, `race` — which asks the
  toolchain whether the host has a race detector at all and, where it does not (windows/arm64:
  Go vendors LLVM's ThreadSanitizer per platform and there is none there), says so loudly and
  passes rather than fail every push on a Go limitation — `bench`, `fuzz`
  as a short smoke of every `Fuzz*` target — run by the canonical CI's own `fuzz` job on
  one leg, not from the `test` aggregate, so the matrix does not fuzz five times over —
  `cover` with an optional minimum gate, `profile`
  with rendered call graphs). No default — see
  above: bare `just do test` refuses, `just test` is the project's aggregate.
- **`fix`** — the mutating counterparts, deliberately separate from `lint`: `limen`
  (rewrite drifted canonical files), `just`, `yaml`, `aqua` (regenerate `aqua-checksums.json`)
  in the default, plus the `go`, `rust`, and `homebrew` submodules and `github` (plan shown,
  applied on consent). What `lint` reports, `fix` repairs — nothing mutates under a lint name.

Two roles deserve emphasis because they close the enforcement loop:

- **`just do lint limen` / `just do fix limen`** run the pinned `limen` binary against the
  repository — the rules in this book, enforced from inside the baseline itself. The
  `LIMEN_BIN` knob exists for exactly one consumer: the limen repository, which points it
  at `go run ./cmd/limen` so its working tree is judged by its own enforcer rather than
  the (always older) released pin.
- **`just do release`** is shared but opt-in. The recipe lives in `.limen/just/release.just`,
  imported *flat* into the `do` namespace (`do.just`, hence `just do release vX.Y.Z` — a
  module invocation could not take the tag argument), and refuses before touching anything unless the repo carries a
  `.goreleaser.yaml` — which stays project-owned, like the root Justfile. Two lanes share
  every guard. The **CI lane** (public repos, the default): `just do release vX.Y.Z`
  verifies a clean tree, creates the *signed* tag — a human signs the intent — and pushes
  it; the tag push triggers the release workflow, which runs `just do release --ci`:
  goreleaser plus **keyless** cosign, the artifacts signed by the workflow's short-lived
  Fulcio identity (no key exists anywhere) and logged in Rekor. The **local lane**
  (private repos — nothing touches the public transparency log — and the escape hatch
  when CI is down): `just do release --local <cosign-key> vX.Y.Z` does the same tag work,
  then runs goreleaser with key-based cosign — the key path is a mandatory argument, and
  the passphrase is prompted (or piped in with `--cosign-password-stdin`, keeping it out of
  argv and shell history). `just do release --local --dry-run` builds an unsigned
  snapshot into `build/release/` in either lane. The recipe is the interface: the
  workflow contains no release logic of its own.

## Extending the baseline

A new shared recipe goes into a module in limen's `.limen/just/` (a new concern gets a new
module plus its `mod` line in `do.just`); the content-pin then carries it
to every repository on the next `limen fix`. A recipe only one project needs goes in that
project's root `Justfile` — the `do` namespace keeps the shared names off the top level, so a
project is free to define its own `lint`/`test`/`build` there. Global changes are
proposed against limen itself, never edited locally: the shared files are locked by the
content-pin, and drift is overwritten.

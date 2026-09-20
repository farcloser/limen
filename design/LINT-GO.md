# LINT-GO — one Go lint baseline, per-project carve-outs, rendered at lint time

Status: **implemented, with the deviations found while building folded into the text
below.** The largest: the driver, `limen-lint-go`, is a second binary of limen's release
rather than a tool module content-pinned into every repository — the root module cannot
embed a nested module's files, and a repository is better served by a binary that
arrives with the limen pin than by nine source files it must carry. Also: in a list of
named mappings a name the baseline has overrides that entry's keys instead of merging
into it (a rule's arguments are a new list, not the old one grown); the carve-out count
is printed by every render, not by `limen check` (limen parses no YAML); and
`limen-lint-go check` also takes a bare version, to ask about a release before pinning
it. Every load-bearing fact below was verified live on 2026-09-20 (golangci-lint source
at `v2.13.2`, its documentation site, its issue tracker, and the enrolled repositories'
trees). Versions are as-researched, not pinned here.

## Problem

Linting Go takes many tools, each configured its own way. golangci-lint reads a
320-line YAML file; go-licenses takes its allowed list and its ignores as flags;
vet, govulncheck and deadcode take flags of their own. Nothing ties them
together, so a project's lint policy is spread across a config file, recipe
arguments, and environment variables exported from the root Justfile.

And golangci-lint, the largest of them, has no overlay: one configuration file,
first found wins, no `extends`, `include`, or merge, and no way to say "the shared
baseline, minus this, plus that". So every Go repository carries its own copy of
`.golangci.yml`, hand-edited, and the copies drift from limen's and from each
other. A change to the lint policy cannot be applied to any of them without a
manual three-way merge, so it is not applied. Measured against limen's copy: the
closest enrolled repository differs on 58 lines, the farthest on 491;
ossein-kernel is missing a gofumpt improvement (`clothe-returns`,
`balance-calls`) that limen made and never reached it.

go-licenses shows the same pattern, smaller: the allowed-license list is
hardcoded in the recipe, and three repositories export `LINT_GO_LICENSES_FLAGS`
from their root Justfile to add `--ignore` entries. A hidden per-project override.

### Why golangci-lint cannot do it, verified

- The `--config` flag takes one path; `--no-config` and `--enable-only` are the
  only other knobs (`run --help`, `v2.13.2`).
- Upstream has parked the request since 2020:
  [golangci-lint#1141](https://github.com/golangci/golangci-lint/issues/1141)
  (open) is the umbrella; every later ask is closed as a duplicate and pointed at
  [discussion 3954](https://github.com/golangci/golangci-lint/discussions/3954),
  most recently
  [golangci-lint#6621](https://github.com/golangci/golangci-lint/issues/6621) in
  June 2026. The maintainer's objection to the "config in a Go module" trick is
  minimal version selection silently bumping the config.
- The loader does accept a config on stdin (`-c /dev/stdin`) and reads YAML,
  TOML or JSON by extension. Nothing more.

## Decisions

1. **One baseline, owned by limen, embedded in a tool of ours.** Projects never
   edit it.
2. **One overlay per repository, `.lint-go.yaml` at the root**, project-owned,
   seeded once. It covers every Go lint lane that has a per-project knob, not
   only golangci-lint. Root placement follows the lychee precedent (canonical
   `.limen/lychee.toml`, project's own `.lychee.toml`); a `config/` directory would
   be a new convention and is not introduced for one file. Root-file
   proliferation is a watched cost.
3. **The effective golangci config is a build artifact**, `build/golangci.yml`,
   rendered by the lint recipe on every run and passed with `-c`. Nothing is
   committed, nothing at the root invites a hand edit, and editing the overlay then
   running `just do lint go` takes effect immediately. IDE auto-discovery of a root
   `.golangci.yml` is deliberately given up: the workflow is `just lint`.
4. **The merge lives in `limen-lint-go`, a second binary of limen's release, not in
   limen.** limen is zero-dependency and has no YAML parser (its pins and aqua
   manifests go through hand-rolled line parsers for their own narrow shapes); a
   golangci config is nested maps, lists of maps and quoted regexes. A third ad-hoc
   parser is a smell; a YAML dependency in limen contradicts zero-dependency. So the
   driver is a nested module in limen's tree, `cmd/limen-lint-go`, built by the release
   workflow into the same archive, verified by the same checksum and signature, pinned
   by the same aqua entry. One clock for the recipes that call it, the baseline it
   embeds and the overlay contract limen's rule seeds. It is the one pure-Go binary
   beside limen a repository takes prebuilt: the doctrine's exception for limen extends
   to limen's own binaries, built by the same pinned toolchain in the same job.
5. **The driver owns configuration, never execution.** Each analyzer keeps its own
   `tools/<name>` module built by `_go-tool` (the reasoning of the golangci-lint
   move: no minimal-version-selection mixing across analyzers), and the just
   recipes stay the only orchestrator. The driver does not import golangci-lint's
   packages, whose API stability upstream does not state.
6. **Baseline and golangci-lint version are checked against each other at lint
   time.** The baseline names linters; a name unknown to an older golangci-lint
   fails the run (the `exhaustruct_v5` line in today's config says so). The
   baseline ships in limen releases, the golangci-lint pin moves per repository
   with Renovate, so they can run ahead of each other. The driver refuses to run
   when the pinned golangci-lint is older than the version the baseline declares.

## Architecture

```
limen's tree                                    every Go repository
cmd/limen-lint-go/        a nested module        .lint-go.yaml         seeded once, the project's own
├── go.mod                (YAML parser only)      tools/golangci-lint/  tool directive, upstream's graph
├── main.go                                       build/                gitignored
├── baseline.yml          embedded                ├── tools/golangci-lint   built by _go-tool
├── .lint-go.yaml         the driver's own        └── golangci.yml          rendered on every run
└── internal/lintgo/      render · merge · check · flags · tests
        │
        ▼ goreleaser (second build, same archive, same checksum and signature)
limen release: limen + limen-lint-go, pinned by the one aqua entry (files: limen, limen-lint-go)

                 just do lint go
                       │
        ┌──────────────┼──────────────────────────┐
        ▼              ▼                          ▼
  _go-tool golangci-lint   _golangci-config     _go-tool go-licenses
                              │                          │
                              ├─ limen-lint-go check build/tools/golangci-lint
                              │    reads the binary's build info (debug/buildinfo);
                              │    refuses a release older than the baseline's floor
                              ├─ limen-lint-go render -o build/golangci.yml
                              │    baseline ⊕ .lint-go.yaml, placeholders filled
                              │    from go.mod; prints the carve-out count
                              │    then: golangci-lint run -c build/golangci.yml
                              │          golangci-lint fmt --diff -c build/golangci.yml
                              └─ limen-lint-go flags licenses
                                   --allowed_licenses=… --ignore=… spliced into go-licenses
```

limen's own repository builds `limen-lint-go` from its tree before linting
(`LIMEN_LINT_GO_BIN`, the twin of `LIMEN_BIN`), and lints and tests the nested
module itself, since the shared Go lanes stop at the root module. The registry
entry keeps releases before the second binary installable with a version
override. `limen-lint-go baseline` prints the baseline as shipped, so a
project reads what it carves out from without limen's source.

## The overlay: `.lint-go.yaml`

One section per lane that has a knob. golangci's section uses golangci's own
vocabulary, same keys and shapes, so it reads as a fragment of the file it
patches. It is not a runnable golangci config on its own.

```yaml
# .lint-go.yaml: this project's carve-outs. The baseline is limen's.
golangci:
  linters:
    disable:
      - zerologlint
    exclusions:
      paths:
        - third_party/vz
      rules:
        - path: testutil/
          linters: [gosec]
    settings:
      wrapcheck:
        ignore-package-globs:
          - github.com/farcloser/ossein/*
  formatters:
    settings:
      golines:
        reformat-tags: false

licenses:
  ignore:
    - gotest.tools/v3   # google/go-licenses#186
```

These are the four kinds of carve-out observed in the enrolled repositories
(ossein, ossein-kernel, primordium against limen): linters taken out, exclusions
added, per-linter settings changed, and project parameters. The last kind
(depguard's allow list, gci's prefix carry the module path) is not a carve-out:
the driver reads `go.mod` and fills them into the baseline itself.

Comments live in the overlay. The rendered file carries none: nobody edits it.

### Merge rules

| Overlay key | Effect on the baseline |
|---|---|
| `golangci.linters.disable` | removes from the enable set (the baseline runs `default: all`, so this is golangci's own meaning too). Already disabled: no-op, reported. |
| `golangci.linters.enable` | adds. Already enabled: no-op, reported. |
| `golangci.linters.exclusions.paths`, `…rules` | append. Nothing in the baseline's exclusions can be removed; if a project needs that, the baseline is wrong and it is a limen change. |
| `golangci.linters.settings.<linter>`, `golangci.formatters.settings.<formatter>` | merge key by key: a scalar overrides, a map recurses, a list **appends**. Observed: forbidigo patterns and depguard's allow list are added to in every carve-out, never rewritten. In a list of maps that all carry a `name` key (revive's rules) a name the baseline has overrides that entry's keys — a rule's arguments are a new list, not the old one grown — and a new name appends. Nothing in a baseline list can be removed; as for exclusions, that is a limen change. |
| `licenses.allowed` | replaces the allowed list. |
| `licenses.ignore` | appends `--ignore` flags. |
| anything else (`run`, `issues`, `version`, unknown keys) | rejected. Those are policy. |

The driver validates the overlay against this table before rendering. The
rendered result is validated by golangci-lint when it runs.

### Version coupling

`baseline.yml` declares the golangci-lint version it was written for. `limen-lint-go
check` reads the module version out of `build/tools/golangci-lint` with
`debug/buildinfo` and fails when it is older. A newer golangci-lint warns on a
retired linter name rather than failing on an unknown one, so Renovate's per-repo
bump is always safe; the limen release that moves the baseline forward bumps
limen's own `tools/golangci-lint` first.

## User stories

**I want gosec off under `testutil/` in my repository.**
Add the exclusion rule to `.lint-go.yaml`, run `just do lint go`. The render is
the recipe's first step; golangci-lint sees the change. Nothing else to run,
nothing to commit but the overlay.

**limen ships a new lint policy (a linter enabled, a threshold changed).**
It lands in `baseline.yml`, content-pinned. Renovate bumps limen in each
repository; the checksum-update workflow runs `limen fix`, which rewrites
`cmd/limen-lint-go/*` to the new canonical. The project's carve-outs are untouched,
because they live in the overlay. If the new baseline drops a linter the overlay
disables, the disable becomes a reported no-op.

**Renovate bumps golangci-lint in one repository ahead of limen.**
The lint runs; a retired name is a warning, not a failure. Green.

**A repository bumps limen (new baseline) but its golangci-lint pin is older
than the baseline declares.**
`limen-lint-go check` fails the lane with the two versions and the recipe to run
(`just do tools update golangci-lint`). Loud, at the right spot.

**Someone edits `.golangci.yml` at the root out of habit.**
golangci-lint never reads it (`-c` disables discovery). `limen check` reports it
as a stray with the pointer to `.lint-go.yaml`.

**I want to see what a repository carves out of the baseline.**
Read its `.lint-go.yaml`. Across the organization: grep the overlays. Every
`just do lint go` prints the overlay's carve-out count, and the entries that
changed nothing as notes, and does no more policing than that. Carve-outs get
cheap on purpose; visibility is the counterweight.

## What changes where

| Where | Change |
|---|---|
| `cmd/limen-lint-go/` | The driver, a nested module: `main.go`, `internal/lintgo/` (render, merge, check, flags, tests), `baseline.yml`, its own `.lint-go.yaml`. |
| `.goreleaser.yaml`, `.limen/aqua-registry.yaml` | A second build into the same archive; the limen entry lists both binaries, with an override keeping earlier releases installable. |
| limen `internal/rules/lintgo.go` | Seed `.lint-go.yaml` once; a root golangci-lint configuration fails check and is an advisory on fix. Go modules only. |
| `.limen/just/lib.just`, `lint-go.just`, `fix-go.just` | `_golangci-config` checks the floor and renders; `code` runs golangci-lint with `-c build/golangci.yml`, `licenses` splices `$(limen-lint-go flags licenses)`; `LINT_GO_LICENSES_FLAGS` retired. |
| limen's root `Justfile` (limen's own, not shared) | Builds the driver from the tree before the lanes run (`LIMEN_LINT_GO_BIN`), lints and tests the nested module. |
| Enrolled repositories | The limen bump brings the binary; `limen fix` seeds `.lint-go.yaml`; the project moves its carve-outs into the overlay by hand and deletes `.golangci.yml` and its `LINT_GO_LICENSES_FLAGS` export. Owners' work, one PR each. |
| `book/` | per-language.md, tooling.md, mandatory-files.md, recipes.md. |

## Not in this cut

- Multi-module repositories. The recipe runs at the root; a second Go module
  would have no overlay of its own. None observed among the enrolled
  repositories (every one has a single root `go.mod` besides `tools/`); named,
  deferred.
- Knobs for vet, vuln, deadcode, bce, mod, the suppression check, the binaries
  lane: they have none today. A lane that grows one gets a section and a `flags`
  subcommand, same pattern.
- A published schema for `.lint-go.yaml`. Nesting the golangci fragment under a
  key costs golangci's own JSON-schema validation of the overlay in an editor;
  the driver validates it instead.

## Risks

- **First hand-picked dependency in limen-shipped code.** The driver's YAML library
  is the only third-party module any binary of limen's release carries. It lives in the
  driver's own module, not in limen's, and Renovate manages it like any requirement.
- **Merge semantics are ours.** The table above is small and every key has one
  defined effect; anything outside it is rejected rather than guessed. The cost
  of a wrong rule is a rendered file golangci-lint rejects, visibly.
- **The baseline moves only with limen releases.** Intended: it is how every
  canonical file moves. A policy change that cannot wait for a release is an
  overlay entry in the meantime.

## Sources (verified 2026-09-20)

- golangci-lint configuration file documentation: single file, lookup order,
  no composition.
- `golangci-lint run --help` at `v2.13.2`: `-c PATH`, `--no-config`,
  `--enable-only`.
- `pkg/config/base_loader.go` at `v2.13.2`: `os.Stdin.Name()` accepted as the
  config path; `go.mod` requires `go.yaml.in/yaml/v3` directly.
- golangci-lint#1141 (open), #4895 and #6621 (closed as duplicates), discussion
  3954.
- `diff` of limen's `.golangci.yml` against every enrolled repository's copy;
  `grep LINT_GO_LICENSES_FLAGS` across root Justfiles.

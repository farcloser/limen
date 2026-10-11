# Mandatory files

Every Farcloser repository, without exception, is a git repository and carries a small set of
files that make it legible, legally clear, and consistent to work in. The lowest bar a repo
can clear, and the first rule `limen` enforces.

| Requirement | What it means |
|-------------|---------------|
| **Git repository** | The project root is a git repository: a `.git` directory (clone) or a `.git` file (worktree, submodule). |
| `LICENSE` | Present, and one of the **allowed licenses** below. |
| `.editorconfig` | Present, and **content-pinned**: equals the [canonical baseline](#canonical-editorconfig) byte for byte. |
| `.gitignore` | Present, carrying the [required patterns](#gitignore). Seeded from the canonical file when absent; beyond the patterns, an existing one is the project's own. |
| `.gitattributes` | Present, and **content-pinned**: the [canonical file](#canonical-gitattributes) disabling git line-ending conversion. |
| `AGENTS.md` | Present, and **content-pinned**: the [working agreement](#canonical-agentsmd) for a coding agent, identical everywhere. |
| `CLAUDE.md` | Present. Seeded as the one-line `@AGENTS.md` import when absent; an existing one is the project's own. |
| `README` | Present, as `README.md`. |
| `.justfile` | Present, carrying the shared-baseline import; the rest is the project's own. The [`.limen/` modules](#justfile) it mounts are canonical. |
| `.limen/lychee.toml` | Present and canonical: the shared [link-checker configuration](#link-checking--limenlycheetoml). |
| `.github/` workflows | The [CI surface](#ci-workflows--github): content-pinned limen pieces, plus seeded-once workflows and Renovate config. |
| `tools/go.mod` | The Go-built tools the recipes run, as `tool` directives; a Go module adds the source analyzers: the [`gotools` rule](#the-gotools-rule--toolsgomod-tool-directives). |
| `.limen/lint-go.yaml` | Go modules only. Present and canonical: the shared [Go lint baseline](./linting-go.md#one-lint-baseline-per-project-carve-outs-lint-goyaml) `limen-lint-go` renders with the project's carve-outs. |
| `.lint-go.yaml` | Go modules only. The project's carve-outs from the Go lint baseline, seeded once and the project's own (the `lintgo` rule). A root `.golangci.yml` is a stray. |

`limen` resolves common variants (`LICENSE`, `LICENSE.md`, `LICENSE.txt`, `COPYING`;
`README`, `README.md`, `README.txt`) so a repo is not failed on a technicality, but
**`README.md` and a bare `LICENSE` are the canonical names**.

Every repository also pins its build and CI tooling through aqua — equally mandatory, with
its own chapter, [project tooling](./tooling.md) — and keeps its GitHub settings at the
baseline: [GitHub settings](./github.md).

You do not hand-create these files: `limen bootstrap <path>` scaffolds a new repository with
all of them, and `limen fix` brings an existing one up to the baseline — writing what is
missing, resetting the content-pinned files, and merging the baseline into the subset files
(`.gitignore`'s patterns, `.aqua/aqua.yaml`, the root `.justfile`'s import line) without
discarding a repo's own additions. What it cannot fix safely — a disallowed `LICENSE`, a
manifest it cannot parse — is reported for a human to resolve.

### Onboarding an existing repository

Enrolling a codebase that predates the baseline applies the lint rules to code that never saw
them, so the lint lanes go red by construction. Green before merge holds for the enrolment
pull request all the same: a red `main` is inherited by every pull request opened after it.
The order:

1. **Enrolment lands green, with the backlog in the overlay.** The pull request carries
   `limen fix` and the mechanical fixes (`just do fix …`). Every Go finding still left is
   exempted in `.lint-go.yaml`, by linter and path, as narrowly as turns the lane green, in
   one block whose comment marks it as the onboarding backlog. The other lanes have no
   overlay ([linting](./linting.md)); their findings are fixed in the enrolment pull request.
2. **Alignment empties the block.** One linter or one package per pull request, each green,
   each finding judged as any other ([judging a finding](./linting.md#judging-a-finding)):
   fixed, silenced inline with its reason, or raised as a rule. The last alignment pull
   request deletes the block.

The backlog block is the one carve-out that is not a judgment: every other carve-out records
a decision that the code is right as it is; this one records findings not yet read. It is
labelled as such and exists only to be emptied. Two shortcuts are ruled out: merging the
enrolment red with the alignment "right behind it" leaves every pull request opened in
between red for a reason unrelated to its change; enrolment and alignment in one pull
request mixes mechanical rewrites with judged changes in one diff, which cannot be reviewed
as either.

Beside the files every repository carries, limen enforces rules that apply **only when a
project uses a given language or tool**: a Go module, YAML files, a Homebrew tap. Present,
the requirement is mandatory; absent, `limen` reports nothing for it. Those rules live with
their topic: [linting](./linting.md) and its sub-documents,
[dependencies](./dependencies.md), [homebrew](./homebrew.md), [rust](./rust.md).

## Allowed licenses

A repository's `LICENSE` must be identifiable as exactly one of the licenses below, split by
what the repository *contains*: **software** versus **content** (documentation, writing,
images). `limen` enforces only that the `LICENSE` is one of these; *which* one is
engineering judgment, captured in the "When to use" notes — a machine cannot tell a small
library from a large platform.

### Software

| Identifier | When to use | How it is recognized |
|------------|-------------|----------------------|
| `MIT` | **Small libraries** and utilities. | MIT grant (*"Permission is hereby granted, free of charge…"*) plus the *"AS IS"* disclaimer. |
| `Apache-2.0` | **Larger projects and anything enterprise-facing.** | Declares *"Apache License"* and *"Version 2.0"*. |
| `GPL-2.0` | **Kernel-derivative works**: Linux modules, patches, tooling that must be GPL-2.0-compatible with the kernel. | Declares *"GNU General Public License"* and *"Version 2, June 1991"*. |
| `AGPL-3.0` | **SaaS / network services**, when the copyleft should reach hosted use. | Declares *"Affero General Public License"* and *"Version 3"*. |
| `BSD-3-Clause` | **Inherited only**: forks of BSD-licensed upstreams. **Never a choice for new code**; `limen bootstrap` will not generate it. | *"Redistribution and use in source and binary forms"* plus the non-endorsement clause, without BSD-4's advertising clause. Tolerant of the personalized clause-3 subjects older projects carry. |
| `Closed-source` | **Proprietary**: anything not released publicly. | Explicit reservation of rights, no open grant. The canonical text below. |

- **MIT for small libraries.** Permissive and friction-free for code whose value is in being
  widely embedded; the patent and contribution machinery of heavier licenses buys nothing for
  a few hundred lines, and MIT is what downstream expects from a small dependency.
- **Apache-2.0 for larger projects and enterprise.** The **express patent grant** and its
  retaliation clause protect users from patent claims by contributors, the assurance
  enterprise legal teams look for; the explicit `NOTICE` and contribution terms scale across
  many contributors.
- **GPL-2.0 for kernel-derivative works.** A kernel module, a patch, or tooling that derives
  from GPL-2.0 code inherits GPL-2.0: an obligation the kernel imposes, allowed for exactly
  that case. `GPL-2.0-only`, not the `WITH Linux-syscall-note` variant, which is a per-file
  grant for exported UAPI headers, not a whole-repo license. New code not forced onto GPL
  takes Apache-2.0 or AGPL-3.0.
- **AGPL-3.0 for SaaS, as an option.** Plain GPL's copyleft is defeated by "we only run it on
  our servers"; AGPL closes that: providing the software over a network triggers the
  obligation to share source. A deliberate option, since the same reciprocity deters
  commercial adopters.
- **BSD-3-Clause for inherited code, and nothing else.** A fork of a BSD-licensed upstream
  cannot shed the upstream's terms, so `limen check` accepts it. MIT already owns the
  permissive slot for code we author, so `bootstrap` never offers it: acceptance of the
  inherited, not endorsement for the new.
- **Closed-source for proprietary.** The default without a license is already "no rights
  granted"; an explicit notice removes the ambiguity for collaborators and tooling.

### Content

| Identifier | When to use | How it is recognized |
|------------|-------------|----------------------|
| `CC-BY-SA-4.0` | **Documentation.** | *"Creative Commons Attribution-ShareAlike"* + *"4.0"*, or the `by-sa/4.0` license URL. |
| `CC-BY-ND-4.0` | **Writings** (essays, posts, opinion). | *"Creative Commons Attribution-NoDerivatives"* + *"4.0"*, or the `by-nd/4.0` license URL. |
| `Closed-source` (All rights reserved) | **Photography** and other images. | All-rights-reserved notice; `limen` reports it as `Closed-source`. |

- **CC BY-SA 4.0 for documentation.** Docs are meant to be copied, adapted and kept current by
  anyone; share-alike keeps improvements flowing back under the same terms.
- **CC BY-ND 4.0 for writings.** For authored prose, integrity matters more than remixing:
  share freely and credit us, but no altered version under our name.
- **All rights reserved for photography.** Images are rarely improved by remixing and their
  value is in controlled use; use is granted case by case. Legally the same instrument as
  closed-source software, hence the classification.

### Anything else is a failure

GPL-3.0, LGPL, BSD-2-Clause, BSD-4-Clause, MPL, an unrecognized or hand-edited license, a CC
variant not listed (`BY-NC`), or any `LICENSE` `limen` cannot classify is a **failure**
(`GPL-2.0-only` is allowed and no other GPL/LGPL; `BSD-3-Clause` only as inherited). If we
genuinely need another license, it is added to this list and to `limen`'s policy first: the
tool is the enforcement, the book the decision.

### Canonical closed-source notice

So that `Closed-source` is detected deterministically, proprietary repositories use this
`LICENSE` text verbatim (adjust the year and holder):

```
Copyright (c) 2026 Farcloser. All rights reserved.

This software and its source code are proprietary and confidential. No license,
express or implied, is granted to any person to use, copy, modify, distribute, or
create derivative works of this software, in whole or in part, without the prior
written permission of Farcloser.
```

## Canonical .editorconfig

The baseline lives in one place: this repository's own [`.editorconfig`](../.editorconfig),
which `limen` embeds at build time and enforces. That file is the source of truth; the book
carries the reasoning, not a copy.

It is **content-pinned**: byte for byte, no extra sections, no edited values; `limen fix`
overwrites drift. The baseline is comprehensive — it covers every language we work in, and a
section only ever matches files that are present — so a repo never needs its own additions.
Each file type uses the indentation its own tooling treats as canonical, so the config never
fights the formatter: tabs where `gofmt` and `make` require them, four spaces where `just
--fmt` and `rustfmt` produce them, two spaces for the data and web-language families that
`jq`, `yamllint`, Prettier and Biome emit; a two-space catch-all for anything without
tooling, and each family pinned explicitly so a format never drifts if the fallback changes.

## .gitignore

A `.gitignore` must exist and carry four patterns: `/build`, `/_scratch`, `.claude` and
`.idea` — what the recipes, the working sessions and the editors write inside a checkout,
none of it ever committed. There is no repository-local `tmp/`: a disposable file that
persists between runs goes under `build/`, in a subdirectory named for what it holds, and one
that lives for a single command goes in a `mktemp -d` under `$TMPDIR`, removed by a `trap`.
Spelling is free (`.idea`, `.idea/`, `/.idea`, `**/.idea` all count); a commented-out or
negated pattern does not. The check names a missing pattern, and `limen fix` appends the
missing ones in one marked block at the end, never touching the rest.

Everything else in the file is the repository's own. When a repository has none, `limen
fix`/`bootstrap` seeds this repository's own [`.gitignore`](../.gitignore), which `limen`
embeds, as a starting point; editing the embedded file changes only future seeds. The
required set is deliberately short: enforcing the whole canonical file imposed every
language's patterns on every repository; enforcing nothing let a repository onboarded with
a `.gitignore` of its own miss what the rig writes.

## Canonical .gitattributes

One rule: `* -text` — no git line-ending magic, for any file, in either direction. What is
on disk is what is committed is what every checkout gets, byte for byte, on every platform.
Plus one exemption that is not about line endings: `*.patch` and `*.diff` are excluded from
git's whitespace check, the one the commit lint runs, since a unified diff's context lines
carry space-before-tab by construction for any tab-indented source, and a repository that
vendors patches could never rebase one and pass.

The failure it prevents: Windows machines (GitHub's runners included) default git to
`core.autocrlf=true`, which rewrites every text file to CRLF at checkout, and every format
checker then fails against its canonical LF output (`just --fmt --check`, `gofmt`,
`yamlfmt`). Two consequences, both inherited from the Go project's identical file:

- **LF is enforced by editors and linters, not by git.** The pinned `.editorconfig` says
  `end_of_line = lf`; the format linters fail loudly on CRLF that sneaks through. A Windows
  contributor needs an editor that writes LF, the bar Go sets.
- **No local additions.** The file is content-pinned: an extra attribute (an `eol=` override,
  an LFS filter) would reintroduce content transformation between the working tree and the
  object store, which is exactly what the pin removes. What a project needs scoped to one
  subtree goes in a per-directory `.gitattributes` there, which the pin does not govern; a
  subtree that turns conversion back on owns the consequence.

## Canonical AGENTS.md

The working agreement for a coding agent — branches and worktrees, commits, green before
pushing, when a review is requested, scope and green lights, silence-by-rule, pin-by-digest —
compressed to the rules that must hold in every repository, each pointing at the chapter
that argues it. The doctrine lives in the book; `AGENTS.md` is its enforceable summary, and
`limen` pins it so the same rules load whichever repository an agent starts in.

Two regimes:

- **`AGENTS.md` is content-pinned.** The harness-neutral file every coding agent reads
  (Codex, Copilot, Cursor, and Claude Code through the import below), and the rules in it are
  the organization's, not a project's: a local addition would make one repository's
  agreement drift from the rest.
- **`CLAUDE.md` is seeded once, then the project's own.** Claude Code reads `CLAUDE.md`, not
  `AGENTS.md`; the seed is the single line `@AGENTS.md`. Everything a project wants to tell
  an agent about itself — a self-hosting knob, a module it must not reconverge, a testing
  quirk — goes below that line. `limen` requires the file to exist and never touches it.

The step-by-step procedure (the commands for a worktree, a signed commit, a pull request and
its review request) is a skill, `skills/contribute` in the limen repository, for harnesses
that load skills; `AGENTS.md` carries the rules without it.

## .justfile

Every repository drives its tasks through [`just`](https://github.com/casey/just), so "how
do I build/test/run this?" has the same answer in every repo, for humans and agents alike.
This section covers the files and their pinning; what the shared recipes do is
[its own chapter](./recipes.md). The setup is a root `.justfile` plus a `.limen/` directory
of modules, split so the shared parts stay identical everywhere while each project keeps room
of its own:

| File | Role | Checked? |
|------|------|----------|
| the root `.justfile` | The shared-baseline import (`import '.limen/just/main.just'`) and this project's **own** recipes: its `lint`/`test` aggregates (what CI runs), `build`/`run`, anything else. `bootstrap`/`fix` add the import when absent and never overwrite the project's recipes. | Only the **import line** is required. |
| `.limen/just/*.just` | The **shared recipe baseline**: `main.just` mounts the `do` tree, sets the hermetic environment and carries the orientation recipes (`default`, `info`); under `do` sit `build`, `tools`, `lint`, `security`, `test`, `perf`, `fix`, each a `mod`, plus `release` imported flat so it can take a tag argument. | Content-pinned: **every `*.just` under `.limen/just/`** matches the canonical exactly, and a `*.just` there that is no canonical module (one an earlier limen shipped and later renamed) fails and is removed by `limen fix`: the directory is limen's alone. |

`.limen/` also parks the non-recipe canonical configs (`.limen/.shellcheckrc`,
`.limen/.yamlfmt`, `.limen/lint-go.yaml`, `.limen/aqua-registry.yaml`, `.limen/aqua.yaml`,
`.limen/lychee.toml`), each governed by its own rule ([linting](./linting.md),
[tooling](./tooling.md), [link checking](#link-checking--limenlycheetoml)), not the
`.justfile` content-pin.

Orientation recipes are flat (`just info`, a project's own `just run`), so the universal
"where am I? / do the project thing" commands are unprefixed everywhere. Every *shared*
recipe lives under `do` (`just do tools add …`, `just do lint go`, `just do security
links`, `just do fix yaml`), which frees the top level: a project defines its own `just
lint` or `just test` with no collision, as this repository does, aggregating the `do::`
recipes CI runs. Invoking a module bare runs its **`default`**, the curated set that applies
safely to every repository: `just do lint` runs `limen`, `just`, `aqua`, `yaml`, `shell`,
`dockerfile`, `commits`; `just do fix` runs `limen`, `just`, `yaml`, `aqua`. A recipe
belongs in a default only when it passes vacuously where it does not apply: `shell` and
`dockerfile` discover their targets and no-op without any, while recipes that need a
language toolchain (`go`, `rust`) are named explicitly. Bare `just do test` refuses — every
test is language-bound — so each project declares `lint` and `test` aggregates in the root
`.justfile`, which is what CI runs. `just do lint aqua` (so the bare `just do lint`) needs
the network; on drift it reports and leaves the file, and `just do fix aqua` regenerates it.
**All customization goes in the root `.justfile`; the shared `.limen/just/` modules are
locked.**

`just info`, in every repo, prints what anyone landing in a checkout needs to orient: the
project name, the git upstream, the closest semver tag, the commit, the date of the last
commit.

`limen` content-pins every `*.just` under `.limen/just/` against the baseline embedded in the
binary; the source of truth is this repository's own [`.limen/just/`](../.limen/just). A repo
whose shared modules differ from the baseline, or omit one, fails. A new shared module is a
new `.limen/just/NAME.just` plus its `mod` line, and it is part of the enforced baseline from
there.

## CI workflows — `.github/`

The `.github` surface mixes two regimes, and the split is the point:

| File | Regime | Why |
|------|--------|-----|
| `.github/workflows/update-aqua-checksum.yaml` | **Content-pinned** | Limen machinery, and a *write-capable* workflow: its hardening (no `pull_request_target`, no secrets near branch-controlled code, env-only branch names) must never drift. Drift here is a vulnerability, not customization. |
| `.github/actions/setup-aqua/action.yaml` | **Content-pinned** | The composite action every canonical workflow bootstraps aqua with; its pins and checksum verification are the supply-chain floor. |
| `.github/actions/windows-cache-image/action.yaml` | **Content-pinned** | The composite action the canonical CI keeps its Windows caches in: one NTFS disk image holding the Go caches, aqua's root and the linter cache, cached as one file and attached at `C:\vcache` with a Defender exclusion ([windows](./windows.md#caches-on-the-windows-runners)). |
| `.github/workflows/ci.yaml` | **Seeded once** | Projects add to CI (jobs, runners, services), so the file is theirs; the shared lanes are not in it. The seed carries two jobs: `limen`, which calls `limen-verify.yaml` (optionally with its own runner list as the `os` input), and `gate`, the one required check, which needs `limen` and every project job. A `ci.yaml` still byte for byte an earlier release's seed was never edited: the check fails naming that release, and fix replaces it with the current seed; an edited one stays the project's, and fix notes when it does not call `limen-verify.yaml` yet. The check also fails a reference to a path limen moved (a cache key hashing the root `aqua.yaml`), naming the file, the line and the new path, in every workflow and `renovate.json`. |
| `.github/workflows/limen-verify.yaml` | **Content-pinned** | The shared CI lanes as a reusable workflow every `ci.yaml` calls: `verify` (lint and test on each runner of the `os` input, with the Windows cache image), `fuzz` (one linux leg) and `tools` (a real install of every pin) ([github](./github.md#mainline-doctrine-pull-requests-always)). Pinned so a change to the lanes reaches every repository with its limen bump. |
| `.github/workflows/security.yaml` | **Content-pinned** | The scans' lane, apart from `ci.yaml`: one linux leg running the project's `just security` on every push and pull request and once a day, since the database a scan judges against moves without a push. Its own check, so a new advisory reddens `security`, never `gate`. What a project adds is a scan, in its `security` recipe; the workflow around it is the same everywhere. |
| `pins.yaml` | **Optional, the project's own**; judged by the `pins` rule when present | The artifacts a build fetches by hand, pinned by digest, declared once and read back by the build; Renovate moves the version through the shared preset and `limen pins refresh` brings the digest along ([tooling](./tooling.md#pinned-artifacts-beyond-aqua-pinsyaml)). |
| `.limen/renovate.json` | **Content-pinned** | The canonical Renovate configuration: cooldowns, groups, the managers for `pins.yaml` and the Go-built tools, the dashboard. Carried byte for byte like the rest of `.limen/`, so a fix reaches every repository with its limen bump, in that pull request's diff, and Renovate reads it from the repository itself. |
| `renovate.json` | **Seeded once**; three things maintained by the `renovate` rule | The project's Renovate config, which extends the canonical preset by the repository's own name (`local>owner/name//.limen/renovate`); the rule's section below. |
| `.github/workflows/release.yaml` | **Seeded once, conditionally**: only where a `.release-go.yaml` exists | Releasing is opt-in by carrying a goreleaser config (the gate the `release` recipe enforces); a non-releasing repo gets no dormant workflow that would fail red on a stray tag. |
| `.github/release.yml` | **Content-pinned** | How GitHub groups the release notes it writes from the merged pull requests' titles: breaking changes, changes, dependencies. Every tag has a release page, so every repository carries it. Where a `.release-go.yaml` exists, its `changelog:` section (`use: github-native`) is pinned with it, the one part of that file that is limen's ([release notes](./releasing.md)). |

`limen check` fails a drifted or missing pinned piece, a missing seeded piece (content is
never judged after the seed), and a missing release workflow in a repo that carries
goreleaser config. `limen fix` resets the pinned pieces and seeds the rest, after which
`ci.yaml`, `release.yaml` and `renovate.json` are the project's own, like the root
`.justfile`.

### The `renovate` rule — who may commit onto Renovate's branches

`renovate.json` is plain JSON under this exact name, on purpose: Renovate reads
`forkProcessing` only from `renovate.json`, the onboarding config it fetches through the
platform API *before* resolving any preset, so a repository that is a GitHub fork is skipped
entirely, silently, without it, and neither the shared preset nor a `renovate.json5` can
carry the setting. Prose goes in the schema's `description` array. Any other config file
Renovate would read (`renovate.json5`, `.renovaterc`, `.github/renovate.json`) is dead once
`renovate.json` exists yet still looks authoritative: the rule fails naming it, and removing
it is a hand step, since what it carries is the project's to move or drop. Overrides and
additions go in `renovate.json` next to the `extends` reference (Renovate merges
`packageRules` in order, last match wins). The canonical configuration is `.limen/renovate.json`,
which the file extends as a [Renovate preset](https://docs.renovatebot.com/config-presets/)
by the repository's own name, because Renovate refuses a relative reference in a
repository's own config (its validator accepts one; the run then fails). The seed lives in
limen's code beside the rule, not in limen's own `renovate.json`: that one carries the preset
author's manager and App identity, false anywhere else, and a seed that copied it once
planted both in another org's repository.

The rule maintains three things and touches nothing else: the `extends` reference, which it
writes from the origin remote on every `limen fix` (a name goes stale on a rename, a
transfer or a fork) and leaves unenforced with no origin to read; `forkProcessing`; and the
`gitIgnoredAuthors` array. A `description` entry still word for word an earlier seed's text
is replaced by the current seed's; one the project edited is its own. A file whose values
comply is left byte for byte, whatever its key order or escaping, so a hand-edited config
does not come back re-serialized in every branch the checksum workflow runs `limen fix` on.

`gitIgnoredAuthors` is the load-bearing one. Renovate treats a commit by an author it does
not know as a human edit and stops rebasing that branch, and the `update-aqua-checksum`
workflow commits onto every aqua-bump branch as the organization's update-App bot user
([tooling](./tooling.md)); so that identity,
`<user-id>+<app-slug>[bot]@users.noreply.github.com`, must be in the array or every aqua
bump quietly goes stale after its first fix-up. The seed cannot know it: the App is
per-organization and its user id exists only once the App is registered. So limen resolves
it at run time, from the organization the `origin` remote names, and `check` and `fix`
resolve it differently on purpose:

- **`limen check` is deterministic.** It assumes the App is named as limen registers it,
  `limen-ci-<org>`, and asks the public users endpoint for that bot's id: no token, nothing
  that depends on who runs it. A laptop with an org-admin `gh` and a CI runner with no
  credentials reach the same verdict on the same tree.
- **`limen fix` discovers.** The human, mutating step may use privilege: with an org-admin
  `gh` it reads the App back from the org (the `UPDATE_AQUA_CHECKSUM_APP_ID` variable names
  it, the installation list gives its slug, so an App registered under any name is found);
  without one it falls back to the convention. It adds the address as the first element of
  the array, and the truth lives in the tree from there.

`check` fails when the convention-named App exists and is missing from the file. Anything
unresolvable — no remote, no network, no App under the convention name — passes without
enforcing. The corollary: an App registered under a non-default name is invisible to `check`,
and only `fix`, run with the org token, puts it in the file. `bootstrap` runs the discovering
step right after registering the App, so a fresh repository is complete from its first commit.

### The `gotools` rule — `tools/go.mod` tool directives

Every repository declares the Go-built tools the shared recipes run as `tool` directives in
`tools/go.mod`, a module of its own, so each is compiled by the repository's pinned toolchain
without the tools' dependency graph entering the project's `go.mod` and every consumer's.
Three are required everywhere — `git-validation`, `godolint`, `dot` — and a repository whose
root carries a `go.mod` adds the Go-source analyzers `deadcode`, `govulncheck`,
`go-licenses`. A `tool` directive in the project's own `go.mod` fails the rule. The reasoning
is in [tooling](./tooling.md#go-built-tools-are-gomod-tools). `limen fix` creates
`tools/go.mod` (module path `<module>/tools` and the root's `go` directive in a Go
repository; `tools` and the aqua-pinned `go` elsewhere), adds a missing directive with `go
-C tools get -tool <pkg>@<version>` followed by `go -C tools mod tidy`, and strips any
directive out of the root `go.mod`, tidying it; when the pinned `go` or the network is
unavailable, the rule ends as an advisory carrying the exact command. The version is the one
limen's own `tools/go.mod` (or `tools/<name>/go.mod`, for an isolated tool) requires in the
running release, never `@latest`, which would make what a repository receives depend on the
day `fix` ran; Renovate bumps it from there. A release carries those `go.mod` files because
its build copies them in first; a development build carries none and refuses to seed.

### The `baretools` rule — no Go-built tool run from `PATH`

The Go-built tools are run as `build/tools/<name>`, where the recipes build them; nothing
puts one on `PATH`. A bare `golangci-lint`, `godolint`, `govulncheck` (any of them) in
command position — opening a line, after a shell separator, after `if`, `exec`, `time` or
another word that takes a command, or as a `run:` step — in the project's own `.justfile`,
under `hack/` or `scripts/`, or in a workflow or composite action under `.github/` runs
whatever `PATH` holds: on a laptop, a stale aqua shim another project left in the shared bin
directory, which aqua resolves against this repository's manifest and refuses; on a clean
machine, nothing. The rule fails every such line, naming it; `limen fix` reports the same as
an advisory, since the line is the project's to rewrite: `just do lint go` or `just do fix
go`, which build the tool, or `build/tools/<name>` after one of them. The canonical `.limen/`
recipes are not scanned: they are content-pinned.

## Link checking — `.limen/lychee.toml`

Links die silently. `just do security links` checks every link in the repository with
[lychee](https://github.com/lycheeverse/lychee), and `.limen/lychee.toml` is its canonical
configuration, **content-pinned** like the shared modules, so the checker behaves
identically everywhere. The baseline carries only exclusions that apply to every repository,
each documented in the file; the source of truth is this repository's own
[`.limen/lychee.toml`](../.limen/lychee.toml), embedded as `rules.CanonicalLychee`. The rule
is unconditional: every repository has a README, so every repository has links. The lane is
the security lane's, not lint's: a link's verdict is the web's and moves without a push, the
shape the [security lane](./github.md#mainline-doctrine-pull-requests-always) exists for, so
a dead or blipping link reddens `security` on one leg instead of every verify leg and `gate`.

**Per-project exclusions go in a root `.lint-links.toml`.** The recipe passes both files to
lychee, which merges them (the exclude lists concatenate), so a project extends the baseline
without touching it; the root file is the project's own, neither checked nor overwritten.
It keeps its `.lint-links.toml` name from the lane's lint days, in the tool's own format; its
former name, `.lychee.toml`, is reported as a stray, since nothing reads it. Both configs
are passed explicitly: any `--config` disables lychee's discovery of `./lychee.toml`.

**A link resolves at lint time, not at release time.** The checker fetches every URL in the
tree as it is, so a link to something the change itself is preparing is a 404 until that
thing exists: a changelog section's `compare/v1.0.1...v1.0.2` written before the tag failed
every verify leg of https://github.com/forkcloser/xz/pull/97. A page written before the tag
links only what exists; the tag's own release page carries the comparison.

## Why these

- **Git repository**: the floor everything else stands on; without it there is no history,
  no upstream, nothing for the other rules to describe.
- **LICENSE**: without it the default is "no rights granted", a legal trap for collaborators
  and a deliberate choice only when intended. We force the choice.
- **.editorconfig**: the one formatting baseline every editor and agent understands with no
  toolchain, so cross-tool contributions do not churn whitespace.
- **.gitignore**: keeps build output, secrets and editor droppings out of history.
- **README**: the entry point; a repo with no README is undocumented by definition.
- **.justfile**: a discoverable, uniform set of commands; `just info` makes any checkout
  self-describing.
- **.limen/lychee.toml**: one shared checker configuration keeps `just do security links`
  identically strict in every repo.

## Enforcement

`limen check [path]` verifies all of the above against a repository and exits non-zero on
any failure. The same command in an editor, in pre-commit, in CI and in an agent's workflow.
See [`../cmd/limen/`](../cmd/limen).

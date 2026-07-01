# Mandatory files

Every Farcloser repository — without exception — must be a git repository and carry a small
set of files that make it legible, legally clear, and consistent to work in. These are the
lowest bar a repo can clear, and the first rule `limen` enforces.

| Requirement | What it means |
|-------------|---------------|
| **Git repository** | The project root is a git repository — a `.git` directory (normal clone) or a `.git` file (worktree/submodule) is present. |
| `LICENSE` | Present, and its content must be one of the **allowed licenses** below. |
| `.editorconfig` | Present, and **content-pinned**: equals the [canonical baseline](#canonical-editorconfig) byte for byte — no extra sections, no edited values. |
| `.gitignore` | Present, and covers the shared [gitignore baseline](#required-gitignore-patterns). |
| `README` | Present, as `README.md`. |
| `Justfile` | Present and canonical, with the shared [`.just/` modules](#justfile). |
| `.just/lychee.toml` | Present and canonical — the shared [link-checker configuration](#link-checking--justlycheetoml). |

`limen` resolves common spelling/extension variants (`LICENSE`, `LICENSE.md`, `LICENSE.txt`,
`COPYING`; `README`, `README.md`, `README.txt`) so a repo is not failed on a technicality,
but **`README.md` and a bare `LICENSE` are the canonical names** and what new repos should
use.

Every repository must *also* pin its build/CI tooling through aqua — equally mandatory, but
substantial enough to have its own chapter. See [project tooling](./tooling.md).

You do not hand-create these files: `limen bootstrap <path>` scaffolds a new repository with
all of them, and `limen fix` brings an existing repository up to the baseline — writing what
is missing, resetting the content-pinned files (`.editorconfig`, `Justfile`, and the `.just/*`
files), and merging the baseline into the subset files (`.gitignore`, `aqua.yaml`) without
discarding a repo's own additions. Anything it cannot fix safely — a disallowed `LICENSE`, a
manifest it cannot parse — is reported for a human to resolve.

## Allowed licenses

A repository's `LICENSE` must be identifiable as exactly one of the licenses below.
We split them by what the repository *contains*: **software** (code) versus **content**
(documentation, writing, images).

`limen` enforces only that the `LICENSE` is one of these recognized identifiers. *Which*
one to choose is engineering judgment, captured as the guidance in the "When to use which"
notes — a machine cannot tell a small library from a large platform, so that decision stays
with us.

### Software

| Identifier | When to use | How it is recognized |
|------------|-------------|----------------------|
| `MIT` | **Small libraries** and utilities. | MIT grant (*"Permission is hereby granted, free of charge…"*) plus the *"AS IS"* disclaimer. |
| `Apache-2.0` | **Larger projects and anything enterprise-facing.** | Declares *"Apache License"* and *"Version 2.0"*. |
| `AGPL-3.0` | **SaaS / network services**, when we want the copyleft to reach hosted use. | Declares *"Affero General Public License"* and *"Version 3"*. |
| `Closed-source` | **Proprietary** — anything not released publicly. | Explicit reservation of rights, no open grant. See the canonical text below. |

**Rationale**

- **MIT for small libraries.** A throwaway-cheap, permissive license keeps friction at zero
  for code whose value is in being widely embedded. The patent and contribution machinery of
  heavier licenses buys nothing for a few hundred lines, and MIT is the license downstream
  users expect from a small dependency — maximum adoption, minimum legal surface.
- **Apache-2.0 for larger projects and enterprise.** Once a project is substantial or likely
  to be adopted by companies, the **express patent grant** (and its retaliation clause)
  matters: it protects users from patent claims by contributors, which is exactly the
  assurance enterprise legal teams look for before depending on us. The explicit `NOTICE`
  and contribution terms also scale better across many contributors than MIT's single
  paragraph.
- **AGPL-3.0 for SaaS, as an option.** Plain GPL's copyleft is defeated by the "we only run
  it on our servers, we never distribute it" loophole. AGPL closes it: providing the software
  *over a network* triggers the obligation to share source. We reach for it when we want a
  hosted competitor to have to give back what they build on our service — and we treat it as
  a deliberate option, because that same reciprocity can deter commercial adopters.
- **Closed-source for proprietary.** When the code is a private advantage or simply not meant
  for release, we say so explicitly. The default in the absence of a license is already "no
  rights granted," but an explicit notice removes all ambiguity for collaborators and tooling.

### Content

| Identifier | When to use | How it is recognized |
|------------|-------------|----------------------|
| `CC-BY-SA-4.0` | **Documentation.** | *"Creative Commons Attribution-ShareAlike"* + *"4.0"*, or the `by-sa/4.0` license URL. |
| `CC-BY-ND-4.0` | **Writings** (essays, posts, opinion). | *"Creative Commons Attribution-NoDerivatives"* + *"4.0"*, or the `by-nd/4.0` license URL. |
| `Closed-source` (All rights reserved) | **Photography** and other images. | All-rights-reserved notice. `limen` reports this as `Closed-source`. |

**Rationale**

- **CC BY-SA 4.0 for documentation.** Docs are meant to be copied, adapted, and kept current
  by anyone — attribution plus **share-alike** lets the community improve them while ensuring
  improvements flow back under the same terms, so the documentation commons can't be enclosed.
- **CC BY-ND 4.0 for writings.** For opinion and authored prose, integrity matters more than
  remixing: **NoDerivatives** lets people share the piece freely and credit us, but not
  publish altered versions that still carry our name. Attribution without distortion.
- **All rights reserved for photography.** Images are rarely "improved" by remixing and their
  value is tied to controlled use and licensing, so we reserve all rights by default and grant
  use case by case. Legally this is the same instrument as closed-source software, which is
  why `limen` classifies it as `Closed-source`.

### Anything else is a failure

GPL, LGPL, BSD, MPL, an unrecognized or hand-edited license, a CC variant we don't list
(e.g. `BY-NC`), or any `LICENSE` file `limen` cannot classify is a **failure**. If we
genuinely need another license, it is added to this list and to `limen`'s policy first; the
tool is the enforcement, the book is the decision.

### Canonical closed-source notice

So that `Closed-source` is detected deterministically, proprietary repositories should use
this `LICENSE` text verbatim (adjust the year and holder):

```
Copyright (c) 2026 Farcloser. All rights reserved.

This software and its source code are proprietary and confidential. No license,
express or implied, is granted to any person to use, copy, modify, distribute, or
create derivative works of this software, in whole or in part, without the prior
written permission of Farcloser.
```

## Canonical .editorconfig

The baseline is defined once and lives in exactly one place: this repository's own
[`.editorconfig`](../.editorconfig), which `limen` embeds at build time and enforces. **That
file is the source of truth** — read it for the exact rules. The book carries the reasoning,
not a copy of the content.

The `.editorconfig` is **content-pinned**: `limen` requires it to equal the canonical **byte for
byte** — no extra sections, no edited values. The baseline is **comprehensive** — it already
covers every language we work in, and a section only ever matches files that are present, so the
full baseline is harmless even for languages a repo does not use. Because it is exhaustive, a
repo never needs its own additions; `limen fix` overwrites a drifted `.editorconfig` back to the
canonical. (This is the same exact-match rule as the `Justfile` and the `.just/*` files — unlike
`.gitignore`, which allows extra patterns.)

The reasoning behind it: **each file type uses the indentation its own tooling treats as
canonical**, so the config never fights the formatter — tabs where `gofmt` and `make` require
them, four spaces where `just --fmt` and `rustfmt` produce them, two spaces for the data and
web-language families that `jq`, `yamllint`, Prettier, and Biome emit. A two-space catch-all
covers anything without tooling of its own, and each family is pinned explicitly so a format
never drifts if that fallback changes.

## Required .gitignore patterns

The baseline is defined once and lives in exactly one place: this repository's own
[`.gitignore`](../.gitignore), which `limen` embeds and enforces. **That file is the source of
truth** — read it for the exact patterns. Like the editorconfig baseline it is
**comprehensive**, spanning every language and tool we work in; a pattern only ever matches
files that exist, so carrying patterns for unused languages is harmless.

`limen` checks that every pattern in the baseline appears in a repository's `.gitignore`,
normalizing spelling first so anchored or directory forms count — `.idea`, `.idea/`, `/.idea`,
and `**/.idea` are all equivalent. A repo may ignore more — its own binary name, a `dist/`, its
test artifacts — as ordinary extras; it may not omit a baseline pattern.

## Justfile

Every repository drives its tasks through [`just`](https://github.com/casey/just) — the one
task runner we standardize on, so "how do I build/test/run this?" has the same answer
(`just …`) in every repo, for humans and agents alike. (This section covers the *files* and
their pinning; what the shared recipes do and the conventions they obey is
[its own chapter](./recipes.md).) The task setup lives in a root `Justfile`
plus a `.just/` directory of modules, split so the shared parts stay identical everywhere while
each project keeps room of its own:

| File | Role | Checked? |
|------|------|----------|
| `Justfile` | The standard shell — **identical in every repo**. Carries the orientation recipes (`default`, `info`), a `mod` line for each shared module, the flat imports (`.just/release.just`), and `import? 'project.just'`. | Content-pinned: must match the canonical exactly. |
| `.just/*.just` (shared modules) | The **shared recipe baseline**, currently `build` (compile — release, debug, race, static variants), `tools` (aqua management), `lint` (report style/quality problems — its `aqua` recipe regenerates `aqua-checksums.json` to detect drift, aqua having no read-only validator, and its `limen` recipe runs `limen check`), `test` (run the suite — unit, race, bench, cover, profile), and `fix` (apply fixes in place, where the tool supports it — including `limen fix`). The same in every repo, each loaded as a `mod`. | Content-pinned: **every `*.just` file under `.just/`** must match the canonical exactly. |
| `project.just` | This project's **own** recipes (its build/test/run), at the repo root for visibility. `bootstrap`/`fix` seed it with a placeholder comment when absent, but never overwrite it. | Not checked — projects own it. |

The `.just/` directory also parks a few non-recipe config files to keep the repo root uncluttered
(`.just/.shellcheckrc`, `.just/.yamlfmt`, `.just/aqua-registry.yaml`, `.just/lychee.toml`). These
are *not* just modules — only `*.just` files are — and they are governed by their own rules
([per-language](./per-language.md), [tooling](./tooling.md),
[link checking](#link-checking--justlycheetoml)), not the Justfile content-pin.

Orientation and project recipes are flat — `just info`, a project's own `just run` — so the
universal "where am I? / do the project thing" commands are unprefixed in every repo (a
project recipe must not reuse a shared module's name — `build`, `tools`, `lint`, `test`,
`fix` — or the two collide). Shared recipe sets are grouped under modules —
`just tools add …`, `just lint go`, `just lint links`, `just fix yaml` (see
[project tooling](./tooling.md) for the `tools` module). Invoking a module bare runs its
**`default` recipe: the curated set that applies safely to every repository** — `just lint`
runs `limen` + `just` + `aqua` + `links` + `yaml` + `shell` + `dockerfile` + `commits`, and
`just fix` runs `limen` + `just` + `yaml`. The rule for what a default may carry: **a recipe
belongs in a default only when it passes vacuously where it does not apply** — `shell` and
`dockerfile` discover their targets and no-op on a repo that has none, while recipes that
need a language toolchain to even run (`go`, `rust`) stay out of every default and are named
explicitly: a Go repo runs `just lint go`, a Rust repo `just lint rust`, and so on. Bare
`just test` refuses outright — every test is language-bound — so each project declares its
aggregates in `project.just` (`lint-all`, `test-all`), which is also what CI runs. Note that `just lint aqua`
(and therefore the bare `just lint`) needs the network, and on drift regenerates
`aqua-checksums.json` in place — aqua has no read-only validator, so the corrected file is
left for review and commit. **All customization goes in `project.just`; the `Justfile` and
every shared module are locked.**

The `Justfile` carries the mandatory **`info` recipe**, which prints meaningful facts so anyone
— or any agent — landing in a checkout can orient with one command: the **project name**, the
**git upstream** (if any), the **closest semver** tag, the current **commit**, and the **date
of the last commit**.

`limen` content-pins the `Justfile` and **every `*.just` file under `.just/`** against the
canonical baseline embedded in the binary — the source of truth is this repository's own
[`Justfile`](../Justfile) and [`.just/`](../.just) modules. So adding a new shared module is just
a new `.just/NAME.just` plus its `mod` line; it becomes part of the enforced baseline automatically.
A repo whose `Justfile` or any shared module differs from the baseline, or omits one, fails; what
`project.just` contains is the project's business.

## Link checking — `.just/lychee.toml`

Documentation rots at its edges: links die silently. `just lint links` checks every link in
the repository with [lychee](https://github.com/lycheeverse/lychee), and `.just/lychee.toml`
is its canonical configuration — **content-pinned** like the shared just modules, so the
checker behaves identically everywhere. The baseline carries only exclusions that apply to
every repository (hosts that appear in verbatim license texts but cannot be checked
reliably), each documented in the file itself. The source of truth is this repository's own
[`.just/lychee.toml`](../.just/lychee.toml), embedded into `limen` and exposed as
`rules.CanonicalLychee`. The rule is unconditional: every repository carries a README, so
every repository has links worth checking.

**Per-project exclusions go in a root `lychee.toml`.** The `lint links` recipe passes both
files to lychee, which merges them — the exclude lists concatenate — so a project extends the
baseline without touching it. Like `project.just`, the root file is the project's own: `limen`
neither checks nor overwrites it. (Both configs must be passed explicitly; passing any
`--config` disables lychee's automatic discovery of `./lychee.toml`, which is why the recipe
names both.)

## Why these

- **Git repository** — version control is the floor everything else stands on; a project
  that is not a git repo has no history, no upstream, and nothing for the other rules (or
  `info`) to describe.
- **LICENSE** — without it, the default is "no rights granted," which is a legal trap for
  collaborators and an explicit, deliberate choice when intended. We force the choice.
- **.editorconfig** — the one formatting baseline every editor and agent understands with no
  toolchain, so cross-tool contributions don't churn whitespace.
- **.gitignore** — keeps build output, secrets, and editor droppings out of history.
- **README** — the entry point. A repo with no README is undocumented by definition.
- **Justfile** — a discoverable, uniform set of commands; `just info` is the universal "where
  am I?" that makes any checkout self-describing.
- **.just/lychee.toml** — dead links are documentation rot; one shared checker configuration
  keeps `just lint links` meaningful (and identically strict) in every repo.

## Enforcement

`limen check [path]` verifies all of the above against a repository and exits non-zero on
any failure. It is the same command in an editor, in pre-commit, in CI, and in an agent's
workflow. See [`../cmd/limen/`](../cmd/limen).

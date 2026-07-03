# Project tooling

Every Farcloser repository pins its build/CI tooling — `just`, `shellcheck`, `go`,
`golangci-lint`, `go-licenses`, and the handful of other Go "sanity" binaries — through
[aqua](https://aquaproj.github.io/). This is mandatory, with no exception, the same way the
[mandatory files](./mandatory-files.md) are: a repository declares its tools in a committed
`aqua.yaml`, and every human and agent who touches it gets the exact same verified versions.

## The requirements

Tooling is not a place for "latest." The tools a repository builds and tests with must be:

- **Pinned exactly.** Any version change can break the build — `golangci-lint` and `go`
  especially. A repository must resolve to one known set of versions, not whatever a machine
  happens to have installed.
- **Per-project.** Different projects need different tool versions, with no global conflict
  between them.
- **Secure.** Verified on install — not "download whatever the source serves."
- **Low-maintenance.** Updates should be a reviewed, automated PR, not hand-edited install
  scripts.

The approach this replaces — a `Makefile` shelling out to `go install` for Go tools and
`brew`/`apt` for the rest — was unpinned, split across two mechanisms, and heavy on manual
maintenance. We wanted one formal, declarative, verifiable tool in its place.

## Why aqua over Nix

Nix and aqua both pin versions and both are cross-platform; the choice came down to fit
against the requirements above.

| Axis | Nix | aqua |
|---|---|---|
| Exact **per-tool** version pin | Clunky — one `nixpkgs` commit pins *everything* together; independent versions require separate inputs | **Native** — `owner/repo@version` |
| Per-project | Yes (flake) | Yes (`aqua.yaml`) |
| Unifies Go + non-Go tools | Yes, but via a heavy apparatus | Yes, one manifest |
| Security model | Hermetic, source-hash-pinned builds (strongest for *source*) | Checksum + cosign/SLSA for binaries; GOSUMDB for `go_install` tools |
| Update automation | Manual / multi-input juggling | **Renovate-native**, per-tool PRs |
| Upstream-release latency | Packaging layer adds delay | Near-zero (vendor release consumed directly) |
| Ops / learning overhead | High (Nix language, `/nix/store`, daemon, GC) | Low (single binary + YAML) |

Nix's genuine advantage is hermetic, source-hash-pinned builds — a stronger trust root for
source-built tools. But that hardens an axis we did **not** prioritize (we need *version
pinning*, not rebuild-from-source), and it does so at the cost of the per-tool pinning
ergonomics that are our **top** requirement. Pinning one tool to an arbitrary version in Nix
means adding a second `nixpkgs` input frozen at the right commit; in aqua it is a single
`@version` suffix on the package.

## Decision: aqua

aqua matches every paramount requirement directly:

- **Exact per-tool pinning** is native: `golangci/golangci-lint@<version>`.
- **Per-project** via a committed `aqua.yaml`; projects never collide.
- **Security**: binary-release tools are checksum-verified and, where the vendor publishes
  them, cosign/SLSA/attestation-verified. Go-only (`go_install`) tools fall back to
  **GOSUMDB** — the *same* trust root the old `Makefile` already relied on, now pinned and
  declarative. A net improvement over the status quo, never a regression.
- **Maintenance**: Renovate opens per-tool version-bump PRs; a checksum-refresh workflow
  keeps `aqua-checksums.json` in sync automatically.

**The one tradeoff we accept:** `go_install` tools are not aqua-checksum-verified (GOSUMDB
instead of a pinned binary checksum). If hermetic rebuild verification of those *specific* Go
tools ever becomes the paramount axis, revisit Nix for that subset.

---

## Machine setup: limen-install (one-time, per machine)

Works the same on **macOS** and **Linux**. Machine setup is one bootstrap — the
[`limen-install`](https://github.com/farcloser/limen-install) script — which sets up the
whole global toolchain:

1. Installs **aqua** via the pinned, checksum-verified official installer, into aqua's own
   root (`${AQUA_ROOT_DIR:-~/.local/share/aquaproj-aqua}/bin`). That directory is the one
   every repo's hermetic `PATH` (see the canonical [`Justfile`](../Justfile)) points at, so
   `aqua` — and every tool it proxies — resolves inside recipes *by construction*.
   **Never `brew install aqua`**: Homebrew's bin directory is deliberately not on the
   hermetic `PATH`, so a brew-installed aqua works in your shell but breaks every recipe.
2. Adds that directory to your shell rc and exports `AQUA_GLOBAL_CONFIG`.
3. Writes the **global aqua config** (`~/.config/aqua/aqua.yaml`), which pins exactly one
   tool: `limen` itself.
4. Runs `aqua i -a` — after which **both `aqua` and `limen` are available globally**.

Run it either way:

```bash
# Homebrew (the formula ships only the bootstrap script; `brew upgrade limen` re-runs it):
brew install farcloser/brews/limen

# or directly:
git clone https://github.com/farcloser/limen-install && ./limen-install/limen-install
```

The script is idempotent — safe on a fresh machine and as an update; open a new shell
afterward if it says it changed your rc. Checksum enforcement and registry policy remain
configured **per project**, not globally — the global config exists only to carry the
scaffolder.

---

## Scaffolding a new project

Create these files at the repo root. This repository's own aqua files
([`aqua.yaml`](../aqua.yaml), [`.limen/aqua-registry.yaml`](../.limen/aqua-registry.yaml),
[`aqua-policy.yaml`](../aqua-policy.yaml)) are the canonical reference — we dogfood this rule.

```
repo/
├── aqua.yaml                              # the manifest: pinned tool versions
├── aqua-checksums.json                    # GENERATED — commit it
├── aqua-policy.yaml                       # authorizes the local registry
├── .limen/aqua-registry.yaml                     # local registry: go_install tools
├── renovate.json5                         # automated version bumps
└── .github/workflows/update-aqua-checksum.yaml   # refreshes checksums in Renovate PRs
```

What the `aqua.yaml` must carry — the manifest is **subset-pinned** (see
[Enforcement](#enforcement)):

- The **canonical `checksum:` section, byte for byte** — `enabled: true` and
  `require_checksum: true` mean a missing or mismatched checksum **fails** the install, and
  `supported_envs` is part of the pinned section: adding an environment (say `windows/amd64`)
  is a change to limen's canonical baseline, not a per-repo edit.
- The **canonical `registries:` section**: the standard registry plus the `local` registry for
  `go_install` tools (e.g. `go-licenses`). One field is the project's: the standard registry's
  `ref`, which Renovate bumps per repo — but it must always be an **exact pin** (a `vX.Y.Z`
  tag or a full commit SHA, never a branch).
- **At least the canonical packages**, matched by name — the *versions* are the project's
  (Renovate bumps them), and extra per-project packages are welcome. A package is never
  listed twice.

> **Content-pinned files.** `aqua-policy.yaml` and `.limen/aqua-registry.yaml` are **canonical
> everywhere** — `limen` requires them to match its embedded copies byte for byte (and `limen
> fix` overwrites drift). `aqua.yaml` is subset-pinned as above; only its package versions,
> extra packages, and the standard registry ref are project-owned. `aqua-checksums.json` is
> **generated, never hand-edited**: `limen fix` regenerates it (`aqua update-checksum`)
> whenever it changes the manifest or the file is missing. Consequence: the catalog of
> `go_install` tools is **shared** — to add one, it goes into limen's canonical registry, not
> a single repo's.

Bootstrap, from the repo root:

```bash
aqua policy allow aqua-policy.yaml   # explicit trust gate (one-time per machine)

aqua update-checksum      # generate aqua-checksums.json for the binary tools
aqua install --only-link  # link every pinned tool (each downloads lazily on first use)

git add aqua.yaml .limen/aqua-registry.yaml aqua-policy.yaml aqua-checksums.json \
        renovate.json5 .github/workflows/update-aqua-checksum.yaml
git commit --message "tooling: pin project CLIs via aqua"
```

After this, every tool resolves to its exact pinned version on first invocation.

> **go-licenses note:** because it is a `go_install` tool, pin it to an exact tag or a raw
> commit SHA in `aqua.yaml`. For the v2-alpha situation, a commit pin is the cleanest, fully
> reproducible escape — no `vendorHash` to maintain.

---

## Cloning an existing aqua project

```bash
git clone <repo-url>
cd <repo>

# Authorize the project's local registry (one-time per machine):
aqua policy allow aqua-policy.yaml

# Install the exact pinned versions, verified against the committed checksums:
aqua install --only-link

# Tools now resolve to the project's pinned versions:
just --version
go version
golangci-lint version
```

**Does it just work? Yes**, with two deliberate conditions:

1. The developer must have **aqua installed** (see above).
2. **`aqua policy allow` is required once per machine** because the project ships a
   non-standard (local) registry. aqua does not trust a custom registry until you explicitly
   authorize it — the same explicit-consent pattern we use everywhere. No environment
   variable is needed: aqua auto-discovers an allowed `aqua-policy.yaml` at the **git
   repository root**. (`AQUA_POLICY_CONFIG` exists for policies that have no git root to be
   discovered from — the machine-global one carrying `limen` is that case, and
   `limen-install` wires it into the shell rc.)

What *genuinely* just works: **byte-identical tool versions.** Because `aqua.yaml` and
`aqua-checksums.json` are committed, every developer (and CI) gets the same verified builds —
no "works on my machine," no version drift. Lazy install also means tools auto-install on
first invocation, so `aqua install --only-link` is optional warm-up rather than a hard prerequisite.

---

## Day-to-day changes — the `just do tools` recipes

The everyday operations — add, pin to a version, bump, remove — are wrapped as recipes in the
`tools` just module so nobody has to remember the exact aqua incantation (or forget the
checksum step). Each takes the **`owner/repo`** exactly as it appears in `aqua.yaml`, and each
leaves `aqua.yaml` **and** `aqua-checksums.json` updated together, ready to commit:

```bash
just do tools add    junegunn/fzf                    # add a tool at its latest version
just do tools set    golangci/golangci-lint <version>  # pin an existing tool to an exact version
just do tools update golangci/golangci-lint          # bump an existing tool to its latest version
just do tools remove junegunn/fzf                    # remove a tool entirely
```

Every recipe ends by refreshing the checksum (`aqua update-checksum`) and installing/verifying
(`aqua install --only-link`), so a green run means the new state is already checksum-verified. Commit both
files afterward:

```bash
git add aqua.yaml aqua-checksums.json
git commit --message "tooling: add fzf"
```

Two of these do work aqua's own CLI deliberately won't, which is why they exist as recipes
rather than raw aliases:

- **`tools set` edits the manifest in place.** `aqua generate -i owner/repo@version` would *append* a
  second entry for an already-present package (its merge is an unconditional list append), not
  update the existing one — so `tools set` rewrites the version on the existing line instead.
- **`tools remove` edits the manifest too.** `aqua remove` only uninstalls the binary; it does not
  touch `aqua.yaml` (and cannot remove `go_install` tools at all). The recipe removes the
  package's entry, then `aqua remove`s the binary, then `aqua update-checksum --prune`s the orphaned checksum.

These recipes are the preferred interface for any hand-made change. Renovate (below) still
owns the routine, unattended version bumps — the recipes are what you reach for when *you*
are the one adding, removing, pinning, or bumping a tool.

## Updating a tool

### Manual

The short path is `just do tools update owner/repo` (latest) or `just do tools set owner/repo <version>`
(exact), as above. Spelled out, that is:

```bash
# 1. Edit aqua.yaml — bump the version, e.g.
#      golangci/golangci-lint@<old>  ->  @<new>
# 2. Refresh the checksum for the new version:
aqua update-checksum
# 3. Install and verify:
aqua install --only-link
golangci-lint version
# 4. Commit BOTH files together:
git add aqua.yaml aqua-checksums.json
git commit --message "tooling: bump golangci-lint"
```

### Automated (Renovate — the intended workflow)

1. Renovate detects the new release and opens a **per-tool** PR bumping the version in
   `aqua.yaml` (e.g. "update golangci/golangci-lint to a newer version").
2. The `update-aqua-checksum` workflow regenerates `aqua-checksums.json` **in the same PR**.
3. You review the changelog and merge — or don't. Each tool is bumped independently.

> **Critical:** Renovate can update versions but **cannot** update `aqua-checksums.json` on
> its own. The checksum-refresh workflow is what keeps the two in sync. Without it, a version
> bump would merge a stale checksum and **every install would fail**. The workflow is
> load-bearing, not optional.

This is the formal, auditable, low-toil update process that replaces the old hand-maintained
`Makefile`: every tool change is a reviewed PR with verified checksums, pinned exactly, per
project.

## Enforcement

`limen check [path]` verifies the aqua rule alongside the [mandatory files](./mandatory-files.md).
It fails a repository that has no `aqua.yaml`; whose `checksum:` section differs from the
canonical baseline; whose `registries:` section differs (beyond the project-owned standard
`ref`, which must itself be an exact pin); that lacks any canonical package (by name); that
lists a package twice; or that is missing the committed `aqua-checksums.json`. A manifest
`limen` cannot confidently parse (flow-style sections and the like) also fails — what cannot
be verified does not pass.

`limen fix` remediates all of that: a missing manifest is seeded from limen's own (and only
then may the matching canonical `aqua-checksums.json` be seeded with it, so a fresh
`bootstrap` is compliant offline); an existing manifest is **merged** — the canonical
sections are reset (keeping a valid project `ref`), missing canonical packages are appended
by name without ever duplicating one the project already pins, and the project's own
packages and versions are untouched. Whenever the manifest changed or the checksums file is
missing, `fix` regenerates the checksums with the real tool — `aqua policy allow
aqua-policy.yaml` then `aqua update-checksum --prune` — rather than guessing; if aqua is
unavailable it says so and leaves the commands for you. Duplicate package entries are
reported, not resolved: only a human knows which version was meant.

Beyond that baseline, *which* extra tools and versions a project pins is engineering
judgment — the same division of labor as the license rule: the tool enforces the invariants,
the book carries the reasoning. The same command runs in pre-commit, in CI, and in an
agent's workflow. See [`../cmd/limen/`](../cmd/limen).

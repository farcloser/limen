# Project tooling

Every Farcloser repository pins its build and CI tooling — `just`, `shellcheck`, `go`, the
Go-built analyzers, and the rest — through [aqua](https://aquaproj.github.io/) and the
`tools/go.mod` directives. Mandatory, with no exception, like the
[mandatory files](./mandatory-files.md): a repository declares its tools in a committed
`.aqua/aqua.yaml`, and every human and agent who touches it gets the same verified versions.

## The requirements

Tooling is not a place for "latest." The tools a repository builds and tests with must be:

- **Pinned exactly.** Any version change can break the build — `golangci-lint` and `go`
  especially. A repository resolves to one known set of versions, not whatever a machine has.
- **Per-project.** Different projects need different versions, with no global conflict.
- **Secure.** Verified on install, not "download whatever the source serves."
- **Low-maintenance.** Updates are a reviewed, automated PR, not hand-edited install scripts.

The approach this replaced — a `Makefile` shelling out to `go install` for Go tools and
`brew`/`apt` for the rest — was unpinned, split across two mechanisms, and heavy on manual
maintenance.

## Why aqua over Nix

Both pin versions and both are cross-platform; the choice came down to fit against the
requirements:

| Axis | Nix | aqua |
|---|---|---|
| Exact **per-tool** version pin | Clunky: one `nixpkgs` commit pins *everything*; independent versions need separate inputs | **Native**: `owner/repo@version` |
| Per-project | Yes (flake) | Yes (`.aqua/aqua.yaml`) |
| Unifies Go + non-Go tools | Yes, through a heavy apparatus | Yes, one manifest |
| Security model | Hermetic, source-hash-pinned builds (strongest for *source*) | Checksum + cosign/SLSA for binaries; GOSUMDB for the `go.mod` tools |
| Update automation | Manual, multi-input juggling | **Renovate-native**, per-tool PRs |
| Upstream-release latency | Packaging layer adds delay | Near zero (vendor release consumed directly) |
| Ops / learning overhead | High (Nix language, `/nix/store`, daemon, GC) | Low (single binary + YAML) |

Nix's genuine advantage, hermetic source-hash-pinned builds, hardens an axis we did not
prioritize — we need version pinning, not rebuild-from-source — at the cost of the per-tool
pinning ergonomics that are the top requirement: pinning one tool to an arbitrary version in
Nix means a second `nixpkgs` input frozen at the right commit; in aqua it is an `@version`
suffix.

## Decision: aqua

aqua matches every requirement directly: exact per-tool pinning is native; per-project
through a committed `.aqua/aqua.yaml`; binary-release tools are checksum-verified and, where
the vendor publishes them, cosign/SLSA/attestation-verified, and the Go-built tools fall back
to **GOSUMDB**, the trust root the old `Makefile` already relied on, now pinned and
declarative; Renovate opens per-tool bump PRs and a checksum-refresh workflow keeps
`.aqua/aqua-checksums.json` in sync. The one tradeoff accepted: Go-only tools are not
aqua-checksum-verified (GOSUMDB instead of a pinned binary checksum). If hermetic rebuild
verification of those tools ever becomes the paramount axis, revisit Nix for that subset.

<a id="go-source-analyzers-are-gomod-tools"></a>
### Go-built tools are `go.mod` tools

aqua's `go_install` package type is `go install <path>@<version>` with whatever `go` is on
`PATH`, cached under the tool's version alone: compiled once per tool version by whichever
project's pinned `go` ran it first, then shared by every project pinning that version, on
that machine, for good. The pin names the tool; the Go that built it is decided by run order.
Nothing in the manifest describes the binary that runs, the opposite of what a pin is for.
So **no Go-built tool is an aqua package**.

**The line is who compiles it, and the pinned toolchain should, wherever `go get` can.** A
prebuilt Go binary from an upstream release carries that upstream's standard library, built
with whatever Go they used that day; a fix in `net/http` or `crypto/tls` reaches it only
when they rebuild, and the `binaries` security lane found most of the pinned ones behind. A
tool built here by the pinned `go` from a `tool` directive carries the toolchain we chose,
its source fixed by module hash and verified against the Go checksum database. So a pure-Go
tool is a tool directive: the analyzers, whose correctness also depends on the compiler
matching the toolchain; the tools whose upstream ships no binary (`git-validation`,
`godolint`, `dot`); and `golangci-lint`, which embeds Go's type checker and, built with the
toolchain it analyzes, can no longer skew from it. What stays in `.aqua/aqua.yaml` is what
nothing here compiles: tools in other languages; limen's own binaries, `limen` and
`limen-lint-go`, one release whose checksums and signature every repository pins (built by
the release workflow with the pinned toolchain, and neither loads Go source); and the Go
tools a write job must run without a toolchain, `gh` and `cosign`, until in-process
verification retires them.

**One module per tool whose graph must stay upstream's.** The shared `tools/go.mod` resolves
every directive's dependencies together, and minimal version selection lifts a dependency two
tools share to the newest either wants — a binary its upstream never tested. golangci-lint
bundles over a hundred analyzers and says so itself: source builds are unsupported, the `go
tool` form tolerable only from a dedicated module. So it lives in `tools/golangci-lint/go.mod`,
alone, resolving to exactly upstream's graph; `_go-tool` in `lib.just` builds from the
isolated module when one exists and from `tools/go.mod` otherwise, and the `gotools` rule
seeds the module in every Go repository. NilAway gets the same in `tools/nilaway/go.mod`, for
a different reason: no tagged release, a pin by pseudo-version, breaking changes announced
upstream. The one hand check at each bump: an upstream `replace` directive, which does not
apply across modules and would make a source build a different program from the release —
none today.

For a tool that **loads Go source** — `deadcode`, `govulncheck`, `go-licenses`, anything on
`go/packages` — the gap is a correctness failure, not only a provenance one: such a tool
embeds the source loader of the Go that compiled *it*, and compiled by go1.N it cannot read a
module that declares go1.N+1. CI never sees it (every runner builds fresh with its own pin);
it surfaces on a laptop that works on two repos with different Go pins, as an analyzer
refusing sources it should read. Met in the wild: a *prebuilt* golangci-lint with gocritic on
`enable-all` exits with no findings at all under the hermetic `GOROOT=''`, because ruleguard
loads its embedded rules through the source loader of the Go that built the binary, whose
root does not exist on this machine; the source-built one carries the pinned toolchain's
root and passes.

The fix is Go's own: every Go-built tool is a `tool` directive, built from a pinned
requirement by the module's own pinned toolchain, so the skew is impossible by construction.
They live in **`tools/go.mod`, a module of their own** (`<module>/tools` in a Go repository,
`tools` elsewhere), not in the project's `go.mod`: a `tool` directive drags the tool's whole
dependency graph into the declaring module as `// indirect` requirements, and everything a
library's `go.mod` requires is inherited by every consumer's module graph, `go.sum` and
dependency scanners. A zero-dependency library must stay one. The nested module keeps the pin
next to the code, under the same toolchain and GOSUMDB, and Renovate's `gomod` manager finds
nested `go.mod` files on its own (the shared preset re-enables these modules, which Go lists
as `// indirect`). The recipes build each tool natively once (`go -C tools build`, into
`build/tools/`) and run that binary — per platform for the analyzers — because `go tool`
honours `GOOS`/`GOARCH` and would cross-compile the tool. Nothing puts one on `PATH`: a bare
`golangci-lint` in a `.justfile`, a script or a workflow runs whatever `PATH` holds — on a
laptop a stale aqua shim another project left behind, on a clean machine nothing — and the
`baretools` rule fails it.

Every repository carries the module, not only Go ones: `git-validation` (commit hygiene),
`godolint` (Dockerfiles) and `dot` (profile graphs; `forkcloser/dot`, graphviz compiled to
WASM) run everywhere, and every repository pins `golang/go` in its own aqua manifest to build
them. A Go repository adds the three source analyzers. The doctrine is enforced from both
sides: the `gotools` rule requires `tools/go.mod` with the directives and rejects a `tool`
directive in the project's `go.mod` (`limen fix` creates the module, adds the directives,
moves any directive out of the root), and the `aqua` rule retires the old `go_install` pins
(`limen fix` removes them). The local registry carries no `go_install` entry, and never will
again.

Adding, pinning, bumping or removing one goes through the same recipes as an aqua package,
never by hand: `just do tools add|set|remove` take the package path as it stands in
`tools/go.mod`, `just do tools update` the command it builds as (or nothing, to move every
tool, aqua packages included), and the recipes tell a Go package from an aqua slug by the
first path element — a module root is a host and carries a dot, a GitHub owner cannot.
Underneath they run `go -C tools get -tool <pkg>@<version>` (`@latest`, `@none`) and `go -C
tools mod tidy`, then build the tool into `build/tools/`, the Go counterpart of the full
`aqua install` the aqua recipes end with: a bad pin fails in the recipe, not at first use.

## Tools without upstream binaries: the sourcing ladder

aqua consumes prebuilt artifacts; it does not build C (`go_install` and `cargo` are its only
compiling package types, and `go_install` is off the table). When a tool's upstream publishes
no usable release binaries, work down this ladder; each rung is a weaker trust position than
the one above, and the rung is chosen deliberately, not by whatever the registry carries:

1. **Upstream binaries on GitHub releases** → the standard registry, done. Beware
   *lookalikes*: a registry hit is not an endorsement. The registry's only curl is a
   solo-maintained build org whose binaries ship unsigned, under an organization name that
   suggests a project it is not affiliated with — for a tool that bundles its own TLS stack,
   below our bar.
2. **A signed, maintainer-operated channel outside GitHub** → an `http`-type entry in the
   local registry, with the signature verified at every version bump (aqua pins the checksum
   after first fetch; the signature check is what makes the *first* fetch trustworthy).
   Renovate cannot watch arbitrary HTTP indexes well, so these entries bump by hand.
3. **Nothing trustworthy exists** → a first-party `build-<tool>` repository: fork and audit
   the best available build scripts once, run them in our CI, attest the artifacts
   (`actions/attest-build-provenance`; aqua verifies GitHub Artifact Attestations natively),
   publish through the local registry like limen itself. The `build-` prefix marks the "we
   build someone else's project" class; a rebuild of the same upstream version is a fourth
   tag segment ([forks](./forks.md)). Building is the last resort, a standing maintenance
   commitment (toolchain rot, upstream CVE cadence), so a repo on this rung has Renovate
   watching the upstream.

### Case study: curl

The baseline needs a curl with dependable TLS 1.3 on every platform, and no single
trustworthy upstream channel covers them all, so curl is packaged first-party by
`forkcloser/curl`, one package for every platform (rung 3 carrying a rung-2 import):

- **Windows**: rung 2 exists. The curl project itself operates
  [curl-for-win](https://github.com/curl/curl-for-win): reproducible static builds, signed
  four ways (sigstore bundle, minisign, SSH, PGP), distributed at
  `https://curl.se/windows/dl-<version>_<rev>/curl-<version>_<rev>-{win64,win64a}-mingw.tar.xz`
  (`win64` = amd64, `win64a` = arm64; a `.txt` sidecar carries the SHA256; the archive root
  is `curl-<version>_<rev>-<cpu>-mingw/` with `bin/curl.exe`). `forkcloser/curl` imports
  these artifacts, sigstore-verified in CI, which is stronger than the hand-bump ritual an
  `http`-type entry would need: the verification is a workflow step, and Renovate watches
  the curl.se index.
- **Linux / macOS**: no rung-2 channel — curl-for-win *builds* these but does not distribute
  them (its deploy lane is windows-only; verified, not assumed). Hence rung 3:
  `forkcloser/curl` runs curl-for-win's own build scripts at an audited commit pin for the
  linux (static musl) and macOS (arm64) legs, the same lineage as the imported windows
  binaries, every archive GitHub-attested.

---

## Signatures: verify whenever published

Checksum pinning is the floor, never the ceiling. A checksum pins *the bytes we saw first*; a
signature binds those bytes to the **publisher's identity**, which is what survives a
compromised release: an attacker who can swap an asset can swap the checksum file next to
it, but cannot sign as the publisher. So, unconditionally: **whenever an upstream publishes
signature material — a cosign signature or Sigstore bundle, SLSA provenance, GitHub artifact
attestations, an Authenticode-signed binary — the consuming side verifies it.** "Present but
unchecked" fails review. Where the verification lives depends on who downloads:

- **aqua-managed tools**: in the *registry entry*; aqua runs cosign / slsa-verifier /
  attestation checks itself when the entry carries the stanza. When adding a tool, check what
  upstream publishes (release assets for `.sig` / `.sigstore.json` / `.intoto.jsonl`; the
  attestations API for the pinned digests). If the standard registry entry lacks a stanza for
  material that exists, contribute it upstream, or carry the entry in the local registry
  until it lands.
- **Our own releases**: sign *and* verify. Releases sign `checksums.txt` keyless from CI (the
  goreleaser cosign lane), and every consumption point verifies the bundle against the
  release workflow's identity at the exact tag: the local registry's `limen` entry carries
  the stanza, so `aqua` enforces it in every repo and on every machine bootstrap.
- **Hand-rolled downloads** (bootstrap scripts and installers that run before aqua exists):
  the script verifies inline — pinned sha256 at minimum, plus whatever the publisher signs
  (`limen-install.ps1` verifies the Git for Windows installer's Authenticode signature and
  pins the signer's identity, on top of the hash).

One blind spot to police deliberately: for standard-registry tools, *whether* a signature
gets verified is the registry entry's decision, and nothing on our side fails when an
upstream **starts** publishing signatures. That gap is closed by sweep: when touching a
tool's pin, look at what its release now publishes; periodically, check the pinned digests
against the attestations API.

## Machine setup: limen-install (one-time, per machine)

Works the same on **macOS** and **Linux**. Machine setup is one bootstrap, the
[`limen-install`](https://github.com/farcloser/limen-install) script:

1. Installs **aqua** via the pinned, checksum-verified official installer, into aqua's own
   root (`${AQUA_ROOT_DIR:-~/.local/share/aquaproj-aqua}/bin`). That directory is the one
   every repo's hermetic `PATH` points at, so `aqua` — and every tool it proxies — resolves
   inside recipes by construction. **Never `brew install aqua`**: Homebrew's bin directory is
   not on the hermetic `PATH`, so a brew-installed aqua works in your shell and breaks every
   recipe.
2. Adds that directory to your shell rc and exports `AQUA_GLOBAL_CONFIG`.
3. Writes the **global aqua config** (`~/.config/aqua/aqua.yaml`), which pins exactly one
   tool: `limen` itself.
4. Runs `aqua i -a`, after which **both `aqua` and `limen` are available globally**.

Run it from a clone:

```bash
git clone https://github.com/farcloser/limen-install && ./limen-install/limen-install
```

Never from a Homebrew formula: a `post_install` runs in Homebrew's sandbox and cannot write
the user's home ([homebrew](./homebrew.md)).

The script is idempotent — safe on a fresh machine and as an update; open a new shell
afterward if it says it changed your rc. Checksum enforcement and registry policy stay
configured **per project**: the global config exists only to carry the scaffolder. The
script runs with the system directories first on its `PATH`: on a machine that already has
limen, `cat`, `mkdir` and the rest of coreutils, and `curl`, are aqua proxies, and a proxy
parses the global config before it runs anything, so a bootstrap that rewrites that config
through a proxy truncates the file, fails in the proxy, and leaves every proxy on the machine
failing. The same holds for any script that edits an aqua config on a limen machine.

A second, optional bootstrap in the same repository, `limen-install-agent`, sets the machine
up so a coding agent can commit and push as its own identity
([identity and keys](./identity.md)).

---

## Scaffolding a new project

aqua's own three files live in `.aqua/`, one of the directories aqua searches (a local
registry path inside them is relative to `.aqua/`, hence `../.limen/…`). This repository's
own files ([`.aqua/aqua.yaml`](../.aqua/aqua.yaml), [`.limen/aqua.yaml`](../.limen/aqua.yaml),
[`.limen/aqua-registry.yaml`](../.limen/aqua-registry.yaml),
[`.aqua/aqua-policy.yaml`](../.aqua/aqua-policy.yaml)) are the canonical reference.

```
repo/
├── .aqua/
│   ├── aqua.yaml                          # the manifest: the limen pin, the import, the project's own tools
│   ├── aqua-checksums.json                # GENERATED — commit it
│   └── aqua-policy.yaml                   # authorizes the local registry
├── .limen/aqua.yaml                              # the canonical tool set, imported by the manifest
├── .limen/aqua-registry.yaml                     # local registry: farcloser/limen and the coreutils names
├── renovate.json                          # automated version bumps
└── .github/workflows/update-aqua-checksum.yaml   # refreshes checksums in Renovate PRs
```

What `.aqua/aqua.yaml` must carry; the manifest is **subset-pinned** ([enforcement](#enforcement)):

- The **canonical `checksum:` section, byte for byte**: `enabled: true` and
  `require_checksum: true` mean a missing or mismatched checksum **fails** the install, and
  `supported_envs` is part of it: adding an environment is a change to limen's baseline,
  not a per-repo edit.
- The **canonical `registries:` section**: the standard registry plus the `local` registry.
  One field is the project's: the standard registry's `ref`, which Renovate bumps per repo,
  and which is always an **exact pin** (a `vX.Y.Z` tag or a full commit SHA, never a branch).
- **The `farcloser/limen` pin and, last in the list, the import of the canonical tool set**
  (`- import: ../.limen/aqua.yaml`). Every other tool limen requires lives in that file, at
  the versions the pinned limen release carries: by default the canonical tools move with
  limen. Above the import, the project's own packages: extras, and **overrides**, a
  canonical tool at a version of the project's choosing, which Renovate then bumps in the
  project. aqua takes a package's first declaration, so an entry above the import wins and
  the same entry below it would be silently shadowed: the import closes the list (`limen
  fix` moves it there). A package is never listed twice, and never a **retired** one (the
  Go-built tools, now in `tools/go.mod`); `limen fix` never edits the project's entries, so a
  lingering retired pin fails the check, which names it, and the owner deletes it by hand.
- **`golang/go` is the project's own, by design, and not in the import.** A standard-library
  vulnerability is fixed by a Go patch release, govulncheck reports it in every repository's
  security lane the day the advisory lands, and the fix must not wait for a limen release. So
  `limen fix` seeds the pin once, from limen's own manifest (the version the running release
  was built and tested with), and Renovate bumps it per repository from then on, in the aqua
  group. The seed is made on `main`, by the owner (`just do tools add golang/go <version>`,
  or `limen fix` there), never on a Renovate branch: Renovate renders `.aqua/aqua.yaml` on
  every run, so a line the converge added there was removed, added and removed again, the
  branch force-pushed every minute (forkcloser/curl#50, seventeen times); on a Renovate
  branch `limen fix` leaves that file alone and `limen check` names what `main` needs. A
  repository that lags is caught twice: `GOTOOLCHAIN=local` refuses a `go.mod` ahead of the
  pin, and the security lane names the stdlib finding until the bump lands. The `go.mod`
  baseline rule is unchanged: the directive stays the earliest supported release, whatever
  the toolchain pinned to build. No other canonical pin meets that test.

Why the import: Renovate rebuilds a branch from scratch whenever the package list of the
file it updates differs from the base branch's. If a limen bump rewrote that list in
`.aqua/aqua.yaml`, the checksum workflow's `limen fix` and Renovate would undo each other on
every push. With the canonical tools in the imported file, a limen bump changes one line of
the manifest, the limen pin, and fix never moves its list. When limen retires a tool a
repository still pins, the bump's checksum workflow fails on the retired entry, and deleting
it is the owner's edit on that pull request.

> **Content-pinned files.** `.limen/aqua.yaml`, `.aqua/aqua-policy.yaml` and
> `.limen/aqua-registry.yaml` are canonical everywhere: `limen` requires them to match its
> embedded copies byte for byte, and `limen fix` overwrites drift. Renovate leaves them alone
> outside limen; in limen it bumps the canonical tools, which every repository receives with
> its next limen version unless it overrides one. `.aqua/aqua-checksums.json` is
> **generated, never hand-edited**: `limen fix` regenerates it (`aqua update-checksum`,
> which follows the import) whenever it changes the manifest or the tool set, or the file is
> missing. The catalog of local-registry packages is shared: to add one, it goes into limen's
> canonical registry, not a single repo's. A Go-built tool never qualifies: it is a
> `tools/go.mod` directive, the project's own.

Bootstrap, from the repo root:

```bash
aqua policy allow .aqua/aqua-policy.yaml   # explicit trust gate (one-time per machine)

aqua update-checksum      # generate .aqua/aqua-checksums.json for the binary tools
aqua install --only-link  # link every pinned tool (each downloads lazily on first use)

git add .aqua renovate.json .github/workflows/update-aqua-checksum.yaml .limen/aqua-registry.yaml
git commit --message "tooling: pin project CLIs via aqua"
```

---

## Cloning an existing aqua project

```bash
git clone <repo-url>
cd <repo>

# Authorize the project's local registry (one-time per machine):
aqua policy allow .aqua/aqua-policy.yaml

# Link the pinned tools (each downloads lazily on first use, verified against
# the committed checksums at that moment):
aqua install --only-link

# Tools now resolve to the project's pinned versions:
just --version
go version
```

Two conditions: aqua installed (above), and `aqua policy allow` once per machine, because the
project ships a local registry that aqua does not trust until authorized. No environment
variable is needed: aqua discovers an allowed `.aqua/aqua-policy.yaml` at the git repository
root (`AQUA_POLICY_CONFIG` is for policies with no git root to be found from — the
machine-global one carrying `limen`, which `limen-install` wires into the rc). What then just
works: byte-identical tool versions for every developer and CI, since `.aqua/aqua.yaml` and
`.aqua/aqua-checksums.json` are committed; lazy install means `aqua install --only-link` is
warm-up, not a prerequisite.

---

## Day-to-day changes — the `just do tools` recipes

The everyday operations — add, pin to a version, bump, remove — are recipes in the `tools`
module, so nobody remembers the aqua incantation or forgets the checksum step. `add`, `set`
and `remove` take the **`owner/repo`** as it appears in `.aqua/aqua.yaml`; `update` takes the
**command name** (the executable, since it delegates to `aqua update`, which resolves
commands). Each leaves `.aqua/aqua.yaml` **and** `.aqua/aqua-checksums.json` updated
together, ready to commit. They are project commands: they edit the project's own pins and
never the imported `.limen/aqua.yaml`. Adding a canonical tool pins it at the project's
version, above the import; removing that pin falls back to limen's version; `update` refuses
a canonical tool the project does not pin, since that one moves with limen.

```bash
just do tools add    junegunn/fzf                    # add a tool at its latest version
just do tools add    casey/just <version>             # or at a given one (here, overriding limen's just)
just do tools set    goreleaser/goreleaser <version>  # pin an existing tool to an exact version
just do tools update golangci-lint                   # bump an existing tool (by COMMAND name) to its latest version
just do tools remove junegunn/fzf                    # remove a tool entirely
```

The mutating recipes (`add`, `set`, `update`) end by refreshing the checksum and performing
a **full** `aqua install`, deliberately not link-only: links verify nothing, so only a real
install proves the new pin downloads and verifies, and a bad pin fails inside the recipe
instead of at first tool use (`remove` ends at the checksum refresh). Commit both files
afterward. Each recipe does work aqua's own CLI will not, which is why they are recipes and
not aliases:

- **`tools add` and `tools update` hide the import from aqua.** aqua follows an import: `aqua
  update` rewrites the imported file's pins, and `aqua generate -i` skips a tool the import
  already declares and appends anything else below the import, which must stay last. So aqua
  runs on a copy of the manifest with the import commented out, from outside the repository
  (inside it, a command the copy lacks is still resolved through the real manifest), and
  `add` places the new entry above the import itself.
- **`tools set` edits the manifest in place.** `aqua generate -i owner/repo@version` would
  *append* a second entry for an already-present package, not update the existing one.
- **`tools remove` edits the manifest, and only the manifest.** `aqua remove` only uninstalls
  the binary and does not touch `.aqua/aqua.yaml`. The recipe removes the entry, then `aqua
  update-checksum --prune`s the orphaned checksum, and never runs `aqua remove`: that
  uninstalls from aqua's root, which every checkout on the machine shares, so removing a tool
  in one repository took it away from every other one still pinning that version, mid-run.
- **Every aqua call names the repository's manifest** (`aqua -c .aqua/aqua.yaml …`). Without
  it aqua also reads any aqua.yaml in the directories above, and a stray one there decides
  what a recipe installs or prunes.

These recipes are the interface for any hand-made change. Renovate owns the routine,
unattended bumps.

## Updating a tool

### Manual

`just do tools update <command>` (latest) or `just do tools set owner/repo <version>`
(exact), as above. Spelled out:

```bash
# 1. Edit .aqua/aqua.yaml — bump the version, e.g.
#      goreleaser/goreleaser@<old>  ->  @<new>
# 2. Refresh the checksum for the new version:
aqua update-checksum
# 3. Install and verify:
aqua install --only-link
golangci-lint version
# 4. Commit BOTH files together:
git add .aqua/aqua.yaml .aqua/aqua-checksums.json
git commit --message "tooling: bump golangci-lint"
```

### Automated (Renovate — the intended workflow)

1. Renovate detects the new release and opens a **per-tool** PR bumping the version in
   `.aqua/aqua.yaml`.
2. The `update-aqua-checksum` workflow does the repo-specific follow-up **in the same PR**:
   it regenerates `.aqua/aqua-checksums.json`, and, when the bumped tool is `limen` itself,
   runs the newly pinned `limen fix` so the canonical files move with the pin (a repo is
   coherent only when the limen that wrote its files is the limen it pins; either half alone
   leaves the repo red).
3. You review the changelog and merge, or don't. Each tool is bumped independently.

> **Critical:** Renovate can update versions but **cannot** update `.aqua/aqua-checksums.json`
> on its own. Without the checksum-refresh workflow, a version bump would merge a stale
> checksum and **every install would fail**. The workflow is load-bearing.

### Pinned artifacts beyond aqua: `pins.yaml`

Some of what a build fetches is not a tool aqua knows: a kernel source tarball, a toolchain
release archive, the sources of a C library the build compiles. Those used to be two
variables in a .justfile or a build script — a URL with a version in it and a sha256 next to
it — with a Renovate regex manager per repository watching the version and a comment saying
the sha256 is a hand step; a Renovate bump then arrived green with a stale digest, because
nothing in CI read the pin, and failed at build time.

`pins.yaml`, at the repository root, is the one place such a pin lives; everything else reads
it:

```yaml
pins:
  - name: llvm
    renovate: github-releases llvm/llvm-project
    extract-version: ^llvmorg-(?<version>.*)$
    version: 22.1.8
    url: https://github.com/llvm/llvm-project/releases/download/llvmorg-${version}/LLVM-${version}-Linux-ARM64.tar.xz
    verify: github-attestation llvm
    digest:
      version: 22.1.8
      sha256: <64 hex>
  - name: guest-kernel
    renovate: github-releases farcloser/ossein-kernel
    versioning: regex:^(?<major>\d+)\.(?<minor>\d+)\.(?<patch>\d+)-ossein\.(?<build>\d+)$
    version: 7.1.5-ossein.2
    url: https://github.com/farcloser/ossein-kernel/releases/download/${version}/kernel-arm64
    verify: cosign-sha256sums https://github.com/farcloser/ossein-kernel/releases/download/${version}/SHA256SUMS https://github.com/farcloser/ossein-kernel/releases/download/${version}/SHA256SUMS.cosign.bundle ^(142371135\+[^@]+@users\.noreply\.github\.com|apostasie@farcloser\.world)$ https://github.com/login/oauth
    digest:
      version: 7.1.5-ossein.2
      sha256: <64 hex>
```

- **The build reads it back**: `limen pins get llvm url` and `limen pins get llvm sha256`,
  inside the recipe that fetches (not at .justfile load: every top-level `shell()` runs on
  every `just` invocation). The value exists once.
- **Renovate reads it, from the shared preset.** The `renovate:` line names the datasource
  and the depName; `extract-version:` (a regexp pulling the version out of a tag such as
  `llvmorg-22.1.8`) and `versioning:` (a scheme for tags semver misreads, such as a
  `-ossein.N` suffix) may follow it; the `version:` line closes the block and is what moves.
  The four come in that order with nothing between them — the preset's regex reads the block
  as one — and the `pins` rule refuses another order. One custom manager in
  `.limen/renovate.json` watches every `pins.yaml`.
- **limen owns the digest.** `digest.version` records the version the sha256 was computed
  for, so a digest left behind by a bump is visible offline: the `pins` rule fails it, naming
  the pin and the command. `limen pins refresh` recomputes every stale digest through the
  method the entry declares and rewrites the two lines in place; the checksum workflow runs
  it on every Renovate branch, in the same step as `limen fix`, from the same checksummed
  release — data the branch can change only in ways the release understands, never recipe
  text in a write job. A new entry leaves `digest:` out: `limen pins refresh` writes the
  block, and until it does the `pins` rule fails the entry and `limen pins get <name>
  sha256` refuses it.
- **A pin can be a workload, not a dependency.** A benchmark's compile input — the kernel tree
  a cross-runtime bench builds inside each container — is a `pins.yaml` entry like any other,
  fetched and verified the same way, but held where it is by a `packageRule` in the
  repository's `renovate.json`: a newer tree re-baselines every number the bench ever
  produced, so the bump is made by hand, on purpose. Before the entry, two scripts carried
  the same URL with no checksum at all
  ([farcloser/ossein#109](https://github.com/farcloser/ossein/pull/109)).

The methods, each a way to obtain a sha256 the entry can stand behind:

| `verify:` | What it does | For |
|---|---|---|
| `download` | hashes the bytes at `url`; TLS is the whole guarantee | a GitHub archive tarball, a source that signs nothing |
| `github-attestation <owner>` | downloads, has `gh attestation verify --owner <owner>` accept the file (SLSA provenance), hashes it | a project whose own release workflow attests its assets (LLVM) |
| `github-release-asset <owner/repo> [<tag>]` | downloads, has `gh release verify-asset <tag> --repo <owner/repo>` accept it, hashes it; the tag is `${version}` unless templated (`v${version}`) | GitHub's own attestation of an immutable release, which `gh attestation verify` does not see (Kata) |
| `cosign-sha256sums <sums-url> <bundle-url> <identity-regexp> <issuer>` | fetches the SHA256SUMS and its cosign bundle, has `cosign verify-blob` accept the pair, takes the artifact's line; the artifact is not downloaded | a release that ships a cosign-signed sums file (ossein-kernel) |
| `pgp-sha256sums <sums-url> <key-url> <fingerprint>` | fetches the clearsigned sums and the signer's public key block, verifies the signature in-process (limen's own OpenPGP reader: v4 keys and signatures, RSA or Ed25519, SHA-2), requires the signing key to be the one the fingerprint pins, takes the artifact's line from the signed text; the artifact is not downloaded | a source that clearsigns its checksums (kernel.org's `sha256sums.asc`) |

`${version}` and `${major}` (the version up to its first dot, the way kernel.org names a
series directory) expand in `url` and in the method's arguments. Under a `regex:` versioning
so do its named groups, so a project that tags `R_<x>_<y>_<z>` and names its release asset
`expat-<x>.<y>.<z>.tar.gz` pins the asset itself, the publisher's file, rather than GitHub's
generated tag archive:

```yaml
    versioning: regex:^R_(?<major>\d+)_(?<minor>\d+)_(?<patch>\d+)$
    version: R_<x>_<y>_<z>
    url: https://github.com/libexpat/libexpat/releases/download/${version}/expat-${major}.${minor}.${patch}.tar.gz
```

A reference that is none of these — a typo, a group the version does not match — is refused
when the file is read, never sent to a host. The arguments are split on whitespace, so an
identity regexp carries none and a fingerprint is forty hex digits unbroken. The tool a
method shells out to, `gh` or `cosign`, must be pinned in `.aqua/aqua.yaml`, and the `pins`
rule says so when it is not: unpinned, aqua's proxy falls through to whatever binary the
machine has. The PGP method shells out to nothing: the fingerprint is the whole trust
decision, the key URL only fetches the block that must hash to it, and limen reads exactly
what a clearsigned checksum file takes and refuses the rest by name (another key version, a
weak digest, an algorithm it does not carry). Downloads retry transient failures the way
every `curl` in the rig does; a verifier's refusal ends the run with the file as it was.

<a id="two-runs-per-renovate-bump"></a>
**Two runs per bump, the first one red — by design.** Renovate's push is a bare commit: the
pin has moved, the checksum has not. CI fires on it and `lint aqua` fails, truthfully, and
every tool that needs the new pin refuses to install. Minutes later the checksum workflow's
fix-up commit lands and its run is the green one. A red run on a Renovate commit that touches
`.aqua/aqua.yaml`, followed by a green run on the bot's fix-up, is the expected shape of such
a pull request, and the red says what it is. It is not made to look otherwise: a job that
skipped the run as "neutral" would add a checkout to every run of every repository and a run
that ends without linting, worse than a true red; and hosted Renovate cannot refresh the
checksum inside its own commit (post-upgrade tasks are self-hosted only). **Cancelled runs**
are the other half of the noise: both workflows cancel an in-progress run on the same ref
when a newer push arrives, so a Renovate rebase or a burst of merges shows a cancelled run
before the one that counts. Merges are gated on a full run of the final tree, the only run
that matters.

**A workflow that pushes to the branch that fires it commits only what it owns.** Its own
push fires it again, so it needs a fixed point: a run on its own commit must find nothing to
commit. The checksum workflow has one, since it commits the whole dirty tree, which is clean
on the next run; that whole-tree guard is safe only while it is the one writer on
`renovate/**`, and the day a second one shares the branch, each guards on its own file. A
workflow that owns one file and guards on the whole tree has no fixed point when a sibling
step dirties anything else: limen-install's installer-checksum workflow ran `setup-aqua` and
the link step, which rewrote `.aqua/aqua-checksums.json` on a registry bump, saw a dirty
tree, committed its own file unchanged, and the empty commit's push fired it again every
thirty seconds (https://github.com/farcloser/limen-install/pull/66). The guard is `git diff
--quiet -- <the file>`, never `git status --porcelain`.

### The push credential — one-time org setup

When the workflow pushes with the default `GITHUB_TOKEN`, GitHub suppresses workflow runs on
that commit: the PR's CI never re-runs on its final state, and with required checks the PR
head has zero check runs and is **unmergeable**. The cure is a credential whose pushes do
trigger CI. The workflow accepts two, in order of preference:

1. **A GitHub App, the recommended route.** Not infrastructure: an App with its webhook
   disabled is a registered identity with a private key. The workflow mints a fresh
   installation token per run — one hour, scoped to that single repository — so no long-lived
   broad credential sits in a secret, and nothing expires on a calendar.

   `limen bootstrap` automates it (`-org <name>`, or inferred from the origin remote): it
   registers the App through GitHub's app-manifest flow — one approval click in the browser,
   one more to install it — and stores the id and key on the org. Idempotent (a configured
   org is verified and left alone); anything it cannot do under the current gh token — no org
   admin, no browser, a half-configured org — is a printed warning, never a failed
   bootstrap. The browser is the command `BROWSER` names when set, the platform opener
   otherwise; a Ctrl-C while it waits ends the step as a warning.

   The manual equivalent, once per org:
   - Register an App on the org (Settings → Developer settings → GitHub Apps): webhook
     disabled, repository permissions **Contents: read and write** and **Workflows: read and
     write**, nothing else. Workflows is not optional: a limen bump converges canonical
     workflow files, and GitHub refuses a commit touching `.github/workflows/` from a token
     without it; the bootstrap's audit reports an installation missing either permission,
     and the lane requests both by name when it mints the token (naming any permission scopes
     the token to exactly the named ones).
   - Generate a private key, and install the App on the org, all repositories.
   - Set the org **variable** `UPDATE_AQUA_CHECKSUM_APP_ID` and the org **secret**
     `UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY`.

   Every org registers **its own** App, named `limen-ci-<org>` (GitHub App names are globally
   unique). Apps are not shareable for this: minting tokens requires the private key, which
   never leaves the org that owns it, and installing someone else's App would grant *that
   org* write access to your repositories while giving your workflows nothing to mint with.
2. **A fine-grained PAT**, contents and workflows read and write, stored as the org secret
   `UPDATE_AQUA_CHECKSUM_TOKEN`: the drop-in fallback. It is bound to a user account and
   expires; when it lapses, the workflow silently degrades to the default token and bump PRs
   go back to being blocked. Prefer the App.

Without either, the workflow still runs and pushes; only the CI re-run is lost: tolerable
without required checks, blocking with them.

## Enforcement

`limen check [path]` verifies the aqua rule alongside the
[mandatory files](./mandatory-files.md). It fails a repository that has no `.aqua/aqua.yaml`;
whose `checksum:` section differs from the canonical; whose `registries:` section differs
(beyond the project-owned standard `ref`, itself an exact pin); that lacks any canonical
package (by name); that lists a package twice; or that is missing the committed
`.aqua/aqua-checksums.json`. A manifest `limen` cannot confidently parse (flow-style sections
and the like) also fails: what cannot be verified does not pass.

`limen fix` remediates all of that: a missing manifest is seeded from limen's own (and only
then may the matching canonical `.aqua/aqua-checksums.json` be seeded with it, so a fresh
`bootstrap` is compliant offline); an existing manifest is **merged** — the canonical
sections reset (keeping a valid project `ref`), missing canonical packages appended by name
without duplicating one the project already pins, the project's own packages and versions
untouched. The one version `fix` does move is an existing `farcloser/limen` pin: a
**released** limen sets it to its own version, because that version is baseline-owned — the
enforcer that wrote the repo's canonical files must be the enforcer the repo pins (a dev
build has no version to stamp and moves nothing). Whenever the manifest changed or the
checksums file is missing, `fix` regenerates the checksums with the real tool (`aqua policy
allow .aqua/aqua-policy.yaml`, then `aqua update-checksum --prune`); if aqua is unavailable it
says so and leaves the commands. Duplicate package entries are reported, not resolved: only a
human knows which version was meant. A repository still carrying aqua's files at its root,
where limen kept them before `.aqua/`, fails the rule naming them; `fix` moves them into
`.aqua/` and regenerates the checksums, which also allows the policy at its new path for the
rest of the run.

Beyond that baseline, *which* extra tools and versions a project pins is engineering
judgment: the tool enforces the invariants, the book carries the reasoning. The same command
runs in pre-commit, in CI and in an agent's workflow. See [`../cmd/limen/`](../cmd/limen).

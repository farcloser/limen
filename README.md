# Limen

The single source of truth for how we build software at Farcloser.

This repository exists for two audiences at once: the **humans** who write our code and
the **coding agents** who increasingly write it alongside us. Everything here is meant to
be read by both — prose that explains *why* we do things a certain way, and tooling that
*enforces and verifies* that we actually did.

## What lives here

| Directory | Purpose |
|-----------|---------|
| [`book/`](./book) | The book of best practices — prose, rationale, and the canonical statement of every rule. If a rule is real, it is written down here first. |
| [`cmd/limen/`](./cmd/limen), [`internal/`](./internal) | Devtools, in Go (one module, rooted here). The executable counterpart to the book: `limen` checks, fixes, and bootstraps a repository against the rules. *Limen* — Latin for "threshold": nothing crosses into our codebases without passing it. |
| `skills/` *(planned)* | AI agent skills — packaged instructions that let a coding agent apply our practices directly, invoking `limen` where machine verification is needed. |

## The operating principle

> Every rule is **written**, **verifiable**, and **enforceable**.

- **Written** — it has a page in the book explaining what and why. No tribal knowledge.
- **Verifiable** — `limen` can decide, mechanically, whether a repository complies.
- **Enforceable** — the same check runs in pre-commit, in CI, and in an agent's workflow,
  so the answer is identical no matter who (or what) is asking.

A rule that cannot be verified is a guideline, and lives in the book as advice. A rule
that *can* be verified gets a `limen` check and becomes policy.

## Requirements

`limen` and its shared `just` recipes shell out to a small set of external binaries that are
**not** managed by aqua — they must already be on the system:

| Binary | Used for |
|--------|----------|
| **git** | `git init` on bootstrap, and the `git ls-files` / `git diff` / `info` recipes. |
| **aqua** | installs the pinned toolchain; bootstrap runs `aqua policy allow` + `aqua install`. aqua **cannot install itself** — [`limen-install`](https://github.com/farcloser/limen-install) sets it up once per machine, into the directory the hermetic `PATH` expects (see [`book/tooling.md`](./book/tooling.md)). |
| **bash** (≥ 3.2) + **env** | every multi-line recipe runs under `#!/usr/bin/env bash`. |
| POSIX userland: **grep, sed, awk, head, tr, mv, mktemp, xargs** | used by the shared `tools` / `lint` / `fix` recipes. Present by default on any Unix; the hermetic PATH keeps `/usr/bin:/bin:/usr/sbin:/sbin` so they resolve. |

Everything else the recipes use — `go`, `just`, and the whole lint/test/release toolbelt
(`shellcheck`, `golangci-lint`, `yamlfmt`, `lychee`, `jq`, `go-licenses`, `git-validation`,
`govulncheck`, `deadcode`, `godolint`, `gotestsum`, `goreleaser`, `cosign`, `dot`, and
`limen` itself) — is pinned and installed **by aqua**, so it is not a manual prerequisite;
[`aqua.yaml`](./aqua.yaml) is the authoritative list.

## Using `limen`

Machine setup is one bootstrap: [`limen-install`](https://github.com/farcloser/limen-install)
installs aqua and, through a global aqua config, `limen` itself — both globally available
afterward (see [`book/tooling.md`](./book/tooling.md)):

```bash
brew install farcloser/brews/limen    # or run the limen-install script directly
```

(For hacking on limen itself, `go install github.com/farcloser/limen/cmd/limen@latest`
still works as a plain Go fallback.)

```bash
limen check [path]            # check the repo at path (default ".")
limen check -json [path]      # machine-readable findings
limen fix [path]              # remediate an existing repo (create/overwrite/merge)
limen bootstrap <path>        # new compliant repo (git init, write everything, install tooling)
limen github check            # audit the repo's GitHub settings against the baseline (via gh)
limen github fix              # repair the fixable settings, plan-then-apply
```

`check` reports; `fix` repairs what it safely can and flags the rest as advisories — for the
aqua rule that includes regenerating `aqua-checksums.json` with aqua itself (`aqua policy
allow` + `aqua update-checksum --prune`) whenever it changed the manifest or the file is
missing. `bootstrap` is `fix` on an empty directory, and finishes by installing the pinned
tooling (`aqua policy allow` + `aqua update-checksum --prune` + `aqua install --only-link`).
Add `-json` to any of them. `bootstrap` writes a `Closed-source`
`LICENSE` by default (`-license <id>`, `-holder <name>`).

Exit codes: `0` success (all passed / all resolved) · `1` a rule failed or needs manual
attention · `2` usage error.

## How a coding agent uses this repo

1. Read the relevant chapter of the `book/` to understand intent.
2. Apply the practice (write the file, structure the package, …).
3. Run `limen check` to confirm the result complies — and rely on the same command in CI
   as the backstop.

## Status

Early. We are bootstrapping from the ground floor:

- [x] Repository shape and operating principle (this README)
- [x] **Common / mandatory files** — every repo must be a git repository and carry a
      recognized `LICENSE`, an `.editorconfig` and `.gitignore` matching the shared baselines,
      a `README`, and a root `Justfile` carrying the shared-baseline import plus the canonical
      `.limen/just/` modules. See
      [`book/mandatory-files.md`](./book/mandatory-files.md) and [`cmd/limen/`](./cmd/limen).
- [x] **Per-language rules** — conditional checks that fire only when a language is present:
      shell → `.limen/.shellcheckrc`, and YAML → `.limen/.yamlfmt`. See [`book/per-language.md`](./book/per-language.md).
- [x] **Project tooling** — every repo pins its build/CI tooling through aqua: a committed
      `aqua.yaml` with checksum enforcement on, plus a committed `aqua-checksums.json`. See
      [`book/tooling.md`](./book/tooling.md) and [`cmd/limen/`](./cmd/limen).
- [x] **GitHub settings** — repository configuration audited and repaired like any other
      rule: merge doctrine, rulesets, Actions hardening, security features. See
      [`book/github.md`](./book/github.md) and `limen github check|fix`.
- [ ] Further rules, one chapter and one check at a time.

## Layout conventions

`limen` is itself a Farcloser repository and is expected to pass its own rules — we
dogfood `limen` against this tree.

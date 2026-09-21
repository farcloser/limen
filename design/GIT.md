# GIT — a pinned git for the hermetic rig: build it, rewrite it, or scope it

Status: **research, not a decision.** Three questions were studied on 2026-09-20: building
git for all three platforms the way `forkcloser/curl` builds curl; replacing git with a Go
implementation; and a scoped `limen-git` covering only what limen and the shared recipes
use. The trials and measurements are the Forkcloser session's, run that day and relayed here;
the inventory of what limen and the recipes ask of git was re-verified against this tree by the
Limen session (`internal/github/infer.go`, `internal/rules/fix.go`, the `.limen/just` modules,
the checksum workflow). Versions are as-researched, not pinned here.

## Why

Every other tool on the hermetic PATH is pinned by aqua or built by the pinned toolchain.
git is the exception: Apple's Command Line Tools on macs (`/usr/bin` is on the hermetic
PATH), Ubuntu's package on the runners, Git for Windows on windows. Three gits, three
versions, one set of recipes parsing their output. That is the shape curl was in before
`forkcloser/curl`, and the same question follows: what does the rig actually need, and
which of the three answers gives it at the least cost.

## What the rig asks of git

limen itself shells out twice: `git -C <dir> remote get-url origin` (slug inference) and
`git init` (the fix path of the `git` rule; the check only stats `.git`).

The canonical just modules, exact invocations:

| Module | Invocations |
|---|---|
| `lint.just`, `fix.just`, `lint-homebrew.just`, `fix-homebrew.just` | `git ls-files -z --cached --others --exclude-standard [pathspec…]`, seven sites, pathspecs like `'*.go'` and literal names with their `**/` twins |
| `lint.just` | `git config --unset-all gpg.ssh.allowedSignersFile` and `--add … .allowed_signers` (best-effort, arms signature display for humans); `git rev-parse --verify -q origin/<ref>` (range detection for the commits lane); then `build/tools/git-validation`, which itself runs `git rev-list <range>` and `git log --pretty=oneline --no-show-signature` |
| `lint-go.just` | `git grep -nE "<pattern>" -- '*.go'`, three ban patterns, all RE2-compatible |
| `main.just` | `git remote get-url origin`, `git describe --tags --abbrev=0`, `git rev-parse --short HEAD`, `git log --max-count=1 --format=%cd --date=short` |
| `build-go.just` | `git describe --tags --always --dirty`, four sites |
| `release.just` | `git status --porcelain`, `git status --short`, `git rev-parse -q --verify "refs/tags/<tag>^{commit}"`, `git rev-parse HEAD`, `git cat-file tag refs/tags/<tag>` (grepped for a SIGNATURE block), `git tag -s`, `git tag -d`, `git push origin HEAD`, `git push origin refs/tags/<tag>`, `git describe --exact-match --tags HEAD` |
| `update-aqua-checksum.yaml` | `git status --porcelain` and `-z`, `git rev-parse HEAD`; the commit itself goes through GraphQL `createCommitOnBranch`, already no `git commit` or push |

Pinned tools that exec git themselves: git-validation (`rev-list`, `log`); gh (about
thirteen subcommands: `config`, `remote`, `checkout`, `branch`, `tag`, `symbolic-ref`,
`status`, `show-ref`, `push`, `pull`, `fetch`, `config --get-regexp`); goreleaser (about
twelve: `status --porcelain`, `describe --exact-match` / `--always` / `--dirty --tags
--match`, `rev-parse --abbrev-ref`, `rev-list -n1` / `--max-parents=0`, `show --format`,
`tag -l`, `ls-remote --get-url`). `actions/checkout` uses the runner's git, outside the
hermetic PATH whatever we do. The contribute skill and humans use `worktree add` and
`remove`, `commit -s` with SSH signing, `%G?` verification against `.allowed_signers`,
`reset --soft`, `merge --ff-only`, `push --force-with-lease`, `fetch --prune`, `branch -d`.

Two edges stay outside git whatever is done: git delegates signing and verification to
`ssh-keygen -Y` and SSH transport to `ssh`, so pinning git moves the unpinned edge to
OpenSSH. `farcloser/homebrew-brews` already carries an openssh formula.

## Option A: build git the way curl is built

Trials run on 2026-09-20.

**linux arm64.** Alpine under ossein (`docker run` through the hermetic front), git from
the kernel.org tarball, `make` with `NO_RUST=1 NO_GETTEXT=1 NO_TCLTK=1 NO_PERL=1
NO_PYTHON=1 NO_EXPAT=1 NO_INSTALL_HARDLINKS=1 NO_REGEX=NeedsStartEnd`, static `CFLAGS` and
`LDFLAGS`, `CURLDIR` pointing at the curl-for-win arm64 package that `forkcloser/curl` built
(its `lib/` ships `libcurl.a` plus every dependency's static library and `include/`), and
`CURL_LDFLAGS` naming that whole stack. Built in 15 s. Stripped: `git` 4.3 MB,
`git-remote-http` 6.8 MB. `git version --build-options` reports our libcurl, LibreSSL and
zlib-ng. `ls-remote` to github.com negotiates TLS 1.3 with the certificate verified; a
shallow clone and `describe` work.

**mac arm64.** Native Xcode clang against SDK libraries only, same knobs plus `NO_OPENSSL=1
APPLE_COMMON_CRYPTO=1`, 12 to 24 s. Dynamic dependencies: libz, libiconv, libSystem,
CoreFoundation, CoreServices; `git-remote-https` on Apple's libcurl by default, and would
link curl-for-win's mac `libcurl.a` for hermetic TLS once `forkcloser/curl` publishes the
mac dev package. An SSH-signed commit round trip verifies (`%G?` = `G`) through
`/usr/bin/ssh-keygen`. The build machine has cargo, so the first mac build even included
git's Rust component.

**windows.** Do not build, import. Git for Windows publishes MinGit per release,
`MinGit-<v>-64-bit.zip` and `-arm64.zip` (35 to 37 MB; busybox variants 31 to 33 MB), sha256
in the release notes. Contents: `git.exe`, `libexec/git-core`, the MSYS runtime, openssh
(`ssh.exe`, `ssh-keygen.exe`), git-credential-manager, the libcurl DLL for the http helper;
no perl, tcl, bash, `curl.exe` or l10n. Each asset carries a GitHub attestation, but it is
GitHub's release attestation (signer SAN `https://dotcom.releases.github.com`, predicate
`https://in-toto.io/attestation/release/v0.2`): it proves the asset belongs to the release,
not how it was built. Weaker than curl-for-win's cosign bundle; the executables inside carry
Authenticode signatures. Git for Windows differs from the upstream release it tracks in 232
files, and upstream's Makefile knows the MinGW target only under `MSYSTEM` inside MSYS2, so
building our own windows git means MSYS2 on a windows runner plus their patches: not worth
it. aqua's standard registry has no git package.

Gotchas that would shape the rig:

- `NO_RUST=1`, or a Rust toolchain in the image: git builds its `src/*.rs` (about 1,600
  lines) via cargo by default, and the project has announced Rust becomes mandatory in 3.0.
  Alpine has cargo.
- `RUNTIME_PREFIX` must be on so git finds `libexec/git-core` relative to its own binary
  wherever aqua installs it; the trial used a fixed prefix.
- git is a tree, not one binary: `bin/git` plus `libexec/git-core`. The one helper that
  matters is `git-remote-http` (`git-remote-https` is a symlink to it). The 33 other distinct
  files are shell, perl and python front ends we would drop. An aqua entry exposes `git`; the
  tree comes along.
- CA bundle: git through this libcurl did not find the host bundle
  (`/etc/ssl/certs/ca-certificates.crt` on Alpine) until `GIT_SSL_CAINFO` named it, while the
  curl binary from the same package finds it on its own. Either ship a bundle beside git or
  set `http.sslCAInfo` in the recipes. Settle before a first release.
- `linux-headers` is needed (`compat/fsmonitor` includes `linux/magic.h`).
- Against Alpine's own libcurl the static link failed twice on ordering (libunistring after
  libidn2, brotlicommon after brotlidec; `pkg-config --static` fixes those) and then on a real
  clash: git's `error()` against gnulib's `error` inside libidn2. Both vanish with the
  curl-for-win libraries, which have no IDN. Lesson: link git against our libcurl, not the
  distribution's.
- Prerequisite: `forkcloser/curl` publishes the curl-for-win dev package (`lib/` plus
  `include/`) as release assets; curl-for-win already produces it, it is what the trial
  linked.

Effort: `forkcloser/curl`'s rig is 527 lines across seven files; git's would be the same
shape with our own build script of about a hundred lines per posix leg in place of
curl-for-win's, a `pins.yaml` entry against the kernel.org tarball (verified download; the
maintainer's PGP `.sign` is available too), Renovate on `git/git` tags and on Git for
Windows releases. A first release is a day or two. Then OpenSSH is the next unpinned tool.

## Option B: git in Go

go-git is 4.8 MB of Go. It covers clone, fetch and push over ssh and https with agent
authentication, commit, tag, log, status, ls-files, ls-remote, cat-file, submodules,
sparse checkout, local config read and write (global and system read-only), force-with-lease,
and verify-commit and verify-tag for OpenPGP only. SSH signatures are recognised by header
only: no signing, no verifying. Missing: describe, rev-parse, reflog, commit-tree, worktree
(partial), hooks, git grep, protocol v2, index v3 and v4, multi-pack-index, push-cert.
go-git's `cli` (`gogit`, since late 2024, active, a handful of stars) is a thin CLI with a
conformance harness that runs upstream's `t/` tests against it: the nearest thing to git in
Go, and early.

git itself is about 412,000 lines of C outside `t/`, 130 builtins, 14 shell scripts. The rig
and its tools touch roughly 45 distinct subcommands, but gh, goreleaser and git-validation
parse exact output, humans expect the real thing, and `ssh` and `ssh-keygen` stay external. A
drop-in is months to first use and open-ended after; limen's own two calls could move to
go-git in an afternoon and buy nothing while the recipes and gh need a `git` on PATH.

Verdict: not a target.

## Option C: `limen-git`, only what limen and the recipes use

The surface in the table above is thirteen operations. Estimated on top of go-git, x/crypto
for the agent, and an sshsig implementation:

| Operation | Estimate |
|---|---|
| `ls-files` with pathspec globbing | ~300 lines |
| `describe` (nearest tag, count, `g<abbrev>`, `--dirty`, `--exact-match`, `--abbrev=0`, `--always`) | ~200 |
| `rev-parse` (`HEAD`, `--short`, `--verify -q`, `^{commit}` peeling) | ~100 |
| `status --porcelain`, `-z`, `--short` | ~80 |
| `tag -s` via sshsig over the agent, `tag -d`, `cat-file tag` | ~300 |
| `push origin HEAD`, `push origin refs/tags/<tag>` | ~60 |
| `config --unset-all`, `--add` | ~40 |
| `grep -nE` over tracked `*.go` | ~80 |
| `log --max-count=1 --format=%cd --date=short` | ~30 |
| `remote get-url`, `init` | trivial |

Plus the CLI shell, and a conformance test that runs each command through real git and
`limen-git` on the organization's repositories and diffs the output. Total 1,500 to 2,500
lines, about a week to a first version with the diff green. It also retires git-validation:
DCO, short-subject and dangling-whitespace over a range is a log walk of about 150 lines once
range resolution exists.

The risk concentrates in three places. `describe` must match git's count and `--dirty`,
because goreleaser embeds the same string from the runner's real git (safe on a release tag,
where both give the bare tag). `status` on windows, where go-git has a history of false
modifications through line endings and file modes; the `.gitattributes` baseline helps, and
the conformance diff must run on the windows lanes. SSH signing output must verify under
`ssh-keygen -Y verify` and under GitHub's check, or release tags lose their signed status.

What it buys: the recipes stop depending on whichever git the host has, one more tool on the
hermetic PATH for the price of a subcommand group inside limen's existing release, and no
third-party build lineage. What it does not: git stays on the machine and the runner for gh,
goreleaser, `actions/checkout` and humans, and `limen-git` must write objects and refs real
git reads back, which go-git does.

## Verdicts, for the maintainer to decide

- **Option A is worth it, but not now.** The case is consistency of what the recipes parse
  across three host gits, not TLS or supply chain: git's HTTPS already negotiates TLS 1.3
  everywhere, and Apple's tools and Ubuntu's package are not the weak point. The sequence if
  pursued: `forkcloser/curl` publishes the dev package; `forkcloser/git` mirrors
  `forkcloser/curl` (linux static in Alpine through ossein linked to our libcurl; mac native
  linked to curl-for-win's mac lib; windows imports MinGit with sha256 and the release
  attestation checked; `RUNTIME_PREFIX`; the smoke test is `ls-remote` over TLS 1.3 plus a
  signed-commit round trip); a registry entry in limen; OpenSSH next. Decide first whether
  the organization accepts a release attestation rather than a build attestation for the
  windows leg.
- **Option B: drop.**
- **Option C: if the goal is deterministic recipes rather than a pinned git for humans, the
  scoped tool is the better fit**, and the conformance diff is what makes it safe to adopt.

# Known upstream bugs

Bugs in tools we pin and do not own: what each looks like when it bites, why it happens,
what we do about it, and when the workaround can go. An entry stays until its lift condition
is met; the workaround it names is a rule in the code or the recipes, and whoever removes
the rule removes the entry in the same change. A bug one of our forks patches is listed the
same way, the fork's patch as the workaround; reporting it upstream is the human's, later,
and no session asks.

## aqua-proxy matches `.exe` case-sensitively (Windows)

**Symptom.** A step whose shell resolves to an aqua link hangs, then prints hundreds of
identical `ERR aqua failed … exe_name=bash.EXE error="exit status 0xffffffff"` lines and
exits `-1`.

**Cause.** aqua-proxy takes its command name from `argv[0]` and strips the extension with
`strings.CutSuffix(cmdName, ".exe")`
([pkg/cli/proxy.go](https://github.com/aquaproj/aqua-proxy/blob/cdf1d68ed0fa4a842ab8c72aac179deba2645c13/pkg/cli/proxy.go#L31)).
The Actions runner resolves a `shell:` program through `PATHEXT`, uppercase by default, so
the proxy is invoked as `bash.EXE` and asks aqua for a command of that name, which no
registry entry provides.

**Workaround.** On Windows, nothing the runner itself resolves is an aqua link: aqua links
no `bash`, `sh` or `cygpath`, and the coreutils come from Git for Windows' own MSYS2
([windows: names](./windows.md#names-pathext-case-and-slashes)).

**Lifts when** aqua-proxy compares the extension without regard to case. Not reported
upstream yet.

## aqua skips its own bin directory by string comparison (Windows)

**Symptom.** The same recursion as above: the proxy, asking for a command aqua cannot map,
finds itself.

**Cause.** When aqua falls back to a `PATH` search, it skips its own bin directory only when
the `PATH` element equals `filepath.Join(rootDir, "bin")` as a string
([pkg/controller/which/lookpath_windows.go](https://github.com/aquaproj/aqua/blob/3587e767ce7134b69d68434962d10d60b77fb3d8/pkg/controller/which/lookpath_windows.go#L33)).
setup-hermetic writes the root with forward slashes (`cygpath -m`), `filepath.Join` produces
backslashes, and the two never match: the first `bash.EXE` on the search is the toolset's
own link.

**Workaround.** The one above. Together the two bugs are the recursion; either fix alone
would stop it.

**Lifts when** aqua compares cleaned paths, case-insensitively, in this skip. Not reported
upstream yet.

## lychee retries 429 only, never a 5xx

**Symptom.** The `links` lane fails with a GitHub `503` for a link that answers `200` a
minute later, most often a `github.com/<owner>/<repo>/blob/...` page.

**Cause.** lychee judges a rejected status retryable only when it is `429`
([lychee-lib/src/retry.rs](https://github.com/lycheeverse/lychee/blob/lychee-v0.24.2/lychee-lib/src/retry.rs)),
so `max_retries` and `retry_wait_time` in `.limen/lychee.toml` never apply to a server
error: one `503` blip is a finding. GitHub's web front answers `503` on a blob page for
seconds to minutes at a time.

**Workaround.** The `links` recipe (`.limen/just/security.just`) runs a second pass over the
links that failed with a server error, once, a minute later; a `5xx` that persists past it
is a finding, since a server still erroring after a minute may be gone. github.com's blob
and tree pages are excluded in `.limen/lychee.toml`: github.com throttles them from Actions
runners whoever owns the repository, a file we own is checked as a file by its repository's
own lane, and a reference to someone else's file is pinned to a commit. A blip longer than
the minute still reddens the lane, and is rerun by the author, named as this flake.

**Lifts when** lychee retries server errors under its own retry settings. Whether upstream
tracks it is unverified.

## aqua records an http package's posix checksums under one wrong environment

**Symptom.** `hermetic install` (or `just do tools add`) of `rust-lang/rustup` from the
standard registry fails on macOS and Linux with `checksum is required` (aqua code 001),
while `.hermetic/aqua-checksums.json` carries entries for the two Windows environments and
one `x86_64-apple-darwin` that no supported environment asked for.

**Cause.** The package's checksum file is one per environment, and the posix ones name the
asset (`<sha> *./rustup-init`) where the Windows ones carry a bare digest. aqua's
`update-checksum` records a bare digest under the fetching environment's own id, and a line
that names an asset under the id of the first supported environment whose rendered asset
name matches (`pkg/controller/updatechecksum/update.go`, `ChecksumIDFromAsset`): every
posix environment renders `rustup-init`, so three fetches land under one id, and two
environments have none.

**Workaround.** The local registry carries the standard entry with the checksum read by
pattern (`file_format: regexp`, `pattern.checksum: "^([0-9a-f]{64})"`), which takes the
bare-digest path for every environment; the pin names `registry: local`. The same copy
carries the Windows URL fix below; it retires when both are fixed upstream.

**Lifts when** aqua records an http package's per-runtime checksum under the runtime it
fetched it for, or the standard registry's entry reads the file by pattern. Reporting it
upstream is pending a decision.

## The standard registry's rustup entry cannot install on Windows

**Symptom.** `hermetic install` of `rust-lang/rustup` from the standard registry fails on
Windows with a 404 from `static.rust-lang.org`.

**Cause.** The entry's download URL is `…/{{.Arch}}-{{.OS}}/rustup-init` for every
platform, and its Windows override changes only the checksum URL (to
`rustup-init.exe.sha256`). The host serves `rustup-init.exe` for the `pc-windows-msvc`
targets and answers 404 for `rustup-init` there, so the checksum can be fetched and the
installer cannot.

**Workaround.** The local registry's copy of the entry (above) gives the Windows override
the `.exe` URL and renames the file to `rustup-init` on the way (`files: src:
rustup-init.exe`), so the link is the same name on every platform.

**Lifts when** the standard registry's entry downloads `rustup-init.exe` on Windows.
Reporting it upstream is pending a decision.

## aqua's downloads have no timeout

**Symptom.** A step that runs a tool for the first time on a runner hangs with no output
until the job's `timeout-minutes` cancels it. A rerun passes
(https://github.com/forkcloser/erofs/actions/runs/37509453884/job/112428768665).

**Cause.** A lazy install, the first run of a tool through its aqua link, downloads the
package with Go's `http.DefaultClient`
([pkg/cli/exec/command.go](https://github.com/aquaproj/aqua/blob/01975b3b0d740b5c4876be30a8a99784ad5ad163/pkg/cli/exec/command.go#L87),
[pkg/download/github_release.go](https://github.com/aquaproj/aqua/blob/01975b3b0d740b5c4876be30a8a99784ad5ad163/pkg/download/github_release.go#L67)),
which has no timeout, and nothing on that path sets a deadline. A connection that stalls
mid-body blocks the read forever.

**Workaround.** None in aqua's path. Every job in the canonical workflows carries a
`timeout-minutes`, so a stall costs that budget rather than the runner's six hours. The red
is rerun and named on the pull request: a flake whose cause is known and whose fix was
declined.

**Lifts when** aqua bounds its downloads. Not reported upstream, by decision.

## MSYS2 bash dies under emulation on windows-11-arm

**Symptom.** A shebang recipe on the windows-11-arm leg fails with `exit code 4` (or `127`,
or `2816`) and nothing on stderr: bash itself died.

**Cause.** Git for Windows' arm64 build pairs a native `git` with an x86-64 MSYS2 userland
run under Windows' x64 emulation. There, roughly once in a few thousand launches, bash dies
at the operating-system level while a child it forked execs a program: a process
substitution (`done < <(git ls-files …)`) or a command substitution (`scratch=$(mktemp -d
…)`). What kills it was never identified. The record is
[design/WINDOWS-ARM-EXIT-4.md](../design/WINDOWS-ARM-EXIT-4.md).

**Workaround.** None that removes it. The shared recipes carry no process substitution, the
shape of every early failure; command substitutions stay, since rewriting them all for one
runner is not worth it, and a silent `4` or `127` on that leg is re-run
([windows: bash dies under emulation](./windows.md#bash-dies-under-emulation)).

**Lifts when** Git for Windows ships its MSYS2 runtime native on arm64
(https://github.com/msys2/msys2-runtime/pull/356, open).

## macOS awk fails on bytes that are not UTF-8

**Symptom.** A recipe that reads arbitrary files with awk on macOS exits 2 with
`awk: towc: multibyte conversion failure on: '…'`, on CI runners and not on a laptop whose
shell runs in the C locale.

**Cause.** macOS's awk, in a UTF-8 locale, converts every input record to wide characters
and aborts on the first byte sequence that is not UTF-8, which every binary file carries.

**Workaround.** An awk that may read binaries runs under `LC_ALL=C` (the shell lint's
shebang scan does); the patterns it matches are ASCII, so nothing it decides changes.

**Lifts when** never, as far as we know: it is how that awk is built. The entry stays as the
reason for the `LC_ALL=C`.

## depguard checks one allow entry per import

**Symptom.** `just do lint go` reports an import of the module's own package,
`github.com/<owner>/<mod>/<pkg>`, as "not allowed from list 'main'", while the rendered
`.build/golangci.yml` carries `github.com/<owner>/<mod>` in the allow list. It starts the day
the project's `.lint-go.yaml` allows a sibling module whose path extends the module's by a
hyphen (`github.com/<owner>/<mod>-<x>`).

**Cause.** depguard sorts the allow list and, for an import, checks as a prefix only the last
entry that sorts at or before it
([settings.go, `strInPrefixList`](https://github.com/OpenPeeDeeP/depguard/blob/v2.2.1/settings.go)).
`-` sorts before `/`, so `…/<mod>-<x>` sits between `…/<mod>` and `…/<mod>/<pkg>`: the
search lands on the sibling, which is no prefix of the import, and the shorter entry that is
one is never looked at.

**Workaround.** The baseline allows `${MODULE_PATH}/` beside `${MODULE_PATH}`
(`.limen/lint-go.yaml`): with the slash the entry sorts after every hyphenated sibling and
before the module's packages, so the search lands on it. The same holds for any other allowed
prefix with a hyphenated sibling in the list: a project that allows `github.com/x/y` and
`github.com/x/y-z` and imports `github.com/x/y/pkg` adds `github.com/x/y/` in its
`.lint-go.yaml`, naming this entry.

**Lifts when** depguard checks every entry that is a prefix of the import, not the one its
search lands on. No upstream issue names it (searched 2026-10-11).

## Go's `(*os.File).Stat` after Close answers "The handle is invalid." (Windows)

**Symptom.** A Stat on a closed `*os.File` returns a `*PathError` wrapping
`ERROR_INVALID_HANDLE` ("The handle is invalid.") on Windows, where every other platform
returns one wrapping `fs.ErrClosed`; `errors.Is(err, fs.ErrClosed)` is true on Unix and
false on Windows for the same program.

**Cause.** `os/stat_windows.go`'s `(*File).Stat` checks only for a nil receiver and then
calls `statHandle(file.name, file.pfd.Sysfd)` on the raw handle field, which Close has set
to `syscall.InvalidHandle` (`internal/poll/fd_windows.go`, `destroy`), so the kernel is
asked about an invalid handle. `os/stat_unix.go`'s goes through `f.pfd.Fstat`, which takes
the descriptor's reference first and reports `ErrFileClosing`, mapped to `ErrClosed`.
Windows alone skips the reference.

**Workaround.** None in our code: xos returns `*os.File`, and the method is Go's. xos's
identity test (held file, Stat after Close) compares xos's answer to os's and tolerates
the Windows one; callers that Stat a closed file are wrong either way.

**Lifts when** `(*File).Stat` on Windows takes the poll descriptor's reference before
touching the handle, as the other methods do. Not reported upstream yet.

## sigstore-go's TUF cache has no path override, so slsa-verifier always writes `$HOME/.sigstore`

**Symptom.** An aqua install of a package with SLSA provenance fails wherever `~/.sigstore`
is not writable, such as the Claude Code sandbox with that directory off its write list:
`SLSA verification failed: open /Users/…/.sigstore/root/tuf-repo-cdn.sigstore.dev/tuf_tmp…:
operation not permitted`, then `download aqua: verify the asset: verify a package with
slsa-verifier` (`aqua update-aqua`, 2026-10-12). `TUF_ROOT` is set and ignored.

**Cause.** slsa-verifier builds its trusted root from sigstore-go's `tuf.DefaultOptions()`
and sets no cache path
([verifiers/utils/sigstore_tuf.go](https://github.com/slsa-framework/slsa-verifier/blob/30d0be3bbab553fc51557377baba2f7572dfc212/verifiers/utils/sigstore_tuf.go#L49)).
`DefaultOptions` puts the cache at `$HOME/.sigstore/root` from `os.UserHomeDir()` and reads
no environment variable
([pkg/tuf/options.go](https://github.com/sigstore/sigstore-go/blob/a061819fe7f864aa13c79377dbd01488b572b442/pkg/tuf/options.go#L132-L142));
the client writes under it, one directory per mirror
([pkg/tuf/client.go](https://github.com/sigstore/sigstore-go/blob/a061819fe7f864aa13c79377dbd01488b572b442/pkg/tuf/client.go#L39)).
The older `sigstore/sigstore` TUF client honours `TUF_ROOT`
([pkg/tuf/client.go](https://github.com/sigstore/sigstore/blob/ac7823789dc6a2935c29a76f896606e4474719d9/pkg/tuf/client.go#L563)),
and so does cosign, which is why the variable looks as if it should work here.

**Workaround.** The cache follows `HOME` and nothing else, so every aqua command that
downloads and verifies runs with `HOME` at the sessions' agent home: the installer sets
`LIMEN_AGENT_HOME`, and the `tools`, `fix aqua` and `lint aqua` recipes prefix
`env ${LIMEN_AGENT_HOME:+HOME="$LIMEN_AGENT_HOME"}` (a word an expansion produces is never
an assignment, hence `env`; [identity.md](./identity.md)). The human's `~/.sigstore` left the
sandbox's write list on 2026-10-12 ([farcloser/limen#367](https://github.com/farcloser/limen/pull/367),
[#373](https://github.com/farcloser/limen/pull/373)).

**Lifts when** sigstore-go reads a cache path from the environment, as `sigstore/sigstore`
does with `TUF_ROOT`, or slsa-verifier passes `WithCachePath`. Not reported upstream yet.

## Claude Code's sandbox denies `kern.hv_support`, so Virtualization.framework reports no hardware

**Symptom.** Any Virtualization.framework program run inside a Claude Code sandboxed Bash call
fails at configuration validation with `VZErrorDomain Code=2 "Invalid virtual machine
configuration. Virtualization is not available on this hardware."` (`ossein run`,
2026-10-12), on hardware where the same binary boots fine from a terminal. The unified log
shows no sandbox denial for the process.

**Cause.** The Seatbelt profile the sandbox runtime writes allows `sysctl-read` for a fixed
list of named keys only
([src/sandbox/macos-sandbox-utils.ts](https://github.com/anthropic-experimental/sandbox-runtime/blob/24eb39e94a6de386e83d126c52317d8e0fa49522/src/sandbox/macos-sandbox-utils.ts#L1167)),
and `kern.hv_support` is not among them. Virtualization.framework reads that key to decide
whether the machine supports virtualization, gets `EPERM`, and reports the only thing it can.
Proven from inside the sandbox with `sysctlbyname`: `hw.ncpu` answers, `kern.hv_support` and
`kern.hv_vmm_present` return `EPERM`. The binary's entitlement is not the problem: `ossein`
carries `com.apple.security.virtualization`. The settings expose no knob for sysctls; only
mach services are configurable (`sandbox.network.allowMachLookup`).

**Workaround.** None inside the sandbox. A VM boot is a human-run step: a session builds and
tests, the human types `just kernel`, `just guest-test` or `ossein run` in a terminal. The
former unsandboxed exclusions for these commands were removed on 2026-10-12, because an
exclusion that resolves into content a session can write (a recipe, a built binary, an aqua
package in the agent root) is an escape, not a permission ([identity.md](./identity.md)).

**Lifts when** the sandbox runtime allows reading `kern.hv_support` and `kern.hv_vmm_present`
(both disclose one bit about the hardware) or exposes a configurable sysctl allowlist beside
`allowMachLookup`. Reported to Anthropic by email on 2026-10-12.

## Claude Code's sandbox refuses `git clone` inside a session's root

**Symptom.** `git clone <url> <dir>` run from a session, with `<dir>` under the session's
working directory, dies at once: `fatal: cannot copy
'…/git-core/templates/hooks/commit-msg.sample' to '<dir>/.git/hooks/commit-msg.sample':
Operation not permitted` (2026-10-12). The same clone under `$TMPDIR` succeeds.

**Cause.** The sandbox runtime protects `.git/hooks` and `**/.git/hooks/**` beneath the
working directory for every command, with no switch
([src/sandbox/macos-sandbox-utils.ts](https://github.com/anthropic-experimental/sandbox-runtime/blob/24eb39e94a6de386e83d126c52317d8e0fa49522/src/sandbox/macos-sandbox-utils.ts#L122)).
`git init`, which `clone` runs first, copies the template hook samples into the new
repository's `.git/hooks`, and the first copy is refused. The protection is right for an
existing repository; a repository being created has no hooks anyone will run. An empty
`--template` directory gets past the hooks and dies one step later: `.git/config` beneath
the working directory is protected under the same list unless `allowGitConfig` is set
([L126](https://github.com/anthropic-experimental/sandbox-runtime/blob/24eb39e94a6de386e83d126c52317d8e0fa49522/src/sandbox/macos-sandbox-utils.ts#L126)),
and `git init` cannot write `core.repositoryformatversion` (2026-10-14).

**Workaround.** Clone under `$TMPDIR`, which is outside the root and unprotected, or use the
harness's worktree tool for a second checkout of a repository already present. The contribute
skill names both.

**Lifts when** the runtime lets `git init` create `.git/hooks` with the `*.sample` files, or
distinguishes a new repository from an existing one. Reported to Anthropic by email on
2026-10-12.

## Claude Code's auto-mode classifier judges a command by the denial before it

**Symptom.** In auto mode, right after refusing `gh api -X PUT repos/<o>/<r>/pulls/<n>/update-branch`
as "Merge Without Review", the classifier refused a plain `cat <task output file>` with the
same label; the Read tool on the same file worked (The Project Manager, 2026-10-12). The
same session saw a PR binding refused as "Merge Without Review" until a "Reviewed at <sha>"
comment preceded it, and a poller restart refused as "Auto-Mode Bypass". On 2026-10-15 a
session refused `git c push --force-with-lease=<branch>:<sha> origin <branch>` twice as "Git
Destructive" after an allow rule for exactly that form, `Bash(git c push
--force-with-lease=*)`, had been added to the live user settings; the same bytes passed the
rule in another session started before the rule as well; and the stuck session's next
command, a plain `date`, was refused "Git Destructive" too (Fluffy).

**Cause.** The classifier judges each command with the recent conversation as context, and a
denial colours the commands that follow it: a read of a file carries nothing of a merge, and
a `date` nothing of a push, and each was refused for the company it kept. Not a sandbox rule.
An allow rule resolves before the classifier, by the documentation, and did in one session;
why the other kept refusing the same bytes under the same rule is not known.

**Workaround.** After a classifier denial, do the next read with the Read tool rather than
`cat`, and put the reviewed sha in a comment before any merge-adjacent call. An allow rule
for the routine commands the classifier keeps refusing is Apostasie's call
([identity.md](./identity.md)); a running session that keeps refusing under a verified rule
is reported, with the exact command and label, rather than retried, since every retry adds
to the context the next judgment reads. What clears such a session, a mode toggle or a
restart, is unverified.

**Lifts when** the classifier judges a command on what it does, not on the preceding
denial. Reported to Anthropic by email on 2026-10-12.

## Graphviz's `webp_size` reads a lossless or extended WebP's size from the wrong bytes

**Symptom.** A WebP image with alpha, animation or metadata (VP8X), or a lossless one
(VP8L), used as a node image comes out tens of thousands to hundreds of millions of points a
side; native `dot` 16.1.0 does the same.

**Cause.** `lib/gvc/gvusershape.c`'s `webp_size` reads the size from where only a plain
lossy VP8 chunk keeps it (two 16-bit fields at offset 26), and for VP8X reads 32-bit fields
at 24 where the format has two 24-bit ones; VP8L, whose width and height, 14 bits each,
follow the 0x2f signature at 20, is not recognised at all.

**Workaround.** forkcloser/go-graphviz's `webp-size.patch` (`internal/wasm/build`, applied
to the Graphviz its `pins.yaml` pins) replaces the function with one that reads each
layout's canvas size where the format keeps it.

**Lifts when.** a Graphviz release reads all three layouts; then the patch and this entry
go. Not reported upstream yet (https://github.com/forkcloser/go-graphviz/issues/144).

## Graphviz's `storeline` never zeroes the span it is about to fill

**Symptom.** A label whose first line is empty frees a garbage layout pointer in
`free_textspan`; under wasm that is an indirect call into nowhere and the render aborts.

**Cause.** `lib/common/labels.c`'s `storeline` passes `gv_recalloc` an old count of
`nspans + 1`, one more than existed, so the new span is never zeroed, and an empty line
skips `textspan_size`, the only other thing that nulls the span's layout fields.

**Workaround.** forkcloser/go-graphviz's `build.sh` rewrites the old count to `nspans`, and
refuses to build a Graphviz whose `storeline` no longer matches.

**Lifts when.** a Graphviz release passes the stored count; then the rewrite and this entry
go. Not reported upstream yet (https://github.com/forkcloser/go-graphviz/issues/144).

## Graphviz keeps every image file it sizes open for the life of the process

**Symptom.** An image file sized by Graphviz cannot be deleted while the process lives on a
file system that pins open files (Windows); Graphviz closes sized files only once 50 are
open.

**Cause.** `gvusershape_file_release` closes the file only when the shape is marked
`nocache`; otherwise the handle stays in the usershape cache, though nothing reads it after
sizing (the renderer reads images itself) and `gvusershape_file_access` reopens a closed
file when anything does.

**Workaround.** forkcloser/go-graphviz's `build.sh` rewrites the condition so the release
always closes the file, and refuses to build a Graphviz whose function no longer matches.

**Lifts when.** a Graphviz release closes the file after sizing; then the rewrite and this
entry go. Not reported upstream yet (https://github.com/forkcloser/go-graphviz/issues/144).

## Intel SDE runs AVX-512 natively on a host that has it, and loses the state under Go

**Symptom.** A Go test binary run under `sde64 -spr` (or `-skx`, `-icx`) computes wrong
results at random in its AVX-512 code paths, a different failing case each run, or hangs;
the same binary passes on real AVX-512 hardware and under SDE on a host without AVX-512.
GitHub's ubuntu-24.04 runners are a mix (AMD EPYC 9V74 has `avx512f`, EPYC 7763 does
not), so the failure comes and goes with the host drawn. `-mix` makes it worse and is not
the cause.

**Cause.** SDE executes natively every instruction the host supports and emulates only the
rest. On an AVX-512 host the kernels run natively under Pin, and Pin does not carry their
vector state (ZMM registers, mask registers) across Go's asynchronous preemption signals:
the runs pass with `GODEBUG=asyncpreemptoff=1`.

**Workaround.** `-force_emulate skx` (repeatable; skx holds AVX-512 F/CD/VL/BW/DQ, so a
kernel using icl-or-later sets adds those too): the state then lives in the emulator on
every host. Measured on the AVX-512 host:
eight of eight plain runs fail, nine of nine forced runs pass (forkcloser/blake3's lane,
https://github.com/forkcloser/blake3/pull/137). That the kernels ran is proven from inside
the binary (a required-ISA check), not from SDE's instruction mix.

**Lifts when** an SDE or Pin release preserves AVX-512 state across signals for natively
executed code, or Go stops preempting with signals on amd64. Not reported upstream yet.

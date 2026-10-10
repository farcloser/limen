# Known upstream bugs

Bugs in tools we pin and do not own, each with what it looks like when it bites, why it
happens, what we do about it, and when that workaround can go. An entry stays until its
lift condition is met; the workaround it names is a rule in the code or the recipes, and
whoever removes the rule removes the entry in the same change. The longer stories live
where the rule does, linked from each entry.

## aqua-proxy matches `.exe` case-sensitively (Windows)

**Symptom.** A step whose shell resolves to an aqua link hangs, then prints hundreds of
identical `ERR aqua failed … exe_name=bash.EXE error="exit status 0xffffffff"` lines and
exits `-1`.

**Cause.** aqua-proxy takes its command name from `argv[0]` and strips the extension with
`strings.CutSuffix(cmdName, ".exe")`
([pkg/cli/proxy.go](https://github.com/aquaproj/aqua-proxy/blob/cdf1d68ed0fa4a842ab8c72aac179deba2645c13/pkg/cli/proxy.go#L31)).
The Actions runner resolves a `shell:` program through `PATHEXT`, uppercase by default, so
the proxy is invoked as `bash.EXE` and asks aqua for a command named `bash.EXE`, which no
registry entry provides.

**Workaround.** On Windows, nothing the runner itself resolves is an aqua link: aqua links
no `bash`, `sh` or `cygpath`, and the coreutils come from Git for Windows' own MSYS2
(see [windows: names](./windows.md#names-pathext-case-and-slashes)).

**Lifts when** aqua-proxy compares the extension without regard to case. Not reported
upstream yet.

## aqua skips its own bin directory by string comparison (Windows)

**Symptom.** The same recursion as above: the proxy, asking for a command aqua cannot map,
finds itself.

**Cause.** When aqua falls back to a `PATH` search for a command, it skips its own bin
directory only when the `PATH` element equals `filepath.Join(rootDir, "bin")` as a string
([pkg/controller/which/lookpath_windows.go](https://github.com/aquaproj/aqua/blob/3587e767ce7134b69d68434962d10d60b77fb3d8/pkg/controller/which/lookpath_windows.go#L33)).
setup-aqua writes the root with forward slashes (`cygpath -m`, so native programs read it
as absolute), `filepath.Join` produces backslashes, and the two never match: the first
`bash.EXE` on the search is the proxy itself.

**Workaround.** The one above. Together the two bugs are the recursion; either fix alone
would stop it.

**Lifts when** aqua compares cleaned paths, case-insensitively, in this skip. Not reported
upstream yet.

## lychee retries 429 only, never a 5xx

**Symptom.** The `links` lane fails on one or several legs with a GitHub `503` for a
link that answers `200` a minute later (a `github.com/<owner>/<repo>/blob/...` page,
most often), while the other legs of the same run pass. The lane fails five times a
run, once per verify leg, since every leg checks the same links.

**Cause.** lychee judges a rejected status retryable only when it is `429`
([lychee-lib/src/retry.rs](https://github.com/lycheeverse/lychee/blob/lychee-v0.24.2/lychee-lib/src/retry.rs)),
so `max_retries` and `retry_wait_time` in `.limen/lychee.toml` never apply to a server
error: one `503` blip is a finding. GitHub's web front answers `503` on a blob page for
seconds to minutes at a time.

**Workaround.** The `links` recipe (`.limen/just/security.just`) runs a second pass over
the links that failed with a server error, once, a minute later; a `5xx` that persists past
it is a finding, since a server still erroring after a minute may be gone. The pages that
failed most, our own repositories' files on github.com (AGENTS.md's links to this book),
are excluded in `.limen/lychee.toml`: github.com throttles them from Actions runners, and a
file in a repository we own is checked as a file by that repository's own lane. A blip longer
than the minute still reddens the lane (limen#318's run, three legs, 503 at both
checks), and is rerun by the author, named as this flake.

**Lifts when** lychee retries server errors under its own retry settings. Whether
upstream tracks it is unverified.

## aqua's downloads have no timeout

**Symptom.** A step that runs a tool for the first time on a runner hangs with no
output until the job's `timeout-minutes` cancels it. A rerun passes. Seen on the
erofs Windows leg
(https://github.com/forkcloser/erofs/actions/runs/37509453884/job/112428768665).

**Cause.** A lazy install, the first run of a tool through its aqua link, downloads the
package with Go's `http.DefaultClient`
([pkg/cli/exec/command.go](https://github.com/aquaproj/aqua/blob/01975b3b0d740b5c4876be30a8a99784ad5ad163/pkg/cli/exec/command.go#L87),
[pkg/download/github_release.go](https://github.com/aquaproj/aqua/blob/01975b3b0d740b5c4876be30a8a99784ad5ad163/pkg/download/github_release.go#L67)),
which has no timeout, and nothing on that path sets a deadline. A connection that stalls
mid-body blocks the read forever. The latest aqua release still uses the same client.

**Workaround.** None in aqua's path. Every job in the canonical workflows carries a
`timeout-minutes`, so a stall costs that budget rather than the runner's six hours. The red
it leaves is rerun and named on the pull request: a flake whose cause is known and whose
fix was declined.

**Lifts when** aqua bounds its downloads (a client timeout, or a deadline on a stalled
body). Not reported upstream, by decision.

## MSYS2 bash dies under emulation on windows-11-arm

**Symptom.** A shebang recipe on the windows-11-arm leg fails with `exit code 4` (or
`127`, or `2816`) and nothing on stderr: bash itself died.

**Cause.** Git for Windows' arm64 build pairs a native `git` with an x86-64 MSYS2 userland
run under Windows' x64 emulation. There, roughly once in a few thousand launches, bash
dies at the operating-system level while a child it forked execs a program: a process
substitution (`done < <(git ls-files …)`), or a command substitution
(`scratch=$(mktemp -d …)`). What kills it was never identified. The full record, with the
mechanism, the calibration and a runner canary, is
[design/WINDOWS-ARM-EXIT-4.md](../design/WINDOWS-ARM-EXIT-4.md).

**Workaround.** None that removes it. The shared recipes carry no process substitution,
the shape of every early failure; command substitutions stay, since rewriting them all
for one runner is not worth it, and a silent `4` or `127` on that leg is re-run
([windows: bash dies under emulation](./windows.md#bash-dies-under-emulation)).

**Lifts when** Git for Windows ships its MSYS2 runtime native on arm64. The port is
https://github.com/msys2/msys2-runtime/pull/356, open.

## macOS awk fails on bytes that are not UTF-8

**Symptom.** A recipe that reads arbitrary files with awk on macOS exits 2 with
`awk: towc: multibyte conversion failure on: '…'`, on CI runners and not on a laptop whose
shell runs in the C locale.

**Cause.** macOS's awk, in a UTF-8 locale, converts every input record to wide characters
and aborts on the first byte sequence that is not UTF-8, which every binary file carries.

**Workaround.** An awk that may read binaries runs under `LC_ALL=C` (the shell lint's
shebang scan does); the patterns it matches are ASCII, so nothing it decides changes.

**Lifts when** never, as far as we know: it is how that awk is built. The entry stays as
the reason for the `LC_ALL=C`.

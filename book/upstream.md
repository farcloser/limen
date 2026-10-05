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

# Windows

The canonical CI matrix has two windows legs, x64 and arm64, and both run the shared
recipes under git-bash. Windows is not Linux with backslashes: a recipe there crosses three
kinds of program, two path syntaxes, one emulation layer and two exit-code translations, and
most of what has bitten us is one of those seams. This chapter is what stays true of the
platform; the machine to reproduce on is [VM testing](./vm_testing.md), and the recipe
conventions it feeds are in [the recipes chapter](./recipes.md).

## Three kinds of program

- **Native Windows programs**: `just`, `aqua`, `go`, git proper (`git.exe`), PowerShell, the
  Actions runner. They see Windows paths (`C:\x` and `C:/x` are the same path), resolve
  commands through `PATH` plus `PATHEXT`, and treat file names as case-insensitive.
- **MSYS2 programs**: Git for Windows' userland under `usr\bin` (`bash`, `env`, `cygpath`,
  the coreutils, `tar`). They see a POSIX tree: `/c/x`, `/usr/bin` mounted on `Git\usr\bin`,
  and `Git\bin` mounted as `/bin`, an alias of `/usr/bin`. When one of them starts a native
  program it rewrites arguments that look like POSIX paths into Windows paths, by heuristic,
  and glob-expands a bare `*`. The reverse never happens: a Windows path handed to an MSYS
  program is a string whose backslashes are escapes.
- **The layers between**: the runner, PowerShell and cmd each re-quote arguments by their own
  rules. Never thread shell code through them; hand them a script file
  ([VM testing](./vm_testing.md)).

The rule that follows: **a tool that exchanges paths with the shell is the shell's kind of
program.** A native `mktemp` answers `C:\…\tmp.X`; an MSYS `tar` reads that as `C\:\\…` and
fails with "No such file or directory". So the coreutils pinned on windows are Git for
Windows' own MSYS2 build, never a native rewrite, and git proper stays native: it exchanges
no paths with the shell that the shell did not type.

## Replacing a file that is open

A file another process holds open cannot be replaced with `os.Rename` on windows, whatever
share mode the holder opened it with. Go's `os.Rename` is
`MoveFileEx(MOVEFILE_REPLACE_EXISTING)`, which refuses a target with any open handle:
`rename …: Access is denied.` `FILE_SHARE_DELETE` on the readers' handles lets the file be
deleted or renamed away under them; it does not let another file take its name. Only a
POSIX-semantics rename does that: `SetFileInformationByHandle` with `FileRenameInfoEx` and
`FILE_RENAME_FLAG_REPLACE_IF_EXISTS | FILE_RENAME_FLAG_POSIX_SEMANTICS`, on NTFS from Windows
10 1709 on. Go uses it inside `os.Root` and nowhere else, so a write-to-temp-then-rename
built on `os.Rename` is atomic against readers on every platform but windows.

Measured on the VM (https://github.com/mycophonic/primordium/pull/152): four goroutines
reading a file in a loop through share-delete handles while a writer renamed a temp file
over it 200 times. With `os.Rename`, 2 to 10 of 200 writes landed; with the POSIX rename,
200 of 200. No reader saw a torn file either way. The fleet's primitive is primordium's
`xos.Rename` (https://github.com/mycophonic/primordium/pull/168): the POSIX rename on a
volume that has it, `os.Rename` where the request itself is refused (FAT, exFAT, some
shares, older Windows), and `os.Rename` for a directory source, since the POSIX rename would
replace an empty directory in its way. Its `WriteFile` renames through it. A consumer that
replaces a file readers may hold open does it with `xos.Rename`, never with `os.Rename`.

An error built with `errors.Join` prints its causes on following lines, so a log filtered to
one line shows the outer sentinel and never the cause. Read the lines after it.

## What runs a recipe

A `shell: bash` step starts the system git-bash launcher, which prepends its own
`/clangarm64/bin:/usr/bin` (or `/mingw64/bin:/usr/bin`) to `PATH`, then runs the step. From
there a `just` recipe with a `#!/usr/bin/env bash` shebang goes: aqua proxy, aqua, `just`
(native), `cygpath` (to translate `/usr/bin/env`), `env` (an MSYS exec stub that waits for
its child), `usr\bin\bash.exe` (the recipe body). Three things follow.

- **`Git\bin` is not a way to the launcher.** `Git\bin` is `/bin` to MSYS and `/bin` is
  `/usr/bin`, so with `Git\bin` first on the Windows `PATH`, `env` resolves `bash` to the
  system `usr\bin\bash.exe`, never to the `bin\bash.exe` launcher. The launcher runs only
  when a native parent (cmd, PowerShell, the runner) resolves `bash` itself.
- **The aqua bin must precede every Git directory** for a pinned tool to shadow the system
  one, `Git\bin` included. Inside a `shell: bash` step the launcher has already put the
  system `/usr/bin` ahead of everything the step inherited; the pins win inside recipes
  because the root `.justfile` prepends the aqua directory when it builds the recipe `PATH`,
  and a step that calls a pinned tool directly, outside `just`, prepends it itself. A `PATH`
  where the system `Git\cmd` is absent but an aqua `git` link is present runs the system
  bash with a pinned git: a mixed state, avoid it.
- **The shell is never pinned through aqua.** An aqua link named `bash` or `sh` becomes the
  shell of every `shell: bash` step, because the runner resolves `bash` through the same
  `PATH`, and the proxy cannot serve as one (next section). Git for Windows' own bash is the
  shell on windows; the pins are the tools it calls.

On windows the hermetic `PATH` is the one sanctioned exception to full hermeticity: the pins
are prepended to the ambient `PATH` rather than replacing it, because there is no knowable
list of base-system directories to keep. The pins still shadow everything; the POSIX legs
keep full hermeticity enforced.

## Names: `PATHEXT`, case and slashes

The runner resolves the program of a `shell:` line by walking `PATH` and, for a name without
extension, appending each `PATHEXT` entry as `PATHEXT` spells it (`.COM;.EXE;…`, uppercase
by default) and keeping the first that exists. The `PATH` element is used verbatim and
joined with a backslash, so a step's shell is spelled
`C:/Users/…/aquaproj-aqua/bin\bash.EXE`: both separators in one string, and an extension in
a case nobody wrote. Windows accepts it; a program that treats that string as text does not.

Two of our tools did. aqua's proxy strips `.exe` from `argv[0]` case-sensitively, so it
asked aqua for `bash.EXE`; aqua matched nothing, fell back to a `PATH` search, and skipped
its own bin directory only when the `PATH` element equalled `filepath.Join(root, "bin")` as
a string, backslashes against the forward slashes it was written with. The first `bash.EXE`
it found was the proxy itself. Proxy ran aqua ran proxy until Windows refused to create
another process; the innermost proxy exited `-1` without a message, and every aqua above it
logged the same line once on the way up:

```
ERR aqua failed … exe_name=bash.EXE error="exit status 0xffffffff"
```

Hundreds of identical lines after a long silence, then `Process completed with exit code
-1`: that is the shape; the depth is the machine's memory, which is why two legs show
different counts. The record, with the reproduction, is
https://github.com/farcloser/limen/pull/205. Two rules come out of it:

- **An aqua link is invoked only by a name that matches its registry entry.** Lowercase,
  with or without `.exe`. Nothing the runner itself resolves (`shell:` programs) may be an
  aqua link on windows, and a stale `bash.exe` or `sh.exe` left in an aqua bin by an earlier
  configuration brings the trigger back on a reused machine.
- **Paths are compared cleaned, never as strings; extensions are matched without case.** In
  our own Go: `filepath.Clean` on both sides (or `filepath.Abs`) before `==`, and
  `strings.EqualFold` for a suffix check, never `strings.CutSuffix(name, ".exe")`. Where an
  upstream tool gets this wrong, the workaround is to feed it one spelling, never to depend
  on which it prefers. The two aqua bugs are in [known upstream bugs](./upstream.md).

What a program sees as `argv[0]` depends on its parent. The runner and .NET's
`ProcessStartInfo` put the file name on the command line verbatim; PowerShell's
`Start-Process` resolves it through command discovery first and may normalise case and
separators; cmd passes what was typed. A reproduction of a name-handling bug uses a parent
that preserves the spelling under test.

## Architecture on windows-11-arm

Git for Windows' arm64 build pairs a native arm64 `git.exe` with an **x86-64** MSYS2
userland, run under Windows' x64 emulation: `bash`, `env`, `cygpath`, the coreutils and
`msys-2.0.dll` are all x86-64 there, and `uname -m` inside git-bash says `x86_64` on an
arm64 machine (https://www.msys2.org/docs/arm64/; the native port is
https://github.com/msys2/msys2-runtime/pull/356). What aqua installs for `windows/arm64`,
where a package ships an arm64 asset, and `just` and `go`, are native. So on that leg every
recipe body runs emulated and every tool it calls runs native, with the emulation boundary
crossed on each fork and exec: bash dies under emulation (below), such a death reads
strangely in a log (below), and an emulated fork is several times slower than a native one,
which is one reason the arm64 leg is the slow one.

`%PROCESSOR_ARCHITECTURE%` lies under emulation (an x64 process on an arm64 machine reads
`AMD64`); the machine's own value is the `PROCESSOR_ARCHITECTURE` under
`HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, and
`[Runtime.InteropServices.RuntimeInformation]::OSArchitecture` in PowerShell.

## Bash dies under emulation

Under emulation, bash dies raw, at the operating-system level and without a word, when it
forks a child that execs an x86-64 MSYS program. On the runners it shows as `recipe X failed
with exit code 4` (or `127`) and nothing else. First pinned on the process substitution
(`done < <(git ls-files -z …)` dies; `< <(printf …)`, whose child execs nothing, does not),
but the trigger is the fork and exec, and every command substitution that runs a program has
the same shape: `lint links` died at `scratch=$(mktemp -d …)` with no process substitution
in it (https://github.com/farcloser/limen/actions/runs/37266312458/job/111623939333). A
sample of a thousand launches is too small to see a rate of one in a few thousand.

The shared recipes carry dozens of command substitutions, and rewriting them all for one
emulated runner is not worth what it does to the scripts. So the failure is accepted:

- **No `< <(…)` and no `>(…)`.** The substitution is the shape every early failure had, and a
  producer's exit status now fails the recipe under `set -e` instead of vanishing inside the
  substitution. The audit is `grep -nE '< <\(|>\(' .limen/just/*.just`, empty.
- **Command substitutions, pipelines and temp files stay.** They lower the rate; none is
  proven to bring it to zero.
- **A silent `exit code 4` or `127` on the windows-11-arm leg is re-run, not debugged**
  ([reading an exit code](#reading-an-exit-code) says why it means bash died).

The record, with the mechanism, the calibration, the reproduction on a guest and the canary,
is [design/WINDOWS-ARM-EXIT-4.md](../design/WINDOWS-ARM-EXIT-4.md). It lifts when Git for
Windows ships its MSYS2 runtime native on arm64.

## Reading an exit code

A Windows exit code is a 32-bit number, and a process that crashes exits with the NTSTATUS
of its exception, at or above `0xC0000000`. Two kinds of parent translate it on the way to
a log.

**An MSYS parent** (bash waiting for a child, or the `env` stub waiting for bash) keeps the
low byte of a code below `0xC0000000` and turns an NTSTATUS into a signal: access violation
to SIGSEGV, illegal instruction to SIGILL, no memory to SIGBUS, control-C to SIGINT, every
other error into a plain `127`, silently. `just` prints the number it is handed with no
signal decoding, as `256 × signal`:

| The recipe's bash | `just` reports |
|---|---|
| `exit 4` | 4 |
| `exit 260` | 4 (the low byte) |
| killed by SIGSEGV, or died with STATUS_ACCESS_VIOLATION | 2816 (256 × 11) |
| killed by SIGKILL | 2304 (256 × 9) |
| died with a code below `0xC0000000` whose low byte is 4 (`0x40010004`, DBG_TERMINATE_PROCESS) | 4 |
| died with any other NTSTATUS (stack overflow, fail-fast, divide by zero) | 127 |
| a native child's `Exit(0x40010004)` under `set -e` | 4, and bash saw `$?` = 4 |

So a silent `exit code 4` or `127` from a shebang recipe on windows-11-arm means bash itself
died: the recipes cannot produce a silent 4, and bash's own 127 ("command not found")
always prints. `2816` is SIGSEGV, caught by the runtime inside bash or read from a raw access
violation; the number alone cannot tell which.

**A native parent** sees the raw number. Go's `os/exec` prints codes of 65536 and above in
hex, `exit status 0xffffffff`; the runner prints the same code as `-1`; and on Windows Go's
`ProcessState.ExitCode()` returns `int(uint32)`, so a `-1` exit arrives as `4294967295`: a
check for `== -1` meaning "the process never started" is dead code there. A program that
exits with that number without printing the error, as aqua's proxy does, leaves a log with
no innermost cause.

## Caches on the Windows runners

On a Windows runner, restoring the Go build and module caches, aqua's packages and the
linter cache as files means writing over 100,000 small files, most of a leg's time (170 to
350 s on windows-11-arm). The canonical CI workflow keeps them in one NTFS disk image,
cached as a single file and attached at `C:\vcache` (`.github/actions/windows-cache-image`):
download, decompression and attach take under a minute. A miss creates an empty image, and
the run fills and saves it.

The image is excluded from Defender's real-time scan for the job: without the exclusion
every file in it is scanned on first read, and lint gives back what the restore saved.
Measured on windows-11-arm, three runs each in one workflow run: 442 s with the file caches,
359 s with the image, 275 s with the image and the exclusion.

Tried and dropped, each slower or within the noise: Windows' own `tar` for the restore
(native, but fed by an emulated `zstd`), no Go cache, no aqua cache, caching only the module
zips, a Dev Drive (ReFS) at the cache paths.

## Line endings and sessions

- Line endings: windows machines default git to `core.autocrlf=true`. The canonical
  `.gitattributes` ([mandatory files](./mandatory-files.md#canonical-gitattributes))
  disables the conversion, and the format linters fail on CRLF that sneaks through.
- Sessions: a process started by a service, a scheduled task with a service principal, or
  UTM's guest agent runs as `SYSTEM` in session 0, with no interactive user's `PATH`, no
  per-user applications, and a smaller desktop heap. A scheduled task with an interactive
  logon type registers but does not run until that user logs on. The runners run jobs as
  `runneradmin`. When a behaviour depends on the session (a per-user install, a console, a
  desktop-heap limit), reproduce it in the same kind of session, and say which in the record.

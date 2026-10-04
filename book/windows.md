# Windows

The canonical CI matrix has two windows legs, x64 and arm64, and both run the same
shared recipes under git-bash. Windows is not Linux with backslashes: a recipe there
crosses three kinds of program, two path syntaxes, one emulation layer and two
exit-code translations, and most of what has bitten us is one of those seams. This
chapter records what stays true of the platform; the machine to reproduce on is
[VM testing](./vm_testing.md), and the recipe conventions it feeds are in
[the recipes chapter](./recipes.md).

## Three kinds of program

- **Native Windows programs**: `just`, `aqua`, `go`, git proper (`git.exe`), PowerShell,
  the Actions runner. They see Windows paths (`C:\x` and `C:/x` are the same path),
  resolve commands through `PATH` plus `PATHEXT`, and treat file names as
  case-insensitive.
- **MSYS2 programs**: Git for Windows' userland under `usr\bin` (`bash`, `env`,
  `cygpath`, the coreutils, `tar`). They see a POSIX tree: `/c/x`, `/usr/bin` mounted on
  `Git\usr\bin`, and `Git\bin` mounted as `/bin`, which is itself an alias of `/usr/bin`.
  When one of them starts a native program it rewrites arguments that look like POSIX
  paths into Windows paths, by heuristic, and glob-expands a bare `*`. The reverse never
  happens: a Windows path handed to an MSYS program is a string whose backslashes are
  escapes.
- **The layers between**: the runner, PowerShell and cmd each re-quote arguments by
  their own rules. [VM testing](./vm_testing.md) has the trap and the cure: never thread
  shell code through them, hand them a script file.

The rule that follows: **a tool that exchanges paths with the shell is the shell's kind
of program.** A native `mktemp` answers `C:\…\tmp.X`; an MSYS `tar` reads that as
`C\:\\…` and fails with "No such file or directory". That is why the coreutils pinned on
windows are Git for Windows' own MSYS2 build, never a native rewrite, and why git proper
stays native: it exchanges no paths with the shell that the shell did not type.

## What runs a recipe

A `shell: bash` step starts the system git-bash launcher, which prepends its own
`/clangarm64/bin:/usr/bin` (or `/mingw64/bin:/usr/bin`) to `PATH`, then runs the step.
From there a `just` recipe with a `#!/usr/bin/env bash` shebang goes: aqua proxy, aqua,
`just` (native), `cygpath` (to translate `/usr/bin/env`), `env` (an MSYS exec stub that
waits for its child), `usr\bin\bash.exe` (the recipe body). Three things follow.

- **`Git\bin` is not a way to the launcher.** `Git\bin` is `/bin` to MSYS and `/bin` is
  `/usr/bin`, so with `Git\bin` first on the Windows `PATH`, `env` resolves `bash` to the
  system `usr\bin\bash.exe`, never to the `bin\bash.exe` launcher. The launcher runs
  only when a native parent (cmd, PowerShell, the runner) resolves `bash` itself.
- **The aqua bin must precede every Git directory** for a pinned tool to shadow the
  system one, `Git\bin` included. Inside a `shell: bash` step the launcher has already
  put the system `/usr/bin` ahead of everything the step inherited, the aqua bin
  included; the pins win inside recipes because the root `.justfile` prepends the aqua
  directory when it builds the recipe `PATH`, and a step that calls a pinned tool
  directly, outside `just`, prepends it itself. A `PATH` where the system `Git\cmd` is
  absent but an aqua `git` link is present runs the system bash with a pinned git: a
  mixed state, avoid it.
- **The shell is never pinned through aqua.** An aqua link named `bash` or `sh` becomes
  the shell of every `shell: bash` step, because the runner resolves `bash` through the
  same `PATH`, and the proxy cannot serve as one (the next section). Git for Windows'
  own bash is the shell on windows; the pins are the tools it calls.

On windows the hermetic `PATH` is the one sanctioned exception to full hermeticity: the
pins are prepended to the ambient `PATH` rather than replacing it, because there is no
knowable list of base-system directories to keep. The pins still shadow everything;
the POSIX legs keep full hermeticity enforced.

## Names: `PATHEXT`, case and slashes

The runner resolves the program of a `shell:` line by walking `PATH` and, for a name
without extension, appending each `PATHEXT` entry as `PATHEXT` spells it (`.COM;.EXE;…`,
uppercase by default) and keeping the first that exists. The `PATH` element is used
verbatim and joined with a backslash, so a step's shell is spelled
`C:/Users/…/aquaproj-aqua/bin\bash.EXE`: both separators in one string, and an extension
in a case nobody wrote. Windows accepts it; a program that treats that string as text
does not.

Two of our tools did, and together they produced a failure worth knowing by its
signature. aqua's proxy takes its command name from `argv[0]` and strips `.exe`
case-sensitively, so it asked aqua for `bash.EXE`; aqua matches a command against the
registry's file names exactly, found nothing, fell back to a `PATH` search, and skipped
its own bin directory only when the `PATH` element equalled `filepath.Join(root, "bin")`
as a string, backslashes against the forward slashes the element was written with. The
first `bash.EXE` it found was the proxy itself. Proxy ran aqua ran proxy, about forty
milliseconds and ten megabytes of commit per level, until Windows refused to create
another process; the innermost proxy then exited `-1` without a message, and every aqua
above it logged the same line once on the way up:

```
ERR aqua failed … exe_name=bash.EXE error="exit status 0xffffffff"
```

Hundreds of identical lines after a long silence, then `Process completed with exit
code -1`: that is the shape. The depth is the machine's memory, which is why two legs
show different counts. The record, with the reproduction, is
https://github.com/farcloser/limen/pull/205. Two rules come out of it:

- **An aqua link is invoked only by a name that matches its registry entry.** Lowercase,
  with or without `.exe`. Nothing the runner itself resolves (`shell:` programs) may be
  an aqua link on windows, and a stale `bash.exe` or `sh.exe` left in an aqua bin by an
  earlier configuration brings the trigger back on a reused machine.
- **Paths are compared cleaned, never as strings; extensions are matched without case.**
  In our own Go this is `filepath.Clean` on both sides (or `filepath.Abs`) before `==`,
  and `strings.EqualFold` for a suffix check, never `strings.CutSuffix(name, ".exe")`.
  Where an upstream tool gets this wrong, the workaround is to feed it one spelling,
  never to depend on which spelling it prefers.

What a program sees as `argv[0]` depends on its parent. The runner and .NET's
`ProcessStartInfo` put the file name on the command line verbatim; PowerShell's
`Start-Process` resolves it through command discovery first and may normalise case and
separators; cmd passes what was typed. A reproduction of a name-handling bug has to use
a parent that preserves the spelling under test.

## Architecture on windows-11-arm

Git for Windows' arm64 build pairs a native arm64 `git.exe` with an **x86-64** MSYS2
userland, run under Windows' x64 emulation: `bash`, `env`, `cygpath`, the coreutils and
`msys-2.0.dll` are all x86-64 there, and `uname -m` inside git-bash says `x86_64` on an
arm64 machine. MSYS2 documents the state at https://www.msys2.org/docs/arm64/; the
native port is tracked at https://github.com/msys2/msys2-runtime/pull/356. What aqua
installs for `windows/arm64`, where a package ships an arm64 asset, and `just` and `go`,
are native arm64. So on that leg every recipe body runs emulated and every tool it calls
runs native, with the emulation boundary crossed on each fork and exec. Two consequences
are documented below: the process-substitution rule, and how a death under emulation reads
in a log. A third is cost: an emulated bash fork is several times slower than a native
one, which is one reason the arm64 leg is the slow one.

`%PROCESSOR_ARCHITECTURE%` lies under emulation (an x64 process on an arm64 machine
reads `AMD64`); the machine's own value is the `PROCESSOR_ARCHITECTURE` under
`HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, and
`[Runtime.InteropServices.RuntimeInformation]::OSArchitecture` in PowerShell.

## No process substitution in shared recipes

Under emulation, bash dies raw, at the operating-system level and without a word,
roughly once in a few thousand launches of a process substitution whose child execs a
program: `done < <(git ls-files -z …)` and `done < <(cat list)` both die, `< <(printf …)`
does not, and neither does a pipeline (`git ls-files -z … | while read …`), a temp file
written by the producer and read by the loop, or an ordinary `$(…)`. On the runners it
showed as `recipe X failed with exit code 4` with nothing else, an estimated once per
hundred recipe launches on windows-11-arm; it needed concurrency to reproduce on a guest
and reproduced once in two thousand launches on a runner canary. The rule for the
shared recipes:

- **No `< <(…)` and no `>(…)`.** The audit is `grep -nE '< <\(|>\(' .limen/just/*.just`,
  empty. A producer's exit status now fails the recipe under `set -e` instead of
  vanishing inside the substitution, which is an improvement on its own.
- Pipelines, waited-for native children and command substitutions are fine.

The record, with the mechanism, the calibration and the canary, is
https://github.com/farcloser/limen/pull/192 and its design document. The rule lifts
when Git for Windows ships its MSYS2 runtime native on arm64.

## Reading an exit code

A Windows exit code is a 32-bit number, and a process that crashes exits with the
NTSTATUS of its exception, at or above `0xC0000000`. Two kinds of parent translate it on
the way to a log.

**An MSYS parent** (bash waiting for a child, or the `env` exec stub waiting for bash)
keeps the low byte of a code below `0xC0000000` and turns an NTSTATUS into a signal:
access violation to SIGSEGV, illegal instruction to SIGILL, no memory to SIGBUS,
control-C to SIGINT, and every other error into a plain `127`, silently. `just` prints
the number it is handed with no signal decoding, as `256 × signal`:

| The recipe's bash | `just` reports |
|---|---|
| `exit 4` | 4 |
| `exit 260` | 4 (the low byte) |
| killed by SIGSEGV, or died with STATUS_ACCESS_VIOLATION | 2816 (256 × 11) |
| killed by SIGKILL | 2304 (256 × 9) |
| died with a code below `0xC0000000` whose low byte is 4 (`0x40010004`, DBG_TERMINATE_PROCESS) | 4 |
| died with any other NTSTATUS (stack overflow, fail-fast, divide by zero) | 127 |
| a native child's `Exit(0x40010004)` under `set -e` | 4, and bash saw `$?` = 4 |

So a silent `exit code 4` or `exit code 127` from a shebang recipe on windows-11-arm
means bash itself died; the recipes cannot produce a silent 4, and bash's own 127
("command not found") always prints. `2816` is SIGSEGV, caught by the runtime inside
bash or read from a raw access violation; the number alone cannot tell which.

**A native parent** sees the raw number. Go's `os/exec` prints codes of 65536 and above
in hex, `exit status 0xffffffff`; the runner prints the same code as `-1`; and on
Windows Go's `ProcessState.ExitCode()` returns `int(uint32)`, so a `-1` exit arrives as
`4294967295`: a check for `== -1` that means "the process never started" is dead code
there. A Go program whose `cmd.Run()` fails before a process exists has a nil
`ProcessState`, whose `ExitCode()` is `-1`; a program that exits with that number
without printing the error, as aqua's proxy does, leaves a log with no innermost cause.

## Line endings and sessions

- Line endings: windows machines default git to `core.autocrlf=true`. The canonical
  `.gitattributes` in [mandatory files](./mandatory-files.md#canonical-gitattributes)
  disables the conversion, and the format linters fail on CRLF that sneaks through.
- Sessions: a process started by a service, a scheduled task with a service principal,
  or UTM's guest agent runs as `SYSTEM` in session 0, with no interactive user's `PATH`,
  no per-user applications, and a smaller desktop heap. A scheduled task with an
  interactive logon type registers but does not run until that user logs on. The runners
  run jobs as `runneradmin`. When a windows behaviour depends on the session (a per-user
  install, a console, a desktop-heap limit), reproduce it in the same kind of session,
  and say which one in the record.

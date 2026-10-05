# WINDOWS-ARM-EXIT-4 — bash dies raw under x86_64 emulation on windows-11-arm

Status: **trigger narrowed, not removed: a forked child that execs an MSYS program;
runner-side terminator unverified.** Revised 2026-10-05, when a recipe with no process
substitution died the same way (section 9). Investigated 2026-09-29 to 2026-10-01 for
https://github.com/farcloser/limen/pull/192. Every claim below is marked
verified (V) or unverified (U). Evidence: eleven failed CI jobs (all logs read), the
sources of just 1.58.0, msys2-runtime 3.6.x, bash 5.3, Git for Windows' launcher, and
the runner image scripts, about 70,000 recipe launches on a UTM Windows 11 arm64
guest, and one 6,000-launch canary job on a windows-11-arm runner (section 7).

## 1. Symptom

A `verify (windows-11-arm)` job fails in a lint recipe with one line and nothing else:

```
▶ lint: shell
error: recipe `shell` failed with exit code 4
##[error]Process completed with exit code 4.
```

The code is 4 in all eleven lint jobs below and 127 in the one canary death (section
7); both read the same way. No stderr from the recipe body. No `fork:` message. No
ERR-trap output where a trap existed (ossein's `lint dockerfile` carried one at the
failing commit, verified in that repository's `.limen/just/lint.just` at 4ad0274f).
bash's own 127, "command not found", always prints a message; a silent 127 is not
bash's. The same commit passes on rerun and on every other platform. (V, from the
logs below and the canary artifact.)

Failing jobs known at the time of writing, all `verify (windows-11-arm)`:

| Job | Date | Recipe | Banner to failure |
|---|---|---|---|
| https://github.com/farcloser/limen/actions/runs/30760723032/job/91530607692 | 2026-08-02 | `dockerfile` | 0.87 s |
| https://github.com/farcloser/limen/actions/runs/31934294436/job/95133731509 | 2026-08-16 | `shell` | 0.77 s |
| https://github.com/farcloser/ossein-kernel/actions/runs/34048123199/job/101526778215 | 2026-09-06 | `dockerfile` | 2.4 s |
| https://github.com/farcloser/limen/actions/runs/34738713883/job/103674659220 | 2026-09-13 | `shell` | 0.80 s |
| https://github.com/forkcloser/xz/actions/runs/35175399273/job/105055953562 | 2026-09-17 | `dockerfile` | 7.6 s |
| https://github.com/forkcloser/dot/actions/runs/35489438120/job/106022264314 | 2026-09-20 | `just` | 1.5 s |
| https://github.com/forkcloser/xz/actions/runs/35807977137/job/107012994615 | 2026-09-23 | `dockerfile` | 7.6 s |
| https://github.com/farcloser/ossein-kernel/actions/runs/36176015596/job/108207267522 | 2026-09-25 | `shell` | 0.84 s |
| https://github.com/forkcloser/godolint/actions/runs/36486203210/job/109143584013 | 2026-09-28 | `just` | 0.83 s |
| https://github.com/forkcloser/godolint/actions/runs/36515367786/job/109236338189 | 2026-09-29 | `just` | 1.3 s |
| https://github.com/farcloser/ossein/actions/runs/36614821830/job/109565036868 | 2026-09-29 | `dockerfile` | 2.0 s |

"Banner to failure" is the gap between the `▶ lint: X` line and the error line in the
job log. (V) A passing `lint shell` takes 5 to 8 s on the same runners (V, same logs:
5.5 s in the ossein job above). The runner images involved: `windows-11-arm64`
20260727.122.1, 20260809.134.1, 20260830.155.1, 20260906.161.1, 20260914.169.1,
20260920.174.1, and `windows-11-vs2026-arm64` 20260920.164.1. (V) Only `lint just`,
`lint shell` and `lint dockerfile` have failed this way. (V, all eleven logs.) The
rate is about one failure per hundred recipe launches on
windows-11-arm. (U, an estimate: eleven failures over roughly three hundred arm64
jobs with three candidate recipes each.)

## 2. Verdict and mechanism

The recipe body's bash process dies at the operating-system level, outside the MSYS2
runtime's own exit and signal handling, while running a process substitution whose
forked child execs the native arm64 `git` and streams to the parent concurrently. The
runtime is x86_64 and runs emulated on the arm64 runner. The parent that waits for
bash, `env`'s exec stub, reads bash's Windows exit code and maps it with one runtime
function: a code below 0xC0000000 becomes its low byte (0x40010004 gives 4); an
NTSTATUS error at or above 0xC0000000 becomes a signal for five named cases
(access violation gives 2816) and 127, silently, for every other one. A process that
dies this way writes nothing. The three codes seen, 4, 127 and 2816, are the same
death with three different NTSTATUS values recorded.

### 2.1 The runtime is emulated (V)

Git for Windows' arm64 installer ships a native arm64 `git.exe` and an **x86_64**
MSYS2 runtime: `usr\bin\bash.exe`, `usr\bin\env.exe`, `usr\bin\cygpath.exe`,
`usr\bin\msys-2.0.dll` and `bin\bash.exe` are x86-64 PE files. Checked with `file` on
the guest's package (Git 2.55.0.windows.2 arm64, bash 5.3.15(1)-release
`x86_64-pc-cygwin`, `uname -r` 3.6.9-b4195d69.x86_64). MSYS2 documents that its arm64
installer contains the x86_64 build: https://www.msys2.org/docs/arm64/. The runner
image installs the arm64 Git for Windows asset
(https://github.com/actions/runner-images/blob/main/images/windows/scripts/build/Install-Git.ps1,
the `Test-IsArm64` branch) and prepends `C:\Program Files\Git\bin` to the machine PATH
(`Add-MachinePathItem`, defined in `images/windows/scripts/helpers/PathHelpers.ps1`).
The canary job recorded the runner's versions: Git 2.55.0.windows.5, bash
5.3.15(2)-release, msys2-runtime 3.6.10-710e5275.x86_64, Windows 10.0.26200.9457.
(V, section 7.) The lint jobs' logs do not record them. (U for those jobs.)

### 2.2 The launch chain (V from sources, the canary's environment record and the guest)

```
git-bash step (shell: bash) → aqua proxy → aqua → just.exe (native arm64)
   → cygpath.exe        x86_64 MSYS, emulated   translates /usr/bin/env
   → env.exe            x86_64 MSYS, emulated   exec stub; PATH lookup gives /usr/bin/bash
   → usr\bin\bash.exe   x86_64 MSYS, emulated   the recipe body
        └ fork()  → child bash.exe             the process substitution
              └ exec git.exe (native arm64)    writes the NUL-separated list
        parent bash reads the pipe through /dev/fd/N, concurrently
```

Git for Windows' launcher, `bin\bash.exe`, is not in the chain. `ci.yaml` sets
`defaults.run.shell: bash`, so every step already runs inside the system git-bash,
whose PATH starts with `/clangarm64/bin:/usr/bin`; the canary's environment record
shows `command -v bash` = `/usr/bin/bash` and `command -v env` = `/usr/bin/env` (V).
PATH order cannot change that: inside MSYS, `C:\Program Files\Git\bin` is the `/bin`
mount point, and `/bin` is mounted on `usr/bin` (`mount` on the guest prints
"C:/Program Files/Git/usr/bin on /bin"), so `env` resolves `bash` to
`usr\bin\bash.exe` even with `Git\bin` first on the Windows PATH. Checked on the guest
with `C:\Program Files\Git\bin` first: `BASH=/bin/bash`, executable
`C:\Program Files\Git\usr\bin\bash.exe`, `MSYSTEM` unset (V). The launcher runs only
when a native parent such as cmd.exe or PowerShell resolves `bash` itself.

Where each step lives:

- just 1.58.0, `src/platform/windows.rs`, `Platform::make_shebang_command`: an
  interpreter path containing `/` goes through `cygpath --windows`; the result is run
  with the recipe's temp file as argument. `src/recipe.rs`, `Recipe::run_shell`, waits
  for it. `src/error.rs` formats "recipe `X` failed with exit code N" from the raw
  Windows exit code; just does no signal decoding on Windows
  (`Platform::signal_from_exit_status` returns none).
- msys2-runtime, `winsup/cygwin/spawn.cc`, `child_info_spawn::worker`: `env`'s exec
  of `bash` leaves `env.exe` behind as a stub that waits for the child and relays its
  status (`wait_for_myself`). `winsup/cygwin/pinfo.cc`,
  `pinfo::maybe_set_exit_code_from_windows`: when the child left no exit record of its
  own, the stub takes the child's Windows exit code and passes it to
  `pinfo::set_exit_code`, which keeps the low byte of a code below 0xC0000000 and
  sends anything at or above 0xC0000000 to `pinfo::status_exit`. That function maps
  STATUS_ACCESS_VIOLATION to SIGSEGV, STATUS_ILLEGAL_INSTRUCTION to SIGILL,
  STATUS_NO_MEMORY to SIGBUS, STATUS_CONTROL_C_EXIT to SIGINT, STATUS_DLL_NOT_FOUND to
  127 with a message on stderr, and every other value to 127 with only a debug trace.
  `winsup/cygwin/exceptions.cc` (`exception::handle`) is the other route to 2816: an
  exception the runtime catches inside bash and turns into a signal. (V)
- bash 5.3, `subst.c`, `process_substitute`: forks, the child redirects to the pipe
  and runs the command; the parent gets a `/dev/fd/N` name. `shell.h` lists bash's own
  `EX_*` exit statuses; none is 4. `execute_cmd.c` and `shell.c` exit 126 or 127 on
  exec failure; the fork-failure path prints `fork: retry` first. bash has no path
  that exits 4 silently. (V, searched the 5.3 tree.)

### 2.3 Exit-code calibration through the chain (V, measured on the guest)

Recipes launched as `just --justfile <file> <recipe>` from cmd.exe with
`C:\Program Files\Git\usr\bin` on PATH, the value being what just printed. The
outside terminations use `TerminateProcess` on the recipe's bash.exe from a separate
PowerShell process, with just's exit code read by that process (section 6.2).

| What happens to the recipe's bash | just reports |
|---|---|
| `exit 4` | 4 |
| `exit 260` | 4 (mod 256) |
| `kill -ILL $$` | 1024 (256 × 4) |
| `kill -SEGV $$` | 2816 (256 × 11) |
| `kill -KILL $$` | 2304 (256 × 9) |
| process-substitution child does `exit 4`, parent continues | 0 |
| native child `[Environment]::Exit(4)` under `set -e` | 4, bash saw `$?` = 4 |
| native child `[Environment]::Exit(0x40010004)` under `set -e` | 4, bash saw `$?` = 4 |
| native child `[Environment]::FailFast` (exit code 0x80131623) | bash saw `$?` = 35, the low byte |
| terminated from outside with code 4 | 4 |
| terminated from outside with 0x40010004 (DBG_TERMINATE_PROCESS) | 4 |
| terminated from outside with 0xC0000005 (STATUS_ACCESS_VIOLATION) | 2816 |
| terminated from outside with 0xC00000FD (STATUS_STACK_OVERFLOW) | 127 |
| terminated from outside with 0xC0000409 (STATUS_STACK_BUFFER_OVERRUN) | 127 |
| terminated from outside with 0xC0000094 (STATUS_INTEGER_DIVIDE_BY_ZERO) | 127 |

Every row matches `pinfo::set_exit_code` and `pinfo::status_exit` in
`winsup/cygwin/pinfo.cc` (section 2.2). Without just, `bash -c 'exit 4'` gives
errorlevel 4 and `env bash -c 'kill -KILL $$'` gives 2304, so the arithmetic is the
runtime's, not just's. (V) Consequences: a silent `exit code 4` means bash.exe died
with a Windows exit code below 0xC0000000 whose low byte is 4, 0x40010004 being the
standard such code; a silent `exit code 127` means an NTSTATUS error outside the five
named cases; `exit code 2816` means SIGSEGV, either caught by the runtime inside bash
or read from a raw death with STATUS_ACCESS_VIOLATION, which the code alone cannot
tell apart. (V for the mapping, U for which NTSTATUS the runner recorded.)

## 3. Evidence, with numbers

All guest runs: UTM Windows 11 arm64 guest, Git for Windows 2.55.0 arm64, just 1.58.0,
`C:\Program Files\Git\bin` first on the Windows PATH as on the runner image (which, per
section 2.2, still gives `usr\bin\bash.exe` through `env`), cmd.exe loops started as
scheduled tasks in the interactive user's session, cwd a small git repository. Guest build 10.0.26200.9168 during the 2026-09-29 runs;
10.0.26200.9457 when read on 2026-10-01 after the 2026-09-30 run (the guest rebooted
between the two campaigns; whether the update applied before the second run started
was not checked). (V for the readings, U for the ordering.)

### 3.1 Isolation matrix, one loop, N = 1000 each (V)

| Recipe body | What it isolates | Deaths |
|---|---|---|
| `x=$(printf 'a')` | fork only, builtins only | 0 / 1000 |
| `while read -d '' …; done < <(printf 'a\0b\0c\0')` | process substitution, child runs only builtins | 0 / 1000 |
| `x=$(git ls-files -z … \| tr -d '\0' \| wc -c)` | fork plus a waited-for native git | 0 / 1000 |
| `git ls-files -z … > /dev/null` | native git alone, no fork | 0 / 1000 |
| `while read -d '' …; done < file` | the NUL read alone | 0 (earlier harness) |

### 3.2 The construct against PR 192's form, six concurrent loops (V)

2026-09-29, six cmd.exe loops in parallel, each alternating the two recipes.

| Recipe body | Launches | Deaths | Death code |
|---|---|---|---|
| `while read -d '' …; done < <(git ls-files -z …)` | 15,012 | **7** | 2816 every time (SIGSEGV, section 2.3) |
| temp file: `git ls-files -z … > "$list"`, then `done < "$list"` | 15,018 | **0** | — |

Same construct, one loop alone: 0 / 4,000. If both forms failed at the same rate,
seven deaths landing on one side has under a 1 % chance.

Earlier runs put `C:\Program Files\Git\usr\bin` first on PATH instead of `Git\bin`
and saw 0 deaths in 6,000 launches. That was first read as "launcher path versus
direct path"; section 2.2 shows the chain is the same either way, so PATH order was
not the variable. Those runs were smaller and used fewer concurrent loops. At the rate
measured later, about 1 in 2,500, the expected count in 6,000 launches is about 2.4
and zero has a probability near 9 %; the two results are not distinguishable (V,
arithmetic). What else differed between them is not established (U).

### 3.3 Pipelines against the construct, six concurrent loops, N = 3000 each (V)

2026-09-30, after the guest reboot, same consumer loop fed three ways.

| Recipe body | Launches | Deaths |
|---|---|---|
| `git ls-files -z … \| while read -d '' …; done` (pipeline, native producer) | 18,015 | **0** |
| `printf 'a\0b\0c\0' \| while read -d '' …; done` (pipeline, builtin producer) | 18,015 | **0** |
| `while read -d '' …; done < <(git ls-files -z …)` (reference) | 18,015 | **6**, all 2816 |

The reference deaths: loop 3000 at iteration 2493; loop 3001 at 698 and 1959; loop
3002 at 636 and 2499; loop 3003 at 2739; loops 3004 and 3005 none.

### 3.4 Totals (V)

| Shape | Launches | Deaths |
|---|---|---|
| process substitution with a native `git` child, under concurrency (Git 2.55, runtime 3.6.9) | 33,027 | 13 (about 1 in 2,500) |
| process substitution with a builtin child | 1,000 | 0 |
| temp file or pipeline, native or builtin producer | 51,048 | 0 |

A pipeline forks the consumer and spawns the producer as an ordinary child. A process
substitution forks a child that execs the producer while the parent opens and reads
`/dev/fd/N`. Only the second shape dies. Which part of that shape the emulated runtime
mishandles (the `/dev/fd` open, the asynchronous child's lifetime, something else) was
not isolated further. (U)

### 3.5 Trace attempt (V)

1,500 and 2,000 launches of the construct with `set -x` to a per-iteration file
(`BASH_XTRACEFD`, `PS4` with `EPOCHREALTIME`) and `ulimit -c unlimited` produced no
death, so no trace of a dying iteration exists. No `*.stackdump` was ever written by a
dying bash on the guest.

### 3.6 What the child must be (V unless marked)

2026-10-01 to 2026-10-02, the 2.56.0.windows.1 tarballs (runtime 3.6.10-5a1665c8)
unpacked on the guest, six concurrent cmd.exe loops as SYSTEM scheduled tasks (nobody
was logged on), each variant interleaved, 24,015 launches per variant. Same consumer
in all five, `while IFS= read -r -d '' f; do …; done < <(PRODUCER)`; every death was
2816.

| Variant | bash | Child of the process substitution | Launches | Deaths |
|---|---|---|---|---|
| native git | arm64 tarball | `git ls-files -z …`, ARM64 native | 24,015 | **0** |
| all x86-64 | 64-bit tarball | `git ls-files -z …`, x86-64 | 24,015 | **0** |
| native, not git | arm64 tarball | `curl.exe -s file:///…` (clangarm64), ARM64 native, the same NUL list | 24,015 | **0** |
| x86-64 MSYS, not git | arm64 tarball | `cat list.nul` (usr\bin), x86-64, the same NUL list | 24,015 | **10** |
| builtin | arm64 tarball | `printf 'a\0b\0c\0'`, no exec | 24,015 | **0** |

- **A native child is not part of the trigger.** The only variant that died has an
  x86-64 MSYS child and no native code anywhere in the process tree.
- **A forked child that execs a program is.** Process substitutions that differ only by
  whether the child execs (`cat` against a builtin) differ by 10 deaths to 0.
- **Running everything x86-64 is no way out.** The all-x86-64 variant did not die here,
  but neither did native git, and the x86-64 MSYS child did.
- **The native-git reference stopped dying.** 0 in 24,015 where section 3.4 saw 13 in
  33,027; at that rate zero has a probability below 1 in 10,000. The runtime, git, bash,
  the logon session and the environment all changed at once and were not separated
  (U). The runner that died with native git (section 7) runs 3.6.10-710e5275, a
  different build of the same series, so "3.6.10 fixes it" is not supported (U).

The rule in section 5 does not change: no process substitution in shared recipes,
whatever the child. It should have been read more widely: a command substitution that
runs an MSYS program is the same fork and exec, and one killed a recipe on the runner
(section 9).

## 4. Unverified, stated plainly

- **Which NTSTATUS, and why it differs.** All three codes seen are raw deaths of
  bash.exe read by `env`'s stub through the mapping in section 2.3: 4 in the eleven
  lint jobs (a code below 0xC0000000 with low byte 4), 127 in the canary (an NTSTATUS
  error outside the five named cases), 2816 on the guest (STATUS_ACCESS_VIOLATION, or
  a SIGSEGV the runtime caught; indistinguishable). The construct and the point of
  death are the same in the canary and on the guest (section 7). Why the runner records
  different codes than the guest is not known. The guest's later build matched the
  runner's (26200.9457) and still produced 2816, so the Windows build alone does not
  explain it.
- **The terminator on the runner.** Not identified. 0x40010004 (DBG_TERMINATE_PROCESS)
  is the standard code with low byte 4 and is what a debugger or the x64 emulation layer
  would use to end a process; the canary's 127 needs a different NTSTATUS, such as a
  stack overflow, heap corruption or fail-fast. The canary's Application event log dump
  came back empty, so no crash record names the exception (section 7).
- **Fresh-VM effects.** The runner is a new VM per job with cold emulator caches; the
  guest is warm. The guest needed concurrency to reproduce at all; the canary ran one
  launch at a time and still died once in 2,000. Whether the runner's cold start
  substitutes for the guest's concurrency is a guess.
- **Rate on the runner for PR 192's form.** 0 in 2,000 on the canary (V); 0 in 15,018
  on the guest (V). Not zero by proof, zero by observation.

## 5. The fix, and the rule that follows

PR 192 (https://github.com/farcloser/limen/pull/192) replaces every `< <(…)` in the
shared recipes with a temp file written by the producer and read by the loop, and
drops the ERR trap added earlier to `lint dockerfile`. Twelve sites on main: `fix.just`,
`fix-homebrew.just`, `lint.just` (five), `lint-go.just` (two), `lint-homebrew.just`
(two), `tools.just`. Main has no `>(…)`. (V, grep on main at 839c72f and the PR diff.)
The producer's exit status now fails the recipe under `set -e`; before, it vanished
inside the substitution. That is a behaviour improvement worth keeping.

Rule for shared recipes:

- **No process substitution** (`< <(…)` or `>(…)`) in any shared recipe. Audit:
  `grep -nE '< <\(|>\(' .limen/just/*.just` must be empty.
- **Pipelines are fine**, including pipelines from native producers (`jq … |` in
  `lint shell`, `go tool cover … |` in `test go`). 0 in 18,015 under the load that kills
  the substitution. *Qualified 2026-10-05* (section 9): far rarer than the substitution
  (6 in 18,015 there), not proven zero. The producer measured was native git; a stage
  that execs an MSYS program was never measured, and is the forked-child shape section
  3.6 found.
- **Waited-for native children and `$(…)` are fine.** 0 in 1,000 each. *Withdrawn
  2026-10-05 for `$(…)`* (section 9): the thousand launches (section 3.1) ran in one
  loop, without the concurrency the guest needed to reproduce anything, and a thousand
  cannot see a rate of one in a few thousand. A command substitution whose child execs
  an MSYS program (`tr` and `wc` there, `mktemp` in section 9) is the trigger section
  3.6 found.
- Read `exit code 4` or `exit code 127` with nothing on stderr from any shebang recipe
  on windows-11-arm as "bash died raw under emulation", not as a script error. The
  recipes cannot produce a silent 4, and bash's own 127 always prints "command not
  found". Read `exit code 2816` as SIGSEGV, caught by the runtime or read from a raw
  access violation. Section 2.3 has the table.

## 6. Reproduction

Self-contained: everything needed to rerun it is below. Nothing here touches limen's
tree.

### 6.1 VM

- UTM on an Apple Silicon Mac, Windows 11 arm64 guest, build 10.0.26200. 4 vCPUs
  were enough. Guest tools (UTM's qemu guest agent) let the host run `utmctl exec` and
  `utmctl file pull`; anything else that gets a cmd.exe prompt and files in and out
  works too.
- Install Git for Windows arm64 (`Git-2.55.0-arm64.exe` from
  https://github.com/git-for-windows/git/releases) with defaults. Confirm the runtime
  is x86_64: `file "C:\Program Files\Git\usr\bin\bash.exe"` from Git Bash.
- Put the arm64 `just` 1.58.0 binary at `C:\Users\Public\repro\just.exe`
  (https://github.com/casey/just/releases, `just-1.58.0-aarch64-pc-windows-msvc.zip`).
- Create `C:\Users\Public\repro\scratch`, `git init` it, add a handful of files and
  commit. The construct reads the file list; size did not matter on the guest.
- Put `C:\Program Files\Git\usr\bin` (for `cygpath` and `env`) and
  `C:\Program Files\Git\cmd` (for `git`) on PATH. Whether `C:\Program Files\Git\bin`
  comes first does not matter: `env` resolves `bash` to `usr\bin\bash.exe` either way
  (section 2.2).

### 6.2 Justfiles

`C:\Users\Public\repro\repro.just`: the construct, PR 192's form, and the isolation
and pipeline variants. `working-directory` points the recipes at the scratch repo.

```just
set working-directory := 'scratch'
set quiet

# The construct, verbatim shape of the failing recipes.
procsub:
    #!/usr/bin/env bash
    set -euo pipefail
    while IFS= read -r -d '' f; do
        [ -f "$f" ] || continue
    done < <(git ls-files -z --cached --others --exclude-standard)

# PR 192's form.
tmpfile:
    #!/usr/bin/env bash
    set -euo pipefail
    list=$(mktemp "${TMPDIR:-/tmp}/repro.XXXXXX")
    trap 'rm -f "$list"' EXIT
    git ls-files -z --cached --others --exclude-standard > "$list"
    while IFS= read -r -d '' f; do
        [ -f "$f" ] || continue
    done < "$list"

# Pipeline from a native producer.
pipegit:
    #!/usr/bin/env bash
    set -euo pipefail
    git ls-files -z --cached --others --exclude-standard | while IFS= read -r -d '' f; do
        [ -f "$f" ] || continue
    done

# Pipeline from a builtin producer.
pipeprintf:
    #!/usr/bin/env bash
    set -euo pipefail
    printf 'a\0b\0c\0' | while IFS= read -r -d '' f; do :; done

# Fork plus a waited-for native git, no process substitution.
subgit:
    #!/usr/bin/env bash
    set -euo pipefail
    x=$(git ls-files -z --cached --others --exclude-standard | tr -d '\0' | wc -c)
    [ "$x" -ge 0 ]

# Process substitution whose child runs only builtins.
procprintf:
    #!/usr/bin/env bash
    set -euo pipefail
    while IFS= read -r -d '' f; do :; done < <(printf 'a\0b\0c\0')

# Fork only, builtins only.
subprintf:
    #!/usr/bin/env bash
    set -euo pipefail
    x=$(printf 'a')
    [ "$x" = a ]

# Native git alone, no fork.
gitonly:
    #!/usr/bin/env bash
    set -euo pipefail
    git ls-files -z --cached --others --exclude-standard > /dev/null

# bash start-up alone.
empty:
    #!/usr/bin/env bash
    set -euo pipefail
    :
```

`C:\Users\Public\repro\calib.just`: the exit-code calibration.

```just
set working-directory := 'scratch'
set quiet

exit4:
    #!/usr/bin/env bash
    exit 4

exit260:
    #!/usr/bin/env bash
    exit 260

sigill:
    #!/usr/bin/env bash
    kill -ILL $$

sigsegv:
    #!/usr/bin/env bash
    kill -SEGV $$

sigkill:
    #!/usr/bin/env bash
    kill -KILL $$

# A native child exits with a raw Windows code; bash reports $?.
child code:
    #!/usr/bin/env bash
    set -uo pipefail
    powershell.exe -NoProfile -Command "[Environment]::Exit({{ code }})"; echo "bash saw \$?=$?" >&2
    exit 0

# Park bash with a known Windows pid, so an outside process can terminate it.
selfwait:
    #!/usr/bin/env bash
    cat /proc/$$/winpid > /c/Users/Public/repro/selfwait.pid
    sleep 30
    echo "still alive after 30s" >&2
```

Run the first six by hand and read just's "failed with exit code N" line. For the
outside terminations run this from PowerShell; it starts just, waits for bash to write
its Windows pid, terminates bash with the chosen code, and records just's exit code.
Killing from inside the recipe does not work: the killer is bash's own child, it
outlives bash holding the inherited handles, and the loop around it stalls.

```powershell
$k = Add-Type -MemberDefinition '[DllImport("kernel32.dll")] public static extern bool TerminateProcess(IntPtr h, uint code);' -Name K -Namespace W -PassThru
$env:PATH = 'C:\Users\Public\repro;C:\Program Files\Git\usr\bin;C:\Program Files\Git\cmd;C:\Windows\System32;C:\Windows'
foreach ($c in @(4, 0x40010004, 0xC0000005, 0xC00000FD, 0xC0000409, 0xC0000094)) {
  Remove-Item -Force C:\Users\Public\repro\selfwait.pid -ErrorAction SilentlyContinue
  $p = Start-Process just.exe -ArgumentList '--justfile','C:\Users\Public\repro\calib.just','selfwait' -WorkingDirectory C:\Users\Public\repro\scratch -WindowStyle Hidden -PassThru -RedirectStandardError "C:\Users\Public\repro\err-$c.txt"
  while (-not (Test-Path C:\Users\Public\repro\selfwait.pid)) { Start-Sleep -Milliseconds 200 }
  Start-Sleep -Milliseconds 500
  $h = (Get-Process -Id ([int](Get-Content C:\Users\Public\repro\selfwait.pid))).Handle
  $r = $k::TerminateProcess($h, [uint32]$c)
  $p.WaitForExit(40000) | Out-Null
  "0x$($c.ToString('X8')) terminated=$r just-exit=$($p.ExitCode) stderr=$(Get-Content "C:\Users\Public\repro\err-$c.txt" -Raw)"
}
```

Expected: 4 and 0x40010004 give 4; 0xC0000005 gives 2816; the other three give 127
(section 2.3).

### 6.3 The loop

`C:\Users\Public\repro\run.cmd`, one argument N. It runs the listed recipes N times
each, counts non-zero exits, and keeps just's stderr for every failure. The temp names
carry N so concurrent loops do not share files (two loops sharing one produced
"Device or resource busy" noise).

```bat
@echo off
setlocal enabledelayedexpansion
cd /d C:\Users\Public\repro\scratch
set PATH=C:\Users\Public\repro;C:\Program Files\Git\bin;C:\Program Files\Git\cmd;C:\Program Files\Git\usr\bin;%PATH%
set N=%1
set OUT=C:\Users\Public\repro\run-%N%.log
set ERRF=C:\Users\Public\repro\stderr-%N%.tmp
echo start %DATE% %TIME% user=%USERNAME% > %OUT%
for %%v in (pipegit pipeprintf tmpfile procsub) do (
  set c0=0
  set cX=0
  for /l %%i in (1,1,%N%) do (
    just --justfile ..\repro.just %%v > nul 2> %ERRF%
    set rc=!errorlevel!
    if !rc! equ 0 (set /a c0+=1) else (set /a cX+=1& echo %%v iter %%i rc=!rc! >> %OUT%& type %ERRF% >> %OUT%)
  )
  echo %%v ok=!c0! fail=!cX! >> %OUT%
)
echo FINISHED >> %OUT%
```

Capture `errorlevel` into a variable before any `set /a`; `set /a` resets it.

### 6.4 Six concurrent loops in the interactive session

One loop alone did not reproduce in 4,000 launches. Six did. Run them as scheduled
tasks in the logged-on user's session, from an elevated PowerShell on the guest
(replace the user name; distinct N values give distinct log files). With nobody logged
on, a `-UserId 'SYSTEM' -LogonType ServiceAccount` principal works and also
reproduces (V, 2026-10-02 run).

```powershell
$principal = New-ScheduledTaskPrincipal -UserId 'USER' -LogonType Interactive -RunLevel Limited
$settings  = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Hours 4) -MultipleInstances Parallel
foreach ($n in 3000..3005) {
  $a = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument "/c C:\Users\Public\repro\run.cmd $n"
  Register-ScheduledTask -TaskName "repro-$n" -Action $a -Principal $principal -Settings $settings -Force | Out-Null
  Start-ScheduledTask -TaskName "repro-$n"
}
```

Remove the tasks
afterwards with `Unregister-ScheduledTask -TaskName 'repro-*' -Confirm:$false`.

### 6.5 What to count

- Per recipe, the `ok=` and `fail=` line in each `run-N.log`.
- Per failure, the `rc=` value. 2816 is SIGSEGV (caught, or a raw access violation).
  4 or 127 is a raw death with a different NTSTATUS: that would be the first guest
  reproduction of a CI code and should be kept with the full stderr. Any other value
  is something new.
- `*.stackdump` files in the scratch directory, if any.
- Expected on the guest: `procsub` dies roughly once per 2,500 launches under six-way
  concurrency; `tmpfile`, `pipegit`, `pipeprintf` do not die.

## 7. CI canary

Purpose: reproduce where it happens and name the process that exits 4. One throwaway
job on a scratch branch, never merged. It ran on 2026-10-02:
https://github.com/farcloser/limen/actions/runs/36966394838 (result in 7.1).

`canary.just` on the scratch branch, imported nowhere:

```just
set quiet

# The construct, verbatim from the pre-PR-192 `lint just`, minus the per-file command.
procsub trace:
    #!/usr/bin/env bash
    set -euo pipefail
    ulimit -c unlimited
    exec {fd}>>"{{ trace }}/xtrace.txt"
    export BASH_XTRACEFD=$fd PS4='+ ${EPOCHREALTIME} $LINENO: '
    set -x
    echo "pid=$$ ppid=$PPID bash=$BASH_VERSION msystem=${MSYSTEM:-} cwd=$PWD"
    while IFS= read -r -d '' f; do
        [ -f "$f" ] || continue
    done < <(git ls-files -z --cached --others --exclude-standard 'justfile' '**/justfile' 'Justfile' '**/Justfile' '.justfile' '**/.justfile' '*.just')
    echo done

# PR 192's form.
tmpfile trace:
    #!/usr/bin/env bash
    set -euo pipefail
    ulimit -c unlimited
    exec {fd}>>"{{ trace }}/xtrace.txt"
    export BASH_XTRACEFD=$fd PS4='+ ${EPOCHREALTIME} $LINENO: '
    set -x
    list=$(mktemp "${TMPDIR:-/tmp}/canary.XXXXXX")
    trap 'rm -f "$list"' EXIT
    git ls-files -z --cached --others --exclude-standard 'justfile' '**/justfile' 'Justfile' '**/Justfile' '.justfile' '**/.justfile' '*.just' > "$list"
    while IFS= read -r -d '' f; do
        [ -f "$f" ] || continue
    done < "$list"
    echo done

# No fork, no git: bash start-up alone through the same launch chain.
empty trace:
    #!/usr/bin/env bash
    set -euo pipefail
    exec {fd}>>"{{ trace }}/xtrace.txt"
    export BASH_XTRACEFD=$fd
    set -x
    echo done
```

The workflow step as specified, after the usual aqua setup so `just` and `git`
resolve as in `verify`. The run itself used a bash loop of the same shape, because
`ci.yaml` launches just from git-bash (section 2.2), a separate `pwsh` step for the
event-log dump, an extra step recording the environment, and a timestamp on each
failure line:

```powershell
$N = 2000
$out = "$env:RUNNER_TEMP\canary"
New-Item -ItemType Directory -Force $out | Out-Null
foreach ($v in 'procsub','tmpfile','empty') {
  $fail = 0
  for ($i = 1; $i -le $N; $i++) {
    $t = "$out\$v-$i"; New-Item -ItemType Directory -Force $t | Out-Null
    $err = "$t\stderr.txt"
    & just --justfile canary.just $v $t 2> $err | Out-Null
    $rc = $LASTEXITCODE
    if ($rc -eq 0) { Remove-Item -Recurse -Force $t } else {
      $fail++
      "$v iter $i rc=$rc" | Tee-Object -Append "$out\failures.txt"
      Get-ChildItem "$env:RUNNER_TEMP","$env:TEMP","$PWD" -Recurse -Filter '*.stackdump' -ErrorAction SilentlyContinue |
        Copy-Item -Destination $t -ErrorAction SilentlyContinue
    }
  }
  "$v failures=$fail of $N" | Tee-Object -Append "$out\summary.txt"
}
wevtutil qe Application "/q:*[System[(EventID=1000 or EventID=1001)]]" /c:50 /rd:true /f:text > "$out\events.txt"
exit 0
```

Then `actions/upload-artifact`, pinned by digest like every action, on
`${{ runner.temp }}/canary`. At one failure per
hundred launches, 2000 iterations should show it 15 to 25 times. (U, from the
estimated rate.)

What the artifact answers:

- `failures.txt` empty: the failure needs `just lint`'s surroundings (a preceding
  aqua download, the banner recipe, the `_go-tool` build, a specific repository's file
  list). Next canary: the real `just do lint just` in a loop.
- A failure with an empty `xtrace.txt`: bash never executed line one. The death came
  from the launch chain, not the script.
- A failure with `xtrace.txt` ending before `done`: the last traced line is where bash
  was when it died; the timestamps say whether it stalled first.
- `stderr.txt` non-empty: quote it; the "silent" premise was a log-capture artefact.
- `rc=2816` on the runner: the runner recorded an access violation, as the guest does.
- `events.txt` with an Application Error (1000) or Windows Error Reporting (1001)
  record for `bash.exe` at a failure's time: the OS saw the crash; the record names
  the faulting module and exception code.
- Both `procsub` and `tmpfile` failing at similar rates: PR 192 does not fix it. Only
  `procsub` failing: it does.
- `empty` failing too: the construct is irrelevant; the launch chain is.

### 7.1 Result (V, from the run's `canary` artifact)

| Variant | Failures | Launches | Duration |
|---|---|---|---|
| `procsub` | 1 | 2000 | 1155 s |
| `tmpfile` | 0 | 2000 | 1370 s |
| `empty` | 0 | 2000 | 972 s |

The one failure, `procsub` iteration 1911 at 05:11:11 UTC, exited **127**, not 4.

- Its `xtrace.txt` has four lines and stops: the `echo pid=... bash=5.3.15(2)-release
  msystem=CLANGARM64` line; the child's `git ls-files -z ...` at depth `++` (the
  process-substitution child about to exec git); the parent's `IFS=`; the parent's
  `read -r -d '' f`. No `done`. bash died in the concurrent phase, the same point as
  the construct's failure mode in section 3.
- Its `stderr.txt` holds just's "recipe `procsub` failed with exit code 127" and
  aqua's relay of the same status. bash wrote nothing. No `*.stackdump` was captured.
- `events.txt` is empty: no Application Error (1000) or Windows Error Reporting (1001)
  record was returned, or the query produced nothing. Which of the two is not known. (U)
- `environment.txt`: PATH begins `/clangarm64/bin:/usr/bin:...`; `command -v` gives
  the aqua proxy for just, `/usr/bin/bash`, `/usr/bin/env`, `/clangarm64/bin/git`;
  just 1.58.0; git 2.55.0.windows.5; bash 5.3.15(2)-release (x86_64-pc-cygwin);
  msys2-runtime 3.6.10-710e5275.x86_64; Windows 10.0.26200.9457. The launcher is not
  in the chain (section 2.2).

Reading: by section 2.3, 127 means bash.exe's Windows exit code was an NTSTATUS error
outside the five named cases, and 4 in the lint jobs means a code below 0xC0000000
with low byte 4. Same construct, same point of death, same silence; different
NTSTATUS. The canary did not name the terminator and did not reproduce the 4.

Rate: 1 in 2,000 against roughly 1 in 100 estimated from the lint jobs. The canary's
recipe does less than the real ones (no `just --fmt --check` per file, no `_go-tool`
build before it) and the estimate was rough; the gap is noted, not explained. (U)

## 8. Revisit triggers

The exposure is the x86_64 MSYS2 runtime under emulation on windows-11-arm. It ends
when a native arm64 runtime ships. Revisit the rule in section 5 when any of these
lands; until then the rule stands.

- **msys2-runtime PR 356**, https://github.com/msys2/msys2-runtime/pull/356: AArch64
  enablement for `aarch64-pc-cygwin`, eight commits, CI green, not merged at the time
  of writing. It depends on Cygwin master's AArch64 work plus unmerged
  `cygwin-patches` and an unpublished two-stage GCC toolchain. (V, PR page read
  2026-10-01.) The earlier draft, https://github.com/msys2/msys2-runtime/pull/348, is
  a Clang-based port with known issues and a prebuilt `msys-2.0.dll` that could be
  dropped onto the guest as a cheap probe. (V, draft state; U, whether the probe runs.)
- **Cygwin 3.7 with AArch64**: Cygwin master (3.7.0-dev) carries the merged AArch64
  commits PR 356 builds on, https://cygwin.com/git/?p=newlib-cygwin.git. A 3.7 release
  with an aarch64 build is the upstream event. (V that the commits exist on master,
  per the PR 356 description; U on release timing.)
- **Git for Windows shipping an arm64 MSYS2 runtime**: watch the arm64 asset's
  `usr\bin\msys-2.0.dll` architecture across releases at
  https://github.com/git-for-windows/git/releases. The MSYS2 arm64 page,
  https://www.msys2.org/docs/arm64/, documents the current x86_64-under-emulation
  state.

Building a native MSYS2 runtime in-house was assessed and declined: it means tracking
Cygwin master, the msys2 rebase and the GCC patches for weeks, for a component the
upstreams are already finishing.

## 9. A death with no process substitution (2026-10-05)

https://github.com/farcloser/limen/actions/runs/37266312458/job/111623939333, a
`verify (windows-11-arm)` job on https://github.com/farcloser/limen/pull/240, one day
after PR 192 merged, on `windows-11-vs2026-arm64` 20260924.168.1. (V, the job log.)

```
▶ lint: links
error: recipe `links` failed with exit code 4
ERR aqua failed program=aqua … exe_name=just … error="exit status 4"
##[error]Process completed with exit code 4.
```

- **No process substitution.** `lint links` has none; its first command that is not a
  builtin is `scratch=$(mktemp -d "${TMPDIR:-/tmp}/lint-links.XXXXXX")`, a command
  substitution whose child execs the x86-64 MSYS `mktemp`. (V, `.limen/just/lint.just`
  at the PR's head.)
- **bash died before lychee finished, and lychee did not fail.** 1.1 s from the banner
  to the error, against 4.4 to 4.8 s from the banner to lychee's first output on the
  three preceding green windows-11-arm runs (V, job logs of runs 37266095369,
  37263514291 and 37260028938). The only aqua `ERR` line names `just`, relaying the
  recipe's 4; aqua logs the same line for any proxied tool that exits non-zero on
  Windows, and none names `lychee`. (V that no line names lychee; U that aqua would
  always log one, inferred from the `just` line, not from aqua's source.)
- **Where exactly it died is not known.** The log cannot place the death between the
  `mktemp` substitution and lychee's start; the substitution is the one fork and exec
  of an MSYS program in that window. (U)

Reading: the trigger is what section 3.6 measured, a forked child that execs an MSYS
program, and a command substitution is one. Section 5's rule removed the shape that
failed most visibly, not the cause. The shared recipes hold dozens of command
substitutions; rewriting them for one emulated runner was declined. The failure is
accepted: a silent `exit code 4` or `127` on windows-11-arm is re-run, and the exposure
ends with the triggers in section 8.

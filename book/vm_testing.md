# VM testing (windows)

CI must never be the first place a windows failure is seen. Local reproduction on a mac is
a Windows 11 (arm64) virtual machine with the repository shared into the guest, so the exact
working tree — not a copy — is what the guest builds and lints. This chapter is the machine
and every trap on the way to it; what is true of windows itself is the
[windows chapter](./windows.md).

## Host: UTM and an installer image

Both from homebrew:

```sh
brew install --cask utm crystalfetch
```

- **UTM** is the hypervisor (QEMU under a native UI, with guest-agent and directory-sharing
  support built in).
- **CrystalFetch** builds a Windows 11 arm64 installer ISO from Microsoft's servers, which
  offer no direct arm64 ISO download.

Create the VM in UTM from that ISO (the Windows wizard provisions the TPM and EFI variables
Windows 11 requires). Two settings matter:

- **Network**: `Shared` mode. The guest lands on UTM's vmnet subnet, reachable from the host;
  its MAC is visible in `arp -a`, which is how to correlate the VM with an IP before the
  guest agent is available.
- **Directory sharing**: `WebDAV` mode, read-write, pointing at the **parent** of the
  repository, so the guest sees the repo at `Z:\<basename>` and log and marker files land at
  the share root (`Z:\`), outside the working tree; `just vm` (below) expects this. WebDAV is
  served by UTM itself over SPICE, nothing to configure on the host, but it needs a client
  inside the guest (next section): until then the share is configured and mounted nowhere.

## Guest: the support tools are not optional

A fresh Windows guest has neither the QEMU guest agent nor the SPICE WebDAV client:
`utmctl exec` / `ip-address` / `file` fail with "The QEMU guest agent is not running or not
installed on the guest", and the shared directory appears nowhere. Install the **UTM guest
support tools** inside Windows (UTM offers the ISO as a mounted CD; run the installer from
Explorer). The one installer provides the QEMU guest agent (`utmctl exec`, `file pull/push`,
`ip-address`), the SPICE WebDAV service (the share as `Z:`), and the virtio drivers.

## Driving the VM from the host

`utmctl` (UTM's CLI) and AppleScript (`osascript`) are the two control channels. Both are
Apple Events clients, with macOS traps:

- **`utmctl` hardcodes `/Applications/UTM.app`**, with no environment override. A cask
  install anywhere else (the user's Applications directory) makes every `utmctl` fail with
  "Application not found". Fix once:

  ```sh
  ln -s "$(readlink -f "$(which utmctl)" | sed 's|/Contents/MacOS/utmctl||')" /Applications/UTM.app
  ```

- **Apple Events need Automation consent.** The first scripting call from a host application
  triggers the "… would like to control UTM" prompt, per calling app: a grant to one
  terminal does not cover another, or an IDE. A denied or unreachable channel shows as
  `utmctl` dying with SIGABRT and no output, or osascript's `Application isn't running.
  (-600)` while UTM is plainly running.
- **AppleScript can target the app by path**, which sidesteps the hardcoded path:

  ```sh
  osascript -e 'tell application "/path/to/UTM.app" to get name of every virtual machine'
  ```

### Agent harness (Claude Code) sandbox

The coding-agent sandbox blocks the Mach/XPC lookups Apple Events ride on, with the same
symptoms as a TCC denial (SIGABRT, `-600`, "Connection Invalid error for service
com.apple.hiservices-xpcservice"). Two settings in `~/.claude/settings.json` fix it:

```json
"sandbox": {
  "excludedCommands": ["utmctl", "utmctl *", "osascript", "osascript *"],
  "allowAppleEvents": true
}
```

Both forms of each pattern are needed (bare for the naked command, `*` for invocations with
arguments). The sandbox profile is built once at session start: settings edits do nothing to
a live session, restart it.

With the exclusion in place, `utmctl` must still be the *whole* command. A pipe, a
redirection, `&&`, `$(…)` or a wrapper script around it aborts the call with exit 134 before
anything runs — so neither `utmctl file push … < script` nor `utmctl file pull … > out` is
available from the sandbox. Each `utmctl` is its own call; `file pull` prints the file to
stdout, and that output is what you read. The loop that works without the share and without
redirection: put the script on the command line as PowerShell's `-EncodedCommand` (the
UTF-16LE base64 of the script, `iconv -f UTF-8 -t UTF-16LE script.ps1 | base64 | tr -d
'\n'` on the host), keep each encoded argument under about 8 KB (larger ones abort the same
way; split a file into single-quoted here-string chunks written with
`Set-Content`/`Add-Content`), have the script write its results to a guest file ending in a
`DONE` marker, and `file pull` that file. `exec` prints nothing and may return before a long
command ends; detach anything long with `Start-Process -WindowStyle Hidden`. `file pull`
fails with "being used by another process" while a cmd.exe `>>` still holds the file open;
retry.

```sh
utmctl exec Windows --cmd powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand "$(cat script.b64)"
utmctl file pull Windows 'C:\Windows\Temp\out.txt'
```

Everything the guest agent runs is `SYSTEM` in session 0 under an x86-64 agent, so a
`powershell.exe` started from it is x64 under emulation on an arm64 guest. A native parent,
or a logged-on user's session, needs a scheduled task: a principal of `-UserId SYSTEM
-LogonType ServiceAccount` runs at once (Task Scheduler is native, so its children are);
`-LogonType Interactive` registers but waits for that user to log on at the UTM console.

## `utmctl exec` semantics

`exec` is fire-and-forget: it neither relays the guest's stdout nor propagates its exit code,
and `utmctl` itself exits 0 on some errors — match on output, never on exit codes. To
observe a guest command, write its output to a file and read it back; the share makes this
trivial, the marker at the share root so it stays out of the working tree:

```sh
utmctl exec Windows --cmd cmd.exe /c "some-command > Z:\out.txt 2>&1"
cat "$(dirname "$REPO")/out.txt"
```

Prefer `utmctl file pull` for anything chatty. The redirect above is safe as one stream
written by cmd, but a guest process that repeatedly opens and appends a file on the share
(PowerShell `Add-Content`-style progress logging) can stall after the first write, after
which even *reading* a script off the share hangs with no error anywhere; in the session
where we hit it, the share never came back. The robust loop is share-free, both `file` verbs
being plain stdin/stdout streams:

```sh
utmctl file push Windows 'C:\Windows\Temp\script.ps1' < script.ps1
utmctl exec Windows --cmd cmd.exe /c \
  "powershell.exe -ExecutionPolicy Bypass -NoProfile -File C:\Windows\Temp\script.ps1 > C:\Windows\Temp\out.txt 2>&1"
utmctl file pull Windows 'C:\Windows\Temp\out.txt'
```

## The `.spice-clipboard` phantom

The SPICE WebDAV server injects a virtual `.spice-clipboard` directory into the share root
(it backs clipboard sharing and does not exist on the host). It serves invalid timestamps,
and Windows' WebDAV redirector chokes on them while enumerating the root: `dir Z:\` aborts
with "The parameter is incorrect", and Explorer blames a *neighboring* entry ("… is not
accessible. The directory name is invalid") that is in fact fine. Do not browse the share
root in Explorer; work from a shell in the guest (`cd /d Z:\<project>`). Subdirectories are
unaffected.

The redirector also caches file content, so a host-side edit can be served stale to the
guest for a while: an edited script re-run in the guest may execute its *previous* content.
When iterating host→guest, write each revision under a fresh filename, or wait out the
cache.

## Guest quirks worth knowing

- The guest agent executes as `NT AUTHORITY\SYSTEM`: no winget (a per-user app), no
  interactive-user PATH.
- Inside any process spawned under x64 emulation (everything the guest agent runs, on an
  arm64 VM), `%PROCESSOR_ARCHITECTURE%` reports `AMD64`. The true architecture is the
  `PROCESSOR_ARCHITECTURE` value under `HKLM:\SYSTEM\CurrentControlSet\Control\Session
  Manager\Environment`.
- Git for Windows' arm64 build ships a native arm64 `git.exe` (`git version
  --build-options` → `cpu: aarch64`) but an x64-emulated MSYS2 userland: `uname -m` in
  git-bash says `x86_64`. Upstream packaging, not a wrong install.
- The share is a UNC path to git (`//localhost@9843/DavWWWRoot/...`), so git's
  dubious-ownership protection fails every recipe that touches git. Set `safe.directory = *`
  in the guest account's global gitconfig: a test VM's driver account, where the blanket
  wildcard is acceptable.
- Argument boundaries between host, PowerShell, bash and native binaries mangle content:
  MSYS glob-expands a bare `*` headed to a native binary, and PowerShell's native-argument
  re-quoting eats backslash escapes. Do not thread shell code through those layers; write a
  bash script file on the share and invoke bash with only the script path.

## Verifying the loop end to end

One round trip proves the chain (agent alive, share mounted, read-write, and *which*
directory is shared). The marker lands at the share root, the repo's parent on the host:

```sh
utmctl exec Windows --cmd cmd.exe /c "echo proof > Z:\proof.txt"
cat "$(dirname "$REPO")/proof.txt"
rm "$(dirname "$REPO")/proof.txt"
```

From here the guest runs the repository's own `just …` against the live working tree under
git-bash: the windows CI leg's local reproduction.

## Driving it: `just vm`

The root `.justfile` wraps the loop in one recipe: `just vm <task>` runs any just task inside
the VM against this same working tree and streams the guest log back.

```sh
just vm lint          # `just lint` in the guest
just vm do lint go    # any task path works
```

It requires the parent-of-repo WebDAV share above (the guest sees the repo at
`Z:\<basename>`; the log and exit marker land at the share root). Transport is `utmctl
exec`, which relays neither stdout nor exit codes, so the guest writes a log and an exit
marker and the recipe polls for the marker, then exits with the guest's own status. Task
arguments travel as argv end to end, never re-parsed by an intermediate shell, and no
environment variables are passed: qemu-ga's exec replaces the guest environment wholesale,
which would strip `APPDATA` and break aqua. Two knobs: **`VM_NAME`** (default `Windows`) and
**`VM_TIMEOUT`** seconds (default `1800`). The recipe is host-specific — macOS, UTM, a
provisioned Windows VM — so it lives in the root `.justfile`, not in the canonical baseline.

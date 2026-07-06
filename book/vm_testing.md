# VM testing (windows)

The CI matrix has a windows leg, and CI must never be the first place a windows
failure is seen. Local reproduction on a mac means a Windows 11 (arm64) virtual
machine, with the repository shared into the guest so the exact working tree —
not a copy — is what the guest builds and lints. This chapter records the full
path to a working setup, including every trap we hit, so nobody walks it blind
again.

## Host: UTM and an installer image

Both come from homebrew:

```sh
brew install --cask utm crystalfetch
```

- **UTM** is the hypervisor (QEMU under a native UI, with guest-agent and
  directory-sharing support built in).
- **CrystalFetch** builds a Windows 11 arm64 installer ISO from Microsoft's
  official servers — Microsoft does not offer a direct arm64 ISO download, so
  this step is not optional.

Create the VM in UTM from that ISO (the Windows wizard provisions the TPM and
EFI variables Windows 11 requires). Two settings matter to us:

- **Network**: `Shared` mode. The guest lands on UTM's vmnet subnet and is
  reachable from the host; the guest's MAC is visible in `arp -a`, which is
  how you correlate the VM with an IP before the guest agent is available.
- **Directory sharing**: `WebDAV` mode, read-write, pointing at the repository.
  WebDAV is served by UTM itself over SPICE — nothing to configure on the host,
  but it needs a client *inside* the guest (next section), and until that
  client exists the share is configured yet mounted nowhere.

## Guest: the support tools are not optional

A fresh Windows guest has neither the QEMU guest agent nor the SPICE WebDAV
client. Consequences, observed: `utmctl exec` / `ip-address` / `file` all fail
with "The QEMU guest agent is not running or not installed on the guest", and
the shared directory does not appear anywhere in the guest.

Install the **UTM guest support tools** inside Windows (UTM offers the ISO as a
mounted CD; run the installer from Explorer). That single installer provides:

- the **QEMU guest agent** — unlocks `utmctl exec`, `utmctl file pull/push`,
  and `utmctl ip-address`;
- the **SPICE WebDAV service** — mounts the shared directory in the guest as
  the `Z:` drive;
- the virtio drivers.

## Driving the VM from the host

`utmctl` (UTM's CLI) and AppleScript (`osascript`) are the two control
channels. Both are Apple Events clients, which brings macOS-specific traps:

- **`utmctl` hardcodes `/Applications/UTM.app`** — the literal string is in the
  binary, with no environment override. A homebrew cask install living
  anywhere else (e.g. under the user's Applications directory) makes every
  `utmctl` invocation fail with "Application not found". Fix once:

  ```sh
  ln -s "$(readlink -f "$(which utmctl)" | sed 's|/Contents/MacOS/utmctl||')" /Applications/UTM.app
  ```

- **Apple Events need Automation consent.** The first scripting call from a
  given host application triggers the macOS "… would like to control UTM"
  prompt — per calling app, so a grant to one terminal does not cover another
  (or an IDE). A denied or unreachable Apple Events channel manifests as
  `utmctl` dying with SIGABRT and no output, or osascript reporting
  `Application isn't running. (-600)` while UTM is plainly running.

- **AppleScript can target the app by path**, which sidesteps the hardcoded
  path problem entirely:

  ```sh
  osascript -e 'tell application "/path/to/UTM.app" to get name of every virtual machine'
  ```

### Agent harness (Claude Code) sandbox

The coding-agent sandbox blocks the Mach/XPC lookups Apple Events ride on, with
the same misleading symptoms as a TCC denial (SIGABRT, `-600`, "Connection
Invalid error for service com.apple.hiservices-xpcservice"). Two settings in
`~/.claude/settings.json` fix it:

```json
"sandbox": {
  "excludedCommands": ["utmctl", "utmctl *", "osascript", "osascript *"],
  "allowAppleEvents": true
}
```

Both forms of each pattern are needed (bare matches the naked command, `*`
matches invocations with arguments — mirror how the other exclusions are
written). The sandbox profile is built once at session start: settings edits do
nothing to a live session, restart it.

## `utmctl exec` semantics

`exec` is fire-and-forget: it neither relays the guest's stdout nor propagates
its exit code (and `utmctl` itself exits 0 even on some errors — match on
output, never on exit codes). To observe a guest command, write its output to
a file and read it back — the shared directory makes this trivial:

```sh
utmctl exec Windows --cmd cmd.exe /c "some-command > Z:\out.txt 2>&1"
cat "$REPO/out.txt"
```

`utmctl file pull` is the alternative when the share is not involved.

## The `.spice-clipboard` phantom

The SPICE WebDAV server injects a virtual `.spice-clipboard` directory into the
share root (it backs clipboard sharing and does not exist on the host
filesystem). It serves invalid timestamps, and Windows' WebDAV redirector
chokes on them while enumerating the root: `dir Z:\` aborts with "The
parameter is incorrect", and Explorer misattributes the failure to a
*neighboring* entry with errors like "… is not accessible. The directory name
is invalid" — pointing at a directory that is, in fact, fine.

Do not browse the share root in Explorer. Work from a shell in the guest
(`cd /d Z:\<project>`); subdirectories are unaffected, because the phantom
lives only in the root.

Related: the Windows WebDAV redirector caches file content, so a host-side
edit can be served stale to the guest for a while — an edited script re-run in
the guest may execute its *previous* content. When iterating host→guest,
write each revision under a fresh filename (or wait out the cache).

## Guest quirks worth knowing

- The guest agent executes as `NT AUTHORITY\SYSTEM`: no winget (it is a
  per-user app), no interactive-user PATH.
- Inside any process spawned under x64 emulation (everything the guest agent
  runs, on an arm64 VM), `%PROCESSOR_ARCHITECTURE%` reports `AMD64`. The
  machine's true architecture is the `PROCESSOR_ARCHITECTURE` value under
  `HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`.
- Git for Windows' arm64 build ships a native arm64 `git.exe` (check with
  `git version --build-options` → `cpu: aarch64`) but an x64-emulated MSYS2
  userland — `uname -m` in git-bash says `x86_64`. That is upstream
  packaging, not a wrong install.
- The share is a UNC path to git (`//localhost@9843/DavWWWRoot/...`), so
  git's dubious-ownership protection fails every recipe that touches git.
  Set `safe.directory = *` in the guest account's global gitconfig (this is
  a test VM's driver account; the blanket wildcard is acceptable there).
- Argument boundaries between host, PowerShell, bash, and native binaries
  mangle content: MSYS glob-expands a bare `*` argument headed to a native
  binary, and PowerShell's native-argument re-quoting eats backslash escapes.
  Do not thread shell code through those layers — write a bash script FILE
  on the share and invoke bash with only the script path.

## Verifying the loop end to end

One round trip proves the whole chain (agent alive, share mounted, read-write,
and *which* directory is shared):

```sh
utmctl exec Windows --cmd cmd.exe /c "echo proof > Z:\proof.txt"
cat "$REPO/proof.txt"   # appears in the shared repository on the host
rm "$REPO/proof.txt"
```

From here, the guest can run the repository's own workflows (`just …`) against
the live working tree under git-bash, giving the windows CI leg a local
reproduction path.

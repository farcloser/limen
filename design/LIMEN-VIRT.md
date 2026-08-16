# LIMEN-VIRT — virtualization on macOS: one map for three needs

Status: **feasibility research, not a decision.** Supersedes and folds in the
earlier `LIMEN-VFKIT.md`; the deep-dive on the *build-an-image* path stays in
[LIMEN-LIMA](./LIMEN-LIMA.md). Every load-bearing fact was verified live against
upstream on 2026-07-07 (Apple's Virtualization docs, `apple/container` v1.1.0,
`apple/containerization` + `cctl`, `utmapp/UTM`, `lima-vm/lima`, `crc-org/vfkit`,
`Code-Hex/vz`, and the local `farcloser/go-vz` tree). Nothing is wired into the tree
yet.

## The three needs

1. **Run Linux containers on macOS.** Minimum: `buildkitd` in a Linux VM (build an
   image). Ideal: run *any* Linux container, not just build.
2. **Run a macOS VM on macOS.**
3. **(Stretch) Run a Windows VM on macOS.**

## The one constraint that decides everything

On Apple Silicon there are exactly **two host virtualization substrates**, and they
partition the three needs before any tool is chosen:

| Substrate | Can host | **Cannot** host | Speed | Ships with |
|---|---|---|---|---|
| **Apple Virtualization.framework** ("VZ") | **Linux, macOS** guests | **Windows — ever** | Native, fast | macOS 12+ |
| **QEMU** (on Hypervisor.framework) | **anything**: Linux, **Windows (ARM)**, … | *macOS guests* (not on Apple Silicon) | Emulates x86 → slow | 3rd-party |

Two hard walls fall out of this, both verified against Apple's own docs and the
Eclectic Light analysis (2026-07-07):

- **VZ will never run Windows.** The framework is built on Virtio, and *"Virtio
  support in the Virtualization framework limits the range of guest operating
  systems to Linux and macOS."* Windows-on-Apple-Silicon is **QEMU or commercial
  (Parallels/VMware) only** — and only **Windows-for-ARM**, with x86 apps emulated.
- **Only VZ can run a macOS guest.** QEMU cannot virtualize macOS on Apple Silicon.
  So a macOS guest **requires** a VZ-based tool *and* an IPSW install flow.

Therefore:

```
Need #1  Linux containers ─► VZ (best) or QEMU        ← lots of options
Need #2  macOS guest      ─► VZ ONLY + IPSW install   ← few options
Need #3  Windows guest    ─► QEMU ONLY                ← essentially one OSS option
```

**No single tool can win all three cleanly**, because #2 forbids QEMU, #3 forbids
VZ, and the #1 "container" tools are not general-purpose VM managers. The only tool
that spans *both substrates* is **UTM** (it has a QEMU backend *and* an Apple-Virt
backend) — which is why UTM is the natural home for #2+#3, while #1 wants a
purpose-built container stack.

## The candidates, placed on the map

Every candidate is a consumer of one substrate — except UTM, which straddles both.

```
        Apple Virtualization.framework (VZ)                 QEMU
        ├─ Code-Hex/vz      (library / binding)             ├─ UTM (also VZ) ─┐
        ├─ vfkit            (CLI + Go config lib)            └─ Lima (qemu) ───┤
        ├─ go-vz            (farcloser homegrown)                              │
        ├─ Lima (vz)  ───────────────────────────────────────────────────────┘
        ├─ apple/container       (product: daemon + .pkg, on ↓)
        ├─ apple/containerization (Swift framework + cctl, daemonless)
        └─ UTM (Apple-Virt backend)
```

### Capability matrix

| Tool | Substrate | Type | #1 Linux containers | #2 macOS guest | #3 Windows guest | License | Doctrine / pinnability |
|---|---|---|---|---|---|---|---|
| **Lima** | VZ **or** QEMU | VM **manager** | ✅ **turnkey** — containerd/nerdctl, buildkit, docker, podman, k8s templates; port/socket forward; `LIMA_HOME` isolation | ❌ Linux guests only | ❌ Linux guests only | Apache-2.0 | ✅ `limactl` **already aqua-packaged**; isolation native |
| **apple/container** | VZ (Containerization) | **container** engine (product) | ✅ **per-container micro-VM**; pull/run/build OCI from any registry | ❌ containers, not VMs | ❌ | Apache-2.0 | ❌ **root `.pkg` into `/usr/local` + system launchd daemon** (`container system start`) — not an aqua-shaped binary; shared-daemon architecture (see #3) |
| **apple/containerization** | VZ (in-proc) | **Swift framework** + `cctl` | ✅ **daemonless** per-invocation micro-VM; pull + run OCI, bring-your-own pinned kernel | ❌ containers, not VMs | ❌ | Apache-2.0 | ◑ **doctrine-aligned architecture** (no daemon, no `/usr/local`); but **Swift source build** (`make all`, Xcode 26) — no aqua artifact; `cctl` is the demo, the *library* is the product (see #3b) |
| **vfkit** | VZ | CLI + Go config lib | ◑ **boot layer only** — headless Linux, virtio-fs, Rosetta, vsock; no provisioning/forwarding | ◑ **boot-only** (no install), GUI + max-2 | ❌ | Apache-2.0 | ✅ CGO-free `pkg/config` + signed binary; ⚠️ not aqua-packaged |
| **go-vz** | VZ | farcloser homegrown | ◑ Linux boot **spike** | ◑ **install + GUI run** (unfinished, its main function) | ❌ | (in-tree) | ❌ unfinished, no tests/releases, forces CGO+entitlement into limen's binary |
| **Code-Hex/vz** | VZ | **library** (binding) | ◑ primitives only | ✅ **incl. IPSW download+install** primitives | ❌ | MIT | ◑ a *foundation to build on*, not a shipped tool |
| **UTM** | **QEMU + VZ** | VM manager (GUI) | ◑ run a Linux VM, containers inside (manual) | ✅ **Apple-Virt backend + auto-IPSW** | ✅ **QEMU + Win-11-ARM (+Rosetta accel)** | Apache-2.0 (bundles QEMU/GPL) | ⚠️ **GUI-first**; `utmctl` works but Apple-Events consent + hardcoded `/Applications/UTM.app` friction (see book/vm_testing.md); `.app` cask, not a pinned binary |

Legend: ✅ first-class · ◑ partial / build-it-yourself · ❌ impossible on this substrate.

## Per-need reading

### Need #1 — Linux containers (the real requirement)

Every serious option is VZ-based (fast, native). The choice is **how much stack the
tool hands you**:

- **Lima — the turnkey pick, and doctrine-fit.** It is a full *VM manager*: guest
  image catalog, cloud-init provisioning, port/socket forwarding, and shipped
  templates for `buildkit`, `containerd`/`nerdctl`, `docker`, `podman`, and k8s. It
  runs *any* Linux container (the ideal), not just builds. `limactl` is **already in
  aqua**, and `LIMA_HOME` gives the hermetic, per-project isolation the tooling
  doctrine wants. This is exactly what [LIMEN-LIMA](./LIMEN-LIMA.md) already
  recommends for the build case — and it scales up to the "any container" case for
  free by swapping the template.
- **apple/container — clean UX, but a doctrine-exception, not a drop-in.** Apple's
  own engine runs each container in its **own lightweight VM**, pulls/runs/builds
  standard OCI images, and is the cleanest, most native *shape* imaginable. **But its
  distribution and architecture both fight the doctrine** (verified against release
  **v1.1.0**, 2026-07-07): it ships as a **signed root `.pkg`** (89 MB) that installs
  into **`/usr/local`** with admin rights and registers a **system-wide launchd
  daemon** (`container system start`). That is *not* an aqua-shaped relocatable binary
  — aqua installs checksum-verified binaries per-user with no root and no daemon, and
  has no mechanism to run a `.pkg` — and the shared system daemon is the *same
  shared-ambient-state pattern LIMEN-LIMA rejected* in Docker Desktop/OrbStack. Repackaging
  the `.pkg` payload into a first-party tarball is possible but throws away Apple's
  signed/notarized trust chain, reverse-engineers the installer's multi-file layout
  (CLI + `container-apiserver`/helpers + XPC/dylibs, possibly `/usr/local`-baked
  paths), and still can't express the daemon in aqua. → **Adopt only as an explicit
  doctrine exception** (install the signed `.pkg`, pin the version + verify the
  published SHA256, accept the `/usr/local` daemon — the UTM-cask model), and only if
  its per-container-VM UX is judged worth conceding the shared-daemon point. Not a
  free promotion over Lima.
- **apple/containerization + `cctl` — the daemonless, doctrine-*aligned* option.** The
  Swift **framework** underneath `apple/container`, shipped with `cctl` (upstream's
  API playground). `cctl run --kernel <vmlinux> --image alpine:3.16 --mount /h:/g
  --rosetta …` boots a Virtualization.framework micro-VM **in-process**, runs the
  container, and exits — **no launchd daemon, no `/usr/local`, bring-your-own pinned
  kernel + image** (build from the included config or reuse Kata's). That per-invocation,
  pin-the-kernel-by-digest shape is *exactly* the hermetic posture LIMEN-LIMA argues
  for — it **resolves the shared-daemon objection** that gates `apple/container`. The
  cost is **packaging, not maturity** (it's 1.0, a year in the field): it's a **Swift
  source package** (`make all`, macOS 26 + Xcode 26), so there's no aqua-pinnable
  release artifact and `cctl` is the demo, not the product. Adopting it means
  **building a pinned first-party CLI on the library** (or vendoring a pinned `cctl`
  build) via a `build-*` job — which pulls a **Swift/Xcode toolchain into CI**, a real,
  un-aqua-able dependency for a Go-centric org. Same "own the appliance" commitment as
  vfkit, in Swift — but with image-pull and per-container isolation already in the box.
- **vfkit — a primitive, not a product, for #1.** It boots a headless Linux VM
  beautifully (virtio-fs, Rosetta, vsock) and its CGO-free `pkg/config` fits the
  pinned-binary doctrine perfectly — but it does **not** provision guests, run
  buildkit, or forward ports. Choosing vfkit for #1 means *authoring a mini
  Podman-machine* (image + provisioning + gvproxy + lifecycle) yourself. Only worth
  it if you deliberately want to own the whole appliance. Podman-machine is literally
  vfkit + that glue; if you'd end up rebuilding Podman-machine, prefer Lima.

**Pick for #1: Lima** off-the-shelf. Two Apple-native paths if the org will fund an
appliance build: **apple/containerization/`cctl`** (daemonless, doctrine-aligned,
Swift-build cost) is the *right* one; **apple/container** (daemon + `.pkg`) only as a
doctrine exception if you want its turnkey UX and will concede the shared daemon.

### Need #2 — macOS guest (VZ-only, needs IPSW install)

Booting a macOS guest is the easy half; **installing** one (download IPSW, create
machine-identifier / hardware-model / auxiliary-storage) is the work. Also inherent
VZ limits: **GUI required (no headless), max 2 concurrent macOS VMs, Apple Silicon
+ macOS 12+.**

- **UTM — the pragmatic pick.** Its Apple-Virt backend runs macOS guests and will
  **auto-download the most compatible IPSW**; clipboard/file-sharing on macOS-15+
  guests. GUI-first, but for an interactive macOS VM that's fine.
- **Code-Hex/vz — if you need *programmatic* control in Go.** It has the IPSW
  download + `VZMacOSInstaller` primitives. This is the only reason to touch the raw
  binding: to script macOS-VM lifecycle from limen's own code.
- **vfkit — no.** Boot-only; can't install. **go-vz — no.** Its macOS installer is
  the one thing it uniquely does, but it's unfinished and single-author; UTM (GUI)
  or Code-Hex/vz (programmatic) both do it better. *(Adjacent, outside the candidate
  list: `tart` is a purpose-built macOS-CI VM tool on VZ — but verify its license
  before considering; it is **not** clearly permissive and limen's doctrine rejects
  paid/commercial-license tooling.)*

**Pick for #2: UTM for interactive; Code-Hex/vz only if programmatic macOS-VM
control becomes a real requirement.**

### Need #3 — Windows guest (QEMU-only)

VZ is out by construction. The only realistic OSS path is **QEMU**, and the org has
**already walked it**: `book/vm_testing.md` documents a working Windows-11-arm64
guest under **UTM**, with `utmctl`/AppleScript control, WebDAV directory sharing,
and every trap catalogued. Lima *technically* has a QEMU backend but is Linux-guest
focused — it is not a Windows-guest tool. Parallels/VMware are commercial (rejected
by doctrine).

**Pick for #3: UTM — already adopted and documented.**

## Does one tool get all three? No — and here's the tight version

- **Substrate walls forbid it:** #2 excludes QEMU, #3 excludes VZ. Any single-substrate
  tool loses one of them automatically.
- **The only dual-substrate tool is UTM**, so UTM *can* technically touch all three —
  but it is a **general VM manager, not a container engine**, so its #1 story is
  "run a Linux VM and manage containers inside by hand," which is strictly worse than
  Lima/apple/container and fights limen's headless/automation posture.
- **The container tools (Lima, apple/container) can't do #2/#3** — they don't
  manage arbitrary full-OS VMs.

So the clean answer is **two tools, both OSS/Apache-2.0, both already in the org's
orbit**:

| Need | Primary | Why | Later / alt |
|---|---|---|---|
| **#1 Linux containers** | **Lima** (+ buildkit/containerd template) | turnkey, aqua-pinned `limactl`, `LIMA_HOME` isolation, runs any container | Apple-native appliance: **containerization/`cctl`** (daemonless, doctrine-aligned, #3b) or **apple/container** (daemon exception, #3) |
| **#2 macOS guest** | **UTM** (Apple-Virt + auto-IPSW) | only VZ-based tools can; UTM installs + runs interactively | **Code-Hex/vz** if programmatic control needed |
| **#3 Windows guest** | **UTM** (QEMU + Win-11-ARM) | QEMU is the only OSS route; already documented in book/vm_testing.md | — |

**UTM consolidates #2 + #3** (both "run a full-OS VM," one per backend); **Lima owns
#1.** That's the whole plan in two tools.

## Where the farcloser-adjacent assets land

- **`go-vz` → retire.** It uniquely does macOS-install (#2), but unfinished and
  single-author, on an aging `Code-Hex/vz v3.1.0`, forcing CGO + the
  `com.apple.security.virtualization` entitlement into *limen's own* binary. UTM and
  Code-Hex/vz both cover #2 better. No need it serves justifies finishing it.
- **`vfkit` → keep on the shelf, don't adopt by default.** Best-in-class *boot
  primitive* on the same VZ backend, doctrine-clean (CGO-free `pkg/config` + pinned
  signed binary). Reach for it **only** if #1 becomes a bespoke, fully-owned headless
  appliance where Lima is too much manager and apple/container isn't ready — i.e. you
  consciously choose to build a slim Podman-machine.
- **`Code-Hex/vz` → the foundation, not a solution.** The binding under vfkit, go-vz,
  and Lima's vz driver. Use directly only for programmatic macOS-VM control (#2) that
  no shipped tool exposes.

## Open questions / risks

1. **Packaging vs the aqua doctrine.** `limactl` is aqua-packaged (✅). **UTM is a
   `.app` cask, not a pinnable single binary** — book/vm_testing.md already installs
   it via `brew install --cask utm`, which is *outside* the aqua/pinned world the
   tooling doctrine mandates. Decide whether UTM gets a doctrine exception (it's a
   GUI app, hard to aqua-ify) or a first-party packaging effort. Same open item for
   **apple/container** and **vfkit** if either is adopted.
2. **UTM automation friction is real.** `utmctl` hardcodes `/Applications/UTM.app`,
   needs per-app Apple-Events consent, and depends on the QEMU guest agent inside the
   guest for `exec`/`file`/`ip-address` — all catalogued in book/vm_testing.md. Fine
   for interactive/CI-with-setup; poor for hands-off headless orchestration.
3. **apple/container is a doctrine exception, not an aqua-pinnable tool (macOS 26 is
   now a given).** Its distribution and architecture — not its OS floor — are the
   blocker. It ships as a **signed root `.pkg` into `/usr/local` + a system launchd
   daemon** (`container system start`); aqua installs per-user checksum-verified
   binaries with no root/daemon and won't run a `.pkg`. Repackaging the payload loses
   Apple's signed/notarized chain and still can't express the daemon. Deeper still,
   the shared system daemon is the **same shared-ambient-state pattern LIMEN-LIMA
   rejected** (Docker Desktop/OrbStack). Decide explicitly: keep Lima (doctrine
   intact) vs accept a UTM-style exception (install signed `.pkg`, pin version +
   verify SHA256, own the `/usr/local` daemon) for its cleaner per-container UX.
3b. **apple/containerization/`cctl` clears the daemon objection but costs a Swift
   build.** The framework under `apple/container` runs containers **daemonless**
   in-process (no `/usr/local`, no launchd) — doctrine-aligned. But it's a **Swift
   source package** (`make all`, macOS 26 + Xcode 26), not an aqua-pinnable binary,
   and `cctl` is upstream's demo. Adopting it = build/pin a first-party CLI on the
   library via a `build-*` job, accepting a **Swift/Xcode toolchain in CI** (foreign
   to a Go-centric, aqua-pinned fleet). Decide whether that toolchain cost is worth an
   Apple-native, hermetic #1. This is the *better* Apple path than #3 if the appliance
   is built at all.
4. **Guest supply chain (unchanged from LIMEN-LIMA #1–2).** Whatever runs #1 must pin
   the guest image + provisioning. Lima's template fetches an Ubuntu cloud image and
   `nerdctl-full` at first boot — unpinned fetch to be vendored/pinned. apple/container
   and containerization pull OCI images (registry-pinnable) but need a **pinned Linux
   kernel** (bring-your-own — build from config or reuse Kata's; pin by digest).
   vfkit-direct pushes all of this work onto us.
5. **Windows is ARM-only + emulation cost.** #3 gets Windows-for-ARM; x86 Windows apps
   emulate (slow). Confirm the CI leg only needs arm64 Windows (book/vm_testing.md
   already assumes Windows-11-arm64).
6. **macOS-guest hard limits.** GUI required, max 2 concurrent macOS VMs, Apple
   Silicon + macOS 12+ — scope #2 accordingly.

## Recommendation

Adopt a **two-tool plan**, both open-source and already in the org's orbit:

- **Need #1 (Linux containers): Lima** + the rootless-BuildKit template for the
  build case, scaling to a containerd/nerdctl template for the "any container" case —
  as [LIMEN-LIMA](./LIMEN-LIMA.md) details. If the org will fund an Apple-native
  appliance instead, the doctrine-aligned target is **apple/containerization/`cctl`**
  (daemonless, pin-the-kernel; cost = a Swift/Xcode build in CI, #3b) — *not*
  **apple/container**, whose daemon + root `.pkg` is a doctrine exception (#3) worth
  taking only for its turnkey UX. Both are real given macOS 26; neither is free.
- **Needs #2 + #3 (macOS and Windows VMs): UTM** — the only OSS tool spanning both
  substrates, already documented for the Windows-arm64 CI leg, and the auto-IPSW path
  for macOS guests. Keep **Code-Hex/vz** in reserve for any *programmatic* macOS-VM
  need.
- **Retire `go-vz`.** Keep **`vfkit`** on the shelf as the boot primitive for a
  possible bespoke #1 appliance; treat **`Code-Hex/vz`** as the underlying foundation,
  used directly only when nothing shipped exposes what you need.

Resolve the **packaging exception for UTM** (and any future apple/container / vfkit
adoption) against the aqua-pinning doctrine before wiring anything in — that, plus
the guest-image supply chain (LIMEN-LIMA #1–2), is the real cost. Everything above
the substrate walls is a settled, best-of-breed mapping.

---

### Sources (verified 2026-07-07)

- Apple Virtualization.framework — Linux + macOS guests only, no Windows (Virtio
  limit): https://developer.apple.com/documentation/virtualization ;
  https://eclecticlight.co/2022/07/28/virtualisation-on-apple-silicon-macs-6-support-limits/
- `apple/container` (product: per-container VM, signed root `.pkg` v1.1.0 + launchd
  daemon, macOS 26, Apache-2.0): https://github.com/apple/container
- `apple/containerization` (Swift framework + `cctl`, **daemonless** in-proc micro-VM,
  `make all`/Xcode 26, bring-your-own kernel, Apache-2.0):
  https://github.com/apple/containerization ;
  https://github.com/apple/containerization/blob/main/Sources/cctl/RunCommand.swift
- UTM (QEMU + Apple-Virt backends, macOS-IPSW, Windows-ARM, `utmctl`, Apache-2.0):
  https://github.com/utmapp/UTM ; https://docs.getutm.app/guest-support/macos/
- Lima (VZ/QEMU, Linux guests, container templates) — see LIMEN-LIMA;
  https://github.com/lima-vm/lima
- vfkit (VZ boot layer, CGO-free `pkg/config`, boot-only macOS): earlier LIMEN-VFKIT
  research; https://github.com/crc-org/vfkit
- `Code-Hex/vz` (VZ binding, IPSW install primitives, MIT): https://github.com/Code-Hex/vz
- Local: `farcloser/go-vz` (`README.md`, `macos/`, `cmd/linux/main.go`); `farcloser/limen`
  (`design/LIMEN-LIMA.md`, `book/tooling.md`, `book/vm_testing.md`).

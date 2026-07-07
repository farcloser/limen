# LIMEN-LIMA — an isolated, aqua-pinned image builder (Lima + BuildKit)

Status: **feasibility research, not a decision.** Verdict: **feasible and a good
fit.** Every load-bearing fact below was verified live against upstream on
2026-07-07 (aqua-registry, the GitHub release APIs, and the Lima source at the
`v2.1.4` tag); versions are noted as-researched and would be aqua/Renovate-managed,
not hand-pinned. Nothing here is wired into the tree yet.

## Why

The baseline needs exactly one container capability: **build an image from a
Dockerfile**. It does not need `docker run`, a local image store, compose, a
daemon on a shared socket, or a GUI. Today that one capability drags in either
Docker Desktop or OrbStack — both of which are (a) heavyweight, (b) commercial
paid licenses for an org our size, and (c) *shared, ambient* state: a developer's
existing OrbStack VMs, Docker contexts, and `~/.docker` config sit right next to
whatever CI or limen would touch. That is the opposite of the hermetic,
per-project, pinned posture the tooling doctrine (book/tooling.md) argues for
everywhere else.

Lima + BuildKit inverts all three: Apache-2.0 (no license question), a single
pinned `limactl` binary driving a throwaway VM, and — the requirement the user
made explicit — a **fully isolated** Lima home that shares nothing with the
user's default `~/.lima` or with OrbStack.

## The proposed shape

```
host (macOS/Linux)                     isolated guest VM (Ubuntu LTS, rootless)
┌────────────────────────────┐         ┌──────────────────────────────────┐
│ aqua-pinned:               │         │ containerd (user) + rootless      │
│   limactl   (lima-vm/lima) │  vz/    │ buildkitd                         │
│   buildctl  (moby/buildkit)│  qemu   │   listening on a unix socket      │
│                            │ ──────► │   /run/user/$UID/.../buildkitd.sock│
│ BUILDKIT_HOST=unix://      │         └──────────────────────────────────┘
│   $LIMA_HOME/<inst>/sock/  │◄── port-forwarded socket ──┘
│   buildkitd.sock           │
└────────────────────────────┘
   buildctl build … --output type=image,push=true  (or OCI/docker tarball)
```

Two aqua packages, one isolated VM, one forwarded socket. `buildctl` on the host
talks to `buildkitd` in the guest; the image is pushed to a registry or written
to an OCI/docker tarball. No daemon on any shared socket, no local image store to
collide with anything.

### Per-OS provider, one interface (confirmed 2026-07-07)

The `buildctl` + `BUILDKIT_HOST` contract is identical on both platforms; only the
thing behind the socket differs, and that difference is **transparent to the user**:

| Host | Provider | aqua-pinned | Why |
|---|---|---|---|
| **macOS** | Lima `vz` VM running rootless buildkitd | `limactl` + `buildctl` | no Linux kernel on the host; `vz` is built into `limactl`, no QEMU |
| **Linux** | bare **rootless buildkitd**, no VM | `buildctl` (+ `buildkitd`) | the host *is* Linux; a VM to run buildkit is pure overhead |

Same `buildctl build …`, same `BUILDKIT_HOST=unix://…/buildkitd.sock`, same recipe
surface. The wrapper (`build-container`, below) picks the provider by `runtime.GOOS`
and exports the socket; callers never know which one answered.

## What is verified

**1. Both binaries are already in the aqua standard registry** — no first-party
packaging, no sourcing-ladder descent (book/tooling.md):

| Tool | aqua package | As-researched | Host artifact |
|---|---|---|---|
| `limactl` (+ `lima`) | `lima-vm/lima` | v2.1.4 | native darwin/linux, arm64 + amd64 |
| `buildctl` | `moby/buildkit` | v0.31.1 | native darwin/linux, arm64 + amd64 |

- Lima ships per-OS tarballs (`lima-<ver>-Darwin-arm64.tar.gz`, `…-Linux-…`)
  carrying `bin/limactl`; the registry's current `version_constraint: "true"`
  override installs them cleanly.
- BuildKit's registry top-level `files:` is **just `buildctl`**; the daemon
  binaries (`buildkitd`, `buildkit-runc`, cni/qemu helpers) are added only under
  `goos: linux`/`windows` overrides — so on the macOS **host** aqua extracts
  `buildctl` alone, which is exactly and only what the host needs. Verified that
  BuildKit publishes native `buildctl` for `darwin-arm64` and `darwin-amd64`
  (v0.31.1 assets) — the host client is **not** trapped inside the guest.
- BuildKit releases carry provenance + SBOM + sigstore bundles per artifact — a
  supply-chain trust root the book's signature doctrine can actually verify.

**2. BuildKit-only is a first-class, shipped Lima template.** `templates/buildkit.yaml`
(at `v2.1.4`) provisions *rootless* buildkit and nothing else — no docker, no
nerdctl CLI, no k8s:

```yaml
base: template:_images/ubuntu-lts
containerd:
  system: false
  user: true            # rootless containerd, no root daemon
portForwards:
- guestSocket: "/run/user/{{.UID}}/buildkit-default/buildkitd.sock"
  hostSocket: "{{.Dir}}/sock/buildkitd.sock"
```

The template's own message documents the host wiring verbatim:
`export BUILDKIT_HOST="unix://{{.Dir}}/sock/buildkitd.sock"; buildctl debug workers`.
So "maybe just BuildKit is enough" is not a maybe — it is the exact configuration
upstream ships for this use case.

**3. No QEMU dependency on modern macOS.** Lima's builtin default `vmType` is
**`vz`** (Apple Virtualization.framework) on macOS 13.5+, `qemu` elsewhere. `vz`
is compiled into `limactl` — on Apple Silicon running a native arm64 guest, aqua
pinning `limactl` alone suffices; **no `qemu` binary to package.** (On Linux
hosts the driver is qemu/kvm — see open questions; the primary target is the
macOS dev/CI fleet.)

**4. Isolation is a native, total knob — `LIMA_HOME`.** Verified in Lima's
environment-variables doc:

- `LIMA_HOME` (default `~/.lima`) relocates **the entire Lima state root** — every
  VM, config, disk, and forwarded socket. Point it at a limen-owned directory
  (e.g. `$XDG_DATA_HOME/limen/lima`, or a repo-local `.limen/lima`) and the builder
  VM, its socket, and its config are physically separate files from the user's
  default `~/.lima`. A developer's personal `lima` instances are invisible to it
  and vice-versa.
- OrbStack shares *nothing* to begin with — different app, different state root,
  different everything — so "don't touch OrbStack" is satisfied by construction.
- The BuildKit template declares **no `networks:`**, so there is **no
  `socket_vmnet` privileged helper** and no shared bridge — user-mode networking
  only. One fewer thing to install, one fewer shared-state surface.
- Instance naming (`LIMA_INSTANCE`, or `limactl … <name>`) scopes further inside
  that home. A dedicated name like `limen-buildkit` makes the isolation legible.

## Mechanics (the whole happy path)

```sh
export LIMA_HOME="$repo/.limen/lima"        # isolation: dedicated state root
limactl start --name limen-buildkit \
        --tty=false <pinned buildkit.yaml>   # first boot provisions the guest
export BUILDKIT_HOST="unix://$LIMA_HOME/limen-buildkit/sock/buildkitd.sock"

buildctl build \
  --frontend dockerfile.v0 \
  --local context=. --local dockerfile=. \
  --output type=image,name=ghcr.io/farcloser/foo:tag,push=true
# or, no registry: --output type=oci,dest=image.tar  (portable OCI archive)
```

Because there is no local image store, the build **output must be declared**:
push to a registry (`type=image,push=true`) or write a tarball (`type=oci` /
`type=docker,dest=…`). For CI that pushes to GHCR this is ideal — arguably cleaner
than a daemon's implicit local store. For a human who wants `docker load`
afterward, the `type=docker` tarball is the bridge.

## Open questions / risks (decide before adopting)

1. **First-boot hermeticity.** `base: template:_images/ubuntu-lts` downloads an
   Ubuntu cloud image, and `containerd: user: true` runs Lima's rootless-containerd
   provisioning, which fetches the `nerdctl-full` bundle (containerd + buildkit +
   cni) at boot. That is **network provisioning outside aqua's pinned/checksummed
   world** — the exact class of unpinned fetch the tooling doctrine exists to kill.
   Mitigations to evaluate: pin the guest image by digest in our own copy of the
   template; pin the nerdctl-full version; or bake a first-party guest image
   (a `build-*`-style artifact) so the VM boots pre-provisioned and offline. **This
   is the real work of adoption — the host side is trivial, the guest supply chain
   is not.**
2. **Template must be vendored + pinned, not fetched.** Consuming `buildkit.yaml`
   by URL reintroduces drift. Embed our own copy (like limen embeds its baseline
   workflows) with the guest image digest and provisioning versions pinned; Renovate
   watches lima + buildkit, a manual ritual (or a `build-guest` repo) watches the
   guest image.
3. **Cross-arch builds need in-guest binfmt.** Native arm64→arm64 is free. Building
   `linux/amd64` on Apple Silicon needs QEMU binfmt handlers registered in the guest
   (e.g. `tonistiigi/binfmt`), which is a further provisioning step and a real
   perf/emulation cost. Scope whether multi-platform is even a requirement.
4. **Cross-arch builds — OUT OF SCOPE (confirmed 2026-07-07).** Native only
   (arm64→arm64, amd64→amd64). No binfmt/QEMU-in-guest, no multi-platform. Removes
   the biggest provisioning and perf cost.
5. **VM lifecycle & cost.** First boot is tens of seconds plus provisioning; a
   warm VM is fast. Decide the lifecycle: ephemeral per-CI-job (clean, slow) vs a
   persisted `limen-buildkit` instance (fast, but state to manage/GC). `limactl
   stop/delete` and the isolated `LIMA_HOME` make teardown clean either way.
6. **The interface is `build-container` (confirmed 2026-07-07).** Whenever the org
   needs to build a container image, the entry point is `build-container` — the
   provider-selecting wrapper (macOS→Lima, Linux→bare buildkitd) over the pinned
   `buildctl`. Callers depend on that name, not on Lima or the socket dance. Open:
   whether `build-container` is a first-party repo (like `build-curl`) or a `just`
   recipe living here — see the questions at the end.

## Adequacy test: build-curl (the chosen proof case)

build-curl is the right stress test because it "requires Docker" — so if the
BuildKit-only stack can do it, it can do the org's real work. Findings from
reading the upstream machinery (curl/curl-for-win, live on 2026-07-07):

- **curl-for-win uses Docker as a *runtime*, not a builder.** Its `_build.sh`
  (1980 lines) contains **no `docker build`**; the container model (per its README)
  is to run that script *inside* a reproducible `debian:testing-slim` container —
  i.e. `docker run debian:testing-slim ./_build.sh`, then collect artifacts. It
  downloads and compiles curl + its TLS/HTTP deps and emits release archives.
- **BuildKit cannot `docker run`** — it builds images, it does not execute
  containers as a runtime. So a literal lift of curl-for-win's invocation fails.
- **But it reframes cleanly, and build-curl is ours to author.** Wrap the build in
  a Dockerfile whose `RUN` executes the compile, and export the artifacts:

  ```dockerfile
  FROM debian:testing-slim
  COPY . /src
  RUN cd /src && ./_build.sh        # compile runs as a build step
  ```
  ```sh
  buildctl build --frontend dockerfile.v0 \
    --local context=. --local dockerfile=. \
    --output type=local,dest=./out   # extract the built archives — not an image push
  ```

  A BuildKit `RUN` *is* a containerized build step; `--output type=local` copies the
  final filesystem (the artifacts) to the host. This is a standard "BuildKit as a
  hermetic build sandbox" pattern.

**Verdict: adequate for build-curl, conditionally.** It works iff build-curl is
authored as a Dockerfile-wrapped build exporting via `type=local` (the natural
`build-*` output shape — artifacts, not a pushed image), **and** the build needs
nothing runtime-only: no `--privileged`, no loop/block devices, no cross-arch
binfmt (out of scope). Native compile-and-extract fits. A build that genuinely
needs `docker run` semantics — privileged, or a long-lived running service — would
*not* fit and would flag that BuildKit-only is too thin.

**Not executed here.** This sandbox has no nested virtualization, no Docker, and
restricted network, so the above is an analytic check against the real upstream
build model — not a live build. The live end-to-end proof must run on a real
macOS/Linux machine (the spike below).

## Recommendation

Adopt **Lima (`vz`) + rootless BuildKit template + host `buildctl`**, both tools
aqua-pinned, driven by a `just` recipe over an isolated `LIMA_HOME`. The host
side is genuinely trivial and rests entirely on already-packaged, signed tools.
**Gate adoption on resolving the guest-image supply chain (open questions 1–2)** —
that is where this either meets the book's pinned/verified bar or quietly
reintroduces the unpinned-fetch problem we removed everywhere else. Recommend a
spike: vendor a pinned `buildkit.yaml`, stand up `limen-buildkit` under a
throwaway `LIMA_HOME`, and build + push one image end-to-end to measure cold/warm
timings and confirm the socket-forward path on the current macOS fleet.

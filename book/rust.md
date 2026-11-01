# Rust

The Rust lanes (`just do lint rust`, `just do fix rust`, `just do test rust`, `just do build
rust`) run cargo from a toolchain the repository pins in two halves, and never from the
machine's.

## cargo is pinned through rustup, never ambient

cargo is not brew: brew is the subject under test and substitutes for nothing, which is why
it alone is captured from the ambient PATH. A rustup-managed cargo in `~/.cargo/bin` is
exactly the machine tool the hermetic PATH exists to hide, and, unlike brew, it has a pin
story. The toolchain is pinned in two halves:

- **rustup itself through the toolset.** `rust-lang/rustup` is an `http` package from the
  project's own static host, checksum-verified against the `.sha256` published next to each
  installer: the sourcing ladder's rung 2 without the signature, since rustup publishes none
  for the installer, so a bump is verified by hand like every other checksum-only pin. The
  entry is carried by the local registry for now: the standard registry's, with two changes
  for two defects in the [known upstream bugs](./upstream.md), the checksum file read by
  pattern (aqua records the posix checksums under one wrong environment otherwise) and the
  Windows download URL ending in `.exe` (the standard entry's answers 404). The pin lives in
  the Rust repository's `.hermetic/hermetic.yaml` as `rust-lang/rustup@<version>` with
  `registry: local` (`just do tools add local,rust-lang/rustup`), not in the baseline: no
  other repository pays for it. What it links is `rustup-init`.
- **The toolchain through `rust-toolchain.toml`.** rustup reads it from the working tree, so
  the channel and components are a committed, reviewed file, the same shape as `go.mod`'s
  `go` line, and rustup verifies what it downloads against the release manifest. The
  `channel` is an exact version, never a channel name (`stable` moves under the build);
  `components` names `clippy` and `rustfmt`, which the lint lanes run; `targets` names every
  target the repository cross-builds, so each `rust-std` is fetched and verified the same
  way; `profile` is `minimal`. A lane refuses to run without the file.

On Windows the host triple is the GNU one, `x86_64-pc-windows-gnu` and
`aarch64-pc-windows-gnullvm`, set on rustup by the lane: rustup's own default there is the
msvc triple, whose host builds (build scripts, proc-macros, the tests `cargo test` runs) link
through the runner image's Visual Studio, which nothing pins. The GNU hosts link through
pinned tools, so the pinned toolchain is the whole build on every leg: x64 through the
toolchain's own `rust-mingw` component; arm64, whose gnullvm host ships no linker of its own
and expects a MinGW clang on PATH, through the pinned zig by way of cargo-zigbuild's `zig cc`,
the linker the cross builds use, pointed at by cargo's linker variable for that target (it
maps the C runtime rustc asks for, `msvcrt`, to zig's; a bare `zig cc` does not). The cross
builds link through zig either way.

## What a lane does

Every Rust lane starts the same way (`rust_env` in `.limen/just/lib.just`). rustup's and
cargo's homes are `.build/rustup` and `.build/cargo`, under the checkout like the Go
build cache, so a checkout owns what it runs and nothing lands in `~/.cargo` or
`~/.rustup`. The first
lane installs rustup from the pinned `rustup-init` there with no toolchain, and the first
cargo call installs the toolchain `rust-toolchain.toml` names. On Windows the two homes are
given to rustup as native paths and hermetic's link resolves `rustup-init` to the `.exe`. A
repository without a `Cargo.toml` passes every lane vacuously, so a lane can sit in a
project's default.

- `lint rust`: `cargo fmt --check` and `cargo clippy` with warnings denied, every target and
  feature. `fix rust`: the same two, mutating.
- `test rust`: `cargo test`, every feature.
- `build rust`: the release binaries for this host, in cargo's own `target/release/`.
  `build rust targets <triple>…`: a release build per target into
  `target/<triple>/release/`, linked through the project's pinned zig (`ziglang/zig` in
  `.hermetic/hermetic.yaml`) by the project's pinned cargo-zigbuild
  (`rust-cross/cargo-zigbuild`, a standard-registry package): one C toolchain for the C and
  the Rust builds, so a crate with a C dependency builds the same way, and a macOS or
  Windows target links from any host (zig ships the libSystem stubs and the MinGW-w64 CRT).
  cargo-zigbuild maps the triple to a zig target and keeps the list of linker arguments
  rustc means for GNU ld that zig's lld refuses, a list that moves with rustc and zig; the
  lane carries no wrapper of its own, which would be a copy of that list, behind. Both pins
  are the Rust repository's, next to rustup's.

In CI the canonical cache action keeps what the lanes install under `.build/` (rustup with
the toolchain, cargo's caches, zig's) when the repository carries a `rust-toolchain.toml`,
keyed on that file and every `Cargo.lock` ([github](./github.md)); nothing to configure.

Every cargo call carries `--locked`: `Cargo.lock` is the crate pin, committed, and a lane
never rewrites it; a dependency moves by a pull request that changes the lock, from Renovate's
`cargo` manager (on by default; the shared preset restricts no manager). A lock the tree does
not have yet is made once, by hand, with `cargo generate-lockfile` from the lane's cargo
(`.build/cargo/bin`). The transitive crates in the lock move too: the shared preset's lock
file maintenance regenerates every lock in the repository weekly, in one pull request the
lanes prove like any other; between those, a transitive crate moves only with a direct bump or
a vulnerability alert.

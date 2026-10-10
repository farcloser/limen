# Rust

The Rust lanes exist ahead of the first Rust repository; this records the toolchain decision
so it is made rather than discovered.

## cargo is pinned through rustup, never ambient

The Rust modules (`just do lint rust`, `just do fix rust`) call `cargo`, and no repository
exercises them yet; this records the decision ahead of the first one, so it is made rather
than discovered. cargo is not brew: brew is the subject under test and substitutes for
nothing, which is why it alone is captured from the ambient PATH. A rustup-managed cargo in
`~/.cargo/bin` is exactly the machine tool the hermetic PATH exists to hide — and, unlike
brew, it has a pin story. The toolchain is pinned in two halves:

- **rustup itself through aqua.** The standard registry carries `rust-lang/rustup` as an
  `http` package from the project's own static host, checksum-verified against the `.sha256`
  published next to each installer. That is the sourcing ladder's rung 2 without the
  signature — rustup publishes no signature for the installer — so a bump is verified by
  hand like every other checksum-only pin, and the pin lives in the Rust repository's
  `.aqua/aqua.yaml`, not the baseline: no other repository pays for it.
- **The toolchain through `rust-toolchain.toml`.** rustup reads it from the working tree,
  so the channel and components are a committed, reviewed file — the same shape as
  `go.mod`'s `go` line — and rustup verifies what it downloads against the release
  manifest. The recipes reach cargo through the pinned rustup, never through the proxies in
  `~/.cargo/bin`; that wiring lands in the Rust modules the day a repository needs it, and
  until then the modules stay as they are: named, never in a default, and not runnable.

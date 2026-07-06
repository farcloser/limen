# TODO

- **(Low priority) Local Windows test lane.** aqua cannot manage UTM (a GUI .app on a
  dmg — outside aqua's archive-and-link model), and the CLI-first alternative (tart)
  cannot boot Windows guests (Apple's Virtualization.framework has no TPM/Secure Boot
  emulation — which is why UTM uses QEMU for Windows). If Windows portability becomes a
  recurring local concern: UTM installs at the machine layer (mumbrew territory), a
  Windows 11 ARM guest gets provisioned once by hand (CrystalFetch image, Git for
  Windows, OpenSSH server, snapshot), and a `just do test windows` recipe in the root Justfile
  drives it via utmctl + ssh. Until then, the windows-2025 CI leg is the test bench.

- **CI baseline: one residual.** The `workflows` rule landed 2026-07-03 (10th rule:
  the checksum-update workflow and setup-aqua action content-pinned; ci.yaml and
  renovate.json5 seeded once; release.yaml seeded where goreleaser config exists — see
  book/mandatory-files.md). Remaining: decide whether the aqua-bootstrap pins
  (installer version/checksum, aqua version) stay duplicated between the setup-aqua
  action and limen-install or get a single source.

- **Rust under the hermetic PATH (AUDIT.md A3).** `just do lint rust` / `just do fix rust`
  cannot work today: cargo is not aqua-installable (rustup owns the toolchain) and
  `~/.cargo/bin` is deliberately not on the hermetic PATH — the recipes are dead code in
  every repo. Decide: extend the hermetic PATH with `~/.cargo/bin` (one documented
  exception to "everything through aqua"), or drop the rust recipes until a rust project
  exists. Also unify the shape while at it: lint has a `rust` submodule
  (`lint-rust.just`), fix has an inline `rust:` recipe.

- **limen-install: working since 2026-07-01, two follow-ups.** The bootstrap ships an
  embedded `github_release` registry entry for `farcloser/limen`, writes config + registry +
  policy next to the global aqua config, allows the policy, pins release checksums
  (`aqua update-checksum` from goreleaser's `checksums.txt`), installs, and exports
  `AQUA_POLICY_CONFIG` in the shell rc **append-preserving**. Verified end-to-end
  2026-07-01 against the public v0.0.0-test.1 release, anonymously. Remaining:
  1. Cut the real `v0.0.1` release and bump the pin in the embedded global config **and** in
     limen's canonical `aqua.yaml` (both currently the disposable `v0.0.0-test.1`).
  2. Now that limen is **public**, graduate to the **standard registry** (PR to
     `aquaproj/aqua-registry`): default-policy clean, no policy plumbing, Renovate-native.
     Keep the local-registry mechanism in limen-install regardless — it is the template for
     any future *private* farcloser tool. The `go_install` route stays **rejected** (would
     drag the Go toolchain onto every machine to save one goreleaser config).

- **Release hardening: remaining pieces.** The CI lane exists (2026-07-02):
  `just do release vX.Y.Z` (signed tag by a human) triggers
  `.github/workflows/release.yaml`, which runs `just do release --ci` — goreleaser +
  **keyless** cosign (Fulcio workflow identity, Rekor-logged). The key-based local lane
  remains for private repos and as the escape hatch (`just do release --local <key> vX.Y.Z`). Still to do:
  1. **Repository ruleset** restricting who can create `v*` tags — the tag push is now the
     release button. (Org-side, manual.)
  2. **Commit `cosign.pub`** so the key-based lane's verifiers have the key (the pair
     exists in the gitignored `_scratch/` and signed v0.0.0-test.1).
  3. **Exercise the CI lane once** (the next `v0.0.0-test.N` tag) and record the keyless
     verify incantation in the release notes / README.
  4. **Provenance**: bolt on SLSA (`slsa-github-generator`) or GitHub's first-party
     `actions/attest-build-provenance` (lighter; `gh attestation verify`) — both need the
     Actions OIDC this lane now has.
  5. **Aqua-side signature verification** (cosign opts in our registry entry — keyless
     identity check on install) lands with the standard-registry work above.

- **Renovate rollout.** The repo side is ready (2026-07-02): `renovate.json5` (aqua
  preset, cooldown, DCO sign-off on bot commits, `gitIgnoredAuthors` so checksum fix-ups
  don't freeze branches) and `.github/workflows/update-aqua-checksum.yaml` (regenerates
  `aqua-checksums.json` on Renovate's branches — the piece Renovate cannot do itself).
  Remaining, org-side and manual:
  1. Install the **Mend Renovate GitHub App** (github.com/apps/renovate) on the org,
     scoped to limen (+ limen-install). Config exists, so it activates without onboarding.
  2. Optional but recommended: add a fine-grained PAT (contents: write) as the
     `UPDATE_AQUA_CHECKSUM_TOKEN` secret — with only the default token, GitHub suppresses
     CI on the checksum fix-up commit, so Renovate PRs would show checks for the
     penultimate commit only.
  3. Watch the first aqua-bump PR end-to-end: bump → checksum commit → green CI → merge.
  (AUDIT.md B3 keeps the book-side residue: bootstrap does not create these files and no
  rule checks them yet.)

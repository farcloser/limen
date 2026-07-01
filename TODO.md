# TODO

- **CI: graduate the workflow to the canonical baseline.** limen now ships
  `.github/workflows/ci.yaml` (2026-07-02): SHA-pinned checkout only, checksum-verified
  aqua bootstrap with a pinned aqua version, `permissions: {}`, pinned runner images
  (ubuntu + macOS — the latter exercises the bash 3.2 floor), and every check running
  through the same `just` recipes as local. The file is deliberately generic (zero
  project-specific content) so limen can content-pin and distribute it like the other
  canonical files. Remaining: teach limen a rule + fix path for `.github/workflows/`
  (embed the workflow, create/overwrite it), and decide whether the aqua-bootstrap pins
  (installer version/checksum, aqua version) stay duplicated with limen-install or get a
  single source. Also still open: Renovate rollout — install the Mend GitHub App on the
  org, and ship the `update-aqua-checksum` workflow (below) with it.

- **Rust under the hermetic PATH (AUDIT.md A3).** `just lint rust` / `just fix rust`
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
  `just release --cut vX.Y.Z` (signed tag by a human) triggers
  `.github/workflows/release.yaml`, which runs `just release --ci` — goreleaser +
  **keyless** cosign (Fulcio workflow identity, Rekor-logged). The key-based local lane
  remains for private repos and as the escape hatch (`just release vX.Y.Z`). Still to do:
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

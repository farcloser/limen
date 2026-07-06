# TODO

- **CI baseline: one residual.** The `workflows` rule landed 2026-07-03 (10th rule:
  the checksum-update workflow and setup-aqua action content-pinned; ci.yaml and
  renovate.json5 seeded once; release.yaml seeded where goreleaser config exists — see
  book/mandatory-files.md). Remaining: decide whether the aqua-bootstrap pins
  (installer version/checksum, aqua version) stay duplicated between the setup-aqua
  action and limen-install or get a single source.

- **Rust under the hermetic PATH.** `just do lint rust` / `just do fix rust`
  cannot work today: cargo is not aqua-installable (rustup owns the toolchain) and
  `~/.cargo/bin` is deliberately not on the hermetic PATH — the recipes are dead code in
  every repo. Decide: extend the hermetic PATH with `~/.cargo/bin` (one documented
  exception to "everything through aqua"), or drop the rust recipes until a rust project
  exists.

- **limen-install: working since 2026-07-01, two follow-ups.** The bootstrap ships an
  embedded `github_release` registry entry for `farcloser/limen`, writes config + registry +
  policy next to the global aqua config, allows the policy, pins release checksums
  (`aqua update-checksum` from goreleaser's `checksums.txt`), installs, and exports
  `AQUA_POLICY_CONFIG` in the shell rc **append-preserving**. Verified end-to-end
  2026-07-01 against the public v0.0.0-test.1 release, anonymously. Remaining:
  1. Cut the real `v0.0.1` release. The embedded global config already moved (it pins
     `v0.0.0-test.3` as of 2026-07-05); limen's canonical `aqua.yaml` still pins the
     disposable `v0.0.0-test.1` — Renovate bumps it once PRs flow, and released binaries
     now move an existing pin on `fix` anyway (the baseline-owned exception, 2026-07-05),
     so only the release itself remains.
  2. Now that limen is **public**, graduate to the **standard registry** (PR to
     `aquaproj/aqua-registry`): default-policy clean, no policy plumbing, Renovate-native.
     Keep the local-registry mechanism in limen-install regardless — it is the template for
     any future *private* farcloser tool. The `go_install` route stays **rejected** (would
     drag the Go toolchain onto every machine to save one goreleaser config).

- **Release hardening: remaining pieces.** The CI lane exists (2026-07-02):
  `just do release vX.Y.Z` (signed tag by a human) triggers
  `.github/workflows/release.yaml`, which runs `just do release --ci` — goreleaser +
  **keyless** cosign (Fulcio workflow identity, Rekor-logged). The key-based local lane
  remains for private repos and as the escape hatch (`just do release --local <key> vX.Y.Z`).
  Done since: the `v*` tag ruleset is automated (`limen github` enforces `limen:tags`,
  restricting tag creation/update/deletion) and the CI lane is proven — `v0.0.0-test.2`
  and `v0.0.0-test.3` both shipped keyless-signed `checksums.txt.sigstore.json` bundles.
  Still to do:
  1. **Commit `cosign.pub`** so the key-based lane's verifiers have the key (the pair
     exists in the gitignored `_scratch/` and signed v0.0.0-test.1).
  2. **Record the keyless verify incantation** in the release notes / README — today it
     lives only in a `.goreleaser.yaml` comment.
  3. **Provenance**: bolt on SLSA (`slsa-github-generator`) or GitHub's first-party
     `actions/attest-build-provenance` (lighter; `gh attestation verify`) — both need the
     Actions OIDC this lane now has.
  4. **Aqua-side signature verification** (cosign opts in our registry entry — keyless
     identity check on install) lands with the standard-registry work above.

- **Renovate rollout.** The repo side is ready (2026-07-02): `renovate.json5` (aqua
  preset, cooldown, DCO sign-off on bot commits, `gitIgnoredAuthors` so checksum fix-ups
  don't freeze branches) and `.github/workflows/update-aqua-checksum.yaml` (on Renovate's
  branches: regenerates `aqua-checksums.json`, and runs the newly pinned `limen fix` on
  limen-bump branches so the baseline files move with the pin — the pieces Renovate cannot
  do itself).
  The Mend Renovate GitHub App is installed (2026-07-02). Remaining:
  1. Optional but recommended: add a fine-grained PAT (contents: write) as the
     `UPDATE_AQUA_CHECKSUM_TOKEN` secret — with only the default token, GitHub suppresses
     CI on the fix-up commit, so Renovate PRs would show checks for the penultimate
     commit only.
  2. Watch the first aqua-bump PR end-to-end: bump → fix-up commit → green CI → merge.
     **As of 2026-07-05 no Renovate PR has appeared** despite bumpable pins (the limen
     pin alone is two releases behind) — if none shows up soon, check the app's repo
     scoping and the dependency dashboard.

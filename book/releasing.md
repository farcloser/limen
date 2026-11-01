# Releasing

A release is a signed tag the human makes, and everything that follows from it. The recipe is
the interface (`just do release`); this chapter is its rules and its lanes.

- **The recipe is shared; artifacts are opt-in.** `.limen/just/release.just` is imported
  *flat* into the `do` namespace (hence `just do release vX.Y.Z`; a module invocation could
  not take the tag argument). Every repository can be released: `just do release vX.Y.Z`
  verifies a clean tree, creates the *signed* tag and pushes it. For a repository without a
  `.release-go.yaml` — a Go module, a tap, a configuration — that signed tag **is** the
  release: Go's module proxy, Renovate and `go get` all read it. **Every tag has a release
  page**, so the recipe then creates it (`gh release create --verify-tag --generate-notes`,
  through the pinned `gh` and the human's own auth), with the notes below; a retry finds the
  page and leaves it. The recipe owns that page: a project's own release workflow uploads
  its artifacts into it (`gh release upload`) and creates it only when none exists — a tag
  pushed by hand — since two creators on one tag leave the second failing with the assets
  unattached. The tag is `vX.Y.Z`, `vX.Y.Z-prerelease`, or a repackaging's rebuild of the
  same upstream version, `vX.Y.Z.N` ([forks](./forks.md)). A `.release-go.yaml`
  (project-owned, like the root `.justfile`) opts the repository into artifacts; the lane
  below and the dry run share every guard. It is goreleaser's configuration under the lane's
  name, like `.lint-go.yaml` (the recipes pass it with `--config`), and its first line is `#
  yaml-language-server: $schema=https://goreleaser.com/static/schema.json`, how an editor
  finds the schema away from goreleaser's default name; `limen check` requires it and `limen
  fix` adds it. The **CI lane** (public repos, the default): `just do release vX.Y.Z`
  creates the signed tag — a human signs the intent — and pushes it; the tag push triggers
  the release workflow, which runs `just do release --ci`: goreleaser plus **keyless**
  cosign, the artifacts signed by the workflow's short-lived Fulcio identity (no key exists
  anywhere) and logged in Rekor, then GitHub's build provenance attestation over every
  archive, every SBOM and `checksums.txt`, which `gh attestation verify <asset> --repo
  <owner>/<repo>` checks wherever `gh` is, no cosign needed: what a bootstrap verifies a
  binary with before any tool exists (a public repository; GitHub stores attestations for a
  private one on Enterprise Cloud only, and the lane skips the step there). `just do release
  --local --dry-run` builds an unsigned snapshot into `.build/release/`, SBOMs included, so
  the whole lane is proved on a laptop short of the signature. There is no key-based lane:
  one existed, and no repository ever used it or committed the public key a verifier would
  need, so a release is signed keyless in CI or not at all. The workflow contains no release
  logic of its own.
- **The signing and provenance policy.** Written once, for the fleet; the canonical lane
  carries it, and a repository's own release lane (a fork's, an app's) conforms to it.
  - **Every release artifact is covered by two independent verifications**: the cosign
    Sigstore bundle over `checksums.txt`, keyless, the workflow's identity, Rekor-logged;
    and GitHub's build provenance attestation over every archive, every SBOM and
    `checksums.txt`. A consumer verifies either: `cosign verify-blob` against the workflow
    identity, or `gh attestation verify <asset> --repo <owner>/<repo>`; hermetic verifies
    the checksums against the bundle on every install. Nothing fetched goes unattested.
  - **Every release carries an SBOM per archive**, SPDX JSON by syft from the toolset
    (goreleaser's `sboms:`), listed in `checksums.txt` and attested like the archives. It
    names the modules the binaries were built from, read off Go's embedded build
    information, so a vulnerability in a dependency is matched to the releases that carry it
    without rebuilding them. The section is limen's inside `.release-go.yaml`: `limen check`
    fails a configuration that makes archives without the canonical one, `limen fix` writes
    it; a configuration saying `archives: []` has nothing to catalog and needs none.
  - **No long-lived signing key exists.** No cosign key pair, no key-based lane, no
    `cosign.pub`: a key is a thing to leak and rotate, and the keyless identity is the
    workflow at the tag, which is what a consumer wants to know.
  - **Apple: sign what macOS evaluates, notarize what a human installs.** A bundle a human
    installs through the Finder or a System Settings pane (a screen saver, an app) is signed
    with the team's Developer ID Application identity, hardened runtime on, notarized and
    stapled: Gatekeeper evaluates it, and without notarization a current macOS refuses it
    outright, or an installer strips the quarantine attribute, which is a workaround and not
    a release. A bundle launched by path and never through Launch Services (Taciturn.app,
    run by its LaunchAgent) is signed with the same identity and not notarized: Gatekeeper
    is not in its path. A command-line binary hermetic installs is neither: hermetic's
    download carries no quarantine attribute, so Gatekeeper never consults a notarization
    for it, and a long-lived Apple certificate does not belong in lanes that are keyless
    otherwise. One exception by necessity: a binary that needs an Apple entitlement to run
    at all (ossein and Virtualization.framework) is signed ad hoc with that entitlement at
    build, which grants the entitlement and claims no identity.
  - **A signing credential is an environment secret, held by one job that publishes
    nothing.** The Developer ID `.p12` and its password, the App Store Connect API key, a
    provisioning profile: each is a secret of a `release` environment, never of the
    repository or the organisation, which every account with write access reads (the
    [`secrets` rule](./mandatory-files.md#the-secrets-rule--a-secret-stays-behind-an-environment)
    says why). The environment admits release tags only (`v*`) and
    names the human as its required reviewer, so no run holds the credential without the
    human's click on that run. The job that holds it (`environment: release`) runs the
    pinned toolchain and the platform's own tools with a read-only token, imports the
    certificate into a keychain it creates, signs, and notarizes and staples what
    Gatekeeper evaluates, then removes the keychain and the key; no action from outside the
    repository runs while the credential exists (checkout before the import, the artifact
    upload after the removal, and the removal is the job's own step, `if: always()`, since
    a composite action cannot clean up after itself). It hands the signed artifacts to a
    second job as a run artifact. That second job holds no Apple
    secret: it verifies what arrived (the signature's chain to Developer ID, the stapled
    ticket, `stapler validate`) and runs goreleaser, cosign and the attestation. The
    installer asserts the same chain and ticket again on the user's machine. Nothing about
    the credential lives in the tree. `limen github check` holds GitHub to it: no such secret
    at the repository or the organisation (`actions-secrets`), and a `release` environment of
    that shape holding what the job reads (`environments`), which `fix` creates or reconciles
    with the owners as reviewers; the values are set by hand (`gh secret set NAME --env
    release`). The tree's `secrets` rule holds the job to `environment: release`
    ([github](./github.md), [mandatory files](./mandatory-files.md)).
  - **What is deliberately not done**: no notarization of command-line binaries, no SBOM for
    a repository without artifacts (its release is the signed tag), no signature of the
    source archive GitHub generates (it is not a release asset of ours).
- **The Apple release lane.** A project that ships a bundle macOS evaluates releases through
  `limen-release-apple.yaml`, a reusable workflow in limen, called at a commit like the other
  lanes.
  - **Declaring it.** The project points its `release.yaml` at the lane, a `uses:` reference of the
    form `farcloser/limen/.github/workflows/limen-release-apple.yaml@<commit>` that Renovate moves,
    and runs `limen fix`: the file is a thin caller from then on, reset around its reference like
    the three every repository carries ([mandatory files](./mandatory-files.md)).
  - **The `sign` job**, on macOS in the `release` environment with a read-only token: it imports
    the Developer ID identity into a job-scoped keychain, reads the team id off the
    certificate, writes the provisioning profile and the notarization key when the environment
    holds them (the job requires the identity; the other two are read as set or absent, never
    half set, since the policy signs every bundle and notarizes only what Launch Services
    evaluates; none is a `required` secret of the call, since GitHub checks those in the
    caller's scope, where an environment secret does not exist yet), exports the lane's names
    (`APPLE_SIGN_IDENTITY`, `APPLE_SIGN_TEAM`, `APPLE_SIGNING_KEYCHAIN`,
    `APPLE_PROVISIONING_PROFILE_PATH`, `APPLE_NOTARY_KEY`, `APPLE_NOTARY_KEY_ID`,
    `APPLE_NOTARY_ISSUER_ID`), runs the project's `just package <version>`, removes every
    credential whatever the outcome, and hands `.build/assets/` to `publish` as a run
    artifact.
  - **The `publish` job** holds no Apple secret: it runs `just do release --ci` on macOS, where
    `verify-assets` has `codesign` and `stapler`, and attests `.build/assets/*` and `checksums.txt`.
  - **What the project owes**: `package <version>`, which builds, signs with the exported identity,
    notarizes where the policy says and writes every release asset under `.build/assets/`; and
    `verify-assets <version>`, goreleaser's before hook, asserting what `.build/assets/` holds. Both
    are composed from `just do apple-verify-signed`, `apple-verify-notarized`, `apple-notarize` and
    `apple-package` ([recipes](./recipes.md)).
  - **Its `.release-go.yaml`** builds nothing: `builds: [{id: none, skip: true}]`, `archives: []`,
    `before.hooks: [just verify-assets {{ .Version }}]`, `.build/assets/*` under
    `checksum.extra_files` and `release.extra_files`, and `snapshot.version_template` set to the
    project's development version, so that a local dry run looks for the zip `package` made
    (goreleaser's default, `-SNAPSHOT-<sha>`, breaks it).
  - **The environment** is `limen github fix`'s to build; its secrets are set by hand (`gh secret
    set APPLE_DEVELOPER_ID_P12 --env release`, and so on).
- **A command under `cmd/` either ships or says it does not.** `cmd/` is where a Go
  repository keeps its main packages, released or not, as Go projects do. The release
  configuration, `.release-go.yaml`, says which of them ship: `limen check` fails a main under
  `cmd/` that is neither one of its `builds` nor named in the project's list of unshipped
  commands, `commands.unshipped` in the root `.lint-go.yaml`
  ([linting Go](./linting-go.md#one-lint-baseline-per-project-carve-outs-lint-goyaml)), naming
  the commands, and `limen fix` reports the same as an advisory, since both files are the
  project's to write (limen's own `.release-go.yaml` is the model). The list is explicit, one
  command per entry, so an unlisted main stays red, and an entry that names no such command
  (one that ships, a directory, a pattern) fails too: a product whose release carries no
  binaries is consumed by building it from source, which decides where it can live in the tool
  layout ([tooling](./tooling.md#go-built-tools-are-gomod-tools)): `godolint` and `dot` were
  tool directives partly because their releases carried no binaries. A helper the recipes build
  and run from the tree, a DNS reconciler, a kernel builder, is an unshipped command and stays
  under `cmd/`, declared; it does not move to `internal/cmd/`, an import fence that says
  nothing of a main, nor to `hack/`, the home of the scripts around a build. A runnable program
  that shows the library in use lives under `examples/<name>/`, Go's usual home for one:
  outside `cmd/`, so the rule does not read it, and unlike `_examples/`, still built, vetted
  and linted by `./...`.
- **A release carries a real change.** A repository releases when it has something new for its
  consumers: a change in its own code, or in an upstream it ships. A change to the tooling it
  is built and checked with (a limen pin, a linter, a CI action) is not one, and neither is
  documentation; it goes out with the next real change.
- **A release waits for an empty queue.** No tag while the repository has an open pull request,
  a bot's included: what is open is either part of the release, in which case the tag is
  early, or not, in which case it lands minutes after the tag and the next release is a bump
  away. The recipe refuses while `gh pr list` is not empty; closing or merging what is open is
  the human's, and the queue it leaves is the release. A pull request the human parks, one
  the release must not carry, is not closed only to tag: its number goes in `PARKED_PRS`
  (space-separated), the recipe prints it as skipped, and every other open one still blocks.
- **A release's notes are its pull requests' titles.** No one writes them, and no file in the
  tree holds them: a hand-kept `CHANGELOG.md` puts every open pull request on the same lines,
  so each merge forces a rebase on the others. The `changelog:` section of `.release-go.yaml`
  is limen's inside a file otherwise the project's, `use: github-native`, so goreleaser asks
  GitHub for the notes, every pull request merged since the previous tag; without goreleaser,
  the recipe asks for the same. `.github/release.yml`, content-pinned everywhere, keeps the
  notes to what changed for a consumer: **Breaking changes** (the `breaking` label), then
  **Changes**; a dependency bump (Renovate's pull requests, and any carrying the
  `dependencies` label) and a pull request labelled `internal` are left out altogether, and
  the commit log has them. So the title is the release note: it says what changed for a
  consumer, not how. A pull request that breaks a consumer (an API removed or changed, a
  behaviour a caller relied on) carries the `breaking` label; one that changes nothing for a
  consumer, a tooling or CI change, a limen bump by hand, documentation for maintainers,
  carries `internal`; `gh label create breaking` and `gh label create internal` make them
  the first time a repository needs them. `limen check` fails a missing or different
  `changelog:` section and a drifted `release.yml`; `limen fix` sets the one and resets the
  other.

# Releasing

A release is a signed tag the human makes, and everything that follows from it. The recipe is
the interface (`just do release`); this chapter is its rules and its lanes.

- **The recipe is shared; artifacts are opt-in.** `.limen/just/release.just` is imported *flat*
  into the `do` namespace (hence `just do release vX.Y.Z`; a module invocation could not take
  the tag argument). Every repository can be released: `just do release vX.Y.Z` verifies a
  clean tree, creates the *signed* tag and pushes it. For a repository without a
  `.release-go.yaml` — a Go module, a tap, a configuration — that signed tag **is** the
  release: Go's module proxy, Renovate and `go get` all read it. **Every tag has a release
  page**, so the recipe then creates it (`gh release create --verify-tag --generate-notes`,
  through the pinned `gh` and the human's own auth), with the notes below; a retry finds the
  page and leaves it. The recipe owns that page: a project's own release workflow uploads its
  artifacts into it (`gh release upload`) and creates it only when none exists — a tag pushed
  by hand — since two creators on one tag leave the second failing with the assets
  unattached. The tag is `vX.Y.Z`, `vX.Y.Z-prerelease`, or a repackaging's rebuild of the
  same upstream version, `vX.Y.Z.N` ([forks](./forks.md)). A `.release-go.yaml` (project-owned,
  like the root `.justfile`) opts the repository into artifacts, and the two lanes below share
  every guard. It is goreleaser's configuration under the lane's name, like `.lint-go.yaml`
  (the recipes pass it with `--config`), and its first line is
  `# yaml-language-server: $schema=https://goreleaser.com/static/schema.json`, how an editor
  finds the schema away from goreleaser's default name; `limen check` requires it and
  `limen fix` adds it. The **CI lane** (public repos, the default): `just do release vX.Y.Z` creates the
  signed tag — a human signs the intent — and pushes it; the tag push triggers the release
  workflow, which runs `just do release --ci`: goreleaser plus **keyless** cosign, the
  artifacts signed by the workflow's short-lived Fulcio identity (no key exists anywhere) and
  logged in Rekor, then GitHub's build provenance attestation over every archive and
  `checksums.txt`, which `gh attestation verify <asset> --repo <owner>/<repo>`
  checks wherever `gh` is, no cosign needed: what a bootstrap verifies a binary with before
  any tool exists (a public repository; GitHub stores attestations for a private one on
  Enterprise Cloud only, and the lane skips the step there). The **local lane** (private
  repos, where nothing touches the public
  transparency log, and the escape hatch when CI is down): `just do release --local
  <cosign-key> vX.Y.Z` does the same tag work, then runs goreleaser with key-based cosign; the
  key path is a mandatory argument, and the passphrase is prompted (or piped in with
  `--cosign-password-stdin`, keeping it out of argv and shell history). `just do release
  --local --dry-run` builds an unsigned snapshot into `.build/release/` in either lane. The
  workflow contains no release logic of its own.
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
  the recipe asks for the same. `.github/release.yml`,
  content-pinned everywhere, groups them: **Breaking changes** (the `breaking` label),
  **Changes**, then **Dependencies** (Renovate's pull requests, and any carrying the
  `dependencies` label). So the title is the release note: it says what changed for a
  consumer, not how. A pull request that breaks a consumer (an API removed or changed, a
  behaviour a caller relied on) carries the `breaking` label; `gh label create breaking` makes
  it the first time a repository needs one. `limen check` fails a missing or different
  `changelog:` section and a drifted `release.yml`; `limen fix` sets the one and resets the
  other.

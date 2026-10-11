# Releasing

A release is a signed tag the human makes, and everything that follows from it. The recipe
is the interface (`just do release`); this chapter is its rules and its lanes.

- **The recipe is shared; artifacts are opt-in.** The recipe lives in `.limen/just/release.just`,
  imported *flat* into the `do` namespace (`do.just`, hence `just do release vX.Y.Z` — a
  module invocation could not take the tag argument). Every repository can be released:
  `just do release vX.Y.Z` verifies a clean tree, creates the *signed* tag and pushes it. For
  a repository without a `.release-go.yaml` — a Go module, a tap, a configuration — that
  signed tag **is** the release: there is nothing to build or publish, and Go's module
  proxy, Renovate and `go get` all read the tag. **Every tag has a release page**, so the
  recipe then creates it (`gh release create --verify-tag --generate-notes`, through the
  pinned `gh` and the human's own auth), with the notes below; a retry finds the page and
  leaves it. The recipe owns that page: a project's own release workflow uploads its
  artifacts into it (`gh release upload`) and never creates it — two creators on one tag,
  and the second fails with the assets unattached. Six library repositories released bare
  tags for months, and no reader had notes for any of them until this. The tag is
  `vX.Y.Z`, `vX.Y.Z-prerelease`, or a fork's rebuild of the same upstream version,
  `vX.Y.Z.N` ([forks](./forks.md)). A `.release-go.yaml` (project-owned, like
  the root .justfile) opts the repository into artifacts, and the two lanes below then share
  every guard. It is goreleaser's configuration under the lane's name, like `.lint-go.yaml`
  (the recipes pass it with `--config`), and its first line is
  `# yaml-language-server: $schema=https://goreleaser.com/static/schema.json`: away from
  goreleaser's default name, that header is how an editor finds the schema. `limen check`
  requires it, and fails a leftover `.goreleaser.yaml`, which nothing reads; `limen fix`
  renames one and adds the header. The **CI lane** (public repos, the default): `just do release vX.Y.Z`
  verifies a clean tree, creates the *signed* tag — a human signs the intent — and pushes
  it; the tag push triggers the release workflow, which runs `just do release --ci`:
  goreleaser plus **keyless** cosign, the artifacts signed by the workflow's short-lived
  Fulcio identity (no key exists anywhere) and logged in Rekor. The **local lane**
  (private repos — nothing touches the public transparency log — and the escape hatch
  when CI is down): `just do release --local <cosign-key> vX.Y.Z` does the same tag work,
  then runs goreleaser with key-based cosign — the key path is a mandatory argument, and
  the passphrase is prompted (or piped in with `--cosign-password-stdin`, keeping it out of
  argv and shell history). `just do release --local --dry-run` builds an unsigned
  snapshot into `build/release/` in either lane. The recipe is the interface: the
  workflow contains no release logic of its own.
- **A release carries a real change.** A repository releases when it has something new for
  its consumers: a change in its own code, or in an upstream it ships. A change to the
  tooling it is built and checked with (a new limen pin, a linter, a CI action) is not one,
  and neither is documentation (the readme, the licence file, `UPSTREAM.md`, a comment): it
  never triggers a release on its own, and goes out with the next real change.
- **A release waits for an empty queue.** No tag while the repository has an open pull
  request, a bot's included: what is open is either part of the release, in which case the
  tag is early, or not, in which case it lands minutes after the tag and the next release is
  a bump away, the churn this rule ends. The release recipe refuses while `gh pr list` is
  not empty; closing or merging what is open is the human's, and the queue it leaves is the
  release.
- **A release's notes are its pull requests' titles.** No one writes them, and no file
  in the tree holds them: a hand-kept `CHANGELOG.md` puts every open pull request on the
  same lines, so each merge forces a rebase on the others. The `changelog:` section of
  `.release-go.yaml` is limen's inside a file otherwise the project's: `use:
  github-native`, so goreleaser asks GitHub for the notes, and GitHub lists every pull
  request merged since the previous tag; without goreleaser, the release recipe asks for
  the same notes. `.github/release.yml`, content-pinned in every repository, groups them: **Breaking changes** (the `breaking` label),
  **Changes**, then **Dependencies** (Renovate's pull requests, and any carrying the
  `dependencies` label). So the title is the release note: it says what changed for a
  consumer, not how. A pull request that breaks a consumer (an API removed or changed, a
  behaviour a caller relied on) carries the `breaking` label; `gh label create breaking`
  makes it the first time a repository needs one. `limen check` fails a missing or
  different `changelog:` section and a drifted `release.yml`; `limen fix` sets the one and
  resets the other.

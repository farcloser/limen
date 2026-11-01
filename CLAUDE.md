@AGENTS.md

## Limen's own

What holds in this repository on top of the working agreement every repository
shares:

- **Never put specific version numbers in the book (`book/`).** Use generic placeholders in
  prose and examples. The real, pinned versions live in `.limen/hermetic.yaml` and
  `.hermetic/hermetic.yaml` and are managed by hermetic and Renovate: the book explains *how*, not
  *which*.
- **Never reconverge the sibling repositories (mumbrew, limen-install, godolint,
  homebrew-brews, …) after changing limen's canonical baseline.** They catch up through
  limen *releases* (Renovate bumps the pin; the checksum-update workflow runs the newly
  pinned `limen fix` on the branch) — running a dev-build fix in them plants files their
  pinned limen flags as drift, failing their CI. The one exception: when canonical files
  are MOVED or renamed outside `.limen/just/` (which fix sweeps itself), the fixer seeds
  the new path but never deletes the old one, so the strays need a manual sweep; and a
  repository's copy of `limen-verify.yaml`, which fix removes.

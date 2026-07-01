# AGENTS.md

> The justfile is the source of truth for all workflows.
> Run `just --list` to discover every available task.

## Workflow rules

- **Every workflow runs through `just`.** If a recipe covers the task, the recipe is the
  interface: `just lint go`, `just fix yaml`, `just test`, `just lint links`,
  `just tools set …`. Never invoke the underlying tools directly — no bare `golangci-lint`,
  `gofmt`, `yamlfmt`, `shellcheck`, `lychee`, `go test`, `aqua`, `cargo` — when a recipe
  exists: recipes run the pinned versions on the hermetic PATH and often do more than the
  obvious command (`just lint go` is `golangci-lint run` *and* `golangci-lint fmt --diff`).
  Direct invocation is fine only when no recipe covers the need (e.g. one package's tests
  while debugging) — say so explicitly when you do it.
- Always name the recipe (`just fix go`, `just lint yaml`); module defaults are curated
  subsets, not "run everything".
- Do not bump tool versions manually; use `just tools set` / `just tools update`.

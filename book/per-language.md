# Per-language rules (moved)

This chapter was split into topic chapters, each generic with a language sub-document
([the index](./index.md)). Where its sections went:

- Shell → [linting-shell](./linting-shell.md); YAML → [linting-yaml](./linting-yaml.md)
- Homebrew formulas → [homebrew](./homebrew.md); Rust → [rust](./rust.md)
- the lint baseline, judging and silencing a finding, the shadowed error, what golangci-lint
  cannot see → [linting](./linting.md) and [linting-go](./linting-go.md)
- the baseline version, no replace, a port from the standard library →
  [dependencies-go](./dependencies-go.md)
- paths, startup preconditions, the umask → [boundaries-go](./boundaries-go.md)
- errors and the chaining API → [errors-go](./errors-go.md); logging → [logging-go](./logging-go.md)
- the tests → [testing-go](./testing-go.md)

A per-language rule applies when its trigger is present and reports nothing otherwise
([mandatory files](./mandatory-files.md)).

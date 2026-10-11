# Linting — YAML

The YAML form of [linting](./linting.md): yamlfmt, on one content-pinned configuration.

## `.limen/.yamlfmt`

| Trigger | Requirement |
|---------|-------------|
| The repository contains YAML files. | A `.limen/.yamlfmt` is present and matches the canonical baseline **exactly**. |

YAML is whitespace-significant and drifts between authors and editors: indentation, quoting,
flow against block style. Any project that ships YAML formats it the same way everywhere.
[yamlfmt](https://github.com/google/yamlfmt) is the formatter; `.yamlfmt` is how its
configuration travels with the code. A repo with YAML and no `.yamlfmt`, or one whose
`.yamlfmt` differs from the baseline, fails.

**What counts as YAML.** A `*.yaml` or `*.yml` file. The scan skips `.git` and vendored
dependency directories (`node_modules`, `vendor`), so a dependency's manifests never trigger
the rule; `.yamlfmt` itself is not matched (its extension is neither). In practice the
trigger always fires, since every compliant repository carries `.aqua/aqua.yaml`; the rule
stays conditional because the mechanism is what limen checks.

**What it must be.** Content-pinned, like `.limen/.shellcheckrc`: the yamlfmt settings that
keep formatting consistent across repos and match the editorconfig indentation. The source
of truth is this repository's own [`.limen/.yamlfmt`](../.limen/.yamlfmt), embedded as
`rules.CanonicalYamlfmt`; extras fail the check, and `limen fix` overwrites drift.

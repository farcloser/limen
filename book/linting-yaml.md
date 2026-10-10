# Linting — YAML

The YAML form of [linting](./linting.md): yamlfmt, on one content-pinned configuration.

## `.limen/.yamlfmt`

| Trigger | Requirement |
|---------|-------------|
| The repository contains YAML files. | A `.limen/.yamlfmt` is present and matches the canonical baseline **exactly**. |

YAML is whitespace-significant and easy to format inconsistently — indentation, quoting, and
flow vs block style all drift between authors and editors. Any project that ships YAML must
format it *the same way everywhere*. [yamlfmt](https://github.com/google/yamlfmt) is the
formatter; `.yamlfmt` is how its configuration travels with the code instead of living in
someone's head or CI script. A repo that contains YAML but no `.yamlfmt`, or one whose
`.yamlfmt` differs from the baseline, is unformatted or formatted inconsistently; both are
failures.

**What counts as YAML.** `limen` treats a file as YAML when it is a `*.yaml` or `*.yml` file.
The scan skips `.git` and vendored dependency directories (`node_modules`, `vendor`), so a
dependency's manifests never trigger the rule — only YAML that is genuinely *ours* does.
(`.yamlfmt` itself is not matched: its extension is `.yamlfmt`, not `.yaml`/`.yml`.)

In practice the trigger always fires: every compliant repository carries `.aqua/aqua.yaml`
([tooling is mandatory](./tooling.md)), so the YAML rule is effectively universal. The rule stays
conditional because the *mechanism* is what limen checks — the trigger, not the mandate.

**What `.limen/.yamlfmt` must be.** As with `.limen/.shellcheckrc`, the file is **content-pinned**:
`limen` requires it to equal the canonical baseline **byte for byte** — the yamlfmt settings that
keep formatting consistent across repos and match our editorconfig indentation. The baseline is
defined once and lives in one place: this repository's own
[`.limen/.yamlfmt`](../.limen/.yamlfmt), embedded into `limen` and exposed as
`rules.CanonicalYamlfmt`. **That file is the source of truth.** A repo may not add, remove, or
reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to the
canonical.

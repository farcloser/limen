# Per-language rules

The [mandatory files](./mandatory-files.md) apply to every repository without exception.
This chapter collects the rules that apply *conditionally* — only when a project uses a
given language or tool. `limen` detects the trigger automatically and enforces the rule only
when it is relevant, so a pure-Go repository is never asked for a shell config, and a
repository that ships shell or YAML cannot quietly skip one.

The shape of every per-language rule is the same: **if the trigger is present, the
requirement is mandatory; if it is absent, the rule does not apply** and `limen` reports
nothing for it.

## Shell — `.limen/.shellcheckrc`

| Trigger | Requirement |
|---------|-------------|
| The repository contains shell sources. | A `.limen/.shellcheckrc` is present and matches the canonical baseline **exactly**. |

Any project that ships shell must lint it, and lint it *the same way everywhere*.
[ShellCheck](https://www.shellcheck.net) is the linter; `.shellcheckrc` is how its
configuration — which checks are disabled, which shell dialect is assumed — travels with the
code.

**What counts as shell.** `limen` treats a file as a shell source when it is:

- a `*.sh` or `*.bash` file, or
- an **extensionless** file whose first line is a shebang for a dialect ShellCheck lints
  (`#!/bin/sh`, `#!/usr/bin/env bash`, and the like — `sh`, `bash`, `dash`, `ksh`).

"Counts as shell" deliberately equals "ShellCheck can lint it": the trigger exists to require
the linter's config, so a `zsh` (or `fish`) script does not fire it — ShellCheck has no
dialect for those, and the config would be dead weight. (A zsh script *named* `*.sh` still
counts: the extension claims a dialect, and ShellCheck holds it to that claim.) The scan skips
`.git` and vendored dependency directories (`node_modules`, `vendor`), so a dependency's
scripts never trigger the rule — only shell that is genuinely *ours* does.

**What `.limen/.shellcheckrc` must be.** The file is **content-pinned**: `limen` requires it to
equal the canonical baseline **byte for byte** — the directives that follow sourced files and
opt into the high-value optional checks ShellCheck ships but does not run by default. The
baseline is defined once and lives in one place: this repository's own
[`.limen/.shellcheckrc`](../.limen/.shellcheckrc), embedded into `limen` and exposed as
`rules.CanonicalShellcheckrc`. **That file is the source of truth.** A repo may not add, remove,
or reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to
the canonical. (This is the same exact-match rule as the `.editorconfig`, the `Justfile`, and
the `.limen/just/*.just` modules — only `.gitignore` and `aqua.yaml` use the subset,
"contains the baseline" model.)

**How to accommodate specific projects**

Projects that need overrides can use inline `# shellcheck` disable directives.
Global changes / improvements to the baseline should be submitted to project limen for review,
as the shared file should never be modified locally in a project.

## YAML — `.limen/.yamlfmt`

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

In practice the trigger always fires: every compliant repository carries `aqua.yaml`
([tooling is mandatory](./tooling.md)), so the YAML rule is effectively universal. It stays a
per-language rule because the *mechanism* is what limen checks — the trigger, not the mandate
— and the uniform shape keeps the chapter honest if the trigger set ever changes.

**What `.limen/.yamlfmt` must be.** As with `.limen/.shellcheckrc`, the file is **content-pinned**:
`limen` requires it to equal the canonical baseline **byte for byte** — the yamlfmt settings that
keep formatting consistent across repos and match our editorconfig indentation. The baseline is
defined once and lives in one place: this repository's own
[`.limen/.yamlfmt`](../.limen/.yamlfmt), embedded into `limen` and exposed as
`rules.CanonicalYamlfmt`. **That file is the source of truth.** A repo may not add, remove, or
reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to the
canonical.

## Homebrew formulas

A repository that is a Homebrew tap — formulas under `Formula/` (sharded subdirectories
included), casks under `Casks/`; the **modern layout only**, by decision: brew still reads
the legacy locations (`HomebrewFormula/`, bare `*.rb` at the repository root), the recipes
deliberately do not — lints them with brew's **own** tooling — `just do lint homebrew` runs `brew style`
(Homebrew's vendored RuboCop with the formula cops) and `brew audit --strict`;
`just do fix homebrew` is `brew style --fix`. No Ruby toolchain is ever installed for
this: brew vendors its own Ruby and RuboCop, and plain RuboCop would not know the
formula rules anyway. Both recipes pass vacuously when the repository carries no
formulas.

Two deliberate exceptions, named because they cut against doctrine:

- **brew is not on the hermetic PATH and never will be.** It is a machine-layer package
  manager that aqua cannot pin, and the PATH exclusion exists to stop machine tools
  substituting for pinned ones — but brew substitutes for nothing here; it *is* the
  subject under test. The shared `main.just` captures its location from the **ambient**
  PATH at startup, before the hermetic PATH locks down (`BREW_BIN`, overridable by
  exporting it) — the invoking shell knows where brew lives, whatever the prefix — and
  the recipes fail with guidance when the capture came up empty; the hermetic PATH
  itself is untouched.
- **`brew audit` needs a tap identity.** brew addresses formulas by tap name, never by
  path, so the project declares which tap it is (`export LINT_HOMEBREW_TAP :=
  'user/name'` in the root `Justfile`) and the audit recipe registers the working tree
  under that name for the duration of the run — a symlink, so the audit judges the
  working tree, not a stale clone. Audit flags that only make sense on CI (`--online`
  does network calls) go through `LINT_HOMEBREW_AUDIT_FLAGS`.

The proof beyond linting — `brew install --build-from-source` plus `brew test` — mutates
the machine's live brew and therefore belongs to disposable CI runners, not to a shared
recipe; a tap wires that in its own workflow.

## Enforcement

`limen check [path]` evaluates the applicable per-language rules alongside the mandatory
ones. A rule whose trigger is absent produces no finding; a rule whose trigger is present
must pass like any other. See [`../cmd/limen/`](../cmd/limen).

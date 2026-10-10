# Linting — Shell

The Shell form of [linting](./linting.md): ShellCheck, on one content-pinned configuration.

## `.limen/.shellcheckrc`

| Trigger | Requirement |
|---------|-------------|
| Always — every repository. | A `.limen/.shellcheckrc` is present and matches the canonical baseline **exactly**. |

Any project that ships shell must lint it, and lint it *the same way everywhere*.
[ShellCheck](https://www.shellcheck.net) is the linter; `.shellcheckrc` is how its
configuration — which checks are disabled, which shell dialect is assumed — travels with the
code.

**Why unconditional.** This rule used to fire
only once a repository actually contained shell, which read as the tidier design and was the
wrong call in practice. `just do lint shell` passes `--rcfile .limen/.shellcheckrc`
unconditionally, so a repository without the file gets `Warning: unable to read --rcfile
.limen/.shellcheckrc` on every run — a message that reads like a broken setup, not like a
rule that does not apply yet. And projects grow shell: the config would appear the day
someone adds a first script, as a surprise edit in an unrelated pull request. A config file
that is simply always there is one less thing to explain, and an unused one costs nothing.

The YAML twin below is still conditional, but only nominally: every repository carries YAML
(the workflows alone guarantee it), so the trigger fires everywhere in practice.

**What `.limen/.shellcheckrc` must be.** The file is **content-pinned**: `limen` requires it to
equal the canonical baseline **byte for byte** — the directives that follow sourced files and
opt into the high-value optional checks ShellCheck ships but does not run by default. The
baseline is defined once and lives in one place: this repository's own
[`.limen/.shellcheckrc`](../.limen/.shellcheckrc), embedded into `limen` and exposed as
`rules.CanonicalShellcheckrc`. **That file is the source of truth.** A repo may not add, remove,
or reorder anything — extras fail the check, and `limen fix` overwrites a drifted file back to
the canonical. (This is the same exact-match rule as the `.editorconfig`, the `.justfile`, and
the `.limen/just/*.just` modules — only `.aqua/aqua.yaml` uses the subset, "contains the baseline"
model, and `.gitignore` is merely seeded once when absent.)

**How to accommodate specific projects**

Projects that need overrides can use inline `# shellcheck` disable directives.
Global changes / improvements to the baseline should be submitted to project limen for review,
as the shared file should never be modified locally in a project.

# Linting — Shell

The Shell form of [linting](./linting.md): ShellCheck, on one content-pinned configuration.

## `.limen/.shellcheckrc`

| Trigger | Requirement |
|---------|-------------|
| Always — every repository. | A `.limen/.shellcheckrc` is present and matches the canonical baseline **exactly**. |

Any project that ships shell lints it, and lints it the same way everywhere.
[ShellCheck](https://www.shellcheck.net) is the linter; `.shellcheckrc` is how its
configuration — which checks are disabled, which dialect is assumed — travels with the code.

**Why unconditional.** `just do lint shell` passes `--rcfile .limen/.shellcheckrc` on every
run, so a repository without the file gets `Warning: unable to read --rcfile` every time, a
message that reads like a broken setup. And projects grow shell: a conditional rule would
make the config appear the day someone adds a first script, as a surprise edit in an
unrelated pull request. A file that is always there is one less thing to explain, and an
unused one costs nothing. (The YAML twin is conditional only nominally: every repository
carries YAML.)

**What it must be.** Content-pinned: `limen` requires it to equal the canonical baseline byte
for byte — the directives that follow sourced files and opt into the high-value optional
checks ShellCheck ships but does not run by default. The source of truth is this
repository's own [`.limen/.shellcheckrc`](../.limen/.shellcheckrc), embedded as
`rules.CanonicalShellcheckrc`; extras fail the check, and `limen fix` overwrites drift.

A project that needs an exception uses an inline `# shellcheck disable=SC####` directive,
with its reason. A change to the baseline is a limen change; the shared file is never
edited locally.

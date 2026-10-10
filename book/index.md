# Farcloser: the engineering book

Everything about tooling, style and architecture that is common to all our projects:
generic where it states doctrine, specific and opinionated where it describes the shared
tooling. One chapter per file, each readable on its own; a chapter holds what applies to
every language, and a sub-document holds one language's form.

- **[Principles](./principles.md)**: what we value above all. Read first.
- **Working together**: how a change travels from a branch to `main`.
  - [Coding agents as contributors](./agents.md): identity, key, sandbox, the workflow, the
    shape of a pull request, how sessions talk.
  - [Reviewing code](./review.md): the reviewer's side; who approves what.
- **The rig**: limen, what every repository carries and runs.
  - [Mandatory files](./mandatory-files.md): the pinned and seeded files, and why.
  - [The shared recipes](./recipes.md): the hermetic environment and the `just` lanes; releasing.
  - [Project tooling](./tooling.md): aqua, the Go-built tools, `pins.yaml`, Renovate, the
    checksum workflow, machine setup.
  - [GitHub settings](./github.md): rulesets, the CI and security lanes, the audit.
  - [Windows](./windows.md): what the Windows legs run and what differs there;
    [testing in a VM](./vm_testing.md).
  - [Known upstream bugs](./upstream.md): bugs in tools we pin, with their workarounds and lifts.
- **Writing code**: one chapter per concern, each with its language forms.
  - [Errors](./errors.md): [Go](./errors-go.md).
  - [Logging](./logging.md): [Go](./logging-go.md).
  - [Linting](./linting.md): [Go](./linting-go.md), [Shell](./linting-shell.md), [YAML](./linting-yaml.md).
  - [Dependencies](./dependencies.md): [Go](./dependencies-go.md).
  - [Boundaries](./boundaries.md): inputs, sizes, types, startup, process state; [Go](./boundaries-go.md).
- **Testing**: [Testing](./testing.md): what a test is for, what it may reach, when a suite is
  replaced; [Go](./testing-go.md).
- **Forks**: [Forks](./forks.md): provenance, onboarding order, pitfalls, the release review.
- **Websites**: [Project websites](./web.md): what a site loads, claims, and how it reads on a phone.
- **Languages**: what fits no topic: [Homebrew](./homebrew.md), [Rust](./rust.md).

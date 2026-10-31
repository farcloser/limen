# Farcloser: the engineering book

Everything about tooling, style and architecture that is common to all our projects:
generic where it states doctrine, specific and opinionated where it describes the shared
tooling. One chapter per file, each readable on its own; a chapter holds what applies to
every language, and a sub-document holds one language's form.

- **[Principles](./principles.md)**: what we value above all. Read first.
- **[Writing](./writing.md)**: every line beside the code states what is true, plainly; who
  reads it, and what is sent back.
- **Working together**: how a change travels from a branch to `main`.
  - [Identity and keys](./identity.md): who a contributor is, human or bot, and the key that
    proves it; what to do when a token is lost.
  - [Coding agents as contributors](./agents.md): the workflow, the shape of a pull request, scope.
  - [Maintaining a repository](./maintaining.md): the two jobs; what a maintainer knows,
    decides, owes, and is not.
  - [Reviewing code](./review.md): the reviewer's side; reviewing a package; who approves what.
  - [Communicating](./communicating.md): where the conversation happens; one-to-one messages.
  - [Releasing](./releasing.md): a signed tag, its page and notes, the CI and local lanes, the rules.
- **The rig**: limen, what every repository carries and runs.
  - [Mandatory files](./mandatory-files.md): the pinned and seeded files, and why.
  - [The shared recipes](./recipes.md): the hermetic environment and the `just` lanes.
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
  - [Boundaries](./boundaries.md): inputs, sizes, types, startup, process state, holders and
    shared files; [Go](./boundaries-go.md).
- **Testing**: [Testing](./testing.md): what a test is for, what a package's tests establish,
  when a test goes, where the tests run; [Go](./testing-go.md).
- **Forks**: [Forks](./forks.md): provenance, onboarding order, pitfalls, the release review.
- **Websites**: [Project websites](./web.md): what a site loads, claims, and how it reads on a phone.
- **Languages**: what fits no topic: [Homebrew](./homebrew.md), [Rust](./rust.md).

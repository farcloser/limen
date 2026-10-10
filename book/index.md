# Farcloser: the engineering book

Everything about tooling, style and architecture that is common to all our projects:
generic where it states doctrine, specific and opinionated where it describes the shared
tooling. One chapter per file, each readable on its own; a chapter holds what applies to
every language, and links a sub-document for what applies to one.

| Chapter | What it holds |
|---|---|
| [Generic principles](./principles.md) | What we value above all: correctness, simplicity, contracts, types, errors, logging, pinning, linters, comments. Read first. |
| [Coding agents as contributors](./agents.md) | A coding agent's identity, key and sandbox; the workflow from branch to merge; the shape of a pull request; how sessions talk to each other. |
| [Reviewing code](./review.md) | The reviewer's side of the same workflow: the order of the questions, who approves what, the shape of a review. |
| [Mandatory files](./mandatory-files.md) | Every file limen requires in every repository, content-pinned or seeded once, and why. |
| [The shared recipes](./recipes.md) | The hermetic execution environment and every `just` recipe the baseline provides: lint, fix, test, build, security, release. |
| [Project tooling](./tooling.md) | aqua and the pinned toolchain, Go-built tools, pinned artifacts, Renovate, the checksum workflow, machine setup. |
| [GitHub settings](./github.md) | Repository and organization settings as configuration: rulesets, auto-merge, the security lane, the audit. |
| [Testing](./testing.md) | What a test is for, what it may reach, and when a suite is replaced. Go: [testing-go](./testing-go.md). |
| [Per-language rules](./per-language.md) | Rules that apply when a project uses a given language or tool: Shell, YAML, Homebrew, Rust, Go (lint baseline, findings, versions, errors, logging, paths). Being split into chapters like testing. |
| [Windows](./windows.md) | What the Windows legs run, what differs there, and what breaks. |
| [Forks](./forks.md) | Onboarding and maintaining a fork of an upstream project: provenance, order of work, pitfalls. |
| [Project websites](./web.md) | A project's static site: what it loads, what it claims, how it reads on a phone. |
| [Known upstream bugs](./upstream.md) | Bugs in tools we pin and do not own, each with its symptom, cause, workaround and lift. |
| [Testing in a VM](./vm_testing.md) | Running the Windows legs in a local virtual machine. |

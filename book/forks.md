# Forks

A fork carries someone else's code forward under our name. Onboarding one has an order,
and most of what went wrong on the forks so far came from doing a step late. Its history
then diverges from upstream's, and from there git cannot answer the question that matters:
what has upstream fixed since, and do we have it? So every fork also records its provenance
by hand, in one file, and its owner reviews upstream's new commits before every release.

## Decided before the first change

These are @apostasie's calls, made when the fork is created, because each one shapes the
work after it:

- **Scope.** Fixes only, or improvements upstream does not have.
- **Carry or diverge.** Track upstream and port its fixes, or let the code go its own way.
  Every local edit to upstream's source is a divergence `UPSTREAM.md` records, and
  reporting a bug upstream is a public act.
- **API.** Keep upstream's shape, or trim it, and the breaking changes that buys.
- **Versions.** What a change of output is worth: a change in what the code produces is a
  minor version, not a patch.
- **Platforms.** Every operating system and architecture the fork claims, since CI has to
  run on each.
- **Owner.** The session that keeps it day to day.

## Onboarding, in order

1. **Every name, settled once.** The module path is changed everywhere in the first pull
   request: every import, nested and vendored modules, generators, the README and the
   examples. A path renamed after the first tag strands every consumer on the old one, and
   Renovate then proposes versions that cannot resolve. What the fork drops (an upstream
   command it replaces) goes in the same pull request. `UPSTREAM.md` (below) exists from
   this pull request on. (Renovate
   skips a GitHub fork unless `renovate.json` says `forkProcessing: enabled`; enrolment's
   `limen fix` writes it, see [mandatory files](./mandatory-files.md).)
2. **A behavioural baseline, before anything changes.** The test suite runs on every CI leg
   the fork claims, amd64 and Windows included: a bug in code that only one architecture
   exercises does not show up on an Apple-silicon laptop. Where upstream ships a reference
   implementation, a differential test compares the fork's output with it over a corpus.
   Anything the fork builds or vendors (a binary blob, generated code) is built twice and
   compared byte for byte, in CI.
3. **Enrolment, green from day one.** The fork is enrolled in limen with the onboarding
   backlog block ([onboarding an existing repository](./mandatory-files.md#onboarding-an-existing-repository)),
   never merged known-red: a real regression then cannot hide among the expected failures.
4. **Linter decisions by class, then one linter at a time.** A rule wrong for the whole
   codebase is disabled or excluded in the overlay before the per-linter series starts; an
   inline silence for a rule later disabled is dead weight. The series then lands one
   linter per pull request, quickly, since each touches the same backlog lines; linters
   that edit code land one at a time, test files last, or they conflict.
5. **Audit, then correctness.** Read the code for what is wrong before improving it, and
   fix what is wrong first.
6. **The first tag.** Its version comes from an API diff (`go doc -all` at the last tag
   against `main`), not from the commit list. Its notes are the titles of the pull requests
   it merges, grouped by label ([release notes](./recipes.md)): every pull request that
   breaks a consumer carried the `breaking` label when it merged, never added after the fact.
7. **Then everything else:** performance, hardening, API work.

## Pitfalls

| Pitfall | Prevention |
|---|---|
| The first tag shipped regressions no test covered, and an amd64-only bug | Step 2 before any tag: whole-output comparison on every leg |
| A module renamed after its first tag: consumers' Renovate PRs could not resolve, and limen's own pin needed a migration | Step 1: every name settled before the first tag |
| Enrolment merged red: findings no one could tell from regressions | The backlog block, never known-red |
| Alignment pull requests rebased up to five times each, on the same backlog lines | Merge them in quick succession; expect a rebase per merge |
| An auto-resolved overlay conflict dropped a branch's own settings | Auto-resolve only when the overlay diff is the rule deletion alone |
| Integer-conversion findings (gosec G115) waved off as noise: four were real truncation bugs | Judge every integer conversion in format-parsing code |
| A squash of stacked branches silently reverted a merged change | Every pull request targets `main`; after a squash or rebase, `git diff` the new parent against the new head shows only the pull request's files |
| A release for tooling alone, then undone | A release carries a real change ([recipes](./recipes.md)) |
| A pin CI cannot exercise (a macOS-only tool, a sandbox that cannot clone) | Say so on the pull request; a human runs it |
| A failure on one operating system dismissed as flaky | A one-OS failure is a bug until shown otherwise (AGENTS.md: a flake is fixed when it is noticed) |

### Case study: go-graphviz

A C library compiled to WebAssembly and run on a Go runtime, with a generated bridge
between the two. Most of its deep bugs were in the bridge (argument widths, getters,
leaks), and its raster output, drawn in Go where upstream uses native libraries, needed a
differential test against upstream's own SVG to be trusted. A graph is untrusted input
that drives allocations, so its memory budgets were set explicitly. None of this
generalizes beyond the shape of step 2: compare against the reference before trusting the
fork.

## `UPSTREAM.md`

One file at the repository root, in this shape:

```markdown
# Upstream

Fork of https://github.com/<owner>/<repo> (`<branch>`).

- Forked from: [`<short-sha>`](https://github.com/<owner>/<repo>/commit/<full-sha>), committed <date>
- Reviewed through: [`<short-sha>`](https://github.com/<owner>/<repo>/commit/<full-sha>), reviewed <date>

## Incorporated

| Upstream commit | Here | What |
|---|---|---|
| [`<short-sha>`](https://github.com/<owner>/<repo>/commit/<full-sha>) | [`<short-sha>`](https://github.com/<our-owner>/<fork>/commit/<full-sha>) | <what it does> |

## Not incorporated

| Upstream commit | Why |
|---|---|
| [`<short-sha>`](https://github.com/<owner>/<repo>/commit/<full-sha>) | <why not> |
```

- **Forked from** is the upstream commit the fork started at, with that commit's date, and
  never moves.
- **Reviewed through** is upstream's head at the last review, with the review's date, not
  the commit's: it says how stale the review is. The next review starts after it.
- **The two tables together are complete.** Every upstream commit after the fork point,
  through *Reviewed through*, sits in exactly one of them. Merge commits and pure release
  bookkeeping go in *Not incorporated*, with that as the reason, so that a commit missing
  from both tables means the review missed it, never that it did not matter.
- **A reason says why, not just no.** "Exists here independently (`Writer.link`)", "touches
  the CLI we removed", "superseded by our own fix": enough for the next reviewer to agree
  without reading the commit again.

## Incorporating a commit

An upstream commit comes over as a hand port: our own commit, adapted to the fork, never a
merge of upstream or a rebase onto it, because the histories have diverged too far for
either to mean anything. The port commit adds its *Incorporated* row in the same change,
and its message carries the upstream commit's URL, so that the link holds from either
side.

## Reviewing upstream before a release

Before a fork releases, its owner reviews every upstream commit after *Reviewed through*,
up to upstream's current head:

```bash
git log --reverse <reviewed-through>..<upstream-remote>/<branch>
```

Each commit is ported, with its row, or gets a *Not incorporated* row saying why. Then
*Reviewed through* moves to the head the review covered. A fork whose *Reviewed through*
is behind upstream's head is not ready to release: upstream may have fixed something the
release would ship without.

A differential test that needs the reference installed runs before every tag, by hand. CI
has no copy of the upstream tool, so go-graphviz's whole-output comparison against the
installed Graphviz is opt-in and never runs there: green CI says the fork agrees with
itself, not with upstream, and a tag cut on it alone ships whatever drifted since the last
time someone ran the comparison. So the release has two checks, not one: upstream reviewed
through its head, above, and the differential test run on the release commit against the
reference at the version the fork embeds, with its result recorded where the release is
prepared (the mean difference, and the inputs over the threshold if any). The raster
canvas's line joins and dash lengths were held to upstream's this way, by a comparison no
CI leg could run.

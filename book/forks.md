# Forks

A fork carries someone else's code forward under our name. Its history diverges from
upstream's soon after the fork, and from then on git cannot answer the question that
matters: what has upstream fixed since, and do we have it? So every fork records its
provenance by hand, in one file, and its owner reviews upstream's new commits before every
release.

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

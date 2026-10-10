# Dependencies — Go

The Go form of [dependencies](./dependencies.md): the `go` directive, `replace`, and ports
from the standard library.

## The baseline version

A module's `go` directive is the earliest Go release still supported upstream, written as
that release's first version, `go 1.N.0`, unless a dependency requires a later patch of the
same release. Go supports its two most recent major releases, so the baseline is the older
of the two, and it moves when a new major release retires it. A module on an earlier
version moves up; none requires a newer release.

- **`.0`, unless a dependency needs a later patch.** The directive is the minimum a builder
  and every consumer must run: a module saying `go 1.N.8` refuses to build with `1.N.4`
  under `GOTOOLCHAIN=local`, and forces every importer up with it. Patch fixes reach a build
  through the toolchain the repository pins (its own aqua `golang/go` pin, kept at the latest
  release), not through the directive. A module cannot sit below its dependencies, though:
  with a dependency on `go 1.N.8`, a module on `go 1.N.0` refuses to build, and
  `go mod tidy` raises it to `1.N.8`. So the directive is the higher of `1.N.0` and the
  highest any dependency requires; `go mod edit -go=1.N.0` followed by `go mod tidy` lands
  on it. `go mod init` writes whatever patch level the toolchain that ran it had, which is
  how repositories drift across a line; a new module's directive is settled the same way.
- **Not the bare `go 1.N`.** Go orders `1.N` before `1.N`'s release candidates, so it
  admits a toolchain that is not a release; `1.N.0` is the release.
- **A dependency that needs a newer release waits.** Taking a dependency on `go 1.(N+1)`
  would raise the module past the baseline: the module stays on the dependency's last
  release that supports the baseline until the baseline moves.
- **The tools modules are the exception.** `tools/go.mod` and `tools/<name>/go.mod` are
  built only by the repository's pinned toolchain and never imported, and `go mod tidy`
  raises their directive to whatever the tools themselves require. Their directive is left
  to that.

## No replace, ever

A `replace` directive is never committed: not in `go.mod`, not in a nested module, not
through a relative path to a sibling checkout, and no configuration that permits one is
committed either (`gomoddirectives`' `replace-local` or `replace-allow-list`, in a
`.lint-go.yaml` or anywhere else). There is no case where it is right.

A local replace has one legitimate use: working on two modules at once, on one machine,
while a change travels from one to the other. It lives in the working tree for as long as
that takes, and is gone before anything is committed. What is committed builds from
published versions only, so that the build a reviewer, CI, or a consumer runs is the one
the author ran — a replace makes the module build from whatever happens to sit at a path,
which nobody else has, and which the checksum database never saw.

When the change a module needs is not tagged yet, require the commit that carries it: a
pseudo-version (`go get <module>@<commit>`) is published like a tag — the module proxy
serves it, the checksum database records it. The commit must already be on the owner's
default branch: one from an unmerged branch can be rewritten or vanish, and the pin then
names code that never landed. When a tag is wanted instead, ask the owning session for a
release. A tool module nested in a repository requires its parent's published version like
any other consumer, or becomes part of the parent module.

The baseline refuses `replace` by default (gomoddirectives), and `exclude` with it, being a
replace by other means.

## A port from the standard library is read beside the toolchain's source when revisited

A function ported from the standard library — `os.CreateTemp` under another share mode,
`syscall.Open` with `FILE_SHARE_DELETE` — drifts as upstream moves. When it is touched,
it is read beside the current toolchain's source, the one aqua pins under
`pkgs/http/golang.org/dl/`, function by function, and each difference is adopted or
named: a divergence kept goes in the package doc with the platforms or versions it
concerns, so the next reader comparing the two does not find it again, and one adopted
gets the differential check beside the original. The source is the reference, never
memory: a review round's "upstream returns EINVAL there" read right and was wrong
against the file. The trap: a difference that looks like a gap may be upstream's own
doing elsewhere. Go's runtime sets the process's long-path bit on Windows from
10.0.15063, so the `\\?\` prefix `os` adds to a long path is dead on every platform
this project runs, and porting it would have been sixty lines nobody could exercise
(https://github.com/mycophonic/primordium/pull/166, which adopted the one real
difference, the FILE_FLAG bits of an open flag, and named the other).

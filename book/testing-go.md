# Testing — Go

The Go form of [testing](./testing.md): the files a package's tests live in, what the lint
baseline and the lanes enforce, the shape the rules take in Go, and the cases that set them.

## The files

A package's tests are four files, one per outcome, so a reader finds the contract without
reading the suite:

| File | Holds |
|---|---|
| `contract_test.go` | The contract as a comment at the top, from its sources, each rule cited; the tests of every exported behaviour and error class against it. |
| `bounded_test.go` | The walk within the bound: every class edge, option combination and size around a boundary; call sequences against a model. |
| `fuzz_test.go` | The `Fuzz*` targets past the bound, seeded from it. A crasher lands under `testdata/fuzz/<Target>/` and is committed. |
| `bench_test.go` | The benchmarks, where the package exists for speed. |

A hand-written case remains where no walk reaches, with a comment saying which: a platform
path (a device name, a path past `MAX_PATH`), a contract across processes, a crash inside a
window. An example stays when it catches what a property cannot.

## What the lint enforces

The baseline enables every test linter golangci-lint ships; none is in its disable list
([one lint baseline](./linting-go.md#one-lint-baseline-per-project-carve-outs-lint-goyaml)).
`testpackage` keeps a test in `package foo_test`, the boundary behind the black-box rule;
`paralleltest` and `tparallel` want `t.Parallel()` on tests and subtests; `thelper` wants
`t.Helper()` first in a helper; `usetesting` wants `t.TempDir`, `t.Setenv` and `t.Context`
over their hand-rolled forms. In `_test.go` the baseline exempts gocognit, goconst, gosec,
varnamelen and wrapcheck, so a `#nosec` in a test is dead and rejected
([silencing a finding](./linting-go.md#silencing-a-finding)). `err113` stays on: a dynamic
error is right in a test fixture only, compared by identity, and a project that wants that
carve-out states it in its own `.lint-go.yaml`.

## The lanes

`just do test go` runs `unit`: gotestsum, `-count=1`, `TEST_GO_TIMEOUT` from the root
`.justfile`. `race` runs the same under the detector (cgo forced, external linkmode; a
platform without a detector says so and passes), and CI's own `race` job runs it on one leg.
`cover` writes a per-function summary and an HTML report under `.build/coverage/`, gated by
`TEST_GO_COVER_MIN` when the project sets it. `fuzz` runs every `Fuzz*` target for
`TEST_GO_FUZZ_TIME`, ten seconds by default; the verdict is a new file under
`testdata/fuzz/`, not the exit code, and CI's own `fuzz` job runs it on one leg. `just do
perf go bench` runs the benchmarks with allocation statistics. A project's `test` aggregate
names `unit`, `race` and `cover`; the bare `just do test go` default is `unit` alone and is
not an aggregate.

A passing test's output is hidden twice, by `go test` without `-v` and by gotestsum's
format: `GOTESTSUM_FORMAT=standard-verbose just do test go` shows it.

## The shape of the rules in Go

- **The second process is the test binary.** `TestMain` reads an environment variable and,
  when set, runs a child mode instead of the suite: hold a lock, grow the index, install a
  signal handler, exit with a code. The parent starts `os.Args[0]` with that variable and
  synchronises on something observable, a readiness line on the child's stdout or a file's
  existence, never a sleep. Where the contract is about a dying holder, the parent kills
  the child at that moment and skips, saying so, when the kill landed too late to show
  what the test is for. A long operation runs inside a bounded wait, so a hang fails the
  test instead of stalling CI, and a handle is closed only after the operation returned: a
  `Close` under a spinning `Get` unmaps the word it spins on. A peer that must wait
  through another's recovery is a goroutine on a second handle, checked after the main
  assertions: a lock protocol tested from one handle never exercises the waiter. A "live
  PID that is not me" is the parent's, `os.Getppid()`, never 1, whose start time another
  user cannot read on linux. The same shape tests a startup precondition's panic
  ([boundaries](./boundaries-go.md#a-precondition-the-program-must-meet-at-startup-panics-when-missed)),
  and it needs no shell, so the Windows legs run it.
- **A service is a local stand-in.** An `httptest.Server` plays the remote and records what
  the code sent; the test reads what arrived, not what the client logged.
- **A drop-in runs beside its original.** Each call of the wrapper and of its namesake, on
  the same filesystem state, compared on the error's kind and text, what was read, what is
  on disk and its mode.
- **The truth is exact.** `math/big` for a parser's arithmetic; `float64` accumulation for
  float32 lanes, with a budget of one float32 ulp per term on the sum of the terms'
  magnitudes. A scalar float32 loop held up as the truth was itself off at length 59.
- **A skip names the capability.** `t.Skip` only for what the host cannot do: no
  pseudo-terminal in the sandbox, no `SIGTERM` on Windows, no race detector on
  windows/arm64; the message says which.
- **The mutation score, by hand.** Single-point mutants (an operator swapped, a condition
  negated, an integer literal incremented, `break` and `continue` exchanged) applied one at
  a time to the package's files; each suite run with `go test -run` against every mutant;
  killed over total per suite, survivors listed and read. Before and after; a test file is
  removed only on it. No lane carries it yet.

## The cases that set the rules

| Rule | Case |
|---|---|
| Writing the contract down finds the bugs: seven in primordium in a week, each a rule the code did not meet once stated | https://github.com/mycophonic/primordium/pull/138, https://github.com/mycophonic/primordium/pull/139, https://github.com/mycophonic/primordium/pull/140, https://github.com/mycophonic/primordium/pull/143, https://github.com/mycophonic/primordium/pull/146, https://github.com/mycophonic/primordium/pull/147, https://github.com/mycophonic/primordium/pull/153 |
| A path validator held to each platform's documented rules, bounded and fuzzed: 281 of 305 mutants caught, every one the hand tables caught | https://github.com/mycophonic/primordium/pull/141 |
| A stateful wrapper held to a model: every sequence of up to four calls over three holders | https://github.com/mycophonic/primordium/pull/145 |
| A drop-in beside `os`, call for call; its 1,789-line port of Go's own `os` tests retired under the score, no mutant caught by the port alone | https://github.com/mycophonic/primordium/pull/151, https://github.com/mycophonic/primordium/pull/165 |
| A sentinels-only package carried 215 lines of tests only the standard library could fail, and two of its own lists missed a sentinel | https://github.com/mycophonic/primordium/pull/160 |
| Fuzz over the input space where every branch was already pinned | https://github.com/mycophonic/primordium/pull/186 |
| Formats × sizes across every block edge; the cut at the trailer found a decoder accepting a truncated stream as complete | https://github.com/mycophonic/primordium/pull/189 |
| A float64 truth with an ulp budget for SIMD lanes | https://github.com/mycophonic/primordium/pull/191 |
| The child as the test binary made the Windows legs run tests they had never run | https://github.com/mycophonic/primordium/pull/200 |
| A contract test found a deadlock: a reporter wrapping slog's built-in handler recursed through the log package | https://github.com/mycophonic/primordium/pull/201 |
| A cross-process contract tested with the test binary as the other holder; its 255-character key found a lock sidecar name too long for Windows | https://github.com/mycophonic/primordium/pull/205, https://github.com/mycophonic/primordium/pull/206 |
| Recovery under live peers, a child killed between the journal write and the end of the grow | https://github.com/mycophonic/primordium/pull/212, https://github.com/mycophonic/primordium/pull/214 |
| A buffer sized by a benchmark: 32 KiB to 4 MiB within noise, 16 MiB slower, a pool no effect | https://github.com/mycophonic/primordium/pull/213 |

# Errors — Go

The Go form of [errors](./errors.md), as the lint baseline enforces it (err113,
errorlint, wrapcheck) and as the forks settled it.

## A sentinel per fault class, wrapped at every site

The baseline enforces the floor (err113, errorlint, wrapcheck): an error value is a
package-level sentinel or wraps one with `%w`, and a caller compares with `errors.Is` or
`errors.As`, never by text. The doctrine is what the sentinels *mean*.

**One sentinel per fault class, not per message.** A caller acts on the class, and the
classes a module has are few: the input is not valid (reject it), the caller's argument is
wrong (fix the call), the transport underneath failed (retry, or report it as the
transport's own), the format is valid but the module does not implement it. Three to five
exported `Err*` per module, each documented by what a caller should do on it; the message
carries the specifics (which field, which offset), the sentinel the class. A module whose
every fault is one sentinel has told its caller nothing: erofs reported every malformed
image and every bad argument as `fs.ErrInvalid`, so a caller could not tell a corrupt image
from a misspelled path (https://github.com/forkcloser/erofs/pull/108 added `ErrCorrupt`).
A module whose faults are bare messages has told its caller nothing either: go-graphviz's
generated binding returned `errors.New("… is not loaded")` and `fmt.Errorf("cannot find
lookup function …")`, matchable only by text
(https://github.com/forkcloser/go-graphviz/pull/89 made them `ErrNotLoaded`,
`ErrMemoryAccess`, `ErrNotRegistered`). The model is xz: `ErrCorrupt`, `ErrUnsupported`,
`ErrClosed` in `errors.go`, the same two in `lzma/errors.go`, every diagnostic of the
decoder matching one of them.

**A transport error is passed through, never reclassified.** An error from the
`io.Reader` or `io.ReaderAt` underneath the module is returned wrapped or as is, and
matches none of the module's sentinels; a short read at a place the format says has bytes
is `io.ErrUnexpectedEOF`, not "corrupt". The caller that owns the transport is the one
that can act on it. The test that pins this is cheap and belongs in every module with a
reader: a `ReaderAt` that fails with its own error, `Open` returning that error and nothing
of the module's (erofs `errcorrupt_test.go`).

**The chain carries the class without carrying its text.** The plain form is
`fmt.Errorf("nid %d is out of range: %w", nid, ErrCorrupt)`, and the sentinel's text is
then a noun phrase that reads at the end of a message ("corrupt image", not "an error
occurred"). Where that text would be noise on every line, a kind type: `Error()` returns
the message, `Unwrap()` the sentinel, built by a one-line constructor (`corruptf` in xz
`errors.go`). Both match with `errors.Is`; neither needs the caller to parse.

**A layer maps the layer below into its own vocabulary at the boundary.** A caller of xz
sees `xz.ErrCorrupt` whether the fault was in the container or in the LZMA2 payload:
`classify` wraps `lzma.ErrCorrupt` in a value whose `Unwrap() []error` returns both the
new sentinel and the original chain, so the lower sentinel stays reachable and the
message stays the decoder's. The alternative, re-exporting the lower module's sentinels,
leaks the layering into the API.

**Widening is additive.** A new sentinel that names a subset of what an old one matched
unwraps to the old one (`ErrCorrupt` in erofs matches `ErrInvalid` too), so every
`errors.Is` a consumer wrote keeps its answer and the change is not `breaking`. The
reverse, moving faults out from under a sentinel consumers test, is.

**Where the lint does not reach, the rule still does.** golangci-lint skips generated
files and the lint lane does not enter a nested module, so a bare `fmt.Errorf` there is
never reported: the binding's errors above were 59 such sites. The fix goes in the
template or the generator, then `just bindings`, and the nested tool gets the same
sentinels by hand. A dynamic error is right in one place only: a test fixture, where the
error is the fake's own and compared by identity; err113 stays on in tests in the
baseline, and a project that wants that carve-out states it in its own `.lint-go.yaml`,
as xz does.

**Not a fault class: a limit.** A refusal that protects the caller from itself, a path
over 4096 bytes, `ReadFile` on a file over the cap, is the caller's argument, not the
input's fault: it stays on the bad-argument sentinel even when the value came off disk.
The package doc lists the limits and the sentinel each reports.

## A chaining API carries its first error

A setter that returns its receiver so that calls chain, `g.SetLabel(…).SetShape(…)`, has
nowhere to return an error, and the three ways out are not equal. Panicking turns a failed
allocation deep in a library into a crash the caller never asked for. Dropping the error,
the usual choice and what go-graphviz did for 210 setters, means the graph silently does
not hold what the program set, and every one of those drops is an errcheck finding carved
out of the lint baseline. The rule is the third way: the receiver records the first error
it meets, `Err()` returns it, and the terminal operation, the one that consumes what the
setters built (`Layout`, and so every render), returns it wrapped before doing anything,
so a dropped error cannot pass silently and a caller that never reads `Err()` still cannot
proceed on a half-built value. Closing the root forgets it. The chaining signature stays;
the carve-out goes. The error is keyed by the root object, so a node's or an edge's failure
is the graph's, which is what the terminal operation sees.

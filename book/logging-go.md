# Logging — Go

The Go form of [logging](./logging.md): `log/slog`, set up once in `main`.

## A library writes nothing; a command speaks once, in main

**A library package imports no logger and writes nothing to standard error.** Not behind
a verbosity flag, not through a package-global logger a command configures: what a
library has to say is a returned error or a returned value, and a consumer that wants a
trace adds it at the call site, where it knows the context. A debug dump in a decoder is
the upstream smell to remove on fork: xz's `reader.go` and `lzma/reader2.go` printed
headers and chunk headers through upstream's own `internal/xlog` under `-vv`, with the
format's `String` methods existing only to feed it
(https://github.com/forkcloser/xz/pull/96 deleted the package, the dumps and the
methods). The survey that preceded it found no other library package of the forks
importing any logger; that is the state to keep.

**A command either logs or prints, and knows which.** A tool with diagnostics at levels
(a linter, a server, anything a `--log-level` flag makes sense for) uses `log/slog`, and
nothing else: the handler and the level are set once in `main`, from the flags, the way
godolint's `cmd/godolint` does it; no third-party logger, no logger set up by a library
on import. A tool whose output to the user is messages (a compressor, a renderer: "file
exists", "format not recognized") has nothing to log. It prints each message to standard
error as `<cmd>: <message>` and sets the exit status, with the quiet levels the tool it
mirrors defines (xz: `-q` hides warnings, `-qq` errors too). No timestamps, no levels, no
logging package for three `Fprintf` calls.

**No logger ends the process.** `Fatal` and `Panic` entry points are what a home-grown
logger grows, and they turn every helper that reports into one that exits. A function
returns its error; `main` prints it and exits. A real programmer error, an invariant the
code itself violated, is a `panic` with its message, not a log line that happens to exit.

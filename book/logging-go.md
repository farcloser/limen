# Logging — Go

The Go form of [logging](./logging.md): `log/slog`, set up once in `main`. It is the only
facility: klog, logr, logrus, zap, zerolog and the slog wrappers are not taken, in a fork
as in a new program
([dependencies Go](./dependencies-go.md#command-lines-and-loggers-the-standard-library-first)).

## A library logs through slog, never through an output of its own; a command sets it up once, in main

**A library package logs through `log/slog`, and through nothing else.** Through the
default logger, or through a `*slog.Logger` the caller injects, at the levels its records
deserve, debug for the trace a maintainer wants and nothing louder for what is not the
operator's business; the application installs the handler, the level and the destination,
once, and a library never does. What a library never does is choose an output of its own:
`fmt.Printf` to standard output or error, the standard `log` package, a package of its own
with its own writer and format. That is the upstream smell to remove on fork: xz's
`reader.go` and `lzma/reader2.go` printed headers and chunk headers through upstream's own
`internal/xlog` under `-vv`, with the format's `String` methods existing only to feed it
(https://github.com/forkcloser/xz/pull/96 deleted the package, the dumps and the methods);
a trace of that kind, worth keeping, is a `slog.Debug` record the application's handler
shows or drops. What a library has to say to its caller is still a returned error or a
returned value: a log record is for the operator, never a substitute for either.

**A command either logs or prints, and knows which.** A tool with diagnostics at levels
(a linter, a server, anything a `--log-level` flag makes sense for) uses `log/slog`, and
nothing else: the handler and the level are set once in `main`, from the flags, the way
godolint's `cmd/godolint` does it; no third-party logger, no logger set up by a library
on import. A command whose console output is for people takes its handler from
primordium's `app/logger`, tint on a terminal and slog's JSON handler when standard error
is redirected, which is still slog, set once. A tool whose output to the user is messages
(a compressor, a renderer: "file exists", "format not recognized") has nothing to log. It
prints each message to standard error as `<cmd>: <message>` and sets the exit status, with
the quiet levels the tool it mirrors defines (xz: `-q` hides warnings, `-qq` errors too).
No timestamps, no levels, no logging package for three `Fprintf` calls.

**No logger ends the process.** `Fatal` and `Panic` entry points are what a home-grown
logger grows, and they turn every helper that reports into one that exits. A function
returns its error; `main` prints it and exits. A real programmer error, an invariant the
code itself violated, is a `panic` with its message, not a log line that happens to exit.

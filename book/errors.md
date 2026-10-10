# Errors

How a module reports a fault, in every language; the Go form is [errors-go](./errors-go.md).

- **One sentinel per fault class, not per message.** A caller acts on the class, and a
  module has few: the input is not valid (reject it), the caller's argument is wrong (fix
  the call), the transport underneath failed (retry, or report it as the transport's own),
  the format is valid but not implemented. Each is documented by what a caller should do on
  it; the message carries the specifics, the sentinel the class. A module whose every fault
  is one class has told its caller nothing, and so has one whose faults are bare text.
- **Every error wraps a sentinel, and a caller compares by identity, never by text.** The
  chain carries the class without carrying its words; a message that would repeat the
  sentinel's text on every line uses a kind type that unwraps to it instead.
- **A transport error passes through unchanged.** An error from the reader or connection
  underneath a module is returned as the transport's, never reclassified as the module's:
  the caller that owns the transport is the one that can act on it. A short read where the
  format promises bytes is an unexpected end of input, not corruption.
- **A layer maps the layer below into its own vocabulary at the boundary**, keeping the
  lower sentinel reachable through the chain, so a caller matches either and the message
  stays the lower layer's.
- **A chaining API carries its first error** and the terminal operation returns it: a
  setter that returns its receiver has nowhere to report, and dropping the error means a
  value that silently does not hold what was set.
- **No error is logged and returned.** It travels up once, and whoever stops it reports it
  ([logging](./logging.md)).

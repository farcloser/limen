# Boundaries

Where a program meets what it does not control: input, the process, the platform. In every
language; the Go form is [boundaries-go](./boundaries-go.md).

- **A type admits only its valid values.** A value that must not exist is removed by the
  type, sealed, unexported, returned by a function, not guarded at every call site and not
  given a meaning by default. Nothing exported is mutable package state.
- **Input is validated where it enters, once.** A path, a name or a number the user hands
  the program is checked in the layer that parses it, and the rest of the program takes the
  validated value as a fact. Validation that wanders into a library meets inputs that are
  not the host's to judge: a path for another system is never checked with the host's rules,
  and a path that is only read is not checked at all.
- **A size that comes from input is bounded before anything is allocated from it**, against
  a named limit with its own error, and a test runs at the limit. An unbounded size is a
  crash the input chooses.
- **A precondition the program must meet at startup panics when missed.** A package whose
  every call needs something set first fails at the first call made without it, naming what
  is missing; it never degrades to a default that looks like it works.
- **Process-wide state is changed once, by a function named for it**, never as the side
  effect of a read, and the package says what the children inherit.
- **A process identifier is liveness, never identity.** The kernel hands identifiers out
  again, so a holder recorded by its identifier alone is impersonated by whatever process
  gets it next. A holder is its identifier and its start time; a start that cannot be read
  counts as the holder's own. The failure runs toward holding, never toward takeover.
- **A file other processes map is extended, never shrunk, and rebuilt in place.** Shrinking
  it faults every peer past the new end; renaming another file over it is refused where the
  mapping lives; a word the peers spin on inside the mapping is never written by a rebuild.
- **A truncated hash is a hint, never an identity.** A key cut from a digest collides,
  eventually. A record carries a second slice of the same digest as its confirmation; a
  mismatch is a miss, and an invalidation never removes another identifier's record.

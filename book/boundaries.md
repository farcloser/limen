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

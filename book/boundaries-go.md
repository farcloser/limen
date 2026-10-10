# Boundaries — Go

The Go form of [boundaries](./boundaries.md): paths, startup preconditions, the umask.

## Paths are validated where they enter

A path the user hands the program — a flag, an argument, a volume source, a cache or log
location — is made absolute and validated at the input boundary, in the command layer
that parses it, and nowhere deeper: `filepath.Abs`, then primordium's
`pathcheck.Validate`; `pathcheck.ValidateComponent` for a bare name that becomes a
directory (an instance id); `pathcheck.ValidateSocket` on the final absolute string, at
the one place that assembles a socket path, because the `sun_path` limit (104 bytes on
macOS, 108 on linux) binds the whole path and only that place has it. The check runs once,
where the whole input exists, and the rest of the program takes a validated absolute path
as a fact. A command-line flag shared by several commands carries its check once, as a
`Validate` hook on a type the commands embed by name — an anonymous embedding promotes
the method and runs it twice.

Two scopes stay out, on purpose. A path that is only read is not checked: the rules are
the filesystem's for what can be *created*, and a failed open says so itself. A path that
belongs to another system — a guest's working directory, a container's mount destination,
anything the host never resolves — is never checked with the host's rules: macOS limits
applied to a linux guest path refuse valid input and catch nothing the guest would refuse.

The trap the rule prevents is validation that wanders into the library, where host and
guest paths meet in one function and a test of an unrelated feature ends up asserting it.
ossein's first version did exactly that — the guest `Cwd` checked with macOS rules, the
checks asserted from the cache tests, the Windows legs red in `internal/cli` — and was
rebuilt as three checks in `cmd/ossein` plus the socket check where the path is joined,
each with its own test ([farcloser/ossein#103](https://github.com/farcloser/ossein/pull/103)).

## A precondition the program must meet at startup panics when missed

A package whose every call needs something set first — an application name before any
directory, a configuration before any client — panics at the first call made without
it, naming what is missing. It does not degrade: no empty-string default, no zero value
standing for "unset" that the code then builds a path or a connection on. This is
[a type admits only its valid values](./principles.md) at the one place
a type cannot reach, the order of calls at startup; the check is a run of the test
binary in a process of its own, where that call comes first, and the same shape checks
a value the setter refuses. The trap: the degraded path looks like it works, and nobody
reads it until it has done damage. primordium's `dirs` with no application name handed
out the user's whole base directory — `~/.cache`, `~/Library/Application Support` — as
the application's own, created it private, and returned it for every caller to fill
and clean (https://github.com/mycophonic/primordium/pull/159).

## A library zeroes the process umask once, by name, and says what the children inherit

The umask is process-wide state: the kernel strips its bits from the mode of every file
and directory the process creates, so code asking for `0o644` gets `0o600` under an
operator's `0o077` and never learns. A library that wants the mode asked for to be the
mode the file gets zeroes the umask once, at startup, through a function named for the
write (`Disable`), never as the side effect of a read: a `Get` that zeroes on its first
call is the footgun the package exists to remove, and a `Set` has no place beside a
`Disable`. `Get` returns the mask found, the operator's, and panics before `Disable`,
since a umask cannot be read without being set. The package documents that every child
process inherits the zeroed umask, so a tool following the `0o666`-less-umask convention
then creates world-writable files; giving the mask back is the child's
(`sh -c 'umask 077; exec "$@"'`), never a toggle around the spawn, which would strip what
every other goroutine creates meanwhile. A drop-in for `os.WriteFile` honours the umask
as `os.WriteFile` does, by creating with `perm`, never by re-applying a captured mask
after the fact. The trap, from https://github.com/mycophonic/primordium/pull/158:
`WriteFile`'s first call zeroed the umask as a side effect, and every program calling it
without the library's initializer relied on that without knowing; the fix broke eight
callers across four repositories, each of which now says what it wants.

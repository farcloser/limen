# Boundaries — Go

The Go form of [boundaries](./boundaries.md): paths, startup preconditions, the umask, the
holders and the files a process shares with its peers.

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

## A holder is its PID and its start time

A lock word or a slot that records a holder by PID is taken over when that PID is dead, and
the kernel reuses PIDs: a process that got a dead holder's number kept the lock as if the
holder lived, and every peer waited on it until the impostor exited. So the start time sits
beside the PID, as the platform reports it: `sysctl kern.proc.pid` on macOS, field 22 of
`/proc/<pid>/stat` counted after the last `)` on linux, `GetProcessTimes` on Windows. A
holder is alive when its PID is and, where a start was recorded, when the process with that
PID started then. A start the platform cannot read counts as the holder's own, so a live
process is never taken for dead; a zero start means PID-only liveness, and is what an older
file carries. The ordering that keeps a checker from pairing one process's PID with
another's start: a start beside a PID is zero or that PID's own, and zero is always safe, so
freeing writes the start first and the word after, putting a dead word back writes the word
first and its own start after, and a lost takeover leaves the start zero. The start is read
once per process and compared on the slow path only; the hot path's cost stayed where it
was. In a test, the live PID that is not the test's own is its parent's, `os.Getppid()`,
never 1: PID 1's start is unreadable to another user on some linux hosts, and an unreadable
start counts as the holder's own, so the test hung instead of taking over
(https://github.com/mycophonic/primordium/pull/212).

## A shared mapping is extended, never shrunk, and rebuilt in place

A file that several processes map is rebuilt inside the mapping every holder already has.
`Truncate(0)` under a peer's mapping is `SIGBUS` past the end on POSIX and refused outright
on Windows; a rename of a rebuilt sibling over the file is refused on Windows while any peer
maps it, so the POSIX-semantics rename that replaces an open file
([windows](./windows.md#replacing-a-file-that-is-open)) does not reach a mapped one. The
rebuild extends the file if its writer never sized it, never shrinks it, clears and refills
the table in its own mapping, writes the header's counts last, syncs, then removes the
journal. The lock word, which sits inside every holder's mapping, is never written by the
rebuild: the holders wait on it and find the new capacity when the lock is theirs. A failure
to rebuild puts the dead writer's word back, so the next taker tries again and nobody reads
the table it died in. One rebuild serves both the peer that takes a dead writer's lock and
`New` on a fresh open; two paths for one job left `New`'s failing on Windows exactly when a
peer was alive to map the file (https://github.com/mycophonic/primordium/pull/212,
https://github.com/mycophonic/primordium/pull/214).

## A truncated hash is a hint, never an identity

An index keyed by the first 64 bits of a 256-bit digest will, in time, see two identifiers
share a key, and a record that says nothing about which identifier wrote it serves one's
content to the other. The record carries the next 64 bits of the same digest as its
confirmation: a record under the key whose confirmation is another's, or that cannot be
read, is no record, a miss and a refetch, never the wrong blob; an invalidation leaves
another identifier's record alone. A wider record is an on-disk change, so a store opened
over an index of the old width discards it with a warning and fetches each identifier again
once; the blobs, addressed by content, stay (https://github.com/mycophonic/primordium/pull/209).

// Package guard runs a command and kills any process of it that holds more
// memory than a limit. A fuzz run starts one worker a core, each a process
// of its own, and nothing in `go test` bounds what a worker allocates: an
// input that makes the target allocate without end takes the machine's
// memory, swap first, long before any deadline fires.
//
// The command is started as the leader of a process group of its own, and
// every process of that group is read on each tick: the processes it starts
// and theirs, whoever their parent has become. A process that leaves the
// group (setsid, setpgid) leaves the guard; `go test` and its fuzz workers do
// not.
//
// A group of its own is also a group the terminal does not signal, so the
// guard passes on what the terminal sends it. An interrupt, a hangup or a
// request to terminate (SIGINT, SIGHUP, SIGTERM) kills the group. A request
// to quit (SIGQUIT, Ctrl-\) goes to the group, whose stacks are the ones worth
// reading, and the group is killed if it has not gone a moment later. A stop
// (SIGTSTP, Ctrl-Z) stops the group and then the guard, and the group
// continues when the guard does: a guard stopped beside a running command
// would be no guard. What the guard cannot do: answer a SIGKILL, or a SIGSTOP
// sent to it alone, either of which leaves its group running unwatched; or
// lend the command the terminal, so a command that reads the terminal is
// stopped by it.
//
// What is read is what the process costs the machine, not what it has in
// core: on macOS the physical footprint, which counts the pages the kernel
// compressed or swapped out, on Linux resident memory plus swap. Resident
// size alone shrinks under the very pressure the guard exists to stop.
package guard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"time"
)

// DefaultInterval is the time between two readings of the group. The guard
// polls, so a process is over its limit for up to one interval before it is
// seen, by whatever it touches in that time: a process that writes fresh
// memory as fast as the machine allows passes its limit by gigabytes in a
// tenth of a second, and still by hundreds of megabytes in a fiftieth. The
// limit bounds what a process settles at, not its last burst.
const DefaultInterval = 20 * time.Millisecond

// grace is what a process asked to quit is given before it is killed
// outright. A Go program asked to quit stops what it was doing, prints every
// goroutine's stack and exits, and what it leaves by exiting (its stacks,
// and for a fuzz worker its input) is lost if it is killed first: a second
// is room for that on a machine already short of memory. One that ignores
// the request, or cannot answer it, goes on for that second.
const grace = time.Second

// mebibyte is the unit limits are reported in.
const mebibyte = 1 << 20

// signalBacklog is the room for signals that arrive while the guard is busy
// with one: signals are not queued for a channel that is full.
const signalBacklog = 8

// exitFailure is the exit code of a guarded command that did not end on an
// exit code of its own (it was killed by a signal), and of one that exited 0
// after the guard killed one of its processes.
const exitFailure = 1

var (
	// ErrUnsupported is a platform the guard cannot watch a process group on.
	// Nothing runs there: a command started without its guard is the failure
	// this package exists to prevent.
	ErrUnsupported = errors.New("no memory guard on this platform")
	// ErrBlind is a group the guard could not read. The whole group is killed:
	// a guard that cannot see is not a guard.
	ErrBlind = errors.New("cannot read the guarded processes")
	// ErrLimit is a limit of zero.
	ErrLimit = errors.New("the memory limit must be above zero")
	// ErrSignal is a guard ended by a signal, its group killed with it.
	ErrSignal = errors.New("ended by a signal")
)

// Options bound one guarded command.
type Options struct {
	// Limit is the memory one process of the command may hold, in bytes.
	Limit uint64
	// Interval is the time between two readings; zero is DefaultInterval.
	Interval time.Duration
	// Dir is the directory the command runs in; empty is the guard's own.
	Dir string
	// Stdin, Stdout and Stderr are the command's own streams.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Report receives one line per process the guard kills.
	Report io.Writer
}

// Result is how a guarded command ended.
type Result struct {
	// ExitCode is the command's exit code; exitFailure when it ended on a
	// signal, or exited 0 with one of its processes killed.
	ExitCode int
	// Killed counts the processes the guard ended for passing the limit.
	Killed int
}

// What a platform gives the guard is its processGroup: the command's process
// group, with
//
//   - leader, the command itself, whose identifier names the group;
//   - members, the processes of the group that are alive;
//   - footprint, the memory one of them holds, in bytes, or errGone when it
//     ended between the listing and the reading;
//   - quit, which asks one to end and say where it was (SIGQUIT);
//   - kill, which ends one at once, and killAll, which ends them all;
//   - relay, which passes a signal the guard received on to the group, and
//     says what is left for the guard to do about it.
//
// and, before any command starts, listen, which has the signals a terminal or
// a supervisor sends the guard delivered on a channel.

// errGone is a process that ended before it was read.
var errGone = errors.New("the process has ended")

// relayed is what is left for the guard to do about a signal it received.
type relayed int

const (
	// relayedOn is a signal the group has received in the guard's place:
	// nothing more to do.
	relayedOn relayed = iota
	// relayedQuit is a request to quit the group has received: the group is
	// killed if it is still there a grace later.
	relayedQuit
	// relayedEnd is a signal that ends the guard: the group is killed.
	relayedEnd
)

// Run starts the command, ends any process of its group that passes the
// limit, and returns when the command has ended. Nothing of the group is left
// running behind it: cancelling the context kills the group, and so does a
// signal that ends the guard (the package comment says which, and what the
// others do).
func Run(ctx context.Context, opts Options, name string, args ...string) (Result, error) {
	if opts.Limit == 0 {
		return Result{ExitCode: exitFailure}, ErrLimit
	}

	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}

	if opts.Report == nil {
		opts.Report = io.Discard
	}

	// Not CommandContext: on cancellation it kills the leader alone, and the
	// processes the leader started would outlive the guard. The group is
	// killed on cancellation below.
	// #nosec G204 -- the command is the caller's to choose: running it is what a guard is for.
	//nolint:noctx // see above.
	cmd := exec.Command(name, args...)
	cmd.Dir = opts.Dir
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	// Before the command starts: a signal that arrived in between would end
	// the guard with its group running.
	signals := make(chan os.Signal, signalBacklog)

	listen(signals)
	defer signal.Stop(signals)

	watched, err := start(cmd)
	if err != nil {
		return Result{ExitCode: exitFailure}, err
	}

	ended := make(chan struct{})

	go func() {
		// How the command ended is read from its state, below.
		_ = cmd.Wait()

		close(ended)
	}()

	killed, watchErr := watch(ctx, opts, watched, ended, signals)

	// Whatever the leader left running goes with it.
	watched.killAll()

	result := Result{ExitCode: cmd.ProcessState.ExitCode(), Killed: killed}
	if result.ExitCode < 0 || (result.ExitCode == 0 && (killed > 0 || watchErr != nil)) {
		result.ExitCode = exitFailure
	}

	return result, watchErr
}

// watcher is one guarded command's state between two readings.
type watcher struct {
	opts    Options
	watched processGroup
	// asked holds when each process over the limit was asked to quit.
	asked map[int]time.Time
	// quitAt is when the whole group was asked to quit, on the guard's being
	// asked; zero when it was not.
	quitAt time.Time
	// killed counts the processes ended for passing the limit.
	killed int
}

// watch reads the group on every tick until the command ends, and returns how
// many processes it ended. It waits for the command in every case, so the
// caller may read its state.
func watch(
	ctx context.Context, opts Options, watched processGroup, ended <-chan struct{}, signals <-chan os.Signal,
) (int, error) {
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	state := &watcher{opts: opts, watched: watched, asked: map[int]time.Time{}}

	for {
		var err error

		select {
		case <-ended:
			return state.killed, nil
		case <-ctx.Done():
			err = fmt.Errorf("guard: %w", ctx.Err())
		case received := <-signals:
			switch watched.relay(received) {
			case relayedEnd:
				err = fmt.Errorf("guard: %w: %s", ErrSignal, received)
			case relayedQuit:
				state.quitAt = time.Now()
			case relayedOn:
			}
		case now := <-ticker.C:
			err = state.sweep(now)
		}

		if err != nil {
			watched.killAll()
			<-ended

			return state.killed, err
		}
	}
}

// sweep reads every process of the group once and ends those past the limit.
//
// A group whose leader is gone is killed whole. The command has ended then,
// and what it left running holds its output open: the wait for the command
// would last as long as the longest of them.
func (state *watcher) sweep(now time.Time) error {
	pids, err := state.watched.members()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBlind, err)
	}

	// A group asked to quit that is still there a grace later is killed too.
	quitOverdue := !state.quitAt.IsZero() && now.Sub(state.quitAt) >= grace

	if quitOverdue || !slices.Contains(pids, state.watched.leader()) {
		state.watched.killAll()

		return nil
	}

	for _, pid := range pids {
		held, err := state.watched.footprint(pid)
		if errors.Is(err, errGone) {
			continue
		}

		if err != nil {
			return fmt.Errorf("%w: process %d: %w", ErrBlind, pid, err)
		}

		if held > state.opts.Limit {
			state.end(pid, held, now)
		}
	}

	return nil
}

// end asks a process over the limit to quit, and kills it when it was asked
// a grace ago and is still there.
//
// The request comes first for what it leaves behind: a Go program answers
// SIGQUIT with its goroutines' stacks, which say what was allocating, and an
// exit code. `go test -fuzz` keeps the input of a worker that exits so, and
// keeps nothing of one that was killed ("terminated by unexpected signal; no
// crash will be recorded").
func (state *watcher) end(pid int, held uint64, now time.Time) {
	since, already := state.asked[pid]
	if !already {
		state.asked[pid] = now
		state.killed++
		state.watched.quit(pid)

		_, _ = fmt.Fprintf(state.opts.Report,
			"limen guard: ended process %d, which held %d MiB, over the limit of %d MiB a process\n",
			pid, held/mebibyte, state.opts.Limit/mebibyte)

		return
	}

	if now.Sub(since) >= grace {
		state.watched.kill(pid)
	}
}

// Supported reports whether this platform has a guard.
func Supported() bool { return supported }

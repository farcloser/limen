package guard_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/farcloser/limen/internal/guard"
)

// The guarded commands are this test binary itself, started with helperEnv
// set: TestMain then plays the named part instead of running the suite.
const helperEnv = "LIMEN_GUARD_HELPER"

// stacksEnv names the file a helper's child prints its stacks to, where a
// part gives its child one.
const stacksEnv = "LIMEN_GUARD_STACKS"

const (
	mebibyte = 1 << 20
	// limit is what a guarded process may hold in these tests: well above
	// what a Go test binary holds at rest, the race detector's own memory
	// included, and well under what the hog takes.
	limit = 256 * mebibyte
	// hogSize is what the hog ends up holding. It is also the most a test
	// costs the machine if the guard does nothing: the hog stops there.
	hogSize = 1024 * mebibyte
	// hogStep is what the hog takes between two pauses, so that the guard
	// reads it on the way up.
	hogStep  = 64 * mebibyte
	hogPause = 20 * time.Millisecond
	pageSize = 4096
	// survived is what a hog prints when nothing stopped it.
	survived = "the hog took all it wanted"
	// lingerFor is how long a process left behind stays, unless it is killed.
	lingerFor = 30 * time.Second
	// countEvery is the counter's pace.
	countEvery = 10 * time.Millisecond
	// crowd is more processes than the guard's first listing of a group holds.
	crowd = 80
	// parkedGoroutines is how many goroutines a crowded process parks, and
	// settle the time it gives them to block.
	parkedGoroutines = 300
	settle           = 100 * time.Millisecond
	// guardOf starts the name of a part that is the guard itself, and
	// pipedGuardOf that of one whose command writes to it through a pipe.
	guardOf      = "guard of "
	pipedGuardOf = "piped guard of "
)

func TestMain(m *testing.M) {
	if part := os.Getenv(helperEnv); part != "" {
		// A helper's exit code is its part's, not a suite's.
		os.Exit(play(part))
	}

	m.Run()
}

// say writes one line on the helper's standard output.
func say(words ...any) {
	_, _ = fmt.Fprintln(os.Stdout, words...)
}

// play is this binary as a guarded command, and returns its exit code.
func play(part string) int {
	switch part {
	case "calm":
		say("calm")
	case "exit3":
		return 3
	case "hog":
		hog()
	case "deaf hog":
		signal.Ignore(syscall.SIGQUIT)
		hog()
	case "parent of a hog":
		// The hog is a grandchild of the guard: the parent reports how it
		// ended, and ends well itself.
		say("the child ended:", helper("hog").Run())
	case "leaves one behind":
		// Starts a process that outlives it, prints its identifier and ends.
		child := helper("linger")
		if err := child.Start(); err != nil {
			say(err)

			return 1
		}

		say(child.Process.Pid)
	case "linger":
		time.Sleep(lingerFor)
	case "announced linger":
		say(os.Getpid())
		time.Sleep(lingerFor)
	case "deaf announced linger":
		signal.Ignore(syscall.SIGQUIT)
		say(os.Getpid())
		time.Sleep(lingerFor)
	case "parent of an announced linger":
		// A parent and a child that both answer a request to quit at once.
		_ = helper("announced linger").Run()
	case "parent of a deaf linger":
		// A parent that answers a request to quit, and a child that does not.
		_ = helper("deaf announced linger").Run()
	case "parent of a crowded linger":
		// Two processes in the group: the parent is silent until its child
		// has announced itself, then waits for it. The child's stacks go to
		// a file of their own: written beside the parent's, the two would
		// land inside each other's lines.
		stacks, err := os.Create(os.Getenv(stacksEnv))
		if err != nil {
			say(err)

			return 1
		}

		child := helper("crowded linger")
		child.Stderr = stacks

		_ = child.Run()
	case "crowded linger":
		// A process with a great many goroutines, whose stacks take longer
		// to print than its parent's: asked to quit with its parent, it is
		// still printing when its parent has gone.
		block := make(chan struct{})

		var entered sync.WaitGroup

		for range parkedGoroutines {
			entered.Add(1)

			go parked(&entered, block)
		}

		// Announced once every goroutine is inside parked and has had the
		// time to block there: one still running on another thread when the
		// request comes has no stack to print.
		entered.Wait()
		time.Sleep(settle)
		say(os.Getpid())
		time.Sleep(lingerFor)
	case "counter":
		// One line every few milliseconds, for as long as nothing stops it.
		say(os.Getpid())

		for until := time.Now().Add(lingerFor); time.Now().Before(until); {
			say("tick")
			time.Sleep(countEvery)
		}
	case "crowd then hog":
		// More processes than the guard's first listing of a group holds,
		// and the leader over the limit among them.
		for range crowd {
			if err := helper("linger").Start(); err != nil {
				say(err)

				return 1
			}
		}

		hog()
	default:
		// "guard of <part>": this binary as the guard itself, a process a
		// test can signal, with another part as its command.
		// "piped guard of <part>": the same, the command's output reaching
		// the guard through a pipe, as it does when the guard's caller gives
		// it anything but a file. The guard then waits for the command until
		// the last process holding the pipe has gone.
		var stdout, stderr io.Writer = os.Stdout, os.Stderr

		inner, isGuard := strings.CutPrefix(part, guardOf)
		if piped, isPiped := strings.CutPrefix(part, pipedGuardOf); isPiped {
			inner, isGuard = piped, true
			stdout, stderr = notAFile{os.Stdout}, notAFile{os.Stderr}
		}

		if !isGuard {
			say("no such part:", part)

			return 1
		}

		result, err := guard.Run(context.Background(), guard.Options{
			Limit:  limit,
			Stdout: stdout,
			Stderr: stderr,
			Report: os.Stderr,
		}, "env", helperEnv+"="+inner, os.Args[0])
		if err != nil {
			say("the guard ended:", err)
		}

		return result.ExitCode
	}

	return 0
}

// notAFile hides a file from os/exec, which hands a command a file as it is
// and anything else through a pipe.
type notAFile struct{ io.Writer }

// parked is where the goroutines of a crowded process wait: a name to count
// in its stacks.
//
//go:noinline
func parked(entered *sync.WaitGroup, block <-chan struct{}) {
	entered.Done()
	<-block
}

// hog takes memory in steps, touching every page, and holds it a while.
func hog() {
	var held [][]byte

	for taken := 0; taken < hogSize; taken += hogStep {
		block := make([]byte, hogStep)
		for at := 0; at < len(block); at += pageSize {
			block[at] = 1
		}

		held = append(held, block)

		time.Sleep(hogPause)
	}

	time.Sleep(time.Second)
	say(survived, len(held))
}

// helper is this binary playing a part, as a command a helper starts itself.
func helper(part string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])

	cmd.Env = append(os.Environ(), helperEnv+"="+part)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

// outcome is what one guarded run returned and wrote.
type outcome struct {
	result guard.Result
	err    error
	// output is the command's two streams; report is the guard's own lines.
	output string
	report string
}

// guarded runs this binary playing a part, under the guard. The part reaches
// the binary through env(1), which becomes it: the tests run in parallel and
// cannot share the process's own environment.
func guarded(ctx context.Context, t *testing.T, part string) outcome {
	t.Helper()

	if !guard.Supported() {
		t.Skip("no memory guard on this platform")
	}

	var output, report bytes.Buffer

	result, err := guard.Run(ctx, guard.Options{
		Limit:  limit,
		Stdout: &output,
		Stderr: &output,
		Report: &report,
	}, "env", helperEnv+"="+part, os.Args[0])

	return outcome{result: result, err: err, output: output.String(), report: report.String()}
}

func TestAProcessPastTheLimitIsEnded(t *testing.T) {
	t.Parallel()

	got := guarded(t.Context(), t, "hog")
	if got.err != nil {
		t.Fatal(got.err)
	}

	if strings.Contains(got.output, survived) {
		t.Fatalf("the hog was left to take %d MiB under a limit of %d", hogSize/mebibyte, limit/mebibyte)
	}

	if got.result.Killed != 1 || got.result.ExitCode == 0 {
		t.Errorf("result = %+v, want one process ended and a failing exit code", got.result)
	}

	want := fmt.Sprintf("over the limit of %d MiB a process", limit/mebibyte)
	if !strings.Contains(got.report, "limen guard: ended process ") || !strings.Contains(got.report, want) {
		t.Errorf("the report does not name the process and the limit:\n%s", got.report)
	}

	// Asked to quit, a Go program says where it was: the stacks are what tells
	// a reader which code was allocating.
	if !strings.Contains(got.output, "SIGQUIT: quit") {
		t.Errorf("the hog was not asked to quit before it was ended:\n%s", got.output)
	}

	if !underRace && !strings.Contains(got.output, "guard_test.hog") {
		t.Errorf("the hog did not print its stacks on the way out:\n%s", got.output)
	}
}

// A process that does not answer the request is killed outright.
func TestAProcessThatIgnoresTheRequestIsKilled(t *testing.T) {
	t.Parallel()

	got := guarded(t.Context(), t, "deaf hog")
	if got.err != nil {
		t.Fatal(got.err)
	}

	if strings.Contains(got.output, survived) {
		t.Fatalf("a hog that ignores SIGQUIT was left to take all it wanted:\n%s", got.output)
	}

	if got.result.Killed != 1 || got.result.ExitCode == 0 {
		t.Errorf("result = %+v, want one process ended and a failing exit code", got.result)
	}
}

func TestAProcessUnderTheLimitEndsAsItWould(t *testing.T) {
	t.Parallel()

	for part, code := range map[string]int{"calm": 0, "exit3": 3} {
		t.Run(part, func(t *testing.T) {
			t.Parallel()

			got := guarded(t.Context(), t, part)
			if got.err != nil {
				t.Fatal(got.err)
			}

			if got.result.ExitCode != code || got.result.Killed != 0 {
				t.Errorf("result = %+v, want exit code %d and nothing ended\n%s", got.result, code, got.output)
			}

			if got.report != "" {
				t.Errorf("the guard reported with nothing to end:\n%s", got.report)
			}
		})
	}
}

// The process over the limit is ended, not the command: a fuzz worker goes,
// and the coordinator that started it lives to record the input.
func TestAGrandchildPastTheLimitIsEndedAlone(t *testing.T) {
	t.Parallel()

	got := guarded(t.Context(), t, "parent of a hog")
	if got.err != nil {
		t.Fatal(got.err)
	}

	if strings.Contains(got.output, survived) {
		t.Fatalf("the hog was left to take all it wanted:\n%s", got.output)
	}

	// A Go program asked to quit exits 2 after its stacks; one that could
	// not print them is killed at the end of its grace.
	want := "the child ended: exit status 2"
	if underRace {
		want = "the child ended: "
	}

	if !strings.Contains(got.output, want) {
		t.Errorf("the parent did not outlive its child to report it:\n%s", got.output)
	}

	// The parent exits 0; a process ended under it is still a failure.
	if got.result.Killed != 1 || got.result.ExitCode != 1 {
		t.Errorf("result = %+v, want one process ended and exit code 1\n%s", got.result, got.report)
	}
}

func TestNothingOutlivesTheCommand(t *testing.T) {
	t.Parallel()

	started := time.Now()

	got := guarded(t.Context(), t, "leaves one behind")
	if got.err != nil || got.result.ExitCode != 0 {
		t.Fatalf("result = %+v, %v\n%s", got.result, got.err, got.output)
	}

	// What is left behind holds the command's output open: a guard that did
	// not kill it would have waited here until it ended by itself.
	if took := time.Since(started); took > lingerFor/2 {
		t.Fatalf("the guard waited %s for what the command left behind", took)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(got.output))
	if err != nil {
		t.Fatalf("no process identifier in %q", got.output)
	}

	waitGone(t, pid)
}

func TestCancellationKillsTheGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	started := time.Now()

	got := guarded(ctx, t, "linger")
	if !errors.Is(got.err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context's", got.err)
	}

	if got.result.ExitCode == 0 {
		t.Errorf("result = %+v, want a failing exit code", got.result)
	}

	if took := time.Since(started); took > lingerFor/2 {
		t.Errorf("the command ran %s after its context ended", took)
	}
}

func TestAZeroLimitRunsNothing(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	_, err := guard.Run(t.Context(), guard.Options{Stdout: &output}, os.Args[0], "-test.list", ".")
	if !errors.Is(err, guard.ErrLimit) {
		t.Errorf("err = %v, want ErrLimit", err)
	}

	if output.Len() != 0 {
		t.Errorf("the command ran without a limit: %q", output.String())
	}
}

func TestAPlatformWithoutAGuardRunsNothing(t *testing.T) {
	t.Parallel()

	if guard.Supported() {
		t.Skip("this platform has a guard")
	}

	var output bytes.Buffer

	_, err := guard.Run(t.Context(), guard.Options{Limit: limit, Stdout: &output}, os.Args[0], "-test.list", ".")
	if !errors.Is(err, guard.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}

	if output.Len() != 0 {
		t.Errorf("the command ran unguarded: %q", output.String())
	}
}

// A command whose output is a file is waited for no longer than it runs:
// nothing in the watch sees its leader gone, and what it left behind is
// killed when the guard returns.
func TestNothingOutlivesACommandThatWritesToAFile(t *testing.T) {
	t.Parallel()

	if !guard.Supported() {
		t.Skip("no memory guard on this platform")
	}

	output, err := os.Create(filepath.Join(t.TempDir(), "output"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = output.Close() }()

	result, err := guard.Run(t.Context(), guard.Options{Limit: limit, Stdout: output, Stderr: output},
		"env", helperEnv+"=leaves one behind", os.Args[0])
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}

	written, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(written)))
	if err != nil {
		t.Fatalf("no process identifier in %q", written)
	}

	waitGone(t, pid)
}

// The listing of a group larger than the room first given it is read again,
// whole: the process over the limit is found among the others.
func TestAProcessPastTheLimitIsFoundInACrowd(t *testing.T) {
	t.Parallel()

	got := guarded(t.Context(), t, "crowd then hog")
	if got.err != nil {
		t.Fatal(got.err)
	}

	if strings.Contains(got.output, survived) {
		t.Fatalf("the hog was left to take all it wanted among %d processes", crowd)
	}

	if got.result.Killed != 1 || !strings.Contains(got.report, "limen guard: ended process ") {
		t.Errorf("result = %+v, want the one process over the limit ended by name\n%s", got.result, got.report)
	}
}

// waitGone fails the test when a process is still there a few seconds on.
func waitGone(t *testing.T, pid int) {
	t.Helper()

	// Killed, a process is gone at once or a moment later, when whoever
	// inherited it reaps it.
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still there", pid)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

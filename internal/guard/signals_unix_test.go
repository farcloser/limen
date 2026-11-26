//go:build darwin || linux

package guard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/farcloser/limen/internal/guard"
)

// What the guard does with a signal sent to it, read from outside: the guard
// is a process of its own here, since a test cannot stop or quit the process
// it runs in.

// guardProcess is the guard as a process of its own, with a part as its
// command, and the file its command and it write to.
type guardProcess struct {
	cmd    *exec.Cmd
	output string
	// done is closed when the guard has ended.
	done chan struct{}
}

// startGuard starts the guard as a process a test can signal, and returns
// once its command has announced its process identifier, with it.
func startGuard(t *testing.T, part string) (*guardProcess, int) {
	t.Helper()

	if !guard.Supported() {
		t.Skip("no memory guard on this platform")
	}

	name := filepath.Join(t.TempDir(), "output")

	output, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0])

	cmd.Env = append(os.Environ(), helperEnv+"="+guardOf+part)
	cmd.Stdout = output
	cmd.Stderr = output

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	_ = output.Close()

	started := &guardProcess{cmd: cmd, output: name, done: make(chan struct{})}

	go func() {
		// How the guard ended is the test's to read from what it wrote.
		_ = cmd.Wait()

		close(started.done)
	}()

	t.Cleanup(func() {
		// Whatever the test left: the guard takes its group with it.
		_ = cmd.Process.Signal(syscall.SIGTERM)
	})

	deadline := time.Now().Add(10 * time.Second)

	for {
		first, _, _ := strings.Cut(started.read(t), "\n")

		if pid, err := strconv.Atoi(strings.TrimSpace(first)); err == nil {
			return started, pid
		}

		if time.Now().After(deadline) {
			t.Fatalf("the guarded command never announced itself:\n%s", started.read(t))
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// read is what the guard and its command have written so far.
func (started *guardProcess) read(t *testing.T) string {
	t.Helper()

	written, err := os.ReadFile(started.output)
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

// wait is the guard's end, or the test's if it does not come.
func (started *guardProcess) wait(t *testing.T) {
	t.Helper()

	select {
	case <-started.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the guard did not end:\n%s", started.read(t))
	}
}

// A guard asked to quit passes the request on: the stacks printed are the
// command's, the command ends, and the guard with it. A guard that took the
// request for itself would die on it and leave its command running in a
// group no terminal signals.
func TestARequestToQuitGoesToTheCommand(t *testing.T) {
	t.Parallel()

	started, pid := startGuard(t, "announced linger")

	if err := started.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}

	started.wait(t)
	waitGone(t, pid)

	written := started.read(t)
	if strings.Count(written, "SIGQUIT: quit") != 1 {
		t.Errorf("want one set of stacks:\n%s", written)
	}

	if !underRace && !strings.Contains(written, "guard_test.play") {
		t.Errorf("the stacks are not the command's:\n%s", written)
	}

	if strings.Contains(written, "guard.Run") {
		t.Errorf("the stacks are the guard's own:\n%s", written)
	}
}

// The request goes to every process of the group, not to its leader alone:
// each says where it was.
func TestARequestToQuitGoesToTheWholeGroup(t *testing.T) {
	t.Parallel()

	started, pid := startGuard(t, "parent of an announced linger")

	if err := started.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}

	started.wait(t)
	waitGone(t, pid)

	if written := started.read(t); strings.Count(written, "SIGQUIT: quit") != 2 {
		t.Errorf("want two sets of stacks, the parent's and its child's:\n%s", written)
	}
}

// A command that does not answer the request passed on to it is killed a
// moment later, and the guard ends: asked to quit, it does not go on
// guarding a command nobody can reach.
func TestACommandThatIgnoresAPassedOnRequestIsKilled(t *testing.T) {
	t.Parallel()

	started, pid := startGuard(t, "deaf announced linger")

	if err := started.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}

	started.wait(t)
	waitGone(t, pid)
}

// Each of the signals that end a guard takes its command with it.
func TestASignalThatEndsTheGuardKillsTheGroup(t *testing.T) {
	t.Parallel()

	for _, sent := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sent.String(), func(t *testing.T) {
			t.Parallel()

			started, pid := startGuard(t, "announced linger")

			if err := started.cmd.Process.Signal(sent); err != nil {
				t.Fatal(err)
			}

			started.wait(t)
			waitGone(t, pid)

			if written := started.read(t); !strings.Contains(written, "the guard ended: guard: ended by a signal") {
				t.Errorf("the guard did not say what ended it:\n%s", written)
			}
		})
	}
}

// A guard that is stopped stops its command, and continues it when it is
// continued: stopped beside a running command it would be no guard.
func TestAStoppedGuardStopsItsCommand(t *testing.T) {
	t.Parallel()

	started, pid := startGuard(t, "counter")

	ticks := func() int { return strings.Count(started.read(t), "tick") }

	if err := started.cmd.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatal(err)
	}

	// Time for the stop to reach the command, then a stretch in which a
	// running counter would have written dozens of lines.
	time.Sleep(300 * time.Millisecond)

	stopped := ticks()

	time.Sleep(500 * time.Millisecond)

	if later := ticks(); later != stopped {
		t.Fatalf("the command wrote %d lines while its guard was stopped", later-stopped)
	}

	if err := started.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for ticks() == stopped {
		if time.Now().After(deadline) {
			t.Fatal("the command did not continue with its guard")
		}

		time.Sleep(20 * time.Millisecond)
	}

	if err := started.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	started.wait(t)
	waitGone(t, pid)
}

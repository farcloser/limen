//go:build darwin || linux

package guard

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

const supported = true

// listen has the signals a terminal or a supervisor sends the guard delivered
// on the channel: the three that end it, the request to quit, the stop and
// the continue.
func listen(signals chan<- os.Signal) {
	signal.Notify(signals,
		os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGCONT)
}

// processGroup is the group the command leads: its identifier is the
// command's process identifier, and stays the group's after the command ends,
// for as long as one process of it lives.
type processGroup struct {
	pgid int
}

// start makes the command the leader of a new process group and starts it.
func start(cmd *exec.Cmd) (processGroup, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return processGroup{}, fmt.Errorf("guard: %w", err)
	}

	return processGroup{pgid: cmd.Process.Pid}, nil
}

func (g processGroup) leader() int { return g.pgid }

func (processGroup) quit(pid int) {
	_ = syscall.Kill(pid, syscall.SIGQUIT)
}

func (processGroup) kill(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func (g processGroup) killAll() {
	_ = syscall.Kill(-g.pgid, syscall.SIGKILL)
}

// relay passes a signal the guard received on to the group.
func (g processGroup) relay(received os.Signal) relayed {
	switch received {
	case syscall.SIGQUIT:
		_ = syscall.Kill(-g.pgid, syscall.SIGQUIT)

		return relayedQuit
	case syscall.SIGTSTP:
		// The group first, then the guard itself. The group is continued
		// when the guard is told it was (SIGCONT, below), never right after
		// the guard's own stop: that signal goes to the process, and on
		// Linux the thread that sent it can run on for a moment before the
		// process stops, long enough to have continued the group at once.
		_ = syscall.Kill(-g.pgid, syscall.SIGSTOP)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)

		return relayedOn
	case syscall.SIGCONT:
		_ = syscall.Kill(-g.pgid, syscall.SIGCONT)

		return relayedOn
	default:
		return relayedEnd
	}
}

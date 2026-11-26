//go:build !darwin && !linux

package guard

import (
	"os"
	"os/exec"
	"os/signal"
)

const supported = false

// listen has the interrupt delivered on the channel.
func listen(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

// processGroup has nothing to hold here: there is no reading a process
// group's memory on this platform, and start never returns one in use.
type processGroup struct{}

// start runs nothing: a command started without its guard is what this
// package prevents.
func start(*exec.Cmd) (processGroup, error) {
	return processGroup{}, ErrUnsupported
}

func (processGroup) leader() int { return 0 }

func (processGroup) members() ([]int, error) { return nil, ErrUnsupported }

func (processGroup) footprint(int) (uint64, error) { return 0, ErrUnsupported }

func (processGroup) quit(int) {}

func (processGroup) kill(int) {}

func (processGroup) killAll() {}

func (processGroup) relay(os.Signal) relayed { return relayedEnd }

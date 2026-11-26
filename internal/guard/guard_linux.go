package guard

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

const (
	procRoot = "/proc"
	// kibibyte is the unit /proc/<pid>/status reports memory in.
	kibibyte = 1 << 10
	// groupField is where the process group stands among the fields of
	// /proc/<pid>/stat that follow the command name: state, parent, group.
	groupField = 2
)

// The two lines of /proc/<pid>/status that say what a process costs: what it
// has in core, and what was swapped out.
const (
	residentKey = "VmRSS:"
	swapKey     = "VmSwap:"
)

// errStat is a /proc/<pid>/stat line that does not read as one.
var errStat = errors.New("unreadable stat line")

func (g processGroup) members() ([]int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("guard: %w", err)
	}

	var pids []int

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// A process the guard may not read is another user's, on a host
		// that hides them (hidepid): not one the command started.
		pgid, err := groupOf(pid)
		if errors.Is(err, errGone) || errors.Is(err, fs.ErrPermission) {
			continue
		}

		if err != nil {
			return nil, err
		}

		if pgid == g.pgid {
			pids = append(pids, pid)
		}
	}

	return pids, nil
}

// groupOf reads a process's group from /proc/<pid>/stat. The command name
// comes second, between parentheses, and may itself hold spaces and
// parentheses: the fields are counted from the last closing one.
func groupOf(pid int) (int, error) {
	stat, err := readProc(pid, "stat")
	if err != nil {
		return 0, err
	}

	closing := bytes.LastIndexByte(stat, ')')
	if closing < 0 {
		return 0, fmt.Errorf("guard: process %d: %w", pid, errStat)
	}

	fields := bytes.Fields(stat[closing+1:])
	if len(fields) <= groupField {
		return 0, fmt.Errorf("guard: process %d: %w", pid, errStat)
	}

	pgid, err := strconv.Atoi(string(fields[groupField]))
	if err != nil {
		return 0, fmt.Errorf("guard: process %d: %w", pid, errStat)
	}

	return pgid, nil
}

// footprint is resident memory plus what was swapped out. A process with no
// address space of its own (a zombie, a kernel thread) has neither line and
// holds nothing.
func (processGroup) footprint(pid int) (uint64, error) {
	status, err := readProc(pid, "status")
	if err != nil {
		return 0, err
	}

	var held uint64

	for line := range bytes.Lines(status) {
		for _, key := range []string{residentKey, swapKey} {
			rest, found := bytes.CutPrefix(line, []byte(key))
			if !found {
				continue
			}

			fields := bytes.Fields(rest)
			if len(fields) == 0 {
				continue
			}

			amount, err := strconv.ParseUint(string(fields[0]), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("guard: process %d: %w", pid, err)
			}

			held += amount * kibibyte
		}
	}

	return held, nil
}

// readProc reads one file of a process's /proc directory. A process that
// ended since it was listed has no such file, or one that reads ESRCH.
func readProc(pid int, name string) ([]byte, error) {
	// #nosec G304 -- the path is /proc, a process identifier and a fixed name.
	content, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/" + name)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return nil, errGone
	}

	if err != nil {
		return nil, fmt.Errorf("guard: %w", err)
	}

	return content, nil
}

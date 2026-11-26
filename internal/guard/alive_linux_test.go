package guard_test

import (
	"bytes"
	"os"
	"strconv"
)

// alive reports whether a process still runs. A process that ended and that
// nothing has reaped yet is not alive: in a container whose first process
// reaps nothing, a killed process stays a zombie for good, and still answers
// a signal.
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}

	// The state is the first field after the command name's closing
	// parenthesis.
	closing := bytes.LastIndexByte(stat, ')')
	if closing < 0 {
		return false
	}

	fields := bytes.Fields(stat[closing+1:])

	return len(fields) > 0 && string(fields[0]) != "Z"
}

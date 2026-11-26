package guard_test

import "syscall"

// alive reports whether a process still exists. launchd reaps what it
// inherits at once, so a process that was killed does not linger as a zombie.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

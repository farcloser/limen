//go:build race

package guard_test

// underRace is true when the tests, and so the helpers they start, are built
// with the race detector. A process built so does not always manage to print
// its stacks when asked to quit: the traceback can fail inside the detector's
// own frames, and the process is then killed at the end of its grace.
const underRace = true

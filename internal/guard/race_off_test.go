//go:build !race

package guard_test

// underRace is false without the race detector: a helper asked to quit
// prints its stacks and exits.
const underRace = false

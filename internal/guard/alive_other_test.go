//go:build !darwin && !linux

package guard_test

// alive is never asked where there is no guard: the tests that would ask
// skip first.
func alive(int) bool { return false }

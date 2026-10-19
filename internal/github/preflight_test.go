// Tests for the gh preflight, through PATH alone. Serial where they set it.

package github_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// TestPreflightNoGH: no gh on PATH is one error naming gh and PATH,
// not an audit of "cannot verify" findings.
func TestPreflightNoGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := github.Preflight(t.Context())
	if err == nil {
		t.Fatal("Preflight() = nil with no gh on PATH")
	}

	for _, want := range []string{"gh", "PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Preflight() error should mention %q, got: %v", want, err)
		}
	}
}

// TestPreflightGH: a gh that runs passes, whatever the API would answer.
func TestPreflightGH(t *testing.T) {
	t.Parallel()

	if err := github.Preflight(t.Context()); err != nil {
		t.Fatalf("Preflight() = %v with the stub on PATH", err)
	}
}

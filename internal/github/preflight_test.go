// Tests for the preflight: the token, resolved before the first call.

package github_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// TestPreflightNoToken: no token in the environment and no gh on PATH is one
// error naming both ways in, not an audit of "cannot verify" findings.
func TestPreflightNoToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())

	err := github.Preflight(t.Context())
	if err == nil {
		t.Fatal("Preflight() = nil with no token and no gh on PATH")
	}

	for _, want := range []string{"GH_TOKEN", "gh auth login"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Preflight() error should mention %q, got: %v", want, err)
		}
	}
}

// TestPreflightEnvironmentToken: a token in the environment is the
// credential, gh asked nothing; GITHUB_TOKEN is what Actions sets.
func TestPreflightEnvironmentToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "from-actions")
	t.Setenv("PATH", t.TempDir())

	if err := github.Preflight(t.Context()); err != nil {
		t.Fatalf("Preflight() = %v with GITHUB_TOKEN set", err)
	}
}

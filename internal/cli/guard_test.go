package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/guard"
)

// TestGuardWantsALimitAndACommand: `limen guard` without a limit above zero,
// or without a command, is a usage error and runs nothing.
func TestGuardWantsALimitAndACommand(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"no command":    {"guard", "-memory", "64"},
		"no limit":      {"guard", "--", os.Args[0], "-test.list", "."},
		"a zero limit":  {"guard", "-memory", "0", "--", os.Args[0], "-test.list", "."},
		"a limit in GB": {"guard", "-memory", "2G", "--", os.Args[0], "-test.list", "."},
		"a limit too large to count in bytes": {
			"guard", "-memory", "18446744073709551615", "--", os.Args[0], "-test.list", ".",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			if code := run(args, &stdout, &stderr); code != 2 {
				t.Errorf("exit code %d, want 2\n%s", code, stderr.String())
			}

			if stdout.Len() != 0 {
				t.Errorf("the command ran: %q", stdout.String())
			}
		})
	}
}

// TestGuardRunsItsCommand: under its limit the command runs, its output is
// the guard's, and its exit code too. This test binary, asked to list its
// tests, is the command.
func TestGuardRunsItsCommand(t *testing.T) {
	t.Parallel()

	if !guard.Supported() {
		t.Skip("no memory guard on this platform")
	}

	var stdout, stderr bytes.Buffer

	args := []string{"guard", "-memory", "1024", "--", os.Args[0], "-test.list", "^TestGuardRunsItsCommand$"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, stderr.String())
	}

	if !strings.Contains(stdout.String(), "TestGuardRunsItsCommand") {
		t.Errorf("the command's output is not the guard's: %q", stdout.String())
	}
}

// TestGuardRunsItsCommandWhereItIsTold: -dir is the command's directory, so
// a recipe can start a test binary in its package's directory without
// leaving its own.
func TestGuardRunsItsCommandWhereItIsTold(t *testing.T) {
	t.Parallel()

	if !guard.Supported() {
		t.Skip("no memory guard on this platform")
	}

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer

	if code := run([]string{"guard", "-memory", "1024", "-dir", dir, "--", "pwd"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, stderr.String())
	}

	if got := strings.TrimSpace(stdout.String()); got != dir {
		t.Errorf("the command ran in %q, want %q", got, dir)
	}
}

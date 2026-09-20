// The rig every test in this package runs against: this test binary doubles
// as the fake go and the fake aqua, copied under those names into a directory
// TestMain puts first on PATH. Dispatch is by the name the binary was invoked
// under, so nothing in the package under test is shaped for the tests; it
// resolves "go" and "aqua" on PATH exactly as it does for a user.

package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

// stubChecksums is what the fake aqua writes for `update-checksum`:
// recognizably NOT limen's canonical checksums, which is the whole point —
// regenerated, never copied.
const stubChecksums = `{"stub":true}` + "\n"

// suiteGuardEnv marks the environment of the running suite. The test binary
// doubles as two stubs, so a child that is this binary and matches neither is
// a bug — and running the suite from inside the suite is a fork bomb. The
// guard turns that into an immediate failure.
const suiteGuardEnv = "LIMEN_TEST_SUITE_RUNNING"

func TestMain(m *testing.M) {
	// Invoked under a stub's name: play it. The stub's exit status IS its
	// contract, so these branches never reach the runner's exit handling.
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") {
	case "go":
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runGoStub())
	case "aqua":
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runAquaStub())
	default:
		// The suite itself.
	}

	if os.Getenv(suiteGuardEnv) != "" {
		fmt.Fprintf(os.Stderr, "test binary re-executed as %q from inside the suite: refusing to recurse\n", os.Args)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	if err := os.Setenv(suiteGuardEnv, "1"); err != nil {
		fmt.Fprintln(os.Stderr, "arming the suite guard:", err)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	// Every test runs against the fakes: the gotools remediation shells out
	// on any repository lacking a directive, the aqua one whenever checksums
	// must be regenerated, and the real tools would reach the network from
	// dozens of parallel tests. Set once, before any test, so tests stay
	// parallel; the two that need a PATH without the tools set their own.
	dir, err := installStubs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "installing the stubs:", err)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, "putting the stubs on PATH:", err)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	// The runner exits with m.Run's status once TestMain returns; the stub
	// directory is removed on the way out.
	m.Run()

	_ = os.RemoveAll(dir)
}

// installStubs copies (or links) this binary into a fresh directory under the
// names TestMain dispatches on — exactly `go` and `aqua`, plus the extension
// Windows needs to execute them. Not the test binary's own extension:
// `rules.test` would yield `go.test`, which TestMain does not recognize, and
// every child would then run the suite.
func installStubs() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "limen-rules-stubs")
	if err != nil {
		return "", err
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	for _, name := range []string{"go", "aqua"} {
		stub := filepath.Join(dir, name+ext)
		if err := os.Symlink(self, stub); err == nil {
			continue
		}

		data, err := os.ReadFile(self)
		if err != nil {
			return "", err
		}

		// 0o700, not 0o600: the stub must be executable.
		if err := os.WriteFile(stub, data, 0o700); err != nil {
			return "", err
		}
	}

	return dir, nil
}

// runAquaStub mimics the two invocations remediation makes: `policy allow`
// (silent success) and `update-checksum`, which writes the stub checksums
// into the working directory (remediation sets cmd.Dir to the repo root).
func runAquaStub() int {
	for _, arg := range os.Args[1:] {
		if arg == "update-checksum" {
			if err := os.WriteFile("aqua-checksums.json", []byte(stubChecksums), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "aqua stub: %v\n", err)

				return 1
			}
		}
	}

	return 0
}

// resolved reports whether an outcome's action counts as the rule resolved,
// the way AllResolved judges a whole set.
func resolved(action rules.Action) bool {
	return slices.Contains(
		[]rules.Action{rules.ActionNone, rules.ActionCreated, rules.ActionOverwrote, rules.ActionMerged},
		action,
	)
}

// fileExists reports whether path names an existing file or directory.
func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// selfPinRE finds the canonical manifest's farcloser/limen pin line, version
// and all, so a fixture can move it without hardcoding what Renovate bumps.
var selfPinRE = regexp.MustCompile(`(?m)^( {2}- name: farcloser/limen@)\S+`)

// withSelfPin is the canonical aqua.yaml with the limen pin at version and
// everything else, the renovate comment included, as it is.
func withSelfPin(t *testing.T, version string) string {
	t.Helper()

	if !selfPinRE.MatchString(limen.CanonicalAquaYAML) {
		t.Fatal("the canonical aqua.yaml carries no farcloser/limen pin")
	}

	return selfPinRE.ReplaceAllString(limen.CanonicalAquaYAML, "${1}"+version)
}

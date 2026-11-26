package limen_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hogTarget is a fuzz target that holds a gibibyte for two seconds on any
// input starting with 'x', which no seed does and a worker reaches within its
// first mutations. The gibibyte is also all a worker costs the machine if
// nothing stops it.
const hogTarget = `package hog

import (
	"testing"
	"time"
)

func FuzzHog(f *testing.F) {
	f.Add([]byte("a"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) == 0 || data[0] != 'x' {
			return
		}

		held := make([]byte, 1<<30)
		for at := 0; at < len(held); at += 4096 {
			held[at] = 1
		}

		time.Sleep(2 * time.Second)

		_ = held[len(held)-1]
	})
}
`

// fuzzRepo makes a repository carrying this checkout's recipes and one
// package whose only test file is source, and returns its path.
//
// The module's name is the run's own. The fuzzer keeps what it found for a
// package in the build cache, under the package's path, and starts the next
// run of that path from it: a second run of a module named as the first
// would meet the first one's findings before it started a worker. The name's
// entry in the cache goes with the test.
func fuzzRepo(t *testing.T, source string) string {
	t.Helper()

	dir := t.TempDir()
	module := "hog-" + filepath.Base(dir) + "-" + filepath.Base(filepath.Dir(dir)) + ".invalid"

	cache, err := command(t, ".", nil, "go", "env", "GOCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOCACHE: %v", err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Join(strings.TrimSpace(string(cache)), "fuzz", module)); err != nil {
			t.Error(err)
		}
	})

	if err := os.CopyFS(filepath.Join(dir, ".limen", "just"), os.DirFS(filepath.Join(".limen", "just"))); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		".justfile":   "import '.limen/just/main.just'\n",
		"go.mod":      "module " + module + "\n\ngo 1.26.0\n",
		"hog.go":      "package hog\n",
		"hog_test.go": source,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// requireGuard skips where `limen guard` cannot read a process group's
// memory: the fuzz lane fuzzes nothing there.
func requireGuard(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("no memory guard on windows: the fuzz lane says so and fuzzes nothing")
	}
}

// TestFuzzEndsAWorkerPastItsMemoryAndKeepsItsInput: the fuzz lane runs the
// workers it was told to, ends the one that passes the memory limit, fails,
// and leaves the input that worker was on under testdata/fuzz, as it leaves
// any other finding.
func TestFuzzEndsAWorkerPastItsMemoryAndKeepsItsInput(t *testing.T) {
	t.Parallel()
	requireGuard(t)

	dev := filepath.Join(t.TempDir(), "limen-dev")
	run(t, ".", nil, "go", "build", "-o", dev, "./cmd/limen")

	repo := fuzzRepo(t, hogTarget)

	out, err := command(t, repo, []string{
		"LIMEN_BIN=" + dev,
		"TEST_GO_FUZZ_WORKERS=2",
		"TEST_GO_FUZZ_MEMORY=256",
		"TEST_GO_FUZZ_TIME=30s",
	}, "just", "do", "test", "go", "fuzz").CombinedOutput()
	if err == nil {
		t.Fatalf("the lane passed with a worker holding a gibibyte under a limit of 256 MiB:\n%s", out)
	}

	for _, want := range []string{
		"now fuzzing with 2 workers",
		"limen guard: ended process ",
		"over the limit of 256 MiB a process",
		"new crasher written",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the lane's output lacks %q:\n%s", want, out)
		}
	}

	kept, err := filepath.Glob(filepath.Join(repo, "testdata", "fuzz", "FuzzHog", "*"))
	if err != nil || len(kept) == 0 {
		t.Fatalf("no input was kept under testdata/fuzz/FuzzHog (%v):\n%s", err, out)
	}

	input, err := os.ReadFile(kept[0])
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(input), `[]byte("x`) {
		t.Errorf("the input kept is not one that made the worker hold memory:\n%s", input)
	}
}

// TestFuzzRefusesABoundThatIsNotOne: a worker count or a memory limit that
// is not a whole number above zero stops the lane before anything runs,
// rather than leaving the run unbounded.
func TestFuzzRefusesABoundThatIsNotOne(t *testing.T) {
	t.Parallel()
	requireGuard(t)

	repo := fuzzRepo(t, hogTarget)

	for _, bound := range []string{
		"TEST_GO_FUZZ_WORKERS=0", "TEST_GO_FUZZ_WORKERS=many", "TEST_GO_FUZZ_MEMORY=-1", "TEST_GO_FUZZ_MEMORY=2G",
	} {
		env := []string{"LIMEN_BIN=false", bound}

		out, err := command(t, repo, env, "just", "do", "test", "go", "fuzz").CombinedOutput()
		if err == nil {
			t.Errorf("%s: the lane passed:\n%s", bound, out)
		}

		name, _, _ := strings.Cut(bound, "=")
		if !strings.Contains(string(out), name+" must be a whole number above zero") {
			t.Errorf("%s: the lane did not name the bound it refuses:\n%s", bound, out)
		}

		if strings.Contains(string(out), " FuzzHog for ") {
			t.Errorf("%s: the lane started fuzzing:\n%s", bound, out)
		}
	}
}

// TestRecipeGitSkipsXcrun: on macOS a recipe's git is the developer
// directory's own binary. /usr/bin/git there is a shim that finds the tool
// through xcrun and, with xcrun_verbose set, says so on stderr; the binary
// itself does not read the variable.
func TestRecipeGitSkipsXcrun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skip("xcrun is macOS's")
	}

	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, ".limen", "just"), os.DirFS(filepath.Join(".limen", "just"))); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(dir, ".justfile"),
		[]byte("import '.limen/just/main.just'\n\nprobe:\n    git --version\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	out, err := command(t, dir, []string{"xcrun_verbose=1"}, "just", "probe").CombinedOutput()
	if err != nil {
		t.Fatalf("just probe: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "git version") {
		t.Fatalf("the recipe ran no git:\n%s", out)
	}

	if strings.Contains(string(out), "xcrun") {
		t.Errorf("a recipe's git went through xcrun:\n%s", out)
	}
}

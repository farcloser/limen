package limen_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

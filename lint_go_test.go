package limen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goModule is a module of one package, its go directive the one this
// repository's lanes run under.
func goModule(t *testing.T, dir, module, source string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		"go.mod":  "module " + module + "\n\ngo 1.26.0\n",
		"main.go": source,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// lintRepo makes a repository carrying this checkout's .limen/just/ recipes, a
// root module and a command that is a module of its own under cmd/, whose
// main.go is source, and returns its path.
func lintRepo(t *testing.T, source string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, ".limen", "just"), os.DirFS(filepath.Join(".limen", "just"))); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(dir, ".justfile"),
		[]byte("import '.limen/just/main.just'\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	// The tools module every repository carries, empty: the mod lane tidies it.
	if err := os.MkdirAll(filepath.Join(dir, ".limen", "tools"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".limen", "tools", "go.mod"),
		[]byte("module example.com/root/.limen/tools\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	goModule(t, dir, "example.com/root", "package main\n\nfunc main() {}\n")
	goModule(t, filepath.Join(dir, "cmd", "nested"), "example.com/root/cmd/nested", source)

	return dir
}

// TestLintGoVetEntersNestedModule: `lint go vet` runs in a command that is a
// module of its own, not only in the root module, so a finding there fails
// the lane and names the module.
func TestLintGoVetEntersNestedModule(t *testing.T) {
	t.Parallel()

	// A build constraint vet's buildtag analyzer refuses: `linux &&` with no
	// right-hand term.
	broken := "//go:build linux &&\n\npackage main\n\nfunc main() {}\n"

	out, err := command(t, lintRepo(t, broken), nil, "just", "do", "lint", "go", "vet").CombinedOutput()
	if err == nil {
		t.Fatalf("a malformed build constraint in cmd/nested passed lint go vet:\n%s", out)
	}

	if !strings.Contains(string(out), "nested") {
		t.Errorf("the failure does not name the nested module:\n%s", out)
	}
}

// TestLintGoVetAndModPassNestedModule: a sound nested module passes the vet
// and tidiness lanes, so entering it reports nothing of its own.
func TestLintGoVetAndModPassNestedModule(t *testing.T) {
	t.Parallel()

	dir := lintRepo(t, "package main\n\nfunc main() {}\n")

	run(t, dir, nil, "just", "do", "lint", "go", "vet")
	run(t, dir, nil, "just", "do", "lint", "go", "mod")
}

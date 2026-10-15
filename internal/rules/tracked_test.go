package rules_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

// TestTracked holds the rule to git's own answer: a canonical file a .gitignore
// pattern swallows is a finding and an advisory, naming the file; a tree git
// cannot answer for is no finding.
func TestTracked(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}

	dir := t.TempDir()

	cmd := exec.CommandContext(t.Context(), "git", "init", "-q")
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	write := func(rel, content string) {
		t.Helper()

		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(".limen/.yamlfmt", "formatter:\n")
	write(".gitignore", "*.log\n")

	if f := findingByRule(rules.Check(dir, rules.Policy{}), "tracked"); !f.OK() {
		t.Fatalf("nothing ignored: %+v", f)
	}

	// Upstream's pattern, as forkcloser/yamlfmt brought it: the tool's own
	// config name, which also matches the canonical file under .limen/.
	write(".gitignore", "*.log\n.yamlfmt\n")

	f := findingByRule(rules.Check(dir, rules.Policy{}), "tracked")
	if f.OK() || f.Path != ".limen/.yamlfmt" {
		t.Fatalf("ignored canonical file: %+v", f)
	}

	var advisory *rules.Outcome

	for _, o := range rules.Fix(t.Context(), dir, rules.FixOptions{}) {
		if o.Rule == "tracked" {
			advisory = &o
		}
	}

	if advisory == nil || advisory.Action != rules.ActionAdvisory || advisory.Path != ".limen/.yamlfmt" {
		t.Fatalf("fix outcome = %+v, want an advisory naming the file", advisory)
	}

	// A .git that is not a repository (the fixtures' empty directory): git
	// cannot answer, and the rule says nothing.
	fake := writeRepo(t, map[string]string{".gitignore": ".yamlfmt\n", ".limen/.yamlfmt": "x\n"})
	if f := findingByRule(rules.Check(fake, rules.Policy{}), "tracked"); !f.OK() {
		t.Fatalf("no repository: %+v", f)
	}
}

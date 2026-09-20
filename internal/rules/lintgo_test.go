package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

const (
	ruleLintGo   = "lintgo"
	lintGoYAML   = ".lint-go.yaml"
	rootGolangci = ".golangci.yml"
)

// goRepoFiles is a compliant Go repository: the compliant fixture plus a
// root go.mod, the analyzers in tools/go.mod and golangci-lint in its module.
func goRepoFiles() map[string]string {
	files := compliantFiles()
	files["go.mod"] = goModBare
	files["tools/go.mod"] = goModWithTools
	files["tools/golangci-lint/go.mod"] = goModGolangci

	return files
}

func TestLintGoOnlyForGoModules(t *testing.T) {
	t.Parallel()

	for _, f := range rules.Check(writeRepo(t, compliantFiles()), rules.DefaultPolicy()) {
		if f.Rule == ruleLintGo {
			t.Fatalf("a repository without go.mod should not be judged on Go lint: %+v", f)
		}
	}

	// Without an overlay the baseline applies as is: a pass that says so.
	f := findingByRule(rules.Check(writeRepo(t, goRepoFiles()), rules.DefaultPolicy()), ruleLintGo)
	if !f.OK() || !strings.Contains(f.Message, "no "+lintGoYAML) {
		t.Fatalf("a Go module without an overlay should pass naming the seed: %+v", f)
	}

	files := goRepoFiles()
	files[lintGoYAML] = "golangci:\n  linters:\n    disable: [dupl]\n"

	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), ruleLintGo); !f.OK() {
		t.Fatalf("a Go module with an overlay should pass: %s", f.Message)
	}
}

// TestLintGoStray: a root golangci-lint configuration, under any of the
// names golangci-lint discovers, fails naming it and the overlay.
func TestLintGoStray(t *testing.T) {
	t.Parallel()

	for _, name := range []string{rootGolangci, ".golangci.yaml", ".golangci.toml", ".golangci.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := goRepoFiles()
			files[name] = "version: \"2\"\n"

			f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), ruleLintGo)
			if f.OK() || f.Path != name || !strings.Contains(f.Message, "move its carve-outs into "+lintGoYAML) {
				t.Fatalf("got %+v, want a failure at %q naming the overlay", f, name)
			}
		})
	}
}

// TestFixSeedsLintGo: fix seeds the overlay once, leaves an existing one
// alone, and reports a stray root configuration as an advisory without
// touching it.
func TestFixSeedsLintGo(t *testing.T) {
	t.Parallel()

	files := goRepoFiles()
	files[rootGolangci] = "version: \"2\"\n"
	dir := writeRepo(t, files)

	outcomes := outcomesFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		ruleLintGo,
	)

	if len(outcomes) != 2 {
		t.Fatalf("want the seed and the advisory, got %+v", outcomes)
	}

	if outcomes[0].Action != rules.ActionCreated || outcomes[0].Path != lintGoYAML {
		t.Errorf("the overlay should be seeded: %+v", outcomes[0])
	}

	if outcomes[1].Action != rules.ActionAdvisory || outcomes[1].Path != rootGolangci ||
		!strings.Contains(outcomes[1].Message, "move its carve-outs") {
		t.Errorf("the stray configuration should be an advisory: %+v", outcomes[1])
	}

	seed, err := os.ReadFile(filepath.Join(dir, lintGoYAML))
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(seed)), "\n") {
		if !strings.HasPrefix(line, "#") {
			t.Errorf("the seed should be comments only, got %q", line)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, rootGolangci)); err != nil {
		t.Errorf("fix must not delete the stray configuration: %v", err)
	}

	// With the stray gone the rule passes, and a second fix changes nothing:
	// the seeded overlay is the project's own now.
	if err := os.Remove(filepath.Join(dir, rootGolangci)); err != nil {
		t.Fatal(err)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), ruleLintGo); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}

	for _, outcome := range outcomesFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		ruleLintGo,
	) {
		if outcome.Action != rules.ActionNone {
			t.Errorf("a second fix should change nothing, got %+v", outcome)
		}
	}
}

package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

// The fake go (see TestMain and installGoStub): `go get -tool a@latest
// b@latest` appends the directives to the go.mod in the working directory,
// `go get -tool a@none` strips them, `go mod tidy` and anything else succeed
// silently.

const goModBare = "module example.com/proj\n\ngo 1.26\n"

// The tool packages the rule requires: everywhere, and the analyzers on top
// of a Go module. Literal here on purpose — the names are what the findings
// carry and what a repository declares, the contract as seen from outside.
var (
	toolsEverywhere = []string{ //nolint:gochecknoglobals // immutable fixture data.
		"github.com/vbatts/git-validation",
		"github.com/farcloser/godolint/cmd/godolint",
		"github.com/forkcloser/dot/cmd/dot",
	}
	sourceAnalyzers = []string{ //nolint:gochecknoglobals // immutable fixture data.
		"golang.org/x/tools/cmd/deadcode",
		"golang.org/x/vuln/cmd/govulncheck",
		"github.com/google/go-licenses/v2",
	}
)

// goModToolsEverywhere is a tools module of a repository without a root
// go.mod: the everywhere set, nothing more — what the compliant fixture
// carries.
const goModToolsEverywhere = `module tools

go 1.26

tool (
	github.com/vbatts/git-validation
	github.com/farcloser/godolint/cmd/godolint
	github.com/forkcloser/dot/cmd/dot
)
`

// goModWithTools declares every required tool, in the block form Go writes,
// plus an unrelated tool and a comment — the parser must see through both.
const goModWithTools = `module example.com/proj

go 1.26

tool (
	github.com/google/go-licenses/v2 // the license scanner
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
	github.com/vbatts/git-validation
	github.com/farcloser/godolint/cmd/godolint
	github.com/forkcloser/dot/cmd/dot
	example.com/other/cmd/thing
)

require golang.org/x/tools v0.49.0 // indirect
`

// goModGolangci is the isolated module a Go repository carries for
// golangci-lint: one directive, its own graph.
const goModGolangci = `module example.com/proj/tools/golangci-lint

go 1.26

tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint
`

func runGoStub() int {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "get" || args[1] != "-tool" {
		return 0 // `go mod tidy` and anything else: succeed silently
	}

	var lines []string

	for _, arg := range args[2:] {
		pkg, version, _ := strings.Cut(arg, "@")
		if version == "none" {
			if !stripGoModToolDirective(pkg) {
				return 1
			}

			continue
		}

		lines = append(lines, "tool "+pkg)
	}

	if len(lines) == 0 {
		return 0
	}

	file, err := os.OpenFile("go.mod", os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}

	defer func() { _ = file.Close() }()

	if _, err := file.WriteString("\n" + strings.Join(lines, "\n") + "\n"); err != nil {
		return 1
	}

	return 0
}

// stripGoModToolDirective removes one package's directive lines from the
// go.mod in the working directory, both forms, the way `go get -tool
// pkg@none` does.
func stripGoModToolDirective(pkg string) bool {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return false
	}

	var kept []string

	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == pkg || trimmed == "tool "+pkg {
			continue
		}

		kept = append(kept, line)
	}

	return os.WriteFile("go.mod", []byte(strings.Join(kept, "\n")), 0o600) == nil
}

// TestGoModToolDirectives: the directive forms a tools/go.mod may carry —
// block with comments and an unrelated tool, one-line, and a require block
// that is not a tool block — judged through the rule.
func TestGoModToolDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		gomod string
		ok    bool
	}{
		{name: "block form with comments and an unrelated tool", gomod: goModWithTools, ok: true},
		{
			name: "one-line form",
			gomod: "module tools\n\ngo 1.26\n\n" +
				"tool github.com/vbatts/git-validation // trailing\n" +
				"tool   github.com/farcloser/godolint/cmd/godolint\n" +
				"tool github.com/forkcloser/dot/cmd/dot\n",
			ok: true,
		},
		{
			name:  "require block is not a tool block",
			gomod: "module tools\n\ngo 1.26\n\nrequire (\n\tgithub.com/vbatts/git-validation v1.2.2\n)\n",
			ok:    false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := compliantFiles()
			files["tools/go.mod"] = testCase.gomod

			f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
			if f.OK() != testCase.ok {
				t.Errorf("ok = %v, want %v: %s", f.OK(), testCase.ok, f.Message)
			}
		})
	}
}

// TestGoToolsEverywhere: a repository without go.mod must still declare the
// everywhere set — and only that: the analyzers are not asked of it.
func TestGoToolsEverywhere(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	if f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("the compliant fixture should pass: %s", f.Message)
	}

	delete(files, "tools/go.mod")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() {
		t.Fatal("a repository without tools/go.mod should fail")
	}

	for _, pkg := range toolsEverywhere {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	for _, pkg := range sourceAnalyzers {
		if strings.Contains(f.Message, pkg) {
			t.Errorf("message asks a non-Go repository for the analyzer %s: %s", pkg, f.Message)
		}
	}
}

func TestGoToolsRequiresDirectives(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["go.mod"] = goModBare

	// The everywhere set alone is not enough once the root carries a go.mod.
	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() {
		t.Fatal("a Go module whose tools/go.mod lacks the analyzers should fail")
	}

	for _, pkg := range sourceAnalyzers {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	delete(files, "tools/go.mod")

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() {
		t.Fatal("a Go module without tools/go.mod should fail")
	}

	for _, pkg := range slices.Concat(toolsEverywhere, sourceAnalyzers) {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	// A complete tools/go.mod is not enough in a Go module: golangci-lint
	// lives in a module of its own, and the check names that module.
	files["tools/go.mod"] = goModWithTools

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() || f.Path != "tools/golangci-lint/go.mod" ||
		!strings.Contains(f.Message, "github.com/golangci/golangci-lint/v2/cmd/golangci-lint") {
		t.Fatalf("a Go module without tools/golangci-lint/go.mod should fail naming it, got: %+v", f)
	}

	files["tools/golangci-lint/go.mod"] = goModGolangci
	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")

	if !f.OK() {
		t.Fatalf("a tools/go.mod declaring every tool, with the isolated module, should pass: %s", f.Message)
	}

	// The directives in the project's own go.mod are the pollution the rule
	// exists to stop, even when tools/go.mod is complete.
	files["go.mod"] = goModWithTools

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() || !strings.Contains(f.Message, "belong in "+"tools/go.mod") {
		t.Fatalf("tool directives in go.mod should fail naming tools/go.mod: ok=%v %s", f.OK(), f.Message)
	}
}

// TestGoToolsRetiredDirective: a tools/go.mod that still carries a retired
// directive beside its replacement fails naming both, and fix removes it
// through `go get -tool pkg@none` (the fake go) and then passes.
func TestGoToolsRetiredDirective(t *testing.T) {
	t.Parallel()

	const retired = "github.com/goccy/go-graphviz/cmd/dot"

	files := compliantFiles()
	files["tools/go.mod"] = goModToolsEverywhere + "\ntool " + retired + "\n"
	dir := writeRepo(t, files)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools")
	if f.OK() || !strings.Contains(f.Message, retired) ||
		!strings.Contains(f.Message, "github.com/forkcloser/dot/cmd/dot") {
		t.Fatalf("a retired directive should fail naming it and its replacement: ok=%v %s", f.OK(), f.Message)
	}

	outcome := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	)

	removed := strings.Contains(outcome.Message, "removed retired tool directive(s) for "+retired)
	if outcome.Action != rules.ActionMerged || !removed {
		t.Fatalf("got %s (%s), want merged with the directive removed", outcome.Action, outcome.Message)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}
}

func TestAquaRejectsRetiredPackage(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: github.com/vbatts/git-validation@v1.2.2\n    registry: local\n")

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a retired canonical package should fail")
	}

	if !strings.Contains(f.Message, "retired") || !strings.Contains(f.Message, "github.com/vbatts/git-validation") {
		t.Errorf("message did not explain the retirement: %s", f.Message)
	}

	// The prebuilt golangci-lint is retired the same way: an isolated module
	// builds it now, and a manifest still pinning the binary fails.
	files["aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: golangci/golangci-lint@v2.0.0\n")

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() || !strings.Contains(f.Message, "golangci/golangci-lint") {
		t.Fatalf("a manifest pinning the prebuilt golangci-lint should fail naming it, got: %+v", f)
	}
}

// TestFixRemovesRetiredPackages: fix strips a retired pin (entry line and its
// continuation) and leaves every other package, then the manifest passes.
func TestFixRemovesRetiredPackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: golang.org/x/vuln/cmd/govulncheck@v1.7.0\n    registry: local\n")
	dir := writeRepo(t, files)

	outcome := outcomeFor(rules.Fix(context.Background(), dir, bootstrapOpts()), "aqua")
	if !resolved(outcome.Action) {
		t.Fatalf("aqua fix unresolved: %s (%s)", outcome.Action, outcome.Message)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "aqua.yaml"))

	manifest := string(data)
	if strings.Contains(manifest, "- name: golang.org/x/vuln/cmd/govulncheck") {
		t.Errorf("retired package survived fix:\n%s", manifest)
	}

	if !strings.Contains(manifest, "- name: casey/just@") {
		t.Error("an unrelated canonical package was lost")
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "aqua"); !f.OK() {
		t.Fatalf("manifest does not pass after fix: %s", f.Message)
	}
}

func TestFixGoToolsNoOpWhenComplete(t *testing.T) {
	t.Parallel()

	outcome := outcomeFor(
		rules.Fix(
			context.Background(),
			writeRepo(t, compliantFiles()),
			rules.FixOptions{Policy: rules.DefaultPolicy()},
		),
		"gotools",
	)
	if outcome.Action != rules.ActionNone ||
		outcome.Message != "tools/go.mod declares the Go-built tools as tool directives" {
		t.Fatalf("got %s (%s), want a no-op", outcome.Action, outcome.Message)
	}
}

// TestFixGoToolsSeedsWithoutGoMod: a repository without a root go.mod gets a
// bare `tools` module at the aqua-pinned go, the everywhere set added through
// `go get -tool` (the fake go), and the rule then passes.
func TestFixGoToolsSeedsWithoutGoMod(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "tools/go.mod")
	dir := writeRepo(t, files)

	outcome := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	)
	if outcome.Action != rules.ActionMerged || !strings.Contains(outcome.Message, "created "+"tools/go.mod") {
		t.Fatalf("got %s (%s), want merged with the module created", outcome.Action, outcome.Message)
	}

	toolsMod, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
	if err != nil {
		t.Fatalf("tools/go.mod not created: %v", err)
	}

	goDirective := "go " + canonicalGoVersion(t)

	if !strings.Contains(string(toolsMod), "\nmodule "+"tools"+"\n") ||
		!strings.Contains(string(toolsMod), "\n"+goDirective+"\n") {
		t.Errorf(
			"tools/go.mod lacks the bare module path or the aqua-pinned go directive (%s):\n%s",
			goDirective,
			toolsMod,
		)
	}

	for _, pkg := range sourceAnalyzers {
		if strings.Contains(string(toolsMod), pkg) {
			t.Errorf("fix added the analyzer %s to a repository without go.mod", pkg)
		}
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}
}

// TestFixGoToolsAddsDirectives: in a Go module, fix adds every missing
// directive through `go get -tool` (the fake go) and the rule then passes.
func TestFixGoToolsAddsDirectives(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["go.mod"] = goModBare
	delete(files, "tools/go.mod")
	dir := writeRepo(t, files)

	outcome := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	)
	if outcome.Action != rules.ActionMerged {
		t.Fatalf("got %s (%s), want merged", outcome.Action, outcome.Message)
	}

	toolsMod, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
	if err != nil {
		t.Fatalf("tools/go.mod not created: %v", err)
	}

	if !strings.Contains(string(toolsMod), "module example.com/proj/tools") ||
		!strings.Contains(string(toolsMod), "go 1.26") {
		t.Errorf("tools/go.mod lacks the derived module path or go directive:\n%s", toolsMod)
	}

	// The isolated module is seeded too, with its own derived path and the
	// one directive the fake go appended.
	isolated, err := os.ReadFile(filepath.Join(dir, "tools", "golangci-lint", "go.mod"))
	if err != nil {
		t.Fatalf("tools/golangci-lint/go.mod not created: %v", err)
	}

	if !strings.Contains(string(isolated), "module example.com/proj/tools/golangci-lint") ||
		!strings.Contains(string(isolated), "tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint") {
		t.Errorf("tools/golangci-lint/go.mod lacks its module path or directive:\n%s", isolated)
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}

	if again := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	); again.Action != rules.ActionNone {
		t.Fatalf("second fix not a no-op: %s (%s)", again.Action, again.Message)
	}
}

// TestFixGoToolsMovesDirectivesOutOfRoot: directives in the project's go.mod
// are stripped (go mod tidy is the fake go's no-op) and tools/go.mod ends
// complete.
func TestFixGoToolsMovesDirectivesOutOfRoot(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["go.mod"] = goModWithTools
	dir := writeRepo(t, files)

	outcome := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	)
	if outcome.Action != rules.ActionMerged || !strings.Contains(outcome.Message, "moved tool directive(s)") {
		t.Fatalf("got %s (%s), want merged with the move reported", outcome.Action, outcome.Message)
	}

	rootMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	for _, pkg := range slices.Concat(toolsEverywhere, sourceAnalyzers, []string{"example.com/other/cmd/thing"}) {
		if strings.Contains(string(rootMod), pkg) {
			t.Errorf("go.mod still carries the tool directive for %s:\n%s", pkg, rootMod)
		}
	}

	for _, keep := range []string{"module example.com/proj", "go 1.26", "require golang.org/x/tools v0.49.0 // indirect"} {
		if !strings.Contains(string(rootMod), keep) {
			t.Errorf("stripping lost %q:\n%s", keep, rootMod)
		}
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}
}

// TestFixGoToolsAdvisoryWithoutGo: no usable go → advisory carrying the
// manual command, go.mod untouched.
func TestFixGoToolsAdvisoryWithoutGo(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	t.Setenv("PATH", t.TempDir()) // nothing on it: no go

	files := compliantFiles()
	files["go.mod"] = goModBare
	dir := writeRepo(t, files)

	outcome := outcomeFor(
		rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}),
		"gotools",
	)
	if outcome.Action != rules.ActionAdvisory {
		t.Fatalf("got %s (%s), want advisory", outcome.Action, outcome.Message)
	}

	if !strings.Contains(outcome.Message, "go -C tools get -tool") {
		t.Errorf("advisory lacks the manual command: %s", outcome.Message)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(data) != goModBare {
		t.Errorf("go.mod was modified despite the failure:\n%s", data)
	}
}

// TestAquaGoDirective: the go directive of a seeded tools/go.mod comes from
// the manifest's golang/go pin — the canonical one, or the project's own,
// quoted and commented as it likes.
func TestAquaGoDirective(t *testing.T) {
	t.Parallel()

	goLine, _ := canonicalPin(t, "golang/go")

	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "canonical", manifest: limen.CanonicalAquaYAML, want: "go " + canonicalGoVersion(t)},
		{
			name:     "quoted, the project's own version",
			manifest: strings.Replace(limen.CanonicalAquaYAML, goLine, "  - name: 'golang/go@go1.25.3' # pinned\n", 1),
			want:     "go 1.25.3",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := compliantFiles()
			files["aqua.yaml"] = testCase.manifest
			delete(files, "tools/go.mod")
			dir := writeRepo(t, files)

			rules.Fix(context.Background(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()})

			toolsMod, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
			if err != nil {
				t.Fatalf("tools/go.mod not created: %v", err)
			}

			if !strings.Contains(string(toolsMod), "\n"+testCase.want+"\n") {
				t.Errorf("tools/go.mod lacks %q:\n%s", testCase.want, toolsMod)
			}
		})
	}
}

// canonicalGoVersion is the golang/go version the canonical aqua.yaml pins,
// read from the manifest itself so a Renovate bump never breaks the test.
func canonicalGoVersion(t *testing.T) string {
	t.Helper()

	_, version := canonicalPin(t, "golang/go")

	return strings.TrimPrefix(version, "go")
}

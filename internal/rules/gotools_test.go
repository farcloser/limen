package rules_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

// The fake go (see TestMain and installGoStub): `go get -tool a@v1
// b@v2` appends the directives, each with its version in a comment, to the go.mod in the working directory,
// `go get -tool a@none` strips them, `go mod tidy` and anything else succeed
// silently.

const goModBare = "module example.com/proj\n\ngo 1.26\n"

// The tool packages the rule requires: everywhere, and the analyzers on top
// of a Go module. Literal here on purpose — the names are what the findings
// carry and what a repository declares, the contract as seen from outside.
var (
	toolsEverywhere = []string{ //nolint:gochecknoglobals // immutable fixture data.
		"github.com/vbatts/git-validation",
		"github.com/forkcloser/godolint/cmd/godolint",
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
	github.com/forkcloser/godolint/cmd/godolint
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
	github.com/forkcloser/godolint/cmd/godolint
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

// goModNilaway is the other isolated module a Go repository carries.
const goModNilaway = `module example.com/proj/tools/nilaway

go 1.26

tool go.uber.org/nilaway/cmd/nilaway
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

		// The version rides along as a comment, which the directive parser
		// ignores, so a test can tell what fix seeded.
		lines = append(lines, "tool "+pkg+" // @"+version)
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
				"tool   github.com/forkcloser/godolint/cmd/godolint\n" +
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

	// NilAway is the other isolated module; the check names each in turn.
	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")
	if f.OK() || f.Path != "tools/nilaway/go.mod" || !strings.Contains(f.Message, "go.uber.org/nilaway/cmd/nilaway") {
		t.Fatalf("a Go module without tools/nilaway/go.mod should fail naming it, got: %+v", f)
	}

	files["tools/nilaway/go.mod"] = goModNilaway
	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "gotools")

	if !f.OK() {
		t.Fatalf("a tools/go.mod declaring every tool, with the isolated modules, should pass: %s", f.Message)
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
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
	files[".aqua/aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
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
	files[".aqua/aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: golangci/golangci-lint@v2.0.0\n")

	f = findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "aqua")
	if f.OK() || !strings.Contains(f.Message, "golangci/golangci-lint") {
		t.Fatalf("a manifest pinning the prebuilt golangci-lint should fail naming it, got: %+v", f)
	}
}

// TestFixLeavesRetiredPackages: a retired pin is the project's entry, so fix
// leaves the manifest untouched and ends advisory, naming the entry to delete.
func TestFixLeavesRetiredPackages(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".aqua/aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: golang.org/x/vuln/cmd/govulncheck@v1.7.0\n    registry: local\n")
	dir := writeRepo(t, files)

	var advisory *rules.Outcome

	for _, o := range rules.Fix(t.Context(), dir, bootstrapOpts()) {
		if o.Rule == "aqua" && o.Action == rules.ActionAdvisory {
			advisory = &o
		}
	}

	if advisory == nil || !strings.Contains(advisory.Message, "golang.org/x/vuln/cmd/govulncheck") ||
		!strings.Contains(advisory.Message, "by hand") {
		t.Fatalf("fix should end advisory, naming the retired entry to delete by hand, got: %+v", advisory)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".aqua", "aqua.yaml"))
	if string(data) != files[".aqua/aqua.yaml"] {
		t.Errorf("fix edited the manifest:\n%s", data)
	}
}

func TestFixGoToolsNoOpWhenComplete(t *testing.T) {
	t.Parallel()

	outcome := outcomeFor(
		rules.Fix(
			t.Context(),
			writeRepo(t, compliantFiles()),
			rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()},
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
		"gotools",
	); again.Action != rules.ActionNone {
		t.Fatalf("second fix not a no-op: %s (%s)", again.Action, again.Message)
	}
}

// limenRequire is the version limen's own go.mod at path (relative to the
// repository root) requires for module.
func limenRequire(t *testing.T, path, module string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("reading limen's %s: %v", path, err)
	}

	m := regexp.MustCompile(`(?m)^[ \t]*(?:require[ \t]+)?` + regexp.QuoteMeta(module) + `[ \t]+(v\S+)`).
		FindSubmatch(data)
	if m == nil {
		t.Fatalf("limen's %s requires no %s", path, module)
	}

	return string(m[1])
}

// TestFixGoToolsSeedsAtLimenPins: a missing directive is seeded at the
// version limen's own tools modules require (the module that provides the
// package, the isolated tool from its own module), never at @latest.
func TestFixGoToolsSeedsAtLimenPins(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["go.mod"] = goModBare
	delete(files, "tools/go.mod")
	delete(files, "tools/golangci-lint/go.mod")
	dir := writeRepo(t, files)

	outcome := outcomeFor(rules.Fix(t.Context(), dir, bootstrapOpts()), "gotools")
	if outcome.Action != rules.ActionMerged {
		t.Fatalf("got %s (%s), want merged", outcome.Action, outcome.Message)
	}

	toolsMod, _ := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))

	for pkg, module := range map[string]string{
		"github.com/vbatts/git-validation":  "github.com/vbatts/git-validation",
		"golang.org/x/tools/cmd/deadcode":   "golang.org/x/tools",
		"golang.org/x/vuln/cmd/govulncheck": "golang.org/x/vuln",
		"github.com/google/go-licenses/v2":  "github.com/google/go-licenses/v2",
	} {
		want := "tool " + pkg + " // @" + limenRequire(t, "tools/go.mod", module)
		if !strings.Contains(string(toolsMod), want) {
			t.Errorf("tools/go.mod lacks %q:\n%s", want, toolsMod)
		}
	}

	if strings.Contains(string(toolsMod), "@latest") {
		t.Errorf("a directive was seeded at @latest:\n%s", toolsMod)
	}

	isolated, _ := os.ReadFile(filepath.Join(dir, "tools", "golangci-lint", "go.mod"))

	want := "tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint // @" +
		limenRequire(t, "tools/golangci-lint/go.mod", "github.com/golangci/golangci-lint/v2")
	if !strings.Contains(string(isolated), want) {
		t.Errorf("tools/golangci-lint/go.mod lacks %q:\n%s", want, isolated)
	}
}

// TestFixGoToolsDevBuildRefusesToSeed: without pins — a development build,
// whose tools go.mod files were never embedded — fix ends as an advisory
// naming the reason instead of guessing a version.
func TestFixGoToolsDevBuildRefusesToSeed(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "tools/go.mod")
	dir := writeRepo(t, files)

	outcome := outcomeFor(rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}), "gotools")
	if outcome.Action != rules.ActionAdvisory || !strings.Contains(outcome.Message, "development build") {
		t.Fatalf("got %s (%s), want the development-build advisory", outcome.Action, outcome.Message)
	}

	if toolsMod, _ := os.ReadFile(
		filepath.Join(dir, "tools", "go.mod"),
	); strings.Contains(
		string(toolsMod),
		"\ntool ",
	) {
		t.Errorf("a directive was seeded without pins:\n%s", toolsMod)
	}
}

// TestFixGoToolsSeedsIsolatedBesideCompleteTools: a Go module whose
// tools/go.mod already declares every tool, but which carries neither
// isolated module — every repository on a limen from before the isolated
// modules — gets both seeded, and the rule then passes.
func TestFixGoToolsSeedsIsolatedBesideCompleteTools(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["go.mod"] = goModBare
	files["tools/go.mod"] = goModWithTools
	dir := writeRepo(t, files)

	outcome := outcomeFor(
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
		"gotools",
	)
	if outcome.Action != rules.ActionMerged {
		t.Fatalf("got %s (%s), want merged", outcome.Action, outcome.Message)
	}

	for _, module := range []string{"golangci-lint", "nilaway"} {
		if _, err := os.Stat(filepath.Join(dir, "tools", module, "go.mod")); err != nil {
			t.Errorf("tools/%s/go.mod not created: %v", module, err)
		}
	}

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "gotools"); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
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
		rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()}),
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
// the repository's own golang/go pin.
func TestAquaGoDirective(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, "tools/go.mod")
	dir := writeRepo(t, files)

	rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy(), ToolPins: limenToolPins()})

	toolsMod, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
	if err != nil {
		t.Fatalf("tools/go.mod not created: %v", err)
	}

	if want := "go " + canonicalGoVersion(t); !strings.Contains(string(toolsMod), "\n"+want+"\n") {
		t.Errorf("tools/go.mod lacks %q:\n%s", want, toolsMod)
	}
}

// canonicalGoVersion is the golang/go version limen's own aqua.yaml pins (the
// seed), read from the manifest itself so a Renovate bump never breaks the test.
func canonicalGoVersion(t *testing.T) string {
	t.Helper()

	_, version := canonicalPin(t, "golang/go")

	return strings.TrimPrefix(version, "go")
}

package rules //nolint:testpackage // white-box: exercises the goBin seam and unexported helpers

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The fake go (see TestMain and installGoStub): `go get -tool a@latest
// b@latest` appends the directives to the go.mod in the working directory,
// `go mod tidy` and anything else succeed silently.

const goModBare = "module example.com/proj\n\ngo 1.26\n"

// goModToolsEverywhere is a tools module of a repository without a root
// go.mod: the everywhere set, nothing more — what the compliant fixture
// carries.
const goModToolsEverywhere = `module tools

go 1.26

tool (
	github.com/vbatts/git-validation
	github.com/farcloser/godolint/cmd/godolint
	github.com/goccy/go-graphviz/cmd/dot
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
	github.com/goccy/go-graphviz/cmd/dot
	example.com/other/cmd/thing
)

require golang.org/x/tools v0.49.0 // indirect
`

func runGoStub() int {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "get" || args[1] != "-tool" {
		return 0 // `go mod tidy` and anything else: succeed silently
	}

	var lines []string

	for _, arg := range args[2:] {
		pkg, _, _ := strings.Cut(arg, "@")
		lines = append(lines, "tool "+pkg)
	}

	file, err := os.OpenFile(goModFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}

	defer func() { _ = file.Close() }()

	if _, err := file.WriteString("\n" + strings.Join(lines, "\n") + "\n"); err != nil {
		return 1
	}

	return 0
}

// installGoStub points goBin at the test binary under the name `go`, which
// TestMain recognizes: a symlink where the platform allows one, a copy
// elsewhere (Windows). Helper-process pattern rather than a generated
// script, like the aqua stub. The returned func removes the directory.
func installGoStub() (func(), error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "limen-go-stub")
	if err != nil {
		return nil, err
	}

	// The name is the dispatch key (TestMain): exactly `go`, plus the
	// extension Windows needs to execute it. Not the test binary's own
	// extension — `rules.test` would yield `go.test`, which TestMain does not
	// recognize, and every child would then run the suite: a fork bomb.
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	stub := filepath.Join(dir, "go"+ext)
	if err := os.Symlink(self, stub); err != nil {
		data, readErr := os.ReadFile(self)
		if readErr != nil {
			return nil, readErr
		}

		// 0o700, not 0o600: the stub must be executable.
		if err := os.WriteFile(stub, data, 0o700); err != nil {
			return nil, err
		}
	}

	goBin = stub

	return func() { _ = os.RemoveAll(dir) }, nil
}

func TestGoModToolDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		gomod string
		want  []string
	}{
		{name: "none", gomod: goModBare, want: nil},
		{
			name:  "block form with comments and an unrelated tool",
			gomod: goModWithTools,
			want: []string{
				"github.com/google/go-licenses/v2", "golang.org/x/tools/cmd/deadcode",
				"golang.org/x/vuln/cmd/govulncheck", "github.com/vbatts/git-validation",
				"github.com/farcloser/godolint/cmd/godolint", "github.com/goccy/go-graphviz/cmd/dot",
				"example.com/other/cmd/thing",
			},
		},
		{
			name:  "one-line form",
			gomod: "module m\n\ngo 1.26\n\ntool golang.org/x/tools/cmd/deadcode // trailing\ntool   golang.org/x/vuln/cmd/govulncheck\n",
			want:  []string{"golang.org/x/tools/cmd/deadcode", "golang.org/x/vuln/cmd/govulncheck"},
		},
		{
			name:  "require block is not a tool block",
			gomod: "module m\n\ngo 1.26\n\nrequire (\n\tgolang.org/x/tools v0.49.0\n)\n",
			want:  nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := goModToolDirectives(testCase.gomod)
			if len(got) != len(testCase.want) {
				t.Fatalf("got %v, want %v", got, testCase.want)
			}

			for _, pkg := range testCase.want {
				if !got[pkg] {
					t.Errorf("missing %s in %v", pkg, got)
				}
			}
		})
	}
}

// TestGoToolsEverywhere: a repository without go.mod must still declare the
// everywhere set — and only that: the analyzers are not asked of it.
func TestGoToolsEverywhere(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools); !f.OK() {
		t.Fatalf("the compliant fixture should pass: %s", f.Message)
	}

	delete(files, goToolsModFile)

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools)
	if f.OK() {
		t.Fatal("a repository without tools/go.mod should fail")
	}

	for _, pkg := range goToolsEverywhere {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	for _, pkg := range goSourceAnalyzers {
		if strings.Contains(f.Message, pkg) {
			t.Errorf("message asks a non-Go repository for the analyzer %s: %s", pkg, f.Message)
		}
	}
}

func TestGoToolsRequiresDirectives(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[goModFile] = goModBare

	// The everywhere set alone is not enough once the root carries a go.mod.
	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools)
	if f.OK() {
		t.Fatal("a Go module whose tools/go.mod lacks the analyzers should fail")
	}

	for _, pkg := range goSourceAnalyzers {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	delete(files, goToolsModFile)

	f = findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools)
	if f.OK() {
		t.Fatal("a Go module without tools/go.mod should fail")
	}

	for _, pkg := range requiredGoTools([]byte(goModBare)) {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	files[goToolsModFile] = goModWithTools
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools); !f.OK() {
		t.Fatalf("a tools/go.mod declaring every tool should pass: %s", f.Message)
	}

	// The directives in the project's own go.mod are the pollution the rule
	// exists to stop, even when tools/go.mod is complete.
	files[goModFile] = goModWithTools

	f = findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools)
	if f.OK() || !strings.Contains(f.Message, "belong in "+goToolsModFile) {
		t.Fatalf("tool directives in go.mod should fail naming tools/go.mod: ok=%v %s", f.OK(), f.Message)
	}
}

func TestStripGoModToolDirectives(t *testing.T) {
	t.Parallel()

	stripped := stripGoModToolDirectives(goModWithTools)
	if len(goModToolDirectives(stripped)) != 0 {
		t.Fatalf("directives survived stripping:\n%s", stripped)
	}

	for _, keep := range []string{"module example.com/proj", "go 1.26", "require golang.org/x/tools v0.49.0 // indirect"} {
		if !strings.Contains(stripped, keep) {
			t.Errorf("stripping lost %q:\n%s", keep, stripped)
		}
	}
}

// The two halves of one doctrine: what tools/go.mod must declare is exactly
// what aqua.yaml must no longer pin, and the canonical manifest pins none of
// it.
func TestGoToolsMatchRetiredAquaPackages(t *testing.T) {
	t.Parallel()

	want := requiredGoTools([]byte(goModBare))
	got := slices.Clone(retiredCanonicalPkgs)

	slices.Sort(want)
	slices.Sort(got)

	if !slices.Equal(want, got) {
		t.Fatalf("required tools %v and retiredCanonicalPkgs %v must be the same set", want, got)
	}

	for _, p := range canonicalAqua.pkgs {
		if slices.Contains(retiredCanonicalPkgs, p.name) {
			t.Errorf("canonical aqua.yaml still pins retired package %s", p.name)
		}
	}
}

func TestAquaRejectsRetiredPackage(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files["aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: github.com/vbatts/git-validation@v1.2.2\n    registry: local\n")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a retired canonical package should fail")
	}

	if !strings.Contains(f.Message, "retired") || !strings.Contains(f.Message, "github.com/vbatts/git-validation") {
		t.Errorf("message did not explain the retirement: %s", f.Message)
	}
}

// TestFixRemovesRetiredPackages: fix strips a retired pin (entry line and its
// continuation) and leaves every other package, then the manifest passes.
//
//nolint:paralleltest // serial by design: mutates the package-level aquaBin.
func TestFixRemovesRetiredPackages(t *testing.T) {
	stubAqua(t)

	files := compliantFiles()
	files["aqua.yaml"] = canonicalAquaWith(t, "packages:\n",
		"packages:\n  - name: golang.org/x/vuln/cmd/govulncheck@v1.7.0\n    registry: local\n")
	dir := writeRepo(t, files)

	outcome := outcomeFor(Fix(dir, bootstrapOpts()), "aqua")
	if !outcome.Action.resolved() {
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

	if f := findingByRule(Check(dir, DefaultPolicy()), "aqua"); !f.OK() {
		t.Fatalf("manifest does not pass after fix: %s", f.Message)
	}
}

func TestFixGoToolsNoOpWhenComplete(t *testing.T) {
	t.Parallel()

	outcome := remediateGoTools(writeRepo(t, compliantFiles()))
	if outcome.Action != ActionNone || outcome.Message != goToolsPassMessage {
		t.Fatalf("got %s (%s), want a no-op", outcome.Action, outcome.Message)
	}
}

// TestFixGoToolsSeedsWithoutGoMod: a repository without a root go.mod gets a
// bare `tools` module at the aqua-pinned go, the everywhere set added through
// `go get -tool` (the fake go), and the rule then passes.
func TestFixGoToolsSeedsWithoutGoMod(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	delete(files, goToolsModFile)
	dir := writeRepo(t, files)

	outcome := remediateGoTools(dir)
	if outcome.Action != ActionMerged || !strings.Contains(outcome.Message, "created "+goToolsModFile) {
		t.Fatalf("got %s (%s), want merged with the module created", outcome.Action, outcome.Message)
	}

	toolsMod, err := os.ReadFile(filepath.Join(dir, goToolsModFile))
	if err != nil {
		t.Fatalf("tools/go.mod not created: %v", err)
	}

	goDirective := aquaGoDirective(dir)
	if goDirective == fallbackGoDirective {
		t.Fatal("the canonical aqua.yaml carries no golang/go pin to derive the go directive from")
	}

	if !strings.Contains(string(toolsMod), "\nmodule "+bareToolsModule+"\n") ||
		!strings.Contains(string(toolsMod), "\n"+goDirective+"\n") {
		t.Errorf(
			"tools/go.mod lacks the bare module path or the aqua-pinned go directive (%s):\n%s",
			goDirective,
			toolsMod,
		)
	}

	declared := goModToolDirectives(string(toolsMod))
	for _, pkg := range goSourceAnalyzers {
		if declared[pkg] {
			t.Errorf("fix added the analyzer %s to a repository without go.mod", pkg)
		}
	}

	if f := checkGoTools(dir); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}
}

// TestFixGoToolsAddsDirectives: in a Go module, fix adds every missing
// directive through `go get -tool` (the fake go) and the rule then passes.
func TestFixGoToolsAddsDirectives(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[goModFile] = goModBare
	delete(files, goToolsModFile)
	dir := writeRepo(t, files)

	outcome := remediateGoTools(dir)
	if outcome.Action != ActionMerged {
		t.Fatalf("got %s (%s), want merged", outcome.Action, outcome.Message)
	}

	toolsMod, err := os.ReadFile(filepath.Join(dir, goToolsModFile))
	if err != nil {
		t.Fatalf("tools/go.mod not created: %v", err)
	}

	if !strings.Contains(string(toolsMod), "module example.com/proj/tools") ||
		!strings.Contains(string(toolsMod), "go 1.26") {
		t.Errorf("tools/go.mod lacks the derived module path or go directive:\n%s", toolsMod)
	}

	if f := checkGoTools(dir); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}

	if again := remediateGoTools(dir); again.Action != ActionNone {
		t.Fatalf("second fix not a no-op: %s (%s)", again.Action, again.Message)
	}
}

// TestFixGoToolsMovesDirectivesOutOfRoot: directives in the project's go.mod
// are stripped (go mod tidy is the fake go's no-op) and tools/go.mod ends
// complete.
func TestFixGoToolsMovesDirectivesOutOfRoot(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[goModFile] = goModWithTools
	dir := writeRepo(t, files)

	outcome := remediateGoTools(dir)
	if outcome.Action != ActionMerged || !strings.Contains(outcome.Message, "moved tool directive(s)") {
		t.Fatalf("got %s (%s), want merged with the move reported", outcome.Action, outcome.Message)
	}

	rootMod, _ := os.ReadFile(filepath.Join(dir, goModFile))
	if len(goModToolDirectives(string(rootMod))) != 0 {
		t.Errorf("go.mod still carries tool directives:\n%s", rootMod)
	}

	if f := checkGoTools(dir); !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}
}

// TestFixGoToolsAdvisoryWithoutGo: no usable go → advisory carrying the
// manual command, go.mod untouched.
//
//nolint:paralleltest // serial by design: mutates the package-level goBin.
func TestFixGoToolsAdvisoryWithoutGo(t *testing.T) {
	previous := goBin
	goBin = filepath.Join(t.TempDir(), "no-such-go")

	t.Cleanup(func() { goBin = previous })

	files := compliantFiles()
	files[goModFile] = goModBare
	dir := writeRepo(t, files)

	outcome := remediateGoTools(dir)
	if outcome.Action != ActionAdvisory {
		t.Fatalf("got %s (%s), want advisory", outcome.Action, outcome.Message)
	}

	if !strings.Contains(outcome.Message, "go -C tools get -tool") {
		t.Errorf("advisory lacks the manual command: %s", outcome.Message)
	}

	data, _ := os.ReadFile(filepath.Join(dir, goModFile))
	if string(data) != goModBare {
		t.Errorf("go.mod was modified despite the failure:\n%s", data)
	}
}

func TestAquaGoDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "canonical", manifest: compliantFiles()["aqua.yaml"], want: "go " + canonicalGoVersion(t)},
		{name: "quoted", manifest: "packages:\n  - name: 'golang/go@go1.25.3' # pinned\n", want: "go 1.25.3"},
		{name: "no pin", manifest: "packages:\n  - name: casey/just@1.0.0\n", want: fallbackGoDirective},
		{name: "no manifest", manifest: "", want: fallbackGoDirective},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			if testCase.manifest != "" {
				files["aqua.yaml"] = testCase.manifest
			}

			if got := aquaGoDirective(writeRepo(t, files)); got != testCase.want {
				t.Errorf("got %q, want %q", got, testCase.want)
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

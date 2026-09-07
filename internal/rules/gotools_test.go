package rules //nolint:testpackage // white-box: exercises the goBin seam and unexported helpers

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// goStubEnv flips the re-executed test binary into fake-go mode (see
// TestMain): `go get -tool a@latest b@latest` appends the directives to the
// go.mod in the working directory, `go mod tidy` is a silent no-op.
const goStubEnv = "LIMEN_TEST_GO_STUB"

const goModBare = "module example.com/proj\n\ngo 1.26\n"

// goModWithTools declares every required analyzer, in the block form Go
// writes, plus an unrelated tool and a comment — the parser must see through
// both.
const goModWithTools = `module example.com/proj

go 1.26

tool (
	github.com/google/go-licenses/v2 // the license scanner
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
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

// stubGo points remediation at the test binary in fake-go mode. Serial by
// design (mutates goBin, sets env), like stubAqua.
func stubGo(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}

	t.Setenv(goStubEnv, "1")

	previous := goBin
	goBin = self

	t.Cleanup(func() { goBin = previous })
}

func hasFinding(findings []Finding, rule string) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool { return f.Rule == rule })
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
				"golang.org/x/vuln/cmd/govulncheck", "example.com/other/cmd/thing",
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

func TestGoToolsSilentWithoutGoMod(t *testing.T) {
	t.Parallel()

	findings := Check(writeRepo(t, compliantFiles()), DefaultPolicy())
	if hasFinding(findings, ruleGoTools) {
		t.Fatal("the gotools rule must not report on a repository without go.mod")
	}
}

func TestGoToolsRequiresDirectives(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[goModFile] = goModBare

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools)
	if f.OK() {
		t.Fatal("a Go module without tools/go.mod should fail")
	}

	for _, pkg := range goModTools {
		if !strings.Contains(f.Message, pkg) {
			t.Errorf("message did not name %s: %s", pkg, f.Message)
		}
	}

	files[goToolsModFile] = goModBare
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools); f.OK() {
		t.Fatal("a tools/go.mod without the analyzer tool directives should fail")
	}

	files[goToolsModFile] = goModWithTools
	if f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), ruleGoTools); !f.OK() {
		t.Fatalf("a tools/go.mod declaring every analyzer should pass: %s", f.Message)
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

// The two halves of one doctrine: what go.mod must declare is exactly what
// aqua.yaml must no longer pin, and the canonical manifest pins none of it.
func TestGoToolsMatchRetiredAquaPackages(t *testing.T) {
	t.Parallel()

	want := slices.Clone(goModTools)
	got := slices.Clone(retiredCanonicalPkgs)

	slices.Sort(want)
	slices.Sort(got)

	if !slices.Equal(want, got) {
		t.Fatalf("goModTools %v and retiredCanonicalPkgs %v must be the same set", goModTools, retiredCanonicalPkgs)
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
		"packages:\n  - name: golang.org/x/tools/cmd/deadcode@v0.47.0\n    registry: local\n")

	f := findingByRule(Check(writeRepo(t, files), DefaultPolicy()), "aqua")
	if f.OK() {
		t.Fatal("a retired canonical package should fail")
	}

	if !strings.Contains(f.Message, "retired") || !strings.Contains(f.Message, "golang.org/x/tools/cmd/deadcode") {
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

func TestFixGoToolsNotApplicableWithoutGoMod(t *testing.T) {
	t.Parallel()

	outcome := remediateGoTools(writeRepo(t, compliantFiles()))
	if outcome.Action != ActionNone || !strings.Contains(outcome.Message, "not applicable") {
		t.Fatalf("got %s (%s), want a not-applicable no-op", outcome.Action, outcome.Message)
	}
}

// TestFixGoToolsAddsDirectives: fix adds the missing directives through
// `go get -tool` (stubbed) and the rule then passes.
//
//nolint:paralleltest // serial by design: mutates the package-level goBin.
func TestFixGoToolsAddsDirectives(t *testing.T) {
	stubGo(t)

	files := compliantFiles()
	files[goModFile] = goModBare
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

	if f, ok := checkGoTools(dir); !ok || !f.OK() {
		t.Fatalf("rule does not pass after fix: %s", f.Message)
	}

	if again := remediateGoTools(dir); again.Action != ActionNone {
		t.Fatalf("second fix not a no-op: %s (%s)", again.Action, again.Message)
	}
}

// TestFixGoToolsMovesDirectivesOutOfRoot: directives in the project's go.mod
// are stripped (go mod tidy stubbed) and tools/go.mod ends complete.
//
//nolint:paralleltest // serial by design: mutates the package-level goBin.
func TestFixGoToolsMovesDirectivesOutOfRoot(t *testing.T) {
	stubGo(t)

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

	if f, ok := checkGoTools(dir); !ok || !f.OK() {
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

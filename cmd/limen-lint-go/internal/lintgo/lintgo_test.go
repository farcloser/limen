package lintgo_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/cmd/limen-lint-go/internal/lintgo"
)

// A small baseline with one of everything the merge rules act on.
const baselineSmall = `lint-go:
  golangci-lint: v2.13.0
licenses:
  allowed: [MIT]
golangci:
  version: "2"
  linters:
    default: all
    enable: [govet, revive]
    disable: [dupl]
    exclusions:
      rules:
        - path: _test.go
          linters: [varnamelen]
    settings:
      forbidigo:
        forbid:
          - pattern: '^fmt\.Print'
            msg: no
      revive:
        enable-all-rules: true
        rules:
          - name: cyclomatic
            arguments: [30]
          - name: line-length-limit
            disabled: true
      depguard:
        rules:
          main:
            allow: [$gostd, "${MODULE_PATH}"]
  formatters:
    enable: [gci]
    settings:
      gci:
        sections: [standard, "prefix(${MODULE_PREFIX})"]
`

const (
	moduleAcme  = "github.com/acme/thing/sub"
	renderCmd   = "render"
	flagsCmd    = "flags"
	checkCmd    = "check"
	licenseLane = "licenses"
)

// writeModule is a module directory for moduleAcme: its go.mod, and its
// .lint-go.yaml when overlay is not empty.
func writeModule(t *testing.T, overlay string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module "+moduleAcme+"\n\ngo 1.26\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if overlay != "" {
		if err := os.WriteFile(filepath.Join(dir, lintgo.OverlayFile), []byte(overlay), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// writeBaseline places baseline as dir's BaselineFile, where the driver
// finds it first.
func writeBaseline(t *testing.T, dir, baseline string) {
	t.Helper()

	path := filepath.Join(dir, lintgo.BaselineFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run is one limen-lint-go invocation in dir, with baseline placed there.
func run(t *testing.T, dir, baseline string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	writeBaseline(t, dir, baseline)

	var out, errs bytes.Buffer

	code = lintgo.Run(append([]string{"-C", dir}, args...), &out, &errs)

	return out.String(), errs.String(), code
}

// TestBaselineAboveTheModule: a nested module has no .limen/ of its own and
// reads its repository's, the nearest above it; a tree without one fails
// naming the file.
func TestBaselineAboveTheModule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBaseline(t, root, baselineSmall)

	nested := filepath.Join(root, "cmd", "tool")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	var out, errs bytes.Buffer
	if code := lintgo.Run([]string{"-C", nested, checkCmd, "v2.13.0"}, &out, &errs); code != 0 {
		t.Errorf("the repository's baseline should be found from a nested module: %d %s", code, errs.String())
	}

	errs.Reset()

	if code := lintgo.Run([]string{"-C", t.TempDir(), checkCmd, "v2.13.0"}, &out, &errs); code != 1 ||
		!strings.Contains(errs.String(), lintgo.BaselineFile) {
		t.Errorf("no baseline anywhere should fail naming it: %d %s", code, errs.String())
	}
}

// rendered is the configuration limen-lint-go renders for baseline with overlay, or
// the failure's stderr when it refuses.
func rendered(t *testing.T, baseline, overlay string) (config, failure string) {
	t.Helper()

	stdout, stderr, code := run(t, writeModule(t, overlay), baseline, renderCmd)
	if code != 0 {
		return "", stderr
	}

	return stdout, ""
}

func TestRenderFillsPlaceholders(t *testing.T) {
	t.Parallel()

	config, failure := rendered(t, baselineSmall, "")
	if failure != "" {
		t.Fatal(failure)
	}

	for _, want := range []string{"- " + moduleAcme, "prefix(github.com/acme)"} {
		if !strings.Contains(config, want) {
			t.Errorf("rendered config lacks %q:\n%s", want, config)
		}
	}

	if strings.Contains(config, "${") {
		t.Errorf("a placeholder survived the render:\n%s", config)
	}

	// The lint-go and licenses sections are the driver's, not golangci's.
	for _, gone := range []string{"lint-go:", "licenses:", "allowed:"} {
		if strings.Contains(config, gone) {
			t.Errorf("rendered config carries %q, which golangci-lint would reject:\n%s", gone, config)
		}
	}
}

func TestRenderReportsTheOverlay(t *testing.T) {
	t.Parallel()

	_, stderr, code := run(t, writeModule(t, ""), baselineSmall, renderCmd)
	if code != 0 || !strings.Contains(stderr, "no "+lintgo.OverlayFile) {
		t.Fatalf("no overlay should render the baseline and say so, got %d: %s", code, stderr)
	}

	_, stderr, code = run(t, writeModule(t, "golangci:\n  linters:\n    disable: [revive, dupl]\n"),
		baselineSmall, renderCmd)
	if code != 0 || !strings.Contains(stderr, "1 carve-out(s)") {
		t.Fatalf("one real carve-out should be counted as one, got %d: %s", code, stderr)
	}

	if !strings.Contains(stderr, "note: golangci.linters.disable already lists dupl") {
		t.Errorf("disabling an already disabled linter should be a note: %s", stderr)
	}
}

func TestRenderToFile(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "build", "golangci.yml")

	stdout, stderr, code := run(t, writeModule(t, ""), baselineSmall, renderCmd, "-o", out)
	if code != 0 {
		t.Fatal(stderr)
	}

	if stdout != "" {
		t.Errorf("-o should leave stdout empty, got %q", stdout)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(data), "# Rendered by limen-lint-go") ||
		!strings.Contains(string(data), "version: \"2\"") {
		t.Errorf("unexpected file:\n%s", data)
	}
}

// TestMergeMovesLinters: disable takes a linter out of enable and into
// disable, enable the reverse; formatters.enable adds.
func TestMergeMovesLinters(t *testing.T) {
	t.Parallel()

	config, failure := rendered(t, baselineSmall, `golangci:
  linters:
    disable: [revive]
    enable: [dupl]
  formatters:
    enable: [gofumpt]
`)
	if failure != "" {
		t.Fatal(failure)
	}

	linters := between(config, "linters:", "formatters:")

	enable := between(linters, "enable:", "exclusions:")
	if strings.Contains(enable, "revive") || !strings.Contains(enable, "dupl") || !strings.Contains(enable, "govet") {
		t.Errorf("enable should be govet and dupl, without revive:\n%s", enable)
	}

	disable := between(linters, "disable:", "enable:")
	if strings.Contains(disable, "dupl") || !strings.Contains(disable, "revive") {
		t.Errorf("disable should be revive, without dupl:\n%s", disable)
	}

	_, formatters, _ := strings.Cut(config, "formatters:")
	if !strings.Contains(formatters, "- gofumpt") || !strings.Contains(formatters, "- gci") {
		t.Errorf("formatters.enable should carry gci and gofumpt:\n%s", formatters)
	}
}

func TestMergeAppendsExclusions(t *testing.T) {
	t.Parallel()

	config, failure := rendered(t, baselineSmall, `golangci:
  linters:
    exclusions:
      paths: [third_party/]
      presets: [comments]
      rules:
        - path: testutil/
          linters: [gosec]
`)
	if failure != "" {
		t.Fatal(failure)
	}

	exclusions := between(config, "exclusions:", "settings:")
	for _, want := range []string{"path: _test.go", "path: testutil/", "- third_party/", "- comments"} {
		if !strings.Contains(exclusions, want) {
			t.Errorf("exclusions lack %q:\n%s", want, exclusions)
		}
	}
}

// TestMergeSettings: a scalar overrides, a list appends (a repeat is a note),
// a list of named mappings merges by name.
func TestMergeSettings(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, `golangci:
  linters:
    settings:
      revive:
        enable-all-rules: false
        rules:
          - name: cyclomatic
            arguments: [20]
          - name: add-constant
            disabled: true
      forbidigo:
        forbid:
          - pattern: '^os\.Exit'
            msg: never
      depguard:
        rules:
          main:
            allow: [$gostd, github.com/other/lib]
      wrapcheck:
        ignore-package-globs: [github.com/acme/thing/*]
`)

	stdout, stderr, code := run(t, dir, baselineSmall, renderCmd)
	if code != 0 {
		t.Fatal(stderr)
	}

	settings := between(stdout, "settings:", "formatters:")

	checks := map[string]bool{
		"enable-all-rules: false": true,  // scalar override
		"enable-all-rules: true":  false, // the baseline's value is gone
		"- 20":                    true,  // cyclomatic merged by name
		"- 30":                    false,
		"name: line-length-limit": true, // the baseline's other rule stays
		"name: add-constant":      true, // a new rule appends
		"pattern: ^fmt\\.Print":   true, // forbidigo appends
		"pattern: ^os\\.Exit":     true,
		"- github.com/other/lib":  true, // depguard appends
		"- " + moduleAcme:         true,
		"ignore-package-globs":    true, // a tool the baseline has no settings for
	}
	for want, present := range checks {
		if strings.Contains(settings, want) != present {
			t.Errorf("%q present=%v, want %v:\n%s", want, !present, present, settings)
		}
	}

	if strings.Count(settings, "- $gostd") != 1 {
		t.Errorf("$gostd should be listed once, the repeat noted:\n%s", settings)
	}

	if !strings.Contains(stderr, "already lists $gostd") || !strings.Contains(stderr, "4 carve-out(s)") {
		t.Errorf("expected the repeat noted and four tools counted: %s", stderr)
	}
}

// TestOverlayRejected: everything outside the carve-out vocabulary is refused
// with the key named, never merged.
func TestOverlayRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		overlay string
		want    string
	}{
		{name: "unknown section", overlay: "linters:\n  disable: [x]\n", want: "unknown section \"linters\""},
		{name: "run is policy", overlay: "golangci:\n  run:\n    timeout: 1m\n", want: "golangci.run is policy"},
		{
			name:    "default is policy",
			overlay: "golangci:\n  linters:\n    default: none\n",
			want:    "linters.default is policy",
		},
		{
			name:    "generated is policy",
			overlay: "golangci:\n  linters:\n    exclusions:\n      generated: strict\n",
			want:    "exclusions.generated is policy",
		},
		{name: "licenses key", overlay: "licenses:\n  deny: [GPL]\n", want: "unknown key licenses.deny"},
		{
			name:    "not a list",
			overlay: "golangci:\n  linters:\n    disable: revive\n",
			want:    "linters.disable must be a list",
		},
		{name: "not a mapping", overlay: "golangci: [a]\n", want: "golangci must be a mapping"},
		{
			name:    "scalar over mapping",
			overlay: "golangci:\n  linters:\n    settings:\n      revive: off\n",
			want:    "settings.revive is a mapping or a list in the baseline",
		},
		{name: "not yaml", overlay: "golangci: [\n", want: lintgo.OverlayFile},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config, failure := rendered(t, baselineSmall, testCase.overlay)
			if failure == "" {
				t.Fatalf("rendered instead of refusing:\n%s", config)
			}

			if !strings.Contains(failure, testCase.want) {
				t.Errorf("failure %q does not name %q", failure, testCase.want)
			}
		})
	}
}

func TestFlagsLicenses(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t, writeModule(t, ""), baselineSmall, flagsCmd, licenseLane)
	if code != 0 || stdout != "--allowed_licenses=MIT\n" {
		t.Fatalf("baseline flags: %d %q %s", code, stdout, stderr)
	}

	dir := writeModule(
		t,
		"licenses:\n  allowed: [Apache-2.0, MIT]\n  ignore: [gotest.tools/v3, example.com/x]\n",
	)

	stdout, stderr, code = run(t, dir, baselineSmall, flagsCmd, licenseLane)
	if code != 0 || stdout != "--allowed_licenses=Apache-2.0,MIT --ignore=gotest.tools/v3 --ignore=example.com/x\n" {
		t.Fatalf("overlay flags: %d %q %s", code, stdout, stderr)
	}

	if _, _, code := run(t, dir, baselineSmall, flagsCmd, "vuln"); code != 2 {
		t.Errorf("an unknown lane should be a usage error, got %d", code)
	}
}

// TestNilaway: the flags bound the analysis to the module and carry the
// overlay's exclusions; the mode is the baseline's blocking unless the
// overlay says otherwise; the section's vocabulary is closed.
func TestNilaway(t *testing.T) {
	t.Parallel()

	const baseline = baselineSmall + `nilaway:
  blocking: true
  exclude-pkgs: []
  exclude-errors-in-files: [third_party/]
`

	stdout, stderr, code := run(t, writeModule(t, ""), baseline, flagsCmd, "nilaway")
	if code != 0 ||
		stdout != "-include-pkgs="+moduleAcme+" -pretty-print=false -exclude-errors-in-files=third_party/\n" {
		t.Fatalf("baseline flags: %d %q %s", code, stdout, stderr)
	}

	stdout, _, code = run(t, writeModule(t, ""), baseline, "mode", "nilaway")
	if code != 0 || stdout != "blocking\n" {
		t.Fatalf("baseline mode: %d %q", code, stdout)
	}

	dir := writeModule(
		t,
		"nilaway:\n  blocking: false\n  exclude-pkgs: [github.com/acme/thing/sub/gen]\n  exclude-errors-in-files: [internal/legacy/]\n",
	)

	stdout, stderr, code = run(t, dir, baseline, flagsCmd, "nilaway")
	want := "-include-pkgs=" + moduleAcme + " -pretty-print=false" +
		" -exclude-pkgs=github.com/acme/thing/sub/gen -exclude-errors-in-files=third_party/,internal/legacy/\n"

	if code != 0 || stdout != want {
		t.Fatalf("overlay flags: %d %q %s", code, stdout, stderr)
	}

	stdout, _, code = run(t, dir, baseline, "mode", "nilaway")
	if code != 0 || stdout != "informational\n" {
		t.Fatalf("overlay mode: %d %q", code, stdout)
	}

	_, stderr, code = run(t, dir, baseline, renderCmd, "-o", filepath.Join(t.TempDir(), "out.yml"))
	if code != 0 || !strings.Contains(stderr, "3 carve-out(s)") {
		t.Errorf("the three nilaway entries should count: %d %s", code, stderr)
	}

	for _, overlay := range []string{"nilaway:\n  blocking: maybe\n", "nilaway:\n  strict: true\n"} {
		if config, failure := rendered(t, baseline, overlay); failure == "" {
			t.Errorf("%q rendered instead of refusing:\n%s", overlay, config)
		}
	}

	// Without a nilaway section in the baseline the lane is informational and
	// the flags carry the module alone.
	stdout, _, code = run(t, writeModule(t, ""), baselineSmall, "mode", "nilaway")
	if code != 0 || stdout != "informational\n" {
		t.Errorf("no section: %d %q", code, stdout)
	}
}

// TestDisabledRevive lists the revive rules the rendered configuration turns
// off, the overlay's disables included, sorted.
func TestDisabledRevive(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := run(t, writeModule(t, ""), baselineSmall, "disabled", "revive")
	if code != 0 || stdout != "line-length-limit\n" {
		t.Fatalf("baseline: %d %q %s", code, stdout, stderr)
	}

	dir := writeModule(
		t,
		"golangci:\n  linters:\n    settings:\n      revive:\n        rules:\n          - name: cyclomatic\n            disabled: true\n",
	)

	stdout, _, code = run(t, dir, baselineSmall, "disabled", "revive")
	if code != 0 || stdout != "cyclomatic\nline-length-limit\n" {
		t.Fatalf("overlay: %d %q", code, stdout)
	}

	if _, _, code := run(t, dir, baselineSmall, "disabled", "gosec"); code != 2 {
		t.Errorf("another linter should be a usage error, got %d", code)
	}
}

// TestCheck: a version below the floor fails naming both, one at or above
// passes; a path that is not a golangci-lint build fails as such.
func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		arg  string
		ok   bool
		want string
	}{
		{arg: "v2.13.0", ok: true},
		{arg: "v2.14.3", ok: true},
		{arg: "v3.0.0", ok: true},
		{arg: "v2.13.0-rc1", ok: true},
		{arg: "v2.12.9", ok: false, want: "v2.12.9, the baseline is written for v2.13.0"},
		{arg: "v1.64.8", ok: false, want: "too old"},
	}

	for _, testCase := range tests {
		t.Run(testCase.arg, func(t *testing.T) {
			t.Parallel()

			_, stderr, code := run(t, t.TempDir(), baselineSmall, checkCmd, testCase.arg)
			if (code == 0) != testCase.ok {
				t.Fatalf("check %s: exit %d, want ok=%v: %s", testCase.arg, code, testCase.ok, stderr)
			}

			if !strings.Contains(stderr, testCase.want) {
				t.Errorf("check %s: %q does not say %q", testCase.arg, stderr, testCase.want)
			}
		})
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, t.TempDir(), baselineSmall, checkCmd, self)
	if code == 0 || !strings.Contains(stderr, "not built from github.com/golangci/golangci-lint/v2") {
		t.Errorf("the test binary is not golangci-lint: %d %s", code, stderr)
	}

	_, stderr, code = run(t, t.TempDir(), baselineSmall, checkCmd, filepath.Join(t.TempDir(), "missing"))
	if code == 0 || !strings.Contains(stderr, "golangci-lint binary") {
		t.Errorf("a missing binary should fail as such: %d %s", code, stderr)
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {"frobnicate"}, {renderCmd, "extra"}, {checkCmd}, {flagsCmd}} {
		_, stderr, code := run(t, t.TempDir(), baselineSmall, args...)
		if code != 2 || !strings.Contains(stderr, "usage:") {
			t.Errorf("%v: exit %d, want 2 with usage: %s", args, code, stderr)
		}
	}

	_, stderr, code := run(t, t.TempDir(), baselineSmall, renderCmd)
	if code != 1 || !strings.Contains(stderr, "go.mod") {
		t.Errorf("a directory without go.mod should fail naming it: %d %s", code, stderr)
	}
}

// TestBaselineShipped: the repository's baseline renders for a module,
// placeholders filled, floor declared.
func TestBaselineShipped(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", lintgo.BaselineFile))
	if err != nil {
		t.Fatal(err)
	}

	config, failure := rendered(t, string(raw), "")
	if failure != "" {
		t.Fatal(failure)
	}

	if strings.Contains(config, "${") || !strings.Contains(config, "- "+moduleAcme) {
		t.Errorf("the shipped baseline did not render for the module:\n%s", config)
	}

	if _, _, code := run(t, t.TempDir(), string(raw), checkCmd, "v2.13.0"); code != 0 {
		t.Error("the shipped floor should accept v2.13.0")
	}

	stdout, _, code := run(t, writeModule(t, ""), string(raw), flagsCmd, licenseLane)
	if code != 0 || !strings.HasPrefix(stdout, "--allowed_licenses=Apache-2.0,") {
		t.Errorf("the shipped licenses lane: %d %q", code, stdout)
	}
}

// between is text from the first occurrence of start to the first later
// occurrence of end (or the end of text).
func between(text, start, end string) string {
	from := strings.Index(text, start)
	if from < 0 {
		return ""
	}

	rest := text[from:]
	if to := strings.Index(rest[len(start):], end); to >= 0 {
		return rest[:len(start)+to]
	}

	return rest
}

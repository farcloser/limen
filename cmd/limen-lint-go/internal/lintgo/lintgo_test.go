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

// run is one limen-lint-go invocation against baseline in dir.
func run(t *testing.T, dir, baseline string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errs bytes.Buffer

	code = lintgo.Run(append([]string{"-C", dir}, args...), []byte(baseline), &out, &errs)

	return out.String(), errs.String(), code
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

	for _, args := range [][]string{{}, {"frobnicate"}, {renderCmd, "extra"}, {checkCmd}, {flagsCmd}, {"baseline", "x"}} {
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

// TestBaselineShipped: the baseline beside the driver renders for a module,
// placeholders filled, floor declared.
func TestBaselineShipped(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "baseline.yml"))
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

	// `baseline` is the file as shipped, comments and placeholders included.
	stdout, _, code = run(t, t.TempDir(), string(raw), "baseline")
	if code != 0 || stdout != string(raw) {
		t.Errorf("baseline should print the shipped file verbatim: %d", code)
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

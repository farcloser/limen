package rules_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

const (
	mainGo        = "// Command thing does the thing.\npackage main\n\nfunc main() {}\n"
	releaseGoHead = releaseGoHeader + "\nversion: 2\n\n" + changelogSection
)

// releasing is a repository whose release configuration builds the given
// mains, in goreleaser's block form, with the seeded workflow beside it.
func releasing(files map[string]string, builds string) map[string]string {
	files[".release-go.yaml"] = releaseGoHead + "\nbuilds:\n" + builds
	files[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease

	return files
}

// workflowsFinding is the workflows rule's finding for files.
func workflowsFinding(t *testing.T, files map[string]string) rules.Finding {
	t.Helper()

	return findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), "workflows")
}

// TestReleasedCommands: a main package under cmd/ that no build in
// .release-go.yaml ships and no commands.unshipped entry in .lint-go.yaml
// declares fails the workflows rule and gets an advisory naming the
// command; one that ships, in either of goreleaser's spellings, or one
// declared passes; a main outside cmd/, a non-main package under cmd/ or a
// repository without a Go module are not read.
func TestReleasedCommands(t *testing.T) {
	t.Parallel()

	undeclared := goRepoFiles()
	undeclared["cmd/thing/main.go"] = mainGo
	undeclared["cmd/other/main.go"] = "//go:build linux\n\n" + mainGo
	dir := writeRepo(t, undeclared)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "workflows")
	if f.OK() || f.Path != "cmd/other" || !strings.Contains(f.Message, "cmd/other, cmd/thing") ||
		!strings.Contains(f.Message, "commands.unshipped") {
		t.Fatalf("undeclared commands: %+v", f)
	}

	outcomes := outcomesFor(rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}), "workflows")

	var advisory bool

	for _, o := range outcomes {
		if o.Action == rules.ActionAdvisory && o.Path == "cmd/other" {
			advisory = true
		}
	}

	if !advisory {
		t.Fatalf("fix should report the undeclared commands as an advisory, got %+v", outcomes)
	}

	// Shipped: a build naming the main package, one naming its main.go
	// file, one running in the command's dir (goreleaser's default main is
	// the current directory), the other keys and nested lists of a build in
	// between.
	shipped := goRepoFiles()
	shipped["cmd/thing/main.go"] = mainGo
	shipped["cmd/file/main.go"] = mainGo
	shipped["cmd/driver/main.go"] = mainGo
	shipped = releasing(shipped, `  - id: thing
    main: "./cmd/thing" # quoted, as YAML allows
    binary: thing
    env:
      - CGO_ENABLED=0
    targets:
      - linux_amd64
  - id: file
    main: ./cmd/file/main.go
    binary: file
  - id: driver
    dir: cmd/driver
    binary: driver
`)

	if f := workflowsFinding(t, shipped); !f.OK() {
		t.Fatalf("shipped commands: %+v", f)
	}

	// A release that ships one command and declares the other.
	mixed := goRepoFiles()
	mixed["cmd/thing/main.go"] = mainGo
	mixed["cmd/gen/main.go"] = mainGo
	mixed[".lint-go.yaml"] = "commands:\n  unshipped:\n    - ./cmd/gen # a generator\n"
	mixed = releasing(mixed, "  - main: ./cmd/thing\n    binary: thing\n")

	if f := workflowsFinding(t, mixed); !f.OK() {
		t.Fatalf("shipped and declared: %+v", f)
	}

	// A repository that never releases declares every main, here as a flow
	// list beside its lint carve-outs; no .release-go.yaml is owed.
	unshipped := goRepoFiles()
	unshipped["cmd/dnsctl/main.go"] = mainGo
	unshipped["cmd/plan/main.go"] = mainGo
	unshipped[".lint-go.yaml"] = "golangci:\n  linters:\n    disable:\n      - godox\ncommands:\n" +
		"  unshipped: [cmd/dnsctl, ./cmd/plan]\n"

	if f := workflowsFinding(t, unshipped); !f.OK() {
		t.Fatalf("declared unshipped: %+v", f)
	}

	// Not read: a main outside cmd/, a library package under cmd/, a test
	// file alone.
	elsewhere := goRepoFiles()
	elsewhere["examples/demo/main.go"] = mainGo
	elsewhere["internal/cmd/gen/main.go"] = mainGo
	elsewhere["cmd/lib/lib.go"] = "package lib\n"
	elsewhere["cmd/lib/lib_test.go"] = "package main\n"

	if f := workflowsFinding(t, elsewhere); !f.OK() {
		t.Fatalf("not read: %+v", f)
	}

	// No Go module: cmd/ means nothing to the rule.
	noModule := compliantFiles()
	noModule["cmd/thing/main.go"] = mainGo

	if f := workflowsFinding(t, noModule); !f.OK() {
		t.Fatalf("no module: %+v", f)
	}
}

// TestUnshippedEntriesAreExplicit: an unshipped entry that names no main
// package under cmd/ that no build ships fails, so the list cannot be a
// directory, a pattern, a command that ships or one since removed.
func TestUnshippedEntriesAreExplicit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		overlay string
		stale   string
	}{
		{name: "directory", overlay: "commands:\n  unshipped: [./cmd]\n", stale: "cmd"},
		{name: "pattern", overlay: "commands:\n  unshipped: [./cmd/*]\n", stale: "cmd/*"},
		{name: "ships", overlay: "commands:\n  unshipped: [./cmd/thing]\n", stale: "cmd/thing"},
		{name: "removed", overlay: "commands:\n  unshipped:\n    - ./cmd/gone\n", stale: "cmd/gone"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := goRepoFiles()
			files["cmd/thing/main.go"] = mainGo
			files[".lint-go.yaml"] = testCase.overlay
			files = releasing(files, "  - main: ./cmd/thing\n    binary: thing\n")

			f := workflowsFinding(t, files)
			if f.OK() || f.Path != ".lint-go.yaml" || !strings.Contains(f.Message, testCase.stale+": named under") {
				t.Fatalf("stale entry %q: %+v", testCase.stale, f)
			}
		})
	}

	// Both at once: the undeclared command is the path, and both are named.
	both := goRepoFiles()
	both["cmd/thing/main.go"] = mainGo
	both[".lint-go.yaml"] = "commands:\n  unshipped: [./cmd/gone]\n"

	f := workflowsFinding(t, both)
	if f.OK() || f.Path != "cmd/thing" || !strings.Contains(f.Message, "cmd/thing: a command") ||
		!strings.Contains(f.Message, "cmd/gone: named under") {
		t.Fatalf("undeclared and stale: %+v", f)
	}
}

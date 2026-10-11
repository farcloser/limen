package rules_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/rules"
)

const ruleBareTools = "baretools"

// TestBareToolsPassesBuiltBinaries: a tool run as build/tools/<name>, named in
// a comment, as an argument, as a path element or as a cache variable is not a
// bare invocation; nor is a recipe that delegates to the shared lanes.
func TestBareToolsPassesBuiltBinaries(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".justfile"] += `
# golangci-lint is built by the lint go lane; the recipes below run that binary.
export GOLANGCI_LINT_CACHE := justfile_directory() / 'build/cache/golangci-lint'

lint-extra:
    build/tools/golangci-lint run -c build/golangci-lint-go.yml ./...
    cd cmd/x && ../../build/tools/golangci-lint fmt --diff ./... # not golangci-lint from PATH
    just do tools update golangci-lint
    just do lint go
`
	files["scripts/check.sh"] = "#!/bin/sh\njust do lint go\nls tools/golangci-lint/go.mod\n"
	files[".github/workflows/extra.yaml"] = "on: push\njobs:\n  x:\n    steps:\n      - run: just do lint go\n" +
		"      - uses: ./.github/actions/build\n        with:\n          tool: golangci-lint\n"

	f := findingByRule(rules.Check(writeRepo(t, files), rules.DefaultPolicy()), ruleBareTools)
	if !f.OK() {
		t.Fatalf("built binaries, comments, arguments and paths should pass, got %+v", f)
	}
}

// TestBareToolsFailsPathInvocations: a Go-built tool named in command position
// in the justfile, a script or a workflow fails the check, every hit listed
// with its line, and fix reports the same as an advisory.
func TestBareToolsFailsPathInvocations(t *testing.T) {
	t.Parallel()

	files := compliantFiles()
	files[".justfile"] += "\nproto:\n    go generate ./...\n    golangci-lint fmt ./... && godolint Dockerfile\n"
	files["hack/vuln.sh"] = "#!/bin/sh\nset -e\ngo build ./... ; govulncheck ./...\n"
	files[".github/workflows/extra.yaml"] = "on: push\njobs:\n  x:\n    steps:\n      - run: go-licenses check ./...\n" +
		"      - run: |\n          deadcode ./...\n"
	dir := writeRepo(t, files)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), ruleBareTools)
	if f.OK() || f.Path != ".justfile" {
		t.Fatalf("bare invocations should fail at the justfile first, got %+v", f)
	}

	for _, want := range []string{
		".justfile:7 runs `golangci-lint` from PATH",
		".justfile:7 runs `godolint` from PATH",
		".github/workflows/extra.yaml:5 runs `go-licenses` from PATH",
		".github/workflows/extra.yaml:7 runs `deadcode` from PATH",
		"hack/vuln.sh:3 runs `govulncheck` from PATH",
		"build/tools/",
	} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message should contain %q, got %q", want, f.Message)
		}
	}

	outcomes := outcomesFor(rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}), ruleBareTools)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionAdvisory || outcomes[0].Message != f.Message {
		t.Fatalf("fix should report the same hits as one advisory, got %+v", outcomes)
	}
}

// TestBareToolsCompliantRepo: the compliant fixture passes and fix has nothing
// to do.
func TestBareToolsCompliantRepo(t *testing.T) {
	t.Parallel()

	dir := writeRepo(t, compliantFiles())

	if f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), ruleBareTools); !f.OK() {
		t.Fatalf("the compliant repository should pass, got %+v", f)
	}

	outcomes := outcomesFor(rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}), ruleBareTools)
	if len(outcomes) != 1 || outcomes[0].Action != rules.ActionNone {
		t.Fatalf("fix should report nothing to do, got %+v", outcomes)
	}
}

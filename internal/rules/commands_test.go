package rules_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

// TestReleasedCommands: a Go repository that builds a command under cmd/ and
// releases nothing fails the workflows rule and gets an advisory naming the
// command; a release configuration, a helper outside cmd/, a non-main package
// under cmd/ or a repository without a Go module are all fine.
func TestReleasedCommands(t *testing.T) {
	t.Parallel()

	const mainGo = "// Command thing does the thing.\npackage main\n\nfunc main() {}\n"

	unreleased := goRepoFiles()
	unreleased["cmd/thing/main.go"] = mainGo
	unreleased["cmd/other/main.go"] = "//go:build linux\n\n" + mainGo
	dir := writeRepo(t, unreleased)

	f := findingByRule(rules.Check(dir, rules.DefaultPolicy()), "workflows")
	if f.OK() || f.Path != "cmd/other" || !strings.Contains(f.Message, "cmd/other, cmd/thing") {
		t.Fatalf("unreleased commands: %+v", f)
	}

	outcomes := outcomesFor(rules.Fix(t.Context(), dir, rules.FixOptions{Policy: rules.DefaultPolicy()}), "workflows")

	var advisory bool

	for _, o := range outcomes {
		if o.Action == rules.ActionAdvisory && o.Path == "cmd/other" {
			advisory = true
		}
	}

	if !advisory {
		t.Fatalf("fix should report the unreleased commands as an advisory, got %+v", outcomes)
	}

	// Released: the configuration and, with it, the seeded workflow.
	released := goRepoFiles()
	released["cmd/thing/main.go"] = mainGo
	released[".release-go.yaml"] = releaseGoHeader + "\nversion: 2\n\n" + changelogSection
	released[".github/workflows/release.yaml"] = limen.CanonicalWorkflowRelease

	if f := findingByRule(rules.Check(writeRepo(t, released), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Fatalf("released command: %+v", f)
	}

	// Not products: a helper outside cmd/, a library package under cmd/, a
	// test file alone.
	helpers := goRepoFiles()
	helpers["internal/cmd/gen/main.go"] = mainGo
	helpers["hack/tool/main.go"] = mainGo
	helpers["cmd/lib/lib.go"] = "package lib\n"
	helpers["cmd/lib/lib_test.go"] = "package main\n"

	if f := findingByRule(rules.Check(writeRepo(t, helpers), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Fatalf("helpers: %+v", f)
	}

	// No Go module: cmd/ means nothing to the rule.
	noModule := compliantFiles()
	noModule["cmd/thing/main.go"] = mainGo

	if f := findingByRule(rules.Check(writeRepo(t, noModule), rules.DefaultPolicy()), "workflows"); !f.OK() {
		t.Fatalf("no module: %+v", f)
	}
}

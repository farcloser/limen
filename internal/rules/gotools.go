package rules

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// The gotools rule: a Go module must declare the Go-source analyzers the
// shared recipes run — deadcode, govulncheck, go-licenses — as go.mod `tool`
// directives, so each is compiled by the module's own pinned toolchain (see
// book/tooling.md, "Go-source analyzers are go.mod tools"). These were aqua
// go_install packages once; aqua builds such a package one time per tool
// version, with whichever project's pinned go ran it first, and then shares
// the binary across projects — and an analyzer that embeds Go's source loader
// cannot read a module newer than the go that built it. The tool directive is
// the only pin that makes that skew impossible. The rule applies exactly when
// the repository root carries a go.mod; every other repository is silent.
const (
	ruleGoTools = "gotools"
	goModFile   = "go.mod"
)

// goModTools are the tool packages every Go module must declare, in the order
// the message lists them. They are exactly the retired aqua packages
// (retiredCanonicalPkgs): one doctrine, two rules enforcing its two halves.
//
//nolint:gochecknoglobals // immutable baseline data.
var goModTools = []string{
	"golang.org/x/tools/cmd/deadcode",
	"golang.org/x/vuln/cmd/govulncheck",
	"github.com/google/go-licenses/v2",
}

// goBin is the go executable remediation shells out to when adding tool
// directives. A package-level seam so tests can substitute a stub.
var goBin = "go" //nolint:gochecknoglobals // test seam: tests substitute a stub binary.

// checkGoTools evaluates the rule. The bool is false when the repository has
// no go.mod (the rule does not apply), true otherwise.
func checkGoTools(root string) (Finding, bool) {
	data, err := readRepoFile(root, goModFile)
	if err != nil {
		return Finding{}, false
	}

	missing := missingGoModTools(string(data))
	if len(missing) == 0 {
		return Finding{
			Rule:    ruleGoTools,
			Status:  StatusOK,
			Path:    goModFile,
			Message: goModFile + " declares the Go-source analyzers as tool directives",
		}, true
	}

	return fail(ruleGoTools, goModFile, goModFile+": "+missingGoModToolsMessage(missing)), true
}

// missingGoModTools returns the required tool packages the go.mod text does
// not declare, in goModTools order.
func missingGoModTools(gomod string) []string {
	declared := goModToolDirectives(gomod)

	var missing []string

	for _, pkg := range goModTools {
		if !declared[pkg] {
			missing = append(missing, pkg)
		}
	}

	return missing
}

// goModToolDirectives parses the tool directives out of a go.mod: both the
// one-line form (`tool pkg`) and the block form (`tool (` … `)`), comments
// stripped. Only the directive shape matters; versions live in the require
// block and are the project's own (Renovate bumps them).
func goModToolDirectives(gomod string) map[string]bool {
	tools := map[string]bool{}
	inBlock := false

	for raw := range strings.SplitSeq(gomod, "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}

		line = strings.TrimSpace(line)

		switch {
		case line == "":
			continue
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			tools[line] = true
		case line == "tool (":
			inBlock = true
		case strings.HasPrefix(line, "tool "):
			tools[strings.TrimSpace(strings.TrimPrefix(line, "tool "))] = true
		default:
			// any other directive: not a tool line
		}
	}

	return tools
}

// missingGoModToolsMessage is the check failure wording; the fix advisory
// appends what it tried.
func missingGoModToolsMessage(missing []string) string {
	return "missing tool directive(s) for " + strings.Join(missing, ", ") +
		" (see book/tooling.md, \"Go-source analyzers are go.mod tools\")"
}

// remediateGoTools adds the missing tool directives with `go get -tool` at the
// latest version (the resolved version is then pinned in go.mod, exactly as
// `just do tools add` pins an aqua tool at its latest, and Renovate bumps it
// from there) followed by `go mod tidy`. Both need the pinned go on PATH and
// the network — when either is unavailable the rule ends as an advisory
// carrying the exact command to run by hand.
func remediateGoTools(root string) Outcome {
	data, err := readRepoFile(root, goModFile)
	if err != nil {
		return Outcome{
			Rule:    ruleGoTools,
			Action:  ActionNone,
			Path:    goModFile,
			Message: "not applicable: no go.mod (not a Go module)",
		}
	}

	missing := missingGoModTools(string(data))
	if len(missing) == 0 {
		return Outcome{
			Rule:    ruleGoTools,
			Action:  ActionNone,
			Path:    goModFile,
			Message: goModFile + " declares the Go-source analyzers as tool directives",
		}
	}

	getArgs := []string{"get", "-tool"}
	for _, pkg := range missing {
		getArgs = append(getArgs, pkg+"@latest")
	}

	for _, args := range [][]string{getArgs, {"mod", "tidy"}} {
		// goBin is "go" outside tests (a package-level seam, not user input),
		// and args are the fixed lists above plus baseline package paths.
		cmd := exec.CommandContext(context.Background(), goBin, args...) // #nosec G204 -- see above.

		cmd.Dir = root
		if combined, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			return Outcome{
				Rule:   ruleGoTools,
				Action: ActionAdvisory,
				Path:   goModFile,
				Message: fmt.Sprintf(
					"%s; `go %s` failed (%v: %s) — run it by hand with the pinned go on PATH: go %s && go mod tidy",
					missingGoModToolsMessage(missing),
					strings.Join(args, " "),
					cmdErr,
					strings.TrimSpace(string(combined)),
					strings.Join(getArgs, " "),
				),
			}
		}
	}

	return Outcome{
		Rule:    ruleGoTools,
		Action:  ActionMerged,
		Path:    goModFile,
		Message: "added tool directive(s) for " + strings.Join(missing, ", ") + " (go get -tool, then go mod tidy)",
	}
}

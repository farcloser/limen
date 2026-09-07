package rules

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// The gotools rule: a Go module must declare the Go-source analyzers the
// shared recipes run — deadcode, govulncheck, go-licenses — as `tool`
// directives in tools/go.mod, a module of its own, so each is compiled by the
// repository's pinned toolchain (see book/tooling.md, "Go-source analyzers are
// go.mod tools"). These were aqua go_install packages once; aqua builds such a
// package one time per tool version, with whichever project's pinned go ran it
// first, and then shares the binary across projects — and an analyzer that
// embeds Go's source loader cannot read a module newer than the go that built
// it. The tool directive is the only pin that makes that skew impossible.
//
// The directives live in tools/go.mod and never in the project's go.mod: a
// tool directive drags the analyzer's dependency graph into the declaring
// module as indirect requirements, and everything a module requires is
// inherited by every consumer's module graph and go.sum. The rule applies
// exactly when the repository root carries a go.mod; every other repository
// is silent.
const (
	ruleGoTools    = "gotools"
	goModFile      = "go.mod"
	goToolsDir     = "tools"
	goToolsModFile = goToolsDir + "/" + goModFile

	// go.mod directive keywords, as they open a line.
	toolDirective   = "tool "
	moduleDirective = "module "
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
	rootMod, err := readRepoFile(root, goModFile)
	if err != nil {
		return Finding{}, false
	}

	if stray := sortedKeys(goModToolDirectives(string(rootMod))); len(stray) > 0 {
		return fail(ruleGoTools, goModFile, goModFile+": "+strayGoModToolsMessage(stray)), true
	}

	toolsMod, err := readRepoFile(root, goToolsModFile)
	if err != nil {
		return fail(
			ruleGoTools,
			goToolsModFile,
			goToolsModFile+": missing; "+missingGoModToolsMessage(goModTools),
		), true
	}

	missing := missingGoModTools(string(toolsMod))
	if len(missing) == 0 {
		return Finding{
			Rule:    ruleGoTools,
			Status:  StatusOK,
			Path:    goToolsModFile,
			Message: goToolsModFile + " declares the Go-source analyzers as tool directives",
		}, true
	}

	return fail(ruleGoTools, goToolsModFile, goToolsModFile+": "+missingGoModToolsMessage(missing)), true
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
		case strings.HasPrefix(line, toolDirective):
			tools[strings.TrimSpace(strings.TrimPrefix(line, toolDirective))] = true
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

// strayGoModToolsMessage names tool directives found in the project's go.mod,
// where they pollute every consumer's module graph.
func strayGoModToolsMessage(stray []string) string {
	return "tool directive(s) for " + strings.Join(stray, ", ") +
		" belong in " + goToolsModFile + ", not in the module consumers import" +
		" (see book/tooling.md, \"Go-source analyzers are go.mod tools\")"
}

// sortedKeys returns a set's keys in order, for stable messages.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// stripGoModToolDirectives returns gomod without its tool directives (both
// forms, comments and all), the inverse of goModToolDirectives.
func stripGoModToolDirectives(gomod string) string {
	var out []string

	inBlock := false

	for raw := range strings.SplitSeq(gomod, "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}

		line = strings.TrimSpace(line)

		switch {
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
		case line == "tool (":
			inBlock = true
		case strings.HasPrefix(line, toolDirective):
		default:
			out = append(out, raw)
		}
	}

	return strings.Join(out, "\n")
}

// remediateGoTools makes tools/go.mod carry the analyzers and the project's
// go.mod carry none: it writes a bare tools/go.mod when there is none (module
// path <root>/tools, the root's go directive), adds the missing directives
// there with `go get -tool` at the latest version (the resolved version is
// then pinned, exactly as `just do tools add` pins an aqua tool at its latest,
// and Renovate bumps it from there) followed by `go mod tidy`, and strips any
// tool directive out of the root go.mod, tidying it too. The go steps need the
// pinned go on PATH and the network — when either is unavailable the rule ends
// as an advisory carrying the exact command to run by hand.
func remediateGoTools(root string) Outcome {
	rootMod, err := readRepoFile(root, goModFile)
	if err != nil {
		return Outcome{
			Rule:    ruleGoTools,
			Action:  ActionNone,
			Path:    goModFile,
			Message: "not applicable: no go.mod (not a Go module)",
		}
	}

	var done []string

	if stray := sortedKeys(goModToolDirectives(string(rootMod))); len(stray) > 0 {
		if err := writeFile(root, goModFile, stripGoModToolDirectives(string(rootMod))); err != nil {
			return goToolsAdvisory(goModFile, "could not rewrite go.mod: "+err.Error(), nil)
		}

		if out := runGo(root, "mod", "tidy"); out != "" {
			return goToolsAdvisory(goModFile, out, nil)
		}

		done = append(done, "moved tool directive(s) for "+strings.Join(stray, ", ")+" out of "+goModFile)
	}

	toolsMod, err := readRepoFile(root, goToolsModFile)
	if err != nil {
		toolsMod = []byte(bareToolsGoMod(string(rootMod)))
		if err := writeFile(root, goToolsModFile, string(toolsMod)); err != nil {
			return goToolsAdvisory(goToolsModFile, "could not write "+goToolsModFile+": "+err.Error(), nil)
		}

		done = append(done, "created "+goToolsModFile)
	}

	missing := missingGoModTools(string(toolsMod))
	if len(missing) == 0 && len(done) == 0 {
		return Outcome{
			Rule:    ruleGoTools,
			Action:  ActionNone,
			Path:    goToolsModFile,
			Message: goToolsModFile + " declares the Go-source analyzers as tool directives",
		}
	}

	if len(missing) > 0 {
		getArgs := []string{"get", "-tool"}
		for _, pkg := range missing {
			getArgs = append(getArgs, pkg+"@latest")
		}

		toolsRoot := filepath.Join(root, goToolsDir)
		for _, args := range [][]string{getArgs, {"mod", "tidy"}} {
			if out := runGo(toolsRoot, args...); out != "" {
				return goToolsAdvisory(goToolsModFile, missingGoModToolsMessage(missing)+"; "+out, getArgs)
			}
		}

		done = append(
			done,
			"added tool directive(s) for "+strings.Join(
				missing,
				", ",
			)+" (go -C tools get -tool, then go -C tools mod tidy)",
		)
	}

	return Outcome{
		Rule:    ruleGoTools,
		Action:  ActionMerged,
		Path:    goToolsModFile,
		Message: strings.Join(done, "; "),
	}
}

// bareToolsGoMod is the tools module before any directive: the root module's
// path with /tools appended and the root's go directive, so the analyzers are
// built for the same language version as the project.
func bareToolsGoMod(rootMod string) string {
	module, goLine := "tools", "go 1.24"

	for raw := range strings.SplitSeq(rootMod, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, moduleDirective):
			module = strings.TrimSpace(strings.TrimPrefix(line, moduleDirective)) + "/" + goToolsDir
		case strings.HasPrefix(line, "go "):
			goLine = line
		default:
			// other directives: not part of the seed
		}
	}

	return "// The Go-source analyzers the shared recipes run, pinned as tool\n" +
		"// directives in a module of their own so their dependency graph never\n" +
		"// reaches the project's go.mod (book/tooling.md).\n" +
		moduleDirective + module + "\n\n" + goLine + "\n"
}

// runGo runs the pinned go with args in dir and returns "" on success, or a
// one-line description of the failure.
func runGo(dir string, args ...string) string {
	// goBin is "go" outside tests (a package-level seam, not user input), and
	// args are fixed lists plus baseline package paths.
	cmd := exec.CommandContext(context.Background(), goBin, args...) // #nosec G204 -- see above.
	cmd.Dir = dir

	combined, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}

	return fmt.Sprintf("`go %s` failed (%v: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(combined)))
}

// goToolsAdvisory is the outcome when a go step could not run: the manual
// commands are spelled out.
func goToolsAdvisory(path, reason string, getArgs []string) Outcome {
	manual := "go mod tidy"
	if getArgs != nil {
		manual = "go -C tools " + strings.Join(getArgs, " ") + " && go -C tools mod tidy"
	}

	return Outcome{
		Rule:    ruleGoTools,
		Action:  ActionAdvisory,
		Path:    path,
		Message: reason + " — run it by hand with the pinned go on PATH: " + manual,
	}
}

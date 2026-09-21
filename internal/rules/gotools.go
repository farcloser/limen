package rules

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The gotools rule: every repository declares the Go-built tools the shared
// recipes run as `tool` directives in tools/go.mod, a module of its own, so
// each is compiled by the repository's pinned toolchain (see book/tooling.md,
// "Go-built tools are go.mod tools"). These were aqua go_install packages
// once; aqua builds such a package one time per tool version, with whichever
// project's pinned go ran it first, and then shares the binary across
// projects — nothing pins the compiler behind the binary that runs, and an
// analyzer that embeds Go's source loader cannot even read a module newer
// than the go that built it. The tool directive is the only pin that makes
// that impossible.
//
// The directives live in tools/go.mod and never in the project's go.mod: a
// tool directive drags the tool's dependency graph into the declaring module
// as indirect requirements, and everything a module requires is inherited by
// every consumer's module graph and go.sum. Three tools are required
// everywhere (git-validation, godolint, dot: the lint and test recipes of
// every repository run them); a repository whose root carries a go.mod adds
// the Go-source analyzers.
const (
	ruleGoTools    = "gotools"
	goModFile      = "go.mod"
	goToolsDir     = "tools"
	goToolsModFile = goToolsDir + slash + goModFile

	// go.mod directive keywords, as they open a line.
	toolDirective   = "tool "
	moduleDirective = "module "

	// The go subcommand that moves a tool directive, and its flag.
	goGetVerb  = "get"
	goToolFlag = "-tool"

	// Module-relative paths always join with a slash, whatever the platform.
	slash = "/"

	// The go directive seeded when neither a root go.mod nor an aqua golang/go
	// pin says otherwise: the first release with tool directives.
	fallbackGoDirective = "go 1.24"
)

// goToolsEverywhere are the tool packages every repository must declare, in
// the order the message lists them: the shared recipes run them regardless
// of language.
//
//nolint:gochecknoglobals // immutable baseline data.
var goToolsEverywhere = []string{
	"github.com/vbatts/git-validation",
	"github.com/farcloser/godolint/cmd/godolint",
	"github.com/forkcloser/dot/cmd/dot",
}

// retiredGoTools are tool directives a repository must no longer carry, each
// with the package that replaced it. A directive that stays beside its
// replacement builds nothing the recipes run but keeps its dependency graph
// in tools/go.mod and go.sum. Check fails while one is declared; fix removes
// it with `go get -tool <pkg>@none`, which go accepts even when the package's
// module no longer provides it.
//
//nolint:gochecknoglobals // immutable baseline data.
var retiredGoTools = map[string]string{
	// Upstream's cmd/dot lost its tags and its library moved on without it.
	"github.com/goccy/go-graphviz/cmd/dot": "github.com/forkcloser/dot/cmd/dot",
}

// goSourceAnalyzers are the tool packages a Go module must declare on top:
// the analyzers that load the project's source.
//
//nolint:gochecknoglobals // immutable baseline data.
var goSourceAnalyzers = []string{
	"golang.org/x/tools/cmd/deadcode",
	"golang.org/x/vuln/cmd/govulncheck",
	"github.com/google/go-licenses/v2",
}

// isolatedGoTools are the tools a Go module declares each in a module of its
// own, tools/<name>/go.mod, keyed by the name the tool builds as. A tool
// goes here when its dependency graph must stay its upstream's exactly:
// minimal version selection across the shared tools/go.mod would lift the
// analyzers golangci-lint bundles past the versions upstream tested, which
// upstream states is unsupported, and a module of its own resolves to
// upstream's go.mod and nothing else. Built here by the pinned toolchain
// like every other directive, so it can never skew from the Go it analyzes.
//
//nolint:gochecknoglobals // immutable baseline data.
var isolatedGoTools = map[string]string{
	"golangci-lint": "github.com/golangci/golangci-lint/v2/cmd/golangci-lint",
}

// isolatedAquaRetired are the aqua packages the isolated tools replaced: a
// manifest still pinning one carries an upstream-built binary beside the
// one the toolchain builds, and the aqua rule retires it.
//
//nolint:gochecknoglobals // immutable baseline data.
var isolatedAquaRetired = []string{
	"golangci/golangci-lint",
}

// aquaGoPin finds the golang/go pin of an aqua manifest (`golang/go@go1.N.M`,
// one-line form, which the aqua rule enforces) and captures its version.
var aquaGoPin = regexp.MustCompile(
	`(?m)^\s*-\s*name:\s*['"]?golang/go@go(\S+?)['"]?\s*(#.*)?$`,
)

// requiredGoTools lists the tool packages a repository must declare: the
// everywhere set, plus the analyzers when the root carries a go.mod (rootMod
// is its text, nil when there is none). The aqua rule's retired set is built
// from these same tables (retiredCanonicalPkgs): one doctrine, two rules
// enforcing its two halves from one source.
func requiredGoTools(rootMod []byte) []string {
	if rootMod == nil {
		return slices.Clone(goToolsEverywhere)
	}

	return slices.Concat(goToolsEverywhere, goSourceAnalyzers)
}

// rootGoMod reads the project's go.mod: its text, or nil when the repository
// is not a Go module.
func rootGoMod(root string) []byte {
	rootMod, err := readRepoFile(root, goModFile)
	if err != nil {
		return nil
	}

	return rootMod
}

// checkGoTools evaluates the rule.
func checkGoTools(root string) Finding {
	rootMod := rootGoMod(root)

	if stray := sortedKeys(goModToolDirectives(string(rootMod))); len(stray) > 0 {
		return fail(ruleGoTools, goModFile, goModFile+": "+strayGoModToolsMessage(stray))
	}

	required := requiredGoTools(rootMod)

	toolsMod, err := readRepoFile(root, goToolsModFile)
	if err != nil {
		return fail(
			ruleGoTools,
			goToolsModFile,
			goToolsModFile+": missing; "+missingGoModToolsMessage(required),
		)
	}

	missing := missingGoModTools(string(toolsMod), required)
	if len(missing) > 0 {
		return fail(ruleGoTools, goToolsModFile, goToolsModFile+": "+missingGoModToolsMessage(missing))
	}

	if retired := retiredGoModTools(string(toolsMod)); len(retired) > 0 {
		return fail(ruleGoTools, goToolsModFile, goToolsModFile+": "+retiredGoModToolsMessage(retired))
	}

	if rootMod != nil {
		if missing := missingIsolatedGoTools(root); len(missing) > 0 {
			return fail(ruleGoTools, isolatedGoModFile(missing[0]), isolatedGoToolsMessage(missing))
		}
	}

	return Finding{
		Rule:    ruleGoTools,
		Status:  StatusOK,
		Path:    goToolsModFile,
		Message: goToolsPassMessage,
	}
}

// isolatedGoModFile is the go.mod of an isolated tool's module.
func isolatedGoModFile(name string) string {
	return goToolsDir + slash + name + slash + goModFile
}

// missingIsolatedGoTools names the isolated tools whose module is absent or
// lacks its directive, sorted.
func missingIsolatedGoTools(root string) []string {
	var missing []string

	for name, pkg := range isolatedGoTools {
		gomod, err := readRepoFile(root, isolatedGoModFile(name))
		if err != nil || !goModToolDirectives(string(gomod))[pkg] {
			missing = append(missing, name)
		}
	}

	slices.Sort(missing)

	return missing
}

// isolatedGoToolsMessage names each missing isolated tool with its module.
func isolatedGoToolsMessage(missing []string) string {
	pairs := make([]string, 0, len(missing))
	for _, name := range missing {
		pairs = append(pairs, isolatedGoModFile(name)+" lacks 'tool "+isolatedGoTools[name]+"'")
	}

	return strings.Join(pairs, listSeparator) +
		" — each is built by the pinned toolchain from a module of its own (limen fix creates it)"
}

// retiredGoModTools returns the retired tool packages the go.mod text still
// declares, sorted.
func retiredGoModTools(gomod string) []string {
	declared := goModToolDirectives(gomod)

	var retired []string

	for pkg := range retiredGoTools {
		if declared[pkg] {
			retired = append(retired, pkg)
		}
	}

	slices.Sort(retired)

	return retired
}

// listSeparator joins package lists in messages.
const listSeparator = ", "

// retiredGoModToolsMessage names each retired directive and its replacement.
func retiredGoModToolsMessage(retired []string) string {
	pairs := make([]string, 0, len(retired))
	for _, pkg := range retired {
		pairs = append(pairs, pkg+" (replaced by "+retiredGoTools[pkg]+")")
	}

	return "retired tool directive(s) for " + strings.Join(pairs, listSeparator) +
		"; remove with go -C tools get -tool <pkg>@none && go -C tools mod tidy"
}

// goGetThenTidy runs one `go get` in the tools module followed by `go mod
// tidy`, and returns the first failure's output, or "" when both succeeded.
func goGetThenTidy(ctx context.Context, toolsRoot string, getArgs []string) string {
	for _, args := range [][]string{getArgs, {"mod", "tidy"}} {
		if out := runGo(ctx, toolsRoot, args...); out != "" {
			return out
		}
	}

	return ""
}

// goToolsPassMessage is the check and fix wording for a complete module.
const goToolsPassMessage = goToolsModFile + " declares the Go-built tools as tool directives"

// missingGoModTools returns the required tool packages the go.mod text does
// not declare, in required order.
func missingGoModTools(gomod string, required []string) []string {
	declared := goModToolDirectives(gomod)

	var missing []string

	for _, pkg := range required {
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
		" (see book/tooling.md, \"Go-built tools are go.mod tools\")"
}

// strayGoModToolsMessage names tool directives found in the project's go.mod,
// where they pollute every consumer's module graph.
func strayGoModToolsMessage(stray []string) string {
	return "tool directive(s) for " + strings.Join(stray, ", ") +
		" belong in " + goToolsModFile + ", not in the module consumers import" +
		" (see book/tooling.md, \"Go-built tools are go.mod tools\")"
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

// remediateGoTools makes tools/go.mod carry the required tools and the
// project's go.mod (when there is one) carry none: it writes a bare
// tools/go.mod when there is none (module path <root>/tools and the root's
// go directive in a Go repository; `tools` and the aqua-pinned go elsewhere),
// adds the missing directives there with `go get -tool` at the latest version
// (the resolved version is then pinned, exactly as `just do tools add` pins
// an aqua tool at its latest, and Renovate bumps it from there) followed by
// `go mod tidy`, and strips any tool directive out of the root go.mod, tidying
// it too. The go steps need the pinned go on PATH and the network — when
// either is unavailable the rule ends as an advisory carrying the exact
// command to run by hand.
func remediateGoTools(ctx context.Context, root string) Outcome {
	rootMod := rootGoMod(root)

	var done []string

	if step, advisory := moveStrayGoTools(ctx, root, rootMod); advisory != nil {
		return *advisory
	} else if step != "" {
		done = append(done, step)
	}

	toolsMod, err := readRepoFile(root, goToolsModFile)
	if err != nil {
		toolsMod = []byte(bareToolsGoMod(string(rootMod), aquaGoDirective(root)))
		if err := writeFile(root, goToolsModFile, string(toolsMod)); err != nil {
			return goToolsAdvisory(goToolsModFile, "could not write "+goToolsModFile+": "+err.Error(), nil)
		}

		done = append(done, "created "+goToolsModFile)
	}

	missing := missingGoModTools(string(toolsMod), requiredGoTools(rootMod))
	retired := retiredGoModTools(string(toolsMod))

	if len(missing) == 0 && len(retired) == 0 && len(done) == 0 {
		return Outcome{
			Rule:    ruleGoTools,
			Action:  ActionNone,
			Path:    goToolsModFile,
			Message: goToolsPassMessage,
		}
	}

	toolsRoot := filepath.Join(root, goToolsDir)

	if len(missing) > 0 {
		step, advisory := goToolsGet(ctx, toolsRoot, missing, "latest", missingGoModToolsMessage(missing),
			"added tool directive(s) for ", " (go -C tools get -tool, then go -C tools mod tidy)")
		if advisory != nil {
			return *advisory
		}

		done = append(done, step)
	}

	if len(retired) > 0 {
		step, advisory := goToolsGet(ctx, toolsRoot, retired, "none", retiredGoModToolsMessage(retired),
			"removed retired tool directive(s) for ", " (go -C tools get -tool <pkg>@none, then go -C tools mod tidy)")
		if advisory != nil {
			return *advisory
		}

		done = append(done, step)
	}

	steps, advisory := seedIsolatedGoTools(ctx, root, rootMod)
	if advisory != nil {
		return *advisory
	}

	return Outcome{
		Rule:    ruleGoTools,
		Action:  ActionMerged,
		Path:    goToolsModFile,
		Message: strings.Join(append(done, steps...), "; "),
	}
}

// seedIsolatedGoTools seeds every isolated tool module a Go repository lacks
// (none is asked of a repository without a root go.mod): the steps done, or
// the advisory that stopped it.
func seedIsolatedGoTools(ctx context.Context, root string, rootMod []byte) ([]string, *Outcome) {
	if rootMod == nil {
		return nil, nil
	}

	var done []string

	for _, name := range missingIsolatedGoTools(root) {
		if outcome := remediateIsolatedGoTool(ctx, root, rootMod, name); outcome != nil {
			return nil, outcome
		}

		done = append(done, "created "+isolatedGoModFile(name)+" with 'tool "+isolatedGoTools[name]+
			"' (go get -tool, then go mod tidy, in that module)")
	}

	return done, nil
}

// moveStrayGoTools strips the tool directives out of the project's go.mod
// and tidies it: the step done ("" when there was nothing to move), or the
// advisory when a write or the tidy failed.
func moveStrayGoTools(ctx context.Context, root string, rootMod []byte) (string, *Outcome) {
	stray := sortedKeys(goModToolDirectives(string(rootMod)))
	if len(stray) == 0 {
		return "", nil
	}

	if err := writeFile(root, goModFile, stripGoModToolDirectives(string(rootMod))); err != nil {
		advisory := goToolsAdvisory(goModFile, "could not rewrite go.mod: "+err.Error(), nil)

		return "", &advisory
	}

	if out := runGo(ctx, root, "mod", "tidy"); out != "" {
		advisory := goToolsAdvisory(goModFile, out, nil)

		return "", &advisory
	}

	return "moved tool directive(s) for " + strings.Join(stray, ", ") + " out of " + goModFile, nil
}

// goToolsGet moves the directives for pkgs to version in the tools module —
// "latest" adds them, "none" removes them — then tidies. The step done reads
// prefix, the packages, suffix; when a go step fails the advisory carries
// failure (the check's wording) and the command to run by hand.
func goToolsGet(
	ctx context.Context,
	toolsRoot string,
	pkgs []string,
	version, failure, prefix, suffix string,
) (string, *Outcome) {
	getArgs := []string{goGetVerb, goToolFlag}
	for _, pkg := range pkgs {
		getArgs = append(getArgs, pkg+"@"+version)
	}

	if out := goGetThenTidy(ctx, toolsRoot, getArgs); out != "" {
		advisory := goToolsAdvisory(goToolsModFile, failure+"; "+out, getArgs)

		return "", &advisory
	}

	return prefix + strings.Join(pkgs, listSeparator) + suffix, nil
}

// remediateIsolatedGoTool seeds one isolated tool module and pins its
// directive at the latest release; nil on success, the advisory otherwise.
func remediateIsolatedGoTool(ctx context.Context, root string, rootMod []byte, name string) *Outcome {
	modFile := isolatedGoModFile(name)
	pkg := isolatedGoTools[name]

	if _, err := readRepoFile(root, modFile); err != nil {
		seed := bareGoMod(string(rootMod), aquaGoDirective(root), goToolsDir+slash+name,
			"// "+name+"'s own module: its dependency graph stays exactly its upstream's,\n"+
				"// which a shared tools module could not promise (book/tooling.md).\n")
		if err := writeFile(root, modFile, seed); err != nil {
			advisory := goToolsAdvisory(modFile, "could not write "+modFile+": "+err.Error(), nil)

			return &advisory
		}
	}

	getArgs := []string{goGetVerb, goToolFlag, pkg + "@latest"}
	if out := goGetThenTidy(ctx, filepath.Join(root, goToolsDir, name), getArgs); out != "" {
		advisory := goToolsAdvisory(modFile, isolatedGoToolsMessage([]string{name})+"; "+out, getArgs)

		return &advisory
	}

	return nil
}

// bareToolsGoMod is the tools module before any directive. In a Go repository
// (rootMod non-empty) the module path is the root's with /tools appended and
// the go directive is the root's, so the tools are built for the same
// language version as the project; elsewhere the module is `tools` and the
// go directive is the caller's (the aqua-pinned go, see aquaGoDirective).
func bareToolsGoMod(rootMod, goDirective string) string {
	return bareGoMod(rootMod, goDirective, goToolsDir,
		"// The Go-built tools the shared recipes run, pinned as tool directives\n"+
			"// in a module of their own so their dependency graph never reaches the\n"+
			"// project's go.mod (book/tooling.md).\n")
}

// bareGoMod is a tools module at relDir before any directive, opened by the
// given comment: the root module's path with relDir appended and the root's
// go directive in a Go repository, else `<relDir's last element>` and the
// caller's go directive.
func bareGoMod(rootMod, goDirective, relDir, comment string) string {
	module, goLine := relDir[strings.LastIndex(relDir, slash)+1:], goDirective

	for raw := range strings.SplitSeq(rootMod, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, moduleDirective):
			module = strings.TrimSpace(strings.TrimPrefix(line, moduleDirective)) + slash + relDir
		case strings.HasPrefix(line, "go "):
			goLine = line
		default:
			// other directives: not part of the seed
		}
	}

	return comment + moduleDirective + module + "\n\n" + goLine + "\n"
}

// aquaGoDirective is the go directive matching the repository's aqua golang/go
// pin — the toolchain that will build the tools, so the seeded module can
// never ask for a newer go than the one on the hermetic PATH. Without a
// manifest or a pin, the fallback.
func aquaGoDirective(root string) string {
	manifest, err := readRepoFile(root, "aqua.yaml")
	if err != nil {
		return fallbackGoDirective
	}

	m := aquaGoPin.FindStringSubmatch(string(manifest))
	if m == nil {
		return fallbackGoDirective
	}

	return "go " + m[1]
}

// runGo runs the pinned go with args in dir and returns "" on success, or a
// one-line description of the failure.
func runGo(ctx context.Context, dir string, args ...string) string {
	// The pinned go on the hermetic PATH; args are fixed lists plus baseline
	// package paths.
	cmd := exec.CommandContext(ctx, "go", args...) // #nosec G204 -- see above.
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

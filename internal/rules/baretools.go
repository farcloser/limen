package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The baretools rule: the Go-built tools are run as build/tools/<name>, where
// the recipes build them from the tools modules; nothing puts one on PATH. A
// bare `golangci-lint` in the project's own .justfile, a script or a workflow
// runs whatever PATH holds instead: on a laptop, a stale aqua shim another
// project left in the shared bin directory, which aqua resolves against this
// repository's manifest and refuses (its code 004); on a clean machine,
// nothing. The line works by accident or not at all, so the rule fails it. The
// canonical .limen/ recipes are not scanned: they are content-pinned.
const (
	ruleBareTools = "baretools"
	workflowsDir  = ".github/workflows"
	actionsDir    = ".github/actions"
	hackDir       = "hack"
	scriptsDir    = "scripts"

	bareToolsPassMessage = "no Go-built tool is run from PATH in " + justfileName + ", " + workflowsDir +
		", " + actionsDir + ", " + hackDir + " or " + scriptsDir
	bareToolsAdvice = " — the recipes build the Go-built tools into build/tools/ and run that binary;" +
		" nothing puts one on PATH, and a bare name finds a stale aqua shim at best:" +
		" run `just do lint go` or `just do fix go`, or build/tools/<name> after one of them" +
		" (see book/tooling.md, \"Go-built tools are go.mod tools\")"
)

// bareToolDirs are the directories whose files the rule scans, every file,
// recursively: the project's own scripts, workflows and composite actions.
//
//nolint:gochecknoglobals // immutable baseline data.
var bareToolDirs = []string{workflowsDir, actionsDir, hackDir, scriptsDir}

// majorSuffix is a module path's major-version element (`/v2`), which a
// binary's name never carries.
var majorSuffix = regexp.MustCompile(`^v\d+$`)

// goToolNames are the names the Go-built tools build as, sorted: the last
// element of each package path, its major-version element skipped, and the
// isolated tools by their keys.
func goToolNames() []string {
	names := make([]string, 0, len(goToolsEverywhere)+len(goSourceAnalyzers)+len(isolatedGoTools))

	for _, pkg := range slices.Concat(goToolsEverywhere, goSourceAnalyzers) {
		elems := strings.Split(pkg, slash)

		name := elems[len(elems)-1]
		if majorSuffix.MatchString(name) && len(elems) > 1 {
			name = elems[len(elems)-2]
		}

		names = append(names, name)
	}

	for name := range isolatedGoTools {
		names = append(names, name)
	}

	slices.Sort(names)

	return slices.Compact(names)
}

// bareToolCommand matches a Go-built tool's name in command position: opening
// the line (after indentation and just's `@` or `-` prefix), after a shell
// separator, a `!` or a `{`, after a shell word that takes a command (`if`,
// `exec`, `time`, `xargs`, …), or as a workflow step's `run:` value. A name
// after any other word, a slash or a dash is an argument, a path or another
// word, and is not matched; so is one behind `env`'s assignments.
var bareToolCommand = regexp.MustCompile(
	`(?:^[ \t]*[@-]?|[;&|(!{` + "`" + `][ \t]*|\b(?:if|elif|then|else|do|exec|env|command|time|nice|xargs)[ \t]+|\brun:[ \t]*)(` +
		strings.Join(
			goToolNames(),
			"|",
		) + `)(?:[ \t]|$)`,
)

// bareToolHit is one line that runs a Go-built tool from PATH.
type bareToolHit struct {
	path string // repository-relative, slash-separated
	line int    // 1-based
	name string
}

// String is the hit as the message lists it.
func (h bareToolHit) String() string {
	return h.path + ":" + strconv.Itoa(h.line) + " runs `" + h.name + "` from PATH"
}

// bareToolLines finds the hits in one file's text: comment lines and trailing
// comments are skipped, since a name there runs nothing.
func bareToolLines(relPath, text string) []bareToolHit {
	var hits []bareToolHit

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSuffix(raw, carriageReturn)
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		if code, _, found := strings.Cut(line, " #"); found {
			line = code
		}

		for _, m := range bareToolCommand.FindAllStringSubmatch(line, -1) {
			hits = append(hits, bareToolHit{path: relPath, line: i + 1, name: m[1]})
		}
	}

	return hits
}

// bareToolsScan lists the files the rule reads, repository-relative with
// slashes, in a stable order: the root justfile, then every file under the
// scanned directories.
func bareToolsScan(root string) []string {
	files := []string{justfileName}

	for _, dir := range bareToolDirs {
		// An absent directory is nothing to scan: the error ends its walk.
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err == nil {
				files = append(files, filepath.ToSlash(rel))
			}

			return nil
		})
	}

	return files
}

// bareTools finds every line, across the scanned files, that runs a Go-built
// tool from PATH.
func bareTools(root string) []bareToolHit {
	var hits []bareToolHit

	for _, rel := range bareToolsScan(root) {
		// #nosec G304 -- the repository's own files, enumerated by the rule under root.
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}

		hits = append(hits, bareToolLines(rel, string(data))...)
	}

	return hits
}

// bareToolsMessage lists the hits and what to do about them.
func bareToolsMessage(hits []bareToolHit) string {
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, h.String())
	}

	return strings.Join(lines, listSeparator) + bareToolsAdvice
}

// checkBareTools evaluates the rule.
func checkBareTools(root string) Finding {
	hits := bareTools(root)
	if len(hits) > 0 {
		return fail(ruleBareTools, hits[0].path, bareToolsMessage(hits))
	}

	return Finding{Rule: ruleBareTools, Status: StatusOK, Path: justfileName, Message: bareToolsPassMessage}
}

// remediateBareTools reports the hits as an advisory: the line is the
// project's own and a human rewrites it.
func remediateBareTools(root string) Outcome {
	hits := bareTools(root)
	if len(hits) > 0 {
		return Outcome{Rule: ruleBareTools, Action: ActionAdvisory, Path: hits[0].path, Message: bareToolsMessage(hits)}
	}

	return Outcome{Rule: ruleBareTools, Action: ActionNone, Path: justfileName, Message: bareToolsPassMessage}
}

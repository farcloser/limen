package rules

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// commandsDir is where a Go repository keeps its main packages, released or
// not. The release configuration's builds say which of them ship; the rest
// are declared under commands.unshipped in the lint overlay, one command per
// entry, so a main nobody can fetch and nobody declared stays red.
const (
	commandsDir  = "cmd"
	keyCommands  = "commands"
	keyUnshipped = "unshipped"
	keyBuilds    = "builds"
	keyMain      = "main"
	keyDir       = "dir"
)

// packageMain matches a Go file's package clause when it is main, past any
// leading comments and build constraints.
var packageMain = regexp.MustCompile(`(?m)^package\s+main\b`)

// commandsVerdict is what the rule has to say about cmd/: the commands no
// build ships and no entry declares, and the unshipped entries that name
// no such command (a main that ships, a directory, a wildcard, a command
// since removed). Both empty is a pass.
type commandsVerdict struct {
	undeclared []string
	stale      []string
}

// commandProblems reads cmd/, the release configuration's builds and the
// overlay's unshipped list; empty when the repository is not a Go module or
// has no main package under cmd/ and no unshipped entry.
func commandProblems(root string) commandsVerdict {
	if !exists(filepath.Join(root, "go.mod")) {
		return commandsVerdict{}
	}

	mains := mainCommands(root)
	shipped := releasedMains(root)
	declared := declaredUnshipped(root)

	var verdict commandsVerdict

	for _, command := range mains {
		if !slices.Contains(shipped, command) && !slices.Contains(declared, command) {
			verdict.undeclared = append(verdict.undeclared, command)
		}
	}

	for _, entry := range declared {
		if !slices.Contains(mains, entry) || slices.Contains(shipped, entry) {
			verdict.stale = append(verdict.stale, entry)
		}
	}

	return verdict
}

// mainCommands lists the main packages directly under cmd/, as cmd/<name>,
// sorted.
func mainCommands(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, commandsDir))
	if err != nil {
		return nil
	}

	var commands []string

	for _, entry := range entries {
		if entry.IsDir() && isMainPackage(filepath.Join(root, commandsDir, entry.Name())) {
			commands = append(commands, commandsDir+"/"+entry.Name())
		}
	}

	sort.Strings(commands)

	return commands
}

// isMainPackage reports whether any non-test Go file directly in dir declares
// package main.
func isMainPackage(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		data, err := os.ReadFile(file) // #nosec G304 -- a Go file under the caller-designated repository.
		if err == nil && packageMain.Match(data) {
			return true
		}
	}

	return false
}

// releasedMains is every package the release configuration builds, as a
// cleaned path from the repository root: goreleaser runs each build in its
// dir (the root by default) and compiles its main (the current directory
// by default, or a main.go file, whose package is its directory) from there.
func releasedMains(root string) []string {
	data, err := readRepoFile(root, releaseGoFile)
	if err != nil {
		return nil
	}

	var mains []string

	for _, item := range yamlItems(yamlBlock(yamlLines(data), keyBuilds)) {
		dir := yamlScalar(item, keyDir)
		if dir == "" {
			dir = "."
		}

		main := yamlScalar(item, keyMain)
		if main == "" {
			main = "."
		}

		if path.Ext(main) == ".go" {
			main = path.Dir(main)
		}

		mains = append(mains, path.Clean(path.Join(dir, main)))
	}

	return mains
}

// declaredUnshipped is the overlay's commands.unshipped list, each entry
// cleaned (./cmd/x and cmd/x are the same command), in the file's order.
func declaredUnshipped(root string) []string {
	data, err := readRepoFile(root, lintGoOverlay)
	if err != nil {
		return nil
	}

	var entries []string

	for _, entry := range yamlList(yamlBlock(yamlLines(data), keyCommands), keyUnshipped) {
		entries = append(entries, path.Clean(entry))
	}

	return entries
}

// The YAML the rule reads, without a parser: limen's module has no
// dependency, and the two files are hand-written in the block form their
// seeds and goreleaser's documentation use (a key, its nested lines indented
// below it, list items as `- `). What this subset does not read, it does not
// read in the safe direction: a build or an entry written another way is
// one the rule does not see, so the command it names is undeclared and the
// finding names it, never silently passed. The one flow form it does read is
// `unshipped: [./cmd/a, ./cmd/b]`, the shape a one-line declaration takes.

// yamlLines is data's lines with trailing space dropped, less the blank ones
// and the comments.
func yamlLines(data []byte) []string {
	var lines []string

	for raw := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			lines = append(lines, line)
		}
	}

	return lines
}

// yamlIndent is the number of leading spaces of line.
func yamlIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// yamlBlock is the lines nested under the first `key:` found at the
// shallowest indentation of lines: the deeper lines that follow it, and the
// list items at its own indentation, which YAML allows. The key's inline
// value, when it has one, is returned as a single line of its own, so a
// flow list reads through yamlList like a block one.
func yamlBlock(lines []string, key string) []string {
	depth := yamlShallowest(lines)

	for i, line := range lines {
		if yamlIndent(line) != depth {
			continue
		}

		name, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found || name != key {
			continue
		}

		var block []string

		if value = strings.TrimSpace(yamlUncomment(value)); value != "" {
			block = append(block, strings.Repeat(" ", depth+1)+value)
		}

		for _, next := range lines[i+1:] {
			nextDepth := yamlIndent(next)
			if nextDepth < depth || (nextDepth == depth && !strings.HasPrefix(strings.TrimSpace(next), "- ")) {
				break
			}

			block = append(block, next)
		}

		return block
	}

	return nil
}

// yamlShallowest is the smallest indentation among lines.
func yamlShallowest(lines []string) int {
	depth := -1

	for _, line := range lines {
		if indent := yamlIndent(line); depth < 0 || indent < depth {
			depth = indent
		}
	}

	return depth
}

// yamlItems splits a block that is a list of mappings into its items, each
// item's lines with the `- ` of its first line turned into indentation so
// the item reads as a mapping of its own.
func yamlItems(block []string) [][]string {
	depth := yamlShallowest(block)

	var items [][]string

	for _, line := range block {
		if yamlIndent(line) == depth && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			items = append(items, []string{strings.Replace(line, "- ", "  ", 1)})

			continue
		}

		if len(items) > 0 {
			items[len(items)-1] = append(items[len(items)-1], line)
		}
	}

	return items
}

// yamlScalar is the value of `key:` at the shallowest indentation of lines,
// unquoted; "" when absent.
func yamlScalar(lines []string, key string) string {
	depth := yamlShallowest(lines)

	for _, line := range lines {
		if yamlIndent(line) != depth {
			continue
		}

		if name, value, found := strings.Cut(strings.TrimSpace(line), ":"); found && name == key {
			return yamlUnquote(yamlUncomment(value))
		}
	}

	return ""
}

// yamlList is the entries of the list under `key:` in lines: block items,
// or one flow list `[a, b]`.
func yamlList(lines []string, key string) []string {
	block := yamlBlock(lines, key)

	var entries []string

	for _, line := range block {
		item := strings.TrimSpace(line)

		if flow, isFlow := strings.CutPrefix(item, "["); isFlow {
			for entry := range strings.SplitSeq(strings.TrimSuffix(yamlUncomment(flow), "]"), ",") {
				if entry = yamlUnquote(entry); entry != "" {
					entries = append(entries, entry)
				}
			}

			continue
		}

		if value, isItem := strings.CutPrefix(item, "- "); isItem {
			entries = append(entries, yamlUnquote(yamlUncomment(value)))
		}
	}

	return entries
}

// yamlUncomment drops a trailing ` # comment` from a value.
func yamlUncomment(value string) string {
	before, _, _ := strings.Cut(value, " #")

	return before
}

// yamlUnquote trims a value and its surrounding quotes, single or double.
func yamlUnquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}

	return value
}

// checkReleasedCommands fails a Go repository with a main package under cmd/
// that neither a build in the release configuration ships nor the overlay
// declares unshipped, or with an unshipped entry naming no such command.
func checkReleasedCommands(root, rule string) *Finding {
	verdict := commandProblems(root)
	if len(verdict.undeclared) == 0 && len(verdict.stale) == 0 {
		return nil
	}

	f := fail(rule, commandsPath(verdict), commandsMessage(verdict))

	return &f
}

// remediateReleasedCommands reports the same: the release configuration and
// the overlay are the project's to write.
func remediateReleasedCommands(root, rule string) Outcome {
	verdict := commandProblems(root)
	if len(verdict.undeclared) == 0 && len(verdict.stale) == 0 {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Message: "every command under " + commandsDir + "/ ships or is declared unshipped, or there is none",
		}
	}

	return Outcome{Rule: rule, Action: ActionAdvisory, Path: commandsPath(verdict), Message: commandsMessage(verdict)}
}

// commandsPath is the one path a finding carries: the first undeclared
// command, else the overlay that holds the stale entry.
func commandsPath(verdict commandsVerdict) string {
	if len(verdict.undeclared) > 0 {
		return verdict.undeclared[0]
	}

	return lintGoOverlay
}

func commandsMessage(verdict commandsVerdict) string {
	var parts []string

	if len(verdict.undeclared) > 0 {
		parts = append(parts, strings.Join(verdict.undeclared, ", ")+": a command under "+commandsDir+
			"/ either ships or says it does not; add it to "+releaseGoFile+"'s "+keyBuilds+
			" (limen fix then seeds the release workflow), or name it under "+keyCommands+"."+keyUnshipped+
			" in "+lintGoOverlay+", one command per entry (book/releasing.md)")
	}

	if len(verdict.stale) > 0 {
		parts = append(parts, strings.Join(verdict.stale, ", ")+": named under "+keyCommands+"."+keyUnshipped+
			" in "+lintGoOverlay+" but not a main package under "+commandsDir+"/ that no build in "+
			releaseGoFile+" ships; an entry is one such command, never a directory or a pattern: drop it")
	}

	return strings.Join(parts, "; ")
}

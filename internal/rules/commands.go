package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// commandsDir is where a Go repository's products live: a main package under
// it is a command this repository builds for others, and a binary nobody can
// fetch is consumed by building it from source, which decides where it can
// live in the tool layout. A helper that is not a product lives elsewhere
// (internal/cmd/, hack/, examples/<name>/).
const commandsDir = "cmd"

// packageMain matches a Go file's package clause when it is main, past any
// leading comments and build constraints.
var packageMain = regexp.MustCompile(`(?m)^package\s+main\b`)

// unreleasedCommands lists the commands under cmd/ of a Go repository that
// carries no .release-go.yaml, as cmd/<name>; none when the repository is not
// a Go module, releases, or has no main package there.
func unreleasedCommands(root string) []string {
	if !exists(filepath.Join(root, "go.mod")) || exists(filepath.Join(root, releaseGoFile)) {
		return nil
	}

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

// checkReleasedCommands fails a Go repository that builds a command under
// cmd/ and releases nothing.
func checkReleasedCommands(root, rule string) *Finding {
	commands := unreleasedCommands(root)
	if len(commands) == 0 {
		return nil
	}

	f := fail(rule, commands[0], releasedCommandsMessage(commands))

	return &f
}

// remediateReleasedCommands reports the same: the release configuration is
// the project's to write.
func remediateReleasedCommands(root, rule string) Outcome {
	commands := unreleasedCommands(root)
	if len(commands) == 0 {
		return Outcome{
			Rule:    rule,
			Action:  ActionNone,
			Message: "every command under " + commandsDir + "/ is released, or there is none",
		}
	}

	return Outcome{Rule: rule, Action: ActionAdvisory, Path: commands[0], Message: releasedCommandsMessage(commands)}
}

func releasedCommandsMessage(commands []string) string {
	return strings.Join(commands, ", ") + ": a command under " + commandsDir + "/ is a product and ships as a binary " +
		"release; write " + releaseGoFile + " (limen fix then seeds the release workflow), or move a helper that is " +
		"not a product out of " + commandsDir + "/ (book/releasing.md)"
}

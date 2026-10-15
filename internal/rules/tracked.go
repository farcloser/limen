package rules

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const (
	trackedRule = "tracked"
	// gitTimeout bounds the one git call: a listing, instant on any tree.
	gitTimeout = 30 * time.Second
)

// canonicalRoots is where every canonical file lives. A pattern in the
// project's .gitignore that swallows one hides it from every commit while every
// local lane, reading the disk, passes.
//
//nolint:gochecknoglobals // immutable table.
var canonicalRoots = []string{
	".limen", ".aqua", ".github", "AGENTS.md", "CLAUDE.md", ".editorconfig", ".gitattributes",
	".justfile", ".lint-go.yaml", ".release-go.yaml", "renovate.json", "pins.yaml",
}

// ignoredCanonical lists the files under the canonical roots that git ignores
// and does not track, by the tree's own .gitignore files alone: a global
// exclude would make the answer differ per machine. A tree git cannot answer
// for yields none: the git rule speaks to a missing repository.
func ignoredCanonical(ctx context.Context, root string) []string {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	args := append(
		[]string{"ls-files", "--others", "--ignored", "--exclude-per-directory=.gitignore", "--"},
		canonicalRoots...,
	)
	cmd := exec.CommandContext(
		ctx,
		"git",
		args...,
	) // #nosec G204 -- git, a fixed option list and the canonical paths table.
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var ignored []string

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			ignored = append(ignored, line)
		}
	}

	return ignored
}

// checkTracked fails when a canonical file is ignored by the tree's own
// .gitignore: it would pass every lane here and never be committed.
func checkTracked(root string) Finding {
	// Check carries no context (see remediateGit); the call is bounded on its own.
	ignored := ignoredCanonical(context.Background(), root)
	if len(ignored) == 0 {
		return Finding{Rule: trackedRule, Status: StatusOK, Message: "no canonical file is ignored by git"}
	}

	return fail(trackedRule, ignored[0], trackedMessage(ignored))
}

// remediateTracked reports the same: the pattern is the project's own, so the
// fixer names it and leaves the edit to a hand.
func remediateTracked(ctx context.Context, root string) Outcome {
	ignored := ignoredCanonical(ctx, root)
	if len(ignored) == 0 {
		return Outcome{Rule: trackedRule, Action: ActionNone, Message: "no canonical file is ignored by git"}
	}

	return Outcome{Rule: trackedRule, Action: ActionAdvisory, Path: ignored[0], Message: trackedMessage(ignored)}
}

func trackedMessage(ignored []string) string {
	return "ignored by a .gitignore pattern, so never committed: " + strings.Join(ignored, ", ") +
		" (narrow or remove the pattern; see book/mandatory-files.md)"
}

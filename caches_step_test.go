package limen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The cache budget's keeper deletes entries, and nothing in the repository
// runs it but a workflow on the default branch. This test lifts its step's
// script out of the workflow file and runs it against a listing made for the
// purpose, through a stand-in gh that answers the three calls the step makes
// and records what it was told to delete.

const cachesWorkflow = ".github/workflows/limen-caches.yaml"

// fakeGH answers `gh api repos/<repo>` with the default branch, `gh cache
// list` with the listing in GH_LISTING as the step's own --jq would print it,
// and `gh cache delete <id>` by writing the id to GH_DELETED.
const fakeGH = `#!/bin/sh
case "$1 $2" in
"api repos/"*) echo main ;;
"cache list") cat "$GH_LISTING" ;;
"cache delete") echo "$3" >> "$GH_DELETED" ;;
*) echo "gh: unexpected call: $*" >&2; exit 1 ;;
esac
`

// TestCachePruneKeepsOneGenerationAFamily: of the entries of one family, the
// newest stays and the others go, for the canonical prefixes and for a
// project's own under `project-`; an entry under any other name is left
// alone, and so is one that shares a key and caches other paths.
func TestCachePruneKeepsOneGenerationAFamily(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the prune runs on Linux")
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatalf("the step is a bash script: %v", err)
	}

	newer, older := strings.Repeat("b", 64), strings.Repeat("a", 64)

	// id, key, version, size, newest first, as `gh cache list` sorts them.
	// The entries whose id starts with "gone" are the ones to delete.
	listing := [][]string{
		{"kept-aqua", "aqua-pkgs-Linux-X64-" + newer, "v1"},
		{"gone-aqua", "aqua-pkgs-Linux-X64-" + older, "v1"},
		{"kept-go", "go-Linux-X64-go1.2.4-" + newer + "-20261125", "v2"},
		{"gone-go", "go-Linux-X64-go1.2.3-" + older + "-20261124", "v2"},
		// A project's own, one family a build group.
		{"kept-project-zig", "project-downloads-Linux-X64-zig-" + newer, "v3"},
		{"gone-project-zig", "project-downloads-Linux-X64-zig-" + older, "v3"},
		{"kept-project-ghc", "project-downloads-Linux-X64-ghc-" + older, "v3"},
		// The same key over other paths is another family, not an older
		// generation.
		{"kept-project-zig-other-paths", "project-downloads-Linux-X64-zig-" + older, "v4"},
		// Under no prefix the prune knows: not its to judge, however stale.
		{"kept-unknown-new", "zig-global-Linux-X64-0.2.0-zig", "v5"},
		{"kept-unknown-old", "zig-global-Linux-X64-0.1.0-zig", "v5"},
		{"kept-unknown-hash-new", "downloads-Linux-X64-" + newer, "v6"},
		{"kept-unknown-hash-old", "downloads-Linux-X64-" + older, "v6"},
	}

	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	listed := filepath.Join(dir, "listing")
	deleted := filepath.Join(dir, "deleted")

	var lines strings.Builder

	for _, entry := range listing {
		lines.WriteString(strings.Join(append(entry, "1000000"), "\t") + "\n")
	}

	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		filepath.Join(tools, "gh"): fakeGH,
		listed:                     lines.String(),
		deleted:                    "",
	} {
		if err := os.WriteFile(name, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	script := liftStepFrom(t, cachesWorkflow, "prune", "name: One generation per key on the default branch")

	cmd := exec.CommandContext(t.Context(), "bash", "-euo", "pipefail", "-c", script)

	cmd.Env = append(os.Environ(),
		"PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_REPO=example/repository", "GH_LISTING="+listed, "GH_DELETED="+deleted)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the prune step failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(deleted)
	if err != nil {
		t.Fatal(err)
	}

	gone := strings.Fields(string(got))
	slices.Sort(gone)

	var want []string

	for _, entry := range listing {
		if strings.HasPrefix(entry[0], "gone-") {
			want = append(want, entry[0])
		}
	}

	slices.Sort(want)

	if !slices.Equal(gone, want) {
		t.Errorf("the prune deleted %v, want %v\n%s", gone, want, out)
	}
}

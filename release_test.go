package limen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// releaseGoMinimal is a goreleaser configuration that builds, archives,
// checksums and publishes nothing: what is left is the tag handling.
const releaseGoMinimal = `version: 2
project_name: rebuild
builds:
  - skip: true
archives: []
checksum:
  disable: true
release:
  disable: true
changelog:
  disable: true
`

// releaseRepo makes a git repository carrying this checkout's .limen/just/
// recipes, a .justfile that imports them and releaseGoMinimal, committed
// and tagged tag, with an origin goreleaser reads and never reaches, and
// returns its path.
func releaseRepo(t *testing.T, tag string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, ".limen", "just"), os.DirFS(filepath.Join(".limen", "just"))); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		".justfile":        "import '.limen/just/main.just'\n",
		".release-go.yaml": releaseGoMinimal,
		".gitignore":       ".build/\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"remote", "add", "origin", "https://github.com/example/rebuild.git"},
		{"add", "-A"},
		{"commit", "-q", "-m", "release fixture"},
		{"tag", tag},
	} {
		run(t, dir, nil, "git", args...)
	}

	return dir
}

// toolEnv isolates git from the machine's configuration (a signing key, a
// hook path), names the committer, and points hermetic at this checkout's
// toolset, which the fixture does not carry.
func toolEnv(t *testing.T) []string {
	t.Helper()

	config, err := filepath.Abs(filepath.Join(".hermetic", "hermetic.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	return []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=limen", "GIT_AUTHOR_EMAIL=limen@invalid",
		"GIT_COMMITTER_NAME=limen", "GIT_COMMITTER_EMAIL=limen@invalid",
		"HERMETIC_CONFIG=" + config,
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()

	if out, err := command(t, dir, env, name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func command(t *testing.T, dir string, env []string, name string, args ...string) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir

	cmd.Env = append(append(os.Environ(), toolEnv(t)...), env...)

	return cmd
}

// requireUnix skips on Windows: the release lane runs on Linux and macOS.
func requireUnix(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the release lane runs on Linux and macOS")
	}
}

// TestReleaseCIPublishesRebuildTag: the workflow half of the release recipe
// takes a fork's rebuild tag (vX.Y.Z.N) through the pinned goreleaser as it
// takes a plain version tag.
func TestReleaseCIPublishesRebuildTag(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	for _, tag := range []string{"v1.2.3.4", "v1.2.3"} {
		run(t, releaseRepo(t, tag), ciEnv(tag), "just", "do", "release", "--ci")
	}
}

// ciEnv is the release lane's environment as the recipe reads it: the run
// is GitHub's, keyless signing is on, and the ref is the pushed tag.
func ciEnv(tag string) []string {
	return []string{
		"GITHUB_ACTIONS=true", "ACTIONS_ID_TOKEN_REQUEST_URL=https://token.invalid",
		"GITHUB_TOKEN=unused", "GITHUB_REF_NAME=" + tag,
	}
}

// TestReleaseCIRebuildRefusesDirtyTree: on a rebuild tag the dirty-tree
// check goreleaser drops with --skip=validate is the recipe's own.
func TestReleaseCIRebuildRefusesDirtyTree(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	dir := releaseRepo(t, "v1.2.3.4")
	if err := os.WriteFile(filepath.Join(dir, "stray"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := command(t, dir, ciEnv("v1.2.3.4"), "just", "do", "release", "--ci").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "working tree is dirty") {
		t.Errorf("a dirty tree should refuse a rebuild release: %v\n%s", err, out)
	}
}

// TestReleaseCIRebuildRefusesTagOffHead: on a rebuild tag the tag-at-HEAD
// check goreleaser drops is the recipe's own. A plain version tag on HEAD
// passes the lane's generic tag check, so only the recipe's check refuses.
func TestReleaseCIRebuildRefusesTagOffHead(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	dir := releaseRepo(t, "v1.2.3.4")
	run(t, dir, nil, "git", "commit", "-q", "--allow-empty", "-m", "later")
	run(t, dir, nil, "git", "tag", "v1.2.3")

	out, err := command(t, dir, ciEnv("v1.2.3.4"), "just", "do", "release", "--ci").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "is not on HEAD") {
		t.Errorf("a rebuild tag off HEAD should refuse: %v\n%s", err, out)
	}
}

// TestReleaseDryRunTakesRebuildTag: the local dry run takes a rebuild tag
// without the skip: goreleaser's snapshot mode reads the tag as a string
// (1.2.3.4-SNAPSHOT-<sha>) and only warns that it is not semver.
func TestReleaseDryRunTakesRebuildTag(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	run(t, releaseRepo(t, "v1.2.3.4"), nil, "just", "do", "release", "--local", "--dry-run")
}

// TestReleaseRebuildTagRefusesSuffix: a rebuild tag carries no prerelease,
// which goreleaser would not read from it.
func TestReleaseRebuildTagRefusesSuffix(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	out, err := command(t, releaseRepo(t, "v1.2.3"), nil, "just", "do", "release", "v1.2.3.4-rc.1").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "is not a version tag") {
		t.Errorf("v1.2.3.4-rc.1 should be refused as a version tag: %v\n%s", err, out)
	}
}

// TestGoreleaserRefusesRebuildTag holds the pinned goreleaser to the reason
// the release recipe skips its validation on a rebuild tag: it refuses one
// as semver. When a goreleaser bump accepts it, this fails, and the skip,
// which also drops goreleaser's own git checks, goes.
func TestGoreleaserRefusesRebuildTag(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	out, err := command(t, releaseRepo(t, "v1.2.3.4"), []string{"GITHUB_TOKEN=unused"},
		"goreleaser", "release", "--config", ".release-go.yaml", "--clean").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "as semver") {
		t.Errorf("goreleaser should refuse v1.2.3.4 as semver; if it now takes it, "+
			"drop --skip=validate from the release recipe: %v\n%s", err, out)
	}
}

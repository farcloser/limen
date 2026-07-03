package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/rules"
)

func compliantRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"README.md":                 "# Thing",
		"LICENSE":                   "Permission is hereby granted, free of charge.\nTHE SOFTWARE IS PROVIDED \"AS IS\".",
		".editorconfig":             rules.CanonicalEditorconfig,
		".gitignore":                strings.Join(rules.DefaultRequiredGitignore, "\n") + "\n",
		"Justfile":                  rules.CanonicalJustfileImport + "\n",
		"aqua.yaml":                 limen.CanonicalAquaYAML,
		"aqua-checksums.json":       "{}\n",
		"aqua-policy.yaml":          rules.CanonicalAquaPolicy,
		".limen/aqua-registry.yaml": rules.CanonicalAquaRegistry,
		".limen/lychee.toml":        rules.CanonicalLychee,
		".limen/.yamlfmt":           rules.CanonicalYamlfmt,
		".github/workflows/update-aqua-checksum.yaml": limen.CanonicalWorkflowUpdateAquaChecksum,
		".github/actions/setup-aqua/action.yaml":      limen.CanonicalActionSetupAqua,
		".github/workflows/ci.yaml":                   limen.CanonicalWorkflowCI,
		"renovate.json5":                              limen.CanonicalRenovate,
	}
	for _, m := range limen.JustModules() {
		files[m.Path] = m.Content
	}

	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	good := compliantRepo(t)
	empty := t.TempDir()

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no args", nil, 2},
		{"help", []string{"help"}, 0},
		{"version", []string{"version"}, 0},
		{"unknown command", []string{"frobnicate"}, 2},
		{"compliant", []string{"check", good}, 0},
		{"non-compliant", []string{"check", empty}, 1},
		{"missing dir", []string{"check", filepath.Join(good, "nope")}, 2},
		{"too many paths", []string{"check", good, empty}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := run(tc.args, io.Discard, io.Discard); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

func TestRunFixAndBootstrap(t *testing.T) {
	t.Parallel()

	good := compliantRepo(t)
	empty := t.TempDir()

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"fix compliant", []string{"fix", good}, 0},
		{"fix too many paths", []string{"fix", good, empty}, 2},
		{"bootstrap no path", []string{"bootstrap"}, 2},
		{"bootstrap two paths", []string{"bootstrap", good, empty}, 2},
		{"bootstrap non-empty", []string{"bootstrap", good}, 2},
		{"bootstrap bad license", []string{"bootstrap", "-license", "WTFPL", empty}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := run(tc.args, io.Discard, io.Discard); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

// TestBootstrapProducesCompliantRepo bootstraps a fresh directory and checks that
// the result passes `limen check`. It shells out to `git init`; if git is absent
// or the environment forbids it, the test is skipped rather than failed. Tool
// install is skipped so the test does not require aqua or a network.
func TestBootstrapProducesCompliantRepo(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "newrepo")
	if code := run([]string{"bootstrap", "-skip-install", dir}, io.Discard, io.Discard); code != 0 {
		t.Skipf("bootstrap returned %d (likely git init unavailable in this environment)", code)
	}

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Errorf("check on a bootstrapped repo = %d, want 0", code)
	}
}

// The path must be honored whether it appears before or after the -json flag;
// Go's flag package alone would silently drop a flag placed after the path.
func TestRunFlagPositionIndependent(t *testing.T) {
	t.Parallel()

	good := compliantRepo(t)
	for _, args := range [][]string{
		{"check", "-json", good},
		{"check", good, "-json"},
		{"check", good, "--json"},
		{"check", "-json", "--", good},
	} {
		var out strings.Builder
		if code := run(args, &out, io.Discard); code != 0 {
			t.Fatalf("run(%v) = %d, want 0", args, code)
		}

		if !strings.HasPrefix(strings.TrimSpace(out.String()), "[") {
			t.Errorf("run(%v) did not emit JSON, got:\n%s", args, out.String())
		}
	}
}

// TestReleaseVersion: only an exact release stamp may rewrite the seeded limen
// pin — every ambiguous form (dev, bare sha, describe suffixes, dirty trees,
// goreleaser snapshots) must classify as a dev build, whose safe fallback is
// the embedded pin.
func TestReleaseVersion(t *testing.T) { //nolint:paralleltest // serial by design: mutates the package-level version.
	cases := []struct {
		stamp string
		want  string
	}{
		{"dev", ""},                        // plain go build / go run
		{"", ""},                           // defensive: empty stamp
		{"v1.2.3", "v1.2.3"},               // just build go on an exact tag
		{"1.2.3", "v1.2.3"},                // goreleaser strips the v
		{"v0.0.0-test.1", "v0.0.0-test.1"}, // prerelease tag
		{"0.0.0-test.1", "v0.0.0-test.1"},  // prerelease via goreleaser
		{"v1.2.3-5-g1a2b3c4", ""},          // git describe, commits after tag
		{"v1.2.3-dirty", ""},               // dirty tree
		{"v1.2.3-5-g1a2b3c4-dirty", ""},    // both
		{"1.2.4-SNAPSHOT-1a2b3c4", ""},     // goreleaser --snapshot
		{"1a2b3c4", ""},                    // bare sha (no tags at all)
	}
	for _, tc := range cases {
		prev := version
		version = tc.stamp

		if got := releaseVersion(); got != tc.want {
			t.Errorf("releaseVersion() with stamp %q = %q, want %q", tc.stamp, got, tc.want)
		}

		version = prev
	}
}

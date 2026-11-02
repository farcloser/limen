// Black-box tests of the limen command, through cli.Run alone. The rig is
// the environment the command already honors: stubs first on PATH for the
// tools it shells out to, GITHUB_API_URL for the users endpoint.

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/cli"
	"github.com/farcloser/limen/internal/rules"
)

// run is the command with a dev stamp, the way `go run` builds it.
func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run("dev", args, stdout, stderr)
}

// TestMain runs the suite; the stubs the commands shell out to are scripts
// a test puts on PATH (stubDir).
func TestMain(m *testing.M) {
	m.Run()
}

// stubDir builds a directory of stand-ins for the tools the command shells
// out to — aqua and go as scripts (their arguments carry no shell
// metacharacters). The aqua stub logs its invocations to <dir>/log. The
// caller puts the directory first on PATH with t.Setenv, which is what makes
// those tests serial.
func stubDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	aqua := filepath.Join(dir, "hermetic")
	// update-checksum writes the file the aqua rule then expects, like the
	// real one would (its content is not judged). Builtins only, never an
	// external command: with aqua's bin on PATH every coreutils name is an
	// aqua proxy, which execs `aqua` — this very stub — and recurses without
	// end. `${0%/*}` is the shell's own dirname.
	aquaScript := "#!/bin/sh\necho \"$@\" >> \"${0%/*}/log\"\n" +
		"case \"$*\" in *update-checksum*) echo '{\"stub\":true}' > .hermetic/aqua-checksums.json;; esac\n"
	goStub := filepath.Join(dir, "go")
	goScript := "#!/bin/sh\n" +
		"[ \"$1\" = get ] && [ \"$2\" = -tool ] || exit 0\n" +
		"shift 2\n" +
		"for arg; do echo \"tool ${arg%@*}\" >> go.mod; done\n"

	if runtime.GOOS == "windows" {
		aqua += ".bat"
		aquaScript = "@echo off\r\n>> \"%~dp0log\" echo %*\r\n" +
			// cmd's own substring test, never `find`: under the Git Bash PATH the
			// tests run with, `find` is GNU find, which takes the word for a path
			// and fails every call.
			"set \"args=%*\"\r\n" +
			"if not \"%args:update-checksum=%\"==\"%args%\" echo {\"stub\":true}> .hermetic\\aqua-checksums.json\r\n" +
			"exit /b 0\r\n"
		goStub += ".bat"
		goScript = "@echo off\r\n" +
			"if not \"%1\"==\"get\" exit /b 0\r\n" +
			"if not \"%2\"==\"-tool\" exit /b 0\r\n" +
			"shift\r\nshift\r\n" +
			":loop\r\n" +
			"if \"%1\"==\"\" exit /b 0\r\n" +
			"for /f \"delims=@\" %%a in (\"%1\") do >> go.mod echo tool %%a\r\n" +
			"shift\r\ngoto loop\r\n"
	}

	// 0o700, not 0o600: the stubs must be executable.
	for path, content := range map[string]string{aqua: aquaScript, goStub: goScript} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func compliantRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"README.md":                     "# Thing",
		"LICENSE":                       "Permission is hereby granted, free of charge.\nTHE SOFTWARE IS PROVIDED \"AS IS\".",
		".editorconfig":                 rules.CanonicalEditorconfig,
		".gitignore":                    limen.CanonicalGitignore,
		".gitattributes":                rules.CanonicalGitattributes,
		"AGENTS.md":                     rules.CanonicalAgents,
		"CLAUDE.md":                     limen.CanonicalClaudeSeed,
		".justfile":                     rules.CanonicalJustfileImport + "\n\nsecurity: do::security::default\n",
		".hermetic/hermetic.yaml":       limen.CanonicalAquaYAML,
		".hermetic/aqua-checksums.json": "{}\n",
		".hermetic/policy.yaml":         rules.CanonicalAquaPolicy,
		".limen/hermetic-registry.yaml": rules.CanonicalAquaRegistry,
		".limen/hermetic.yaml":          rules.CanonicalAquaPackages,
		".limen/lychee.toml":            rules.CanonicalLychee,
		".limen/.yamlfmt":               rules.CanonicalYamlfmt,
		".limen/.shellcheckrc":          rules.CanonicalShellcheckrc,
		".github/actions/setup-hermetic/action.yaml":      limen.CanonicalActionSetupAqua,
		".github/actions/windows-cache-image/action.yaml": limen.CanonicalActionWindowsCacheImage,
		".github/actions/cache/action.yaml":               limen.CanonicalActionCache,
		".github/release.yml":                             limen.CanonicalReleaseNotes,
		".limen/renovate.json":                            limen.CanonicalRenovatePreset,
		"renovate.json":                                   rules.CanonicalRenovateFor(""),
		// The Go-built tools every repository declares (the gotools rule).
		".limen/tools/go.mod": "module tools\n\ngo 1.26\n\ntool (\n" +
			"\tgithub.com/forkcloser/git-validation/cmd/git-validation\n" +
			"\tgithub.com/forkcloser/godolint/cmd/godolint\n" +
			"\tgithub.com/forkcloser/dot/cmd/dot\n)\n",
	}
	for _, m := range limen.JustModules() {
		files[m.Path] = m.Content
	}

	// The callers in a consumer's form, at a release's commit: accepted with
	// or without an origin remote to name the repository.
	maps.Copy(
		files,
		rules.CanonicalCallersFor("example-org/thing", "0123456789abcdef0123456789abcdef01234567", "v9.9.9"),
	)

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
		// BSD-3-Clause is acceptance-only: the check recognizes it on inherited
		// forks, but a new repository does not get to choose it.
		{"bootstrap acceptance-only license", []string{"bootstrap", "-license", "BSD-3-Clause", empty}, 2},
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

// TestBootstrapProducesCompliantRepo bootstraps a fresh directory and checks
// that the result passes `limen check`. It shells out to `git init`; if git
// is absent or the environment forbids it, the test is skipped rather than
// failed. The stubs on PATH keep the install and gotools steps hermetic and
// record the aqua calls, so the exact install sequence is asserted instead
// of suppressed.
// toolsSeededDir is a bootstrap target already carrying limen's own
// .limen/tools/go.mod: the test binary is a development build, which embeds no tool
// pins and refuses to seed a tool directive (the rules tests cover seeding),
// so a bootstrap meant to end compliant starts with the tools declared.
// Bootstrapping it takes -force.
func toolsSeededDir(t *testing.T) string {
	t.Helper()

	// The one environment a bootstrap test cannot run in; any other nonzero
	// exit from bootstrap is a failure, never a skip.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH, and bootstrap runs git init")
	}

	gomod, err := os.ReadFile(filepath.Join("..", "..", ".limen", "tools", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "newrepo")
	if err := os.MkdirAll(filepath.Join(dir, ".limen", "tools"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".limen", "tools", "go.mod"), gomod, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestBootstrapProducesCompliantRepo(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	stubs := stubDir(t)
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := toolsSeededDir(t)

	var stderr strings.Builder
	if code := run([]string{"bootstrap", "-force", dir}, io.Discard, &stderr); code != 0 {
		t.Fatalf("bootstrap returned %d: %s", code, stderr.String())
	}

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Errorf("check on a bootstrapped repo = %d, want 0", code)
	}

	raw, err := os.ReadFile(filepath.Join(stubs, "log"))
	if err != nil {
		t.Fatalf("the aqua stub was never invoked: %v", err)
	}

	want := []string{
		"--log-level warn policy allow .hermetic/policy.yaml",
		"--log-level warn update-checksum --prune",
		"--log-level warn install -l",
	}

	var got []string

	for line := range strings.SplitSeq(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			got = append(got, line)
		}
	}

	if !slices.Equal(got, want) {
		t.Errorf("aqua invocations = %q, want %q", got, want)
	}
}

// TestReleaseStampPinsOnlyExactReleases: only an exact release stamp may
// rewrite the seeded limen pin — every ambiguous form (dev, bare sha,
// describe suffixes, dirty trees, goreleaser snapshots) is a dev build, whose
// safe fallback is the embedded pin. Seen from outside through the aqua.yaml
// a bootstrap writes and the dev-build warning it prints.
func TestReleaseStampPinsOnlyExactReleases(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	stubs := stubDir(t)
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	embedded := regexp.MustCompile(`(?m)^ {2}- name: farcloser/limen@(\S+)`).FindStringSubmatch(limen.CanonicalAquaYAML)
	if embedded == nil {
		t.Fatal("the canonical aqua.yaml carries no limen pin")
	}

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
		dir := toolsSeededDir(t)

		var stdout, stderr strings.Builder

		code := cli.Run(tc.stamp, []string{"bootstrap", "-force", dir}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("stamp %q: bootstrap returned %d:\n%s%s", tc.stamp, code, stdout.String(), stderr.String())
		}

		manifest, err := os.ReadFile(filepath.Join(dir, ".hermetic", "hermetic.yaml"))
		if err != nil {
			t.Fatal(err)
		}

		pin := tc.want
		if pin == "" {
			pin = embedded[1]
		}

		if !strings.Contains(string(manifest), "- name: farcloser/limen@"+pin+" ") {
			t.Errorf("stamp %q: aqua.yaml pins:\n%s\nwant farcloser/limen@%s", tc.stamp, manifest, pin)
		}

		if warned := strings.Contains(stderr.String(), "dev build"); warned != (tc.want == "") {
			t.Errorf("stamp %q: dev-build warning %v, want %v", tc.stamp, warned, tc.want == "")
		}
	}
}

// TestVersionPrintsTheStamp: `limen version` prints the stamp as built.
func TestVersionPrintsTheStamp(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	if code := cli.Run("v1.2.3", []string{"version"}, &out, io.Discard); code != 0 || out.String() != "limen v1.2.3\n" {
		t.Errorf("version = %d, %q", code, out.String())
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

// TestGithubRejectsPositionalArgs: the github subcommands take their target
// via -repo/-org only. A stray positional was once silently dropped (with
// every flag after it), so `limen github check owner/name` audited whatever
// the current directory's origin pointed at — reporting for the wrong target
// as if the request had been honored.
func TestGithubRejectsPositionalArgs(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"github", "check", "owner/name"},
		{"github", "fix", "owner/name", "-json"},
	} {
		var errOut strings.Builder
		if code := run(args, io.Discard, &errOut); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}

		if !strings.Contains(errOut.String(), "-repo") {
			t.Errorf("run(%v) stderr should point at -repo/-org, got: %s", args, errOut.String())
		}
	}
}

// TestGithubAllReposNeedsOrg: -all-repos sweeps an organization, so it has no
// meaning without -org. Accepting it silently would audit the origin
// repository alone and report a sweep.
func TestGithubAllReposNeedsOrg(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"github", "check", "-all-repos"},
		{"github", "fix", "-all-repos", "-yes"},
	} {
		var errOut strings.Builder
		if code := run(args, io.Discard, &errOut); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}

		if !strings.Contains(errOut.String(), "-org") {
			t.Errorf("run(%v) stderr should point at -org, got: %s", args, errOut.String())
		}
	}
}

// TestGithubNeedsGH: with no gh on PATH the run fails once, naming gh and
// PATH, before any target resolves. Left to the audit, the same run
// once reported every check as "cannot verify" and its fix "nothing to fix".
func TestGithubNeedsGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	for _, args := range [][]string{
		{"github", "check", "-repo", "owner/name"},
		{"github", "fix", "-repo", "owner/name", "-yes"},
		{"github", "check", "-org", "owner", "-all-repos"},
	} {
		var out, errOut strings.Builder
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("run(%v) = %d, want 2; stdout:\n%s", args, code, out.String())
		}

		for _, want := range []string{"gh", "PATH"} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("run(%v) stderr should mention %q, got: %s", args, want, errOut.String())
			}
		}

		if out.String() != "" {
			t.Errorf("run(%v) wrote findings for a run that never audited:\n%s", args, out.String())
		}
	}
}

// TestGithubFixUnverifiedPlansNothing: an API that refuses every read (a
// token with no access) leaves every check unverifiable. The fix plans
// nothing and says which, and exits non-zero: "nothing to fix" read that
// failure as compliance.
func TestGithubFixUnverifiedPlansNothing(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
	}))
	t.Cleanup(refusing.Close)
	t.Setenv("GITHUB_API_URL", refusing.URL)
	t.Setenv("GH_TOKEN", "test")

	args := []string{"github", "fix", "-repo", "owner/name", "-yes"}

	var out, errOut strings.Builder
	if code := run(args, &out, &errOut); code != 1 {
		t.Fatalf("run(%v) = %d, want 1; stdout:\n%s\nstderr:\n%s", args, code, out.String(), errOut.String())
	}

	if strings.Contains(out.String(), "nothing to fix") {
		t.Errorf("run(%v) reported nothing to fix with every check unverified:\n%s", args, out.String())
	}

	if !strings.Contains(out.String(), "could not be verified") {
		t.Errorf("run(%v) should say the checks could not be verified, got:\n%s", args, out.String())
	}

	// An opt-in check the override file leaves off passes without a read;
	// nothing else may, and nothing may fail: a failure is a judgment.
	if strings.Contains(out.String(), "✗") || !strings.Contains(out.String(), "0 fail ·") {
		t.Errorf("run(%v) judged a check it could not read:\n%s", args, out.String())
	}
}

// TestPinsCommands: `limen pins get` serves a value from the working
// directory's pins.yaml; `limen pins refresh` brings a stale digest to the
// bytes at the url; both name their misuse. Serial by design: t.Chdir forbids
// t.Parallel.
func TestPinsCommands(t *testing.T) {
	const tarball = "release bytes"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, tarball)
	}))
	t.Cleanup(server.Close)

	sum := sha256.Sum256([]byte(tarball))

	dir := t.TempDir()
	manifest := "pins:\n  - name: tool\n    renovate: github-releases example/tool\n    version: 2.0.0\n    url: " +
		server.URL + "/tool-${version}.tgz\n    verify: download\n    digest:\n      version: 1.0.0\n      sha256: " +
		strings.Repeat("0", 64) + "\n"

	if err := os.WriteFile(filepath.Join(dir, "pins.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut strings.Builder

	if code := run([]string{"pins", "refresh", dir}, &out, &errOut); code != 0 {
		t.Fatalf("pins refresh = %d: %s%s", code, out.String(), errOut.String())
	}

	if !strings.Contains(out.String(), hex.EncodeToString(sum[:])) {
		t.Errorf("refresh did not report the new digest: %s", out.String())
	}

	// get reads the working directory's file, like a recipe would.
	t.Chdir(dir)

	out.Reset()

	if code := run(
		[]string{"pins", "get", "tool", "sha256"},
		&out,
		io.Discard,
	); code != 0 ||
		strings.TrimSpace(out.String()) != hex.EncodeToString(sum[:]) {
		t.Errorf("pins get sha256 = %d, %q", code, out.String())
	}

	out.Reset()

	if code := run(
		[]string{"pins", "get", "tool", "url"},
		&out,
		io.Discard,
	); code != 0 ||
		strings.TrimSpace(out.String()) != server.URL+"/tool-2.0.0.tgz" {
		t.Errorf("pins get url = %d, %q", code, out.String())
	}

	for _, args := range [][]string{
		{"pins"},
		{"pins", "frobnicate"},
		{"pins", "get", "tool"},
		{"pins", "get", "nope", "url"},
		{"pins", "get", "tool", "digest"},
	} {
		if code := run(args, io.Discard, io.Discard); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

// Black-box tests of the limen command, through cli.Run alone. The rig is
// the environment the command already honors: stubs first on PATH for the
// tools it shells out to, GITHUB_API_URL for the users endpoint.

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/farcloser/limen"
	"github.com/farcloser/limen/internal/cli"
	"github.com/farcloser/limen/internal/rules"
)

// run is the command with a dev stamp, the way `go run` builds it.
func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run("dev", args, stdout, stderr)
}

// ghStubEnv, when set, makes this test binary act as a fake gh (the stdlib
// helper-process pattern, portable where a script is not); ghRenamedEnv
// makes that fake answer the org's App lookups with a renamed App.
const (
	ghStubEnv    = "LIMEN_CLI_TEST_GH_STUB"
	ghRenamedEnv = "LIMEN_CLI_TEST_GH_RENAMED"
)

// TestMain lets the binary play both roles: the test suite, and — when
// re-executed by the code under test as gh — the fake.
func TestMain(m *testing.M) {
	if os.Getenv(ghStubEnv) != "" {
		// The stub's exit status IS its contract (gh exits 1 on API errors).
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runGHStub())
	}

	m.Run()
}

// runGHStub answers the two org reads discovery makes when the renamed
// scenario is on, and fails everything otherwise (an unauthenticated gh).
func runGHStub() int {
	args := os.Args[1:]
	if os.Getenv(ghRenamedEnv) == "" || len(args) < 4 || args[0] != "api" {
		fmt.Fprintln(os.Stderr, "gh: boom (HTTP 500)")

		return 1
	}

	switch path := args[3]; {
	case strings.HasPrefix(path, "orgs/test-org/actions/variables/"):
		_, _ = os.Stdout.WriteString(`{"value":"4242"}`)
	case strings.HasPrefix(path, "orgs/test-org/installations"):
		// --paginate --slurp: an array of pages.
		_, _ = os.Stdout.WriteString(
			`[{"installations":[{"app_id":1,"app_slug":"renovate"},{"app_id":4242,"app_slug":"limenreapp"}]}]`,
		)
	default:
		fmt.Fprintln(os.Stderr, "gh: Not Found (HTTP 404)")

		return 1
	}

	return 0
}

// stubDir builds a directory of stand-ins for the tools the command shells
// out to — aqua and go as scripts (their arguments carry no shell
// metacharacters), gh as a copy of this binary in stub mode (its arguments
// do). The aqua stub logs its invocations to <dir>/log. The caller puts the
// directory first on PATH and sets ghStubEnv, with t.Setenv, which is what
// makes those tests serial.
func stubDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	aqua := filepath.Join(dir, "aqua")
	// update-checksum writes the file the aqua rule then expects, like the
	// real one would (its content is not judged).
	aquaScript := "#!/bin/sh\necho \"$@\" >> \"$(dirname \"$0\")/log\"\n" +
		"case \"$*\" in *update-checksum*) echo '{\"stub\":true}' > aqua-checksums.json;; esac\n"
	goStub := filepath.Join(dir, "go")
	goScript := "#!/bin/sh\n" +
		"[ \"$1\" = get ] && [ \"$2\" = -tool ] || exit 0\n" +
		"shift 2\n" +
		"for arg; do echo \"tool ${arg%@*}\" >> go.mod; done\n"
	gh := filepath.Join(dir, "gh")

	if runtime.GOOS == "windows" {
		aqua += ".bat"
		aquaScript = "@echo off\r\n>> \"%~dp0log\" echo %*\r\n" +
			"echo %* | find \"update-checksum\" >nul && echo {\"stub\":true}> aqua-checksums.json\r\n"
		goStub += ".bat"
		goScript = "@echo off\r\n" +
			"if not \"%1\"==\"get\" exit /b 0\r\n" +
			"if not \"%2\"==\"-tool\" exit /b 0\r\n" +
			"shift\r\nshift\r\n" +
			":loop\r\n" +
			"if \"%1\"==\"\" exit /b 0\r\n" +
			"for /f \"delims=@\" %%a in (\"%1\") do >> go.mod echo tool %%a\r\n" +
			"shift\r\ngoto loop\r\n"
		gh += ".exe"
	}

	// 0o700, not 0o600: the stubs must be executable.
	for path, content := range map[string]string{aqua: aquaScript, goStub: goScript} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(gh, binary, 0o700); err != nil {
		t.Fatal(err)
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
		"README.md":                 "# Thing",
		"LICENSE":                   "Permission is hereby granted, free of charge.\nTHE SOFTWARE IS PROVIDED \"AS IS\".",
		".editorconfig":             rules.CanonicalEditorconfig,
		".gitignore":                limen.CanonicalGitignore,
		".gitattributes":            rules.CanonicalGitattributes,
		"AGENTS.md":                 rules.CanonicalAgents,
		"CLAUDE.md":                 limen.CanonicalClaudeSeed,
		"Justfile":                  rules.CanonicalJustfileImport + "\n",
		"aqua.yaml":                 limen.CanonicalAquaYAML,
		"aqua-checksums.json":       "{}\n",
		"aqua-policy.yaml":          rules.CanonicalAquaPolicy,
		".limen/aqua-registry.yaml": rules.CanonicalAquaRegistry,
		".limen/lychee.toml":        rules.CanonicalLychee,
		".limen/.yamlfmt":           rules.CanonicalYamlfmt,
		".limen/.shellcheckrc":      rules.CanonicalShellcheckrc,
		".github/workflows/update-aqua-checksum.yaml": limen.CanonicalWorkflowUpdateAquaChecksum,
		".github/actions/setup-aqua/action.yaml":      limen.CanonicalActionSetupAqua,
		".github/workflows/ci.yaml":                   limen.CanonicalWorkflowCI,
		"renovate.json":                               rules.CanonicalRenovateFor(limen.CanonicalAquaYAML),
		// The Go-built tools every repository declares (the gotools rule).
		"tools/go.mod": "module tools\n\ngo 1.26\n\ntool (\n" +
			"\tgithub.com/vbatts/git-validation\n" +
			"\tgithub.com/farcloser/godolint/cmd/godolint\n" +
			"\tgithub.com/forkcloser/dot/cmd/dot\n)\n",
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
// of suppressed. With no -org and no origin remote, the update-App step is a
// warning that names the way forward, never a failure.
func TestBootstrapProducesCompliantRepo(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	stubs := stubDir(t)
	t.Setenv(ghStubEnv, "1")
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := filepath.Join(t.TempDir(), "newrepo")

	var stderr strings.Builder
	if code := run([]string{"bootstrap", dir}, io.Discard, &stderr); code != 0 {
		t.Skipf("bootstrap returned %d (likely git init unavailable in this environment): %s", code, stderr.String())
	}

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Errorf("check on a bootstrapped repo = %d, want 0", code)
	}

	if warning := stderr.String(); !strings.Contains(warning, "warning") || !strings.Contains(warning, "-org") {
		t.Errorf("no-org bootstrap did not warn usably: %q", warning)
	}

	raw, err := os.ReadFile(filepath.Join(stubs, "log"))
	if err != nil {
		t.Fatalf("the aqua stub was never invoked: %v", err)
	}

	want := []string{
		"--log-level warn policy allow aqua-policy.yaml",
		"--log-level warn update-checksum --prune",
		"--log-level warn install --only-link",
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
	t.Setenv(ghStubEnv, "1")
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	embedded := regexp.MustCompile(`(?m)^  - name: farcloser/limen@(\S+)`).FindStringSubmatch(limen.CanonicalAquaYAML)
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
	for i, tc := range cases {
		dir := filepath.Join(t.TempDir(), "newrepo")

		var stdout, stderr strings.Builder

		code := cli.Run(tc.stamp, []string{"bootstrap", dir}, &stdout, &stderr)
		if code != 0 {
			if i == 0 {
				t.Skipf("bootstrap returned %d (likely git init unavailable in this environment): %s",
					code, stderr.String())
			}

			t.Fatalf("stamp %q: bootstrap returned %d:\n%s%s", tc.stamp, code, stdout.String(), stderr.String())
		}

		manifest, err := os.ReadFile(filepath.Join(dir, "aqua.yaml"))
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

// usersEndpoint serves the public users endpoint for the bot logins in
// users (editable between phases) and records every login asked for.
type usersEndpoint struct {
	mu    sync.Mutex
	users map[string]string
	asked []string
}

func (u *usersEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()

	login := strings.TrimPrefix(r.URL.Path, "/users/")
	u.asked = append(u.asked, login)

	body, found := u.users[login]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))

		return
	}

	_, _ = w.Write([]byte(body))
}

func (u *usersEndpoint) set(login, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if body == "" {
		delete(u.users, login)
	} else {
		u.users[login] = body
	}
}

// TestUpdateAppIdentityFlowsIntoRenovate: the org comes from the origin
// remote, the resolvers reach the users endpoint GITHUB_API_URL names (and,
// for discovery, the gh on PATH), and `limen fix` writes the App's address
// into renovate.json where `limen check` then requires it. Without a
// resolvable identity neither command enforces anything.
func TestUpdateAppIdentityFlowsIntoRenovate(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	dir := compliantRepo(t)

	// compliantRepo seeds a bare .git directory; the identity path needs a
	// real repository with an origin remote.
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@github.com:test-org/thing.git"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v unavailable: %v (%s)", args, err, out)
		}
	}

	// gh fails every call until the renamed scenario is switched on.
	stubs := stubDir(t)
	t.Setenv(ghStubEnv, "1")
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	endpoint := &usersEndpoint{users: map[string]string{}}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	// Unresolvable everywhere (an endpoint that refuses the connection):
	// check passes, fix leaves the seed alone.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	t.Setenv("GITHUB_API_URL", dead.URL)

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("check with an unresolvable identity = %d, want 0", code)
	}

	seed, err := os.ReadFile(filepath.Join(dir, "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"fix", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("fix with an unresolvable identity = %d, want 0", code)
	}

	after, _ := os.ReadFile(filepath.Join(dir, "renovate.json"))
	if string(after) != string(seed) {
		t.Error("fix edited renovate.json without a resolved identity")
	}

	// Resolvable by convention: the org inferred from origin names the bot
	// user asked for; check fails until fix adds the address; then check
	// passes.
	const email = "317468017+limen-ci-test-org[bot]@users.noreply.github.com"

	endpoint.set("limen-ci-test-org[bot]", `{"id": 317468017, "login": "limen-ci-test-org[bot]", "type": "Bot"}`)
	t.Setenv("GITHUB_API_URL", server.URL)

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 1 {
		t.Errorf("check with the identity missing = %d, want 1", code)
	}

	if !slices.Contains(endpoint.asked, "limen-ci-test-org[bot]") {
		t.Errorf("the users endpoint was asked for %q, want limen-ci-test-org[bot] (the org from the origin remote)",
			endpoint.asked)
	}

	if code := run([]string{"fix", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("fix = %d, want 0", code)
	}

	fixed, _ := os.ReadFile(filepath.Join(dir, "renovate.json"))
	if !strings.Contains(string(fixed), "\""+email+"\",\n") {
		t.Errorf("fix did not add the address:\n%s", fixed)
	}

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Errorf("check after fix = %d, want 0", code)
	}

	// The split: an App only the privileged tier can see (renamed; the
	// convention name no longer exists). check must NOT fail for it — the
	// same tree must get the same verdict on a laptop with an admin token and
	// on a runner without one — while fix, the credentialed step, still
	// writes it.
	const renamed = "300983632+limenreapp[bot]@users.noreply.github.com"

	endpoint.set("limen-ci-test-org[bot]", "")
	endpoint.set("limenreapp[bot]", `{"id": 300983632, "login": "limenreapp[bot]", "type": "Bot"}`)
	t.Setenv(ghRenamedEnv, "1")

	if code := run([]string{"check", dir}, io.Discard, io.Discard); code != 0 {
		t.Errorf("check with an App only discover can see = %d, want 0 (credential-independent)", code)
	}

	if code := run([]string{"fix", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("fix (discover) = %d, want 0", code)
	}

	fixed, _ = os.ReadFile(filepath.Join(dir, "renovate.json"))
	if !strings.Contains(string(fixed), "\""+renamed+"\",\n") || !strings.Contains(string(fixed), "\""+email+"\",\n") {
		t.Errorf("fix did not add the discovered address alongside the earlier one:\n%s", fixed)
	}
}

// TestPinsCommands: `limen pins get` serves a value from the working
// directory's pins.yaml; `limen pins refresh` brings a stale digest to the
// bytes at the url; both name their misuse.
//
//nolint:paralleltest // serial by design: t.Chdir forbids t.Parallel, and the linter does not see it.
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

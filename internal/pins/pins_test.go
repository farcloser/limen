// Black-box tests of the pins manifest: parsed, read, and refreshed through
// the exported API, with a local server standing in for the artifact hosts
// and this binary standing in for gh and cosign on PATH.

package pins_test

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/pins"
	"github.com/farcloser/limen/internal/pins/pinstest"
	"github.com/farcloser/limen/internal/verify/cms"
	"github.com/farcloser/limen/internal/verify/cms/cmstest"
	"github.com/farcloser/limen/internal/verify/openpgp/openpgptest"
)

// verifierEnv names the file the fake gh and cosign append their argv to,
// so a test can assert what was asked of them; when it is unset the fakes
// refuse, playing a verifier that does not vouch.
const verifierEnv = "LIMEN_TEST_VERIFIER_LOG"

// TestMain lets the binary play gh and cosign: invoked under either name it
// logs its arguments and exits 0 when verifierEnv names a log, 1 otherwise.
func TestMain(m *testing.M) {
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") {
	case "gh", "cosign":
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(runVerifierStub())
	default:
		// The suite itself.
	}

	dir, err := installStubs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "installing the stubs:", err)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, "putting the stubs on PATH:", err)
		//revive:disable-next-line:redundant-test-main-exit
		os.Exit(1)
	}

	m.Run()

	_ = os.RemoveAll(dir)
}

func runVerifierStub() int {
	log := os.Getenv(verifierEnv)
	if log == "" {
		fmt.Fprintln(os.Stderr, "verification refused")

		return 1
	}

	file, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}

	defer func() { _ = file.Close() }()

	_, _ = fmt.Fprintln(file, strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")+" "+strings.Join(os.Args[1:], " "))

	return 0
}

// installStubs copies (or links) this binary under the names TestMain
// dispatches on, plus the extension Windows needs.
func installStubs() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "limen-pins-stubs")
	if err != nil {
		return "", err
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	for _, name := range []string{"gh", "cosign"} {
		stub := filepath.Join(dir, name+ext)
		if err := os.Symlink(self, stub); err == nil {
			continue
		}

		data, err := os.ReadFile(self)
		if err != nil {
			return "", err
		}

		if err := os.WriteFile(stub, data, 0o700); err != nil {
			return "", err
		}
	}

	return dir, nil
}

func sha256Of(data string) string {
	sum := sha256.Sum256([]byte(data))

	return hex.EncodeToString(sum[:])
}

// artifactHost serves the named files; it is where every url in a fixture
// points.
func artifactHost(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, found := files[r.URL.Path]
		if !found {
			http.NotFound(w, r)

			return
		}

		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	return server
}

// writePins puts a manifest in a fresh repository root and returns the root.
func writePins(t *testing.T, text string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, pins.File), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	return root
}

const fixture = `# pins.yaml — comments and blank lines survive a refresh.
pins:
  - name: tool
    renovate: github-releases example/tool
    extract-version: ^v(?<version>.*)$
    version: 2.0.0
    url: %s/releases/tool-${version}.tar.gz
    verify: download
    digest:
      version: 1.0.0
      sha256: 0000000000000000000000000000000000000000000000000000000000000000 # computed for 1.0.0

  - name: kernel
    renovate: github-tags example/linux
    version: 7.2.6
    url: %s/pub/v${major}.x/linux-${version}.tar.xz
    verify: cosign-sha256sums %s/pub/v${major}.x/SHA256SUMS %s/pub/v${major}.x/SHA256SUMS.bundle ^https://github.com/example/ https://token.actions.githubusercontent.com
    digest:
      version: 7.2.6
      sha256: 1111111111111111111111111111111111111111111111111111111111111111
`

func TestParseAndGet(t *testing.T) {
	t.Parallel()

	text := pinstest.Recorded(t, fmt.Sprintf(fixture, "https://h", "https://h", "https://h", "https://h"), "kernel")

	manifest, err := pins.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(manifest.Entries))
	}

	for _, tc := range []struct{ name, field, want string }{
		{"tool", "version", "2.0.0"},
		{"tool", "url", "https://h/releases/tool-2.0.0.tar.gz"},
		{"tool", "sha256", strings.Repeat("0", 64)},
		{"kernel", "url", "https://h/pub/v7.x/linux-7.2.6.tar.xz"},
	} {
		got, getErr := manifest.Get(tc.name, tc.field)
		if getErr != nil || got != tc.want {
			t.Errorf("Get(%s, %s) = %q, %v; want %q", tc.name, tc.field, got, getErr, tc.want)
		}
	}

	if _, err = manifest.Get("tool", "digest"); !errors.Is(err, pins.ErrNoSuchField) {
		t.Errorf("an unserved field: %v, want ErrNoSuchField", err)
	}

	if _, err = manifest.Get("nope", "version"); !errors.Is(err, pins.ErrNoSuchPin) {
		t.Errorf("an unknown pin: %v, want ErrNoSuchPin", err)
	}

	if got := manifest.Stale(); !slices.Equal(got, []string{"tool"}) {
		t.Errorf("stale = %v, want [tool]: the digest was computed for 1.0.0, the pin is 2.0.0", got)
	}

	if tool := manifest.Entries[0]; tool.ExtractVersion != "^v(?<version>.*)$" || tool.Versioning != "" {
		t.Errorf("tool's Renovate companions = %q, %q", tool.ExtractVersion, tool.Versioning)
	}

	// The optional lines in their order, a versioning scheme included.
	versioned := "pins:\n  - name: gk\n    renovate: github-releases example/kernel\n" +
		"    extract-version: ^v(?<version>.*)$\n    versioning: regex:^(?<major>\\d+)\\.(?<minor>\\d+)\\.(?<patch>\\d+)-ossein\\.(?<build>\\d+)$\n" +
		"    version: 7.1.5-ossein.2\n    url: https://h/${version}/kernel\n    verify: download\n" +
		"    digest:\n      version: 7.1.5-ossein.2\n      sha256: " + strings.Repeat("0", 64) + "\n"

	parsed, err := pins.Parse([]byte(versioned))
	if err != nil {
		t.Fatalf("an entry with extract-version and versioning must parse: %v", err)
	}

	if !strings.HasPrefix(parsed.Entries[0].Versioning, "regex:") {
		t.Errorf("versioning = %q", parsed.Entries[0].Versioning)
	}
}

// TestCommitPin: an upstream pinned to a commit of a branch. The commit is a
// template variable and a served field, a digest computed for another commit
// is stale, and a refresh writes the digest's commit line beside its version.
func TestCommitPin(t *testing.T) {
	t.Parallel()

	const (
		old     = "e491d2ec76709975f09d1820367a92a1772ccfb1"
		current = "0123456789abcdef0123456789abcdef01234567"
		tarball = "the sources at the new head"
	)

	host := artifactHost(t, map[string]string{"/archive/" + current + ".tar.gz": tarball})
	text := "pins:\n  - name: fox\n    renovate: git-refs https://h/fox\n    version: master\n" +
		"    commit: " + current + "\n    url: " + host.URL + "/archive/${commit}.tar.gz\n    verify: download\n" +
		"    digest:\n      version: master\n      commit: " + old + "\n      sha256: " + strings.Repeat("0", 64) + "\n"

	manifest, err := pins.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	for _, tc := range []struct{ field, want string }{
		{"version", "master"},
		{"commit", current},
		{"url", host.URL + "/archive/" + current + ".tar.gz"},
	} {
		if got, getErr := manifest.Get("fox", tc.field); getErr != nil || got != tc.want {
			t.Errorf("Get(fox, %s) = %q, %v; want %q", tc.field, got, getErr, tc.want)
		}
	}

	if got := manifest.Stale(); !slices.Equal(got, []string{"fox"}) {
		t.Errorf("stale = %v, want [fox]: the digest was computed for another commit", got)
	}

	root := writePins(t, text)

	if changed, refreshErr := pins.Refresh(t.Context(), root, io.Discard); refreshErr != nil ||
		!slices.Equal(changed, []string{"fox"}) {
		t.Fatalf("refresh = %v, %v; want [fox]", changed, refreshErr)
	}

	after, _ := os.ReadFile(filepath.Join(root, pins.File))
	want := pinstest.Recorded(t, strings.Replace(strings.Replace(text, "commit: "+old, "commit: "+current, 1),
		strings.Repeat("0", 64), sha256Of(tarball), 1), "fox")

	if string(after) != want {
		t.Errorf("after refresh:\n%s\nwant:\n%s", after, want)
	}

	// A digest written before the pin had a commit line gains one.
	without := strings.Replace(text, "      commit: "+old+"\n", "", 1)
	root = writePins(t, without)

	if _, refreshErr := pins.Refresh(t.Context(), root, io.Discard); refreshErr != nil {
		t.Fatalf("refresh of a digest without a commit line: %v", refreshErr)
	}

	after, _ = os.ReadFile(filepath.Join(root, pins.File))
	if string(after) != want {
		t.Errorf("after refresh without a digest commit line:\n%s\nwant:\n%s", after, want)
	}

	// Not a commit pin: the commit field is not served.
	plain, _ := pins.Parse([]byte(fmt.Sprintf(fixture, "https://h", "https://h", "https://h", "https://h")))
	if _, getErr := plain.Get("tool", "commit"); !errors.Is(getErr, pins.ErrNoSuchField) {
		t.Errorf("Get(tool, commit) on a version pin = %v, want ErrNoSuchField", getErr)
	}
}

// TestRenovateManagerReadsBothShapes: the shared preset's pins.yaml manager,
// read from the canonical .limen/renovate.json, captures a commit pin's
// branch as currentValue and its commit as currentDigest, and a version
// pin's version alone.
// renovatePinsPattern is the shared preset's regex for pins.yaml, as Renovate
// reads it.
func renovatePinsPattern(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", ".limen", "renovate.json"))
	if err != nil {
		t.Fatal(err)
	}

	type customManager struct {
		ManagerFilePatterns []string `json:"managerFilePatterns"`
		MatchStrings        []string `json:"matchStrings"`
	}

	var preset struct {
		CustomManagers []customManager `json:"customManagers"`
	}

	if err := json.Unmarshal(data, &preset); err != nil {
		t.Fatal(err)
	}

	var pattern string

	for _, manager := range preset.CustomManagers {
		if slices.ContainsFunc(
			manager.ManagerFilePatterns,
			func(p string) bool { return strings.Contains(p, "pins") },
		) {
			pattern = manager.MatchStrings[0]
		}
	}

	if pattern == "" {
		t.Fatal("no pins.yaml manager in the preset")
	}

	return pattern
}

func TestRenovateManagerReadsBothShapes(t *testing.T) {
	t.Parallel()

	matcher := regexp.MustCompile(renovatePinsPattern(t))
	groups := func(text string) map[string]string {
		t.Helper()

		match := matcher.FindStringSubmatch(text)
		if match == nil {
			t.Fatalf("the manager does not match:\n%s", text)
		}

		out := map[string]string{}

		for index, name := range matcher.SubexpNames() {
			if name != "" && match[index] != "" {
				out[name] = match[index]
			}
		}

		return out
	}

	commitPin := "    renovate: git-refs https://h/fox\n    version: master\n" +
		"    commit: e491d2ec76709975f09d1820367a92a1772ccfb1\n    url: https://h/${commit}.tar.gz\n"
	versionPin := "    renovate: github-releases x/y\n    extract-version: ^v(?<version>.*)$\n" +
		"    version: 2.0.0\n    url: https://h/${version}.tar.gz\n"

	if got, want := groups(commitPin), (map[string]string{
		"datasource": "git-refs", "depName": "https://h/fox",
		"currentValue": "master", "currentDigest": "e491d2ec76709975f09d1820367a92a1772ccfb1",
	}); !maps.Equal(got, want) {
		t.Errorf("commit pin groups = %v, want %v", got, want)
	}

	if got, want := groups(versionPin), (map[string]string{
		"datasource": "github-releases", "depName": "x/y",
		"extractVersion": "^v(?<version>.*)$", "currentValue": "2.0.0",
	}); !maps.Equal(got, want) {
		t.Errorf("version pin groups = %v, want %v", got, want)
	}
}

// TestVersioningGroupsExpand: a tag whose release asset spells the version
// another way is rebuilt from the named groups of its regex versioning, in the
// url and in the method's arguments.
func TestVersioningGroupsExpand(t *testing.T) {
	t.Parallel()

	text := "pins:\n  - name: expat\n    renovate: github-tags libexpat/libexpat\n" +
		"    versioning: regex:^R_(?<major>\\d+)_(?<minor>\\d+)_(?<patch>\\d+)$\n    version: R_2_8_5\n" +
		"    url: https://h/releases/download/${version}/expat-${major}.${minor}.${patch}.tar.gz\n" +
		"    verify: github-release-asset libexpat/libexpat ${version}\n" +
		"    digest:\n      version: R_2_8_5\n      sha256: " + strings.Repeat("0", 64) + "\n"

	manifest, err := pins.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	url, _ := manifest.Get("expat", "url")
	if want := "https://h/releases/download/R_2_8_5/expat-2.8.5.tar.gz"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}

	if args := manifest.Entries[0].VerifyArgs(); !slices.Equal(args, []string{"libexpat/libexpat", "R_2_8_5"}) {
		t.Errorf("verify args = %v", args)
	}
}

// TestParseRejects: every shape the commands could not act on is refused at
// parse time, naming the entry and the problem.
func TestParseRejects(t *testing.T) {
	t.Parallel()

	const good = "pins:\n  - name: a\n    renovate: github-tags x/y\n    version: 1\n    url: https://h/a\n    verify: download\n    digest:\n      version: 1\n      sha256: " + "0000000000000000000000000000000000000000000000000000000000000000" + "\n"

	if _, err := pins.Parse([]byte(good)); err != nil {
		t.Fatalf("the minimal manifest must parse: %v", err)
	}

	cases := map[string]struct {
		text string
		want error
	}{
		"no pins section":   {"other:\n  - name: a\n", pins.ErrSyntax},
		"field before pins": {"  - name: a\npins:\n", pins.ErrSyntax},
		"bad indentation":   {strings.Replace(good, "    url:", "   url:", 1), pins.ErrSyntax},
		"unknown field":     {strings.Replace(good, "    url:", "    where:", 1), pins.ErrEntry},
		"bad name":          {strings.Replace(good, "name: a", "name: A_b", 1), pins.ErrEntry},
		"lane identity without a repository": {
			strings.Replace(
				good,
				"    verify: download\n",
				"    verify: cosign-sha256sums https://h/sums https://h/bundle"+
					" ^https://github\\.com/farcloser/limen/\\.github/workflows/limen-release-apple\\.yaml@"+
					" https://token.actions.githubusercontent.com\n",
				1,
			),
			pins.ErrEntry,
		},
		"attestation lane signer with an owner scope": {
			strings.Replace(good, "    verify: download\n", "    verify: github-attestation example"+
				" farcloser/limen/.github/workflows/limen-release-apple.yaml\n", 1),
			pins.ErrEntry,
		},
		"attestation scope not an owner or a repository": {
			strings.Replace(good, "    verify: download\n", "    verify: github-attestation fork/closer/app\n", 1),
			pins.ErrEntry,
		},
		"attestation signer not a workflow path": {
			strings.Replace(
				good,
				"    verify: download\n",
				"    verify: github-attestation example/app farcloser/limen\n",
				1,
			),
			pins.ErrEntry,
		},
		"cosign repository not owner/repo": {
			strings.Replace(
				good,
				"    verify: download\n",
				"    verify: cosign-sha256sums https://h/sums https://h/bundle"+
					" ^https://github\\.com/example/ https://token.actions.githubusercontent.com example\n",
				1,
			),
			pins.ErrEntry,
		},
		"short commit": {
			strings.Replace(good, "    version: 1\n", "    version: 1\n    commit: abc\n", 1),
			pins.ErrEntry,
		},
		"commit before version": {strings.Replace(good, "    version: 1\n",
			"    commit: "+strings.Repeat("a", 40)+"\n    version: 1\n", 1), pins.ErrEntry},
		"digest commit alone": {strings.Replace(good, "      version: 1\n",
			"      version: 1\n      commit: "+strings.Repeat("a", 40)+"\n", 1), pins.ErrEntry},
		"commit before renovate": {strings.Replace(good, "    renovate: github-tags x/y\n",
			"    commit: "+strings.Repeat("a", 40)+"\n    renovate: github-tags x/y\n", 1), pins.ErrEntry},
		"field between version and commit": {strings.Replace(good, "    version: 1\n    url: https://h/a\n",
			"    version: 1\n    url: https://h/a\n    commit: "+strings.Repeat("a", 40)+"\n", 1), pins.ErrEntry},
		"commit variable on a version pin": {
			strings.Replace(good, "url: https://h/a", "url: https://h/${commit}", 1),
			pins.ErrEntry,
		},
		"no version":         {strings.Replace(good, "    version: 1\n", "", 1), pins.ErrEntry},
		"renovate too short": {strings.Replace(good, "github-tags x/y", "github-tags", 1), pins.ErrEntry},
		"renovate too long": {
			strings.Replace(good, "github-tags x/y", "github-tags x/y ^v(.*)$", 1),
			pins.ErrEntry,
		},
		"block out of order": {
			strings.Replace(
				good,
				"    version: 1\n    url: https://h/a\n",
				"    url: https://h/a\n    version: 1\n",
				1,
			),
			pins.ErrEntry,
		},
		"extract-version after version": {
			strings.Replace(good, "    version: 1\n", "    version: 1\n    extract-version: ^v(.*)$\n", 1),
			pins.ErrEntry,
		},
		"unknown method": {strings.Replace(good, "verify: download", "verify: pgp", 1), pins.ErrEntry},
		"wrong arity": {
			strings.Replace(good, "verify: download", "verify: github-attestation", 1),
			pins.ErrEntry,
		},
		"bad sha256": {strings.Replace(good, strings.Repeat("0", 64), "abc", 1), pins.ErrEntry},
		"pgp fingerprint with spaces": {
			strings.Replace(good, "verify: download",
				"verify: pgp-sha256sums https://x/s.asc https://x/k.asc B886 8C80", 1), pins.ErrEntry,
		},
		"pgp fingerprint too short": {
			strings.Replace(good, "verify: download",
				"verify: pgp-sha256sums https://x/s.asc https://x/k.asc 632D3A06589DA6B1", 1), pins.ErrEntry,
		},
		"pgp-detached with a key id, not a fingerprint": {
			strings.Replace(good, "verify: download",
				"verify: pgp-detached https://x/t.sig https://x/k.asc 632D3A06589DA6B1", 1), pins.ErrEntry,
		},
		"pgp-detached without the key": {
			strings.Replace(good, "verify: download", "verify: pgp-detached https://x/t.sig", 1), pins.ErrEntry,
		},
		"half a digest":  {strings.Replace(good, "      version: 1\n", "", 1), pins.ErrEntry},
		"duplicate name": {good + strings.TrimPrefix(good, "pins:\n"), pins.ErrEntry},
		"unknown variable in url": {
			strings.Replace(good, "url: https://h/a", "url: https://h/a-${minor}", 1),
			pins.ErrEntry,
		},
		"unknown variable in verify": {
			strings.Replace(good, "verify: download", "verify: github-release-asset x/y v${vresion}", 1),
			pins.ErrEntry,
		},
		"group the version does not match": {
			strings.Replace(
				strings.Replace(
					good,
					"    version: 1\n",
					"    versioning: regex:^R_(?<major>\\d+)_(?<minor>\\d+)$\n    version: 1\n",
					1,
				),
				"url: https://h/a",
				"url: https://h/a-${minor}",
				1,
			),
			pins.ErrEntry,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := pins.Parse([]byte(tc.text)); !errors.Is(err, tc.want) {
				t.Errorf("Parse = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestRefreshDownload: a stale download-verified pin gets the sha256 of the
// bytes at its url, in place — the two digest lines change and the source
// line is written, comments and spacing included nothing else does — and a
// current pin is left alone.
func TestRefreshDownload(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	const tarball = "the tool, version 2"

	host := artifactHost(t, map[string]string{"/releases/tool-2.0.0.tar.gz": tarball})
	text := pinstest.Recorded(t, fmt.Sprintf(fixture, host.URL, host.URL, host.URL, host.URL), "kernel")
	root := writePins(t, text)

	t.Setenv(verifierEnv, filepath.Join(t.TempDir(), "log")) // kernel is current: cosign must not even be asked

	var progress strings.Builder

	changed, err := pins.Refresh(t.Context(), root, &progress)
	if err != nil {
		t.Fatalf("refresh: %v\n%s", err, progress.String())
	}

	if !slices.Equal(changed, []string{"tool"}) {
		t.Errorf("changed = %v, want [tool]", changed)
	}

	after, _ := os.ReadFile(filepath.Join(root, pins.File))

	want := strings.Replace(
		text,
		"      version: 1.0.0\n      sha256: "+strings.Repeat("0", 64)+" # computed for 1.0.0",
		"      version: 2.0.0\n      source: "+pinstest.Source(t, text, "tool")+
			"\n      sha256: "+sha256Of(tarball)+" # computed for 1.0.0",
		1,
	)
	if string(after) != want {
		t.Errorf("refresh did more than rewrite the two digest lines and write the source:\n%s", after)
	}

	manifest, err := pins.Parse(after)
	if err != nil {
		t.Fatal(err)
	}

	if stale := manifest.Stale(); len(stale) != 0 {
		t.Errorf("still stale after refresh: %v", stale)
	}

	if log, _ := os.ReadFile(os.Getenv(verifierEnv)); len(log) != 0 {
		t.Errorf("a current pin was verified again:\n%s", log)
	}

	// Nothing stale: no write, no change reported.
	before, _ := os.ReadFile(filepath.Join(root, pins.File))

	if changed, err := pins.Refresh(t.Context(), root, io.Discard); err != nil || len(changed) != 0 {
		t.Errorf("second refresh: %v, %v; want nothing", changed, err)
	}

	if again, _ := os.ReadFile(filepath.Join(root, pins.File)); !bytes.Equal(again, before) {
		t.Error("a refresh with nothing to do rewrote the file")
	}
}

// TestRefreshAfterAnEdit: the digest stands for the url and the verification
// it was computed through. An entry whose url or verify line was edited at
// the same version is stale, and a refresh recomputes it through the entry
// as it now reads, so a new verify line is exercised and never taken on
// trust; a digest written before the source was recorded is recomputed the
// same way. An edit that resolves to the same url leaves the digest current.
func TestRefreshAfterAnEdit(t *testing.T) {
	t.Parallel()

	const (
		archive = "the tag archive"
		tarball = "the signed release tarball"
	)

	signer, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	other, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	signature, err := signer.SignDetached([]byte(tarball), crypto.SHA512, openpgptest.Detached{Armored: true})
	if err != nil {
		t.Fatal(err)
	}

	host := artifactHost(t, map[string]string{
		"/archive/R_2_10_0.tar.gz":          archive,
		"/releases/expat-2.10.0.tar.xz":     tarball,
		"/releases/expat-2.10.0.tar.xz.asc": string(signature),
		"/keys/signer.asc":                  string(signer.PublicKeyBlock()),
		"/keys/other.asc":                   string(other.PublicKeyBlock()),
	})

	// The entry as it was: the tag archive, TLS alone, its digest current.
	legacy := `pins:
  - name: expat
    renovate: github-releases libexpat/libexpat
    version: 2.10.0
    url: ` + host.URL + `/archive/R_2_10_0.tar.gz
    verify: download
    digest:
      version: 2.10.0
      sha256: ` + sha256Of(archive) + ` # the tag archive
`
	before := pinstest.Recorded(t, legacy, "expat")

	if stale := mustParse(t, before).Stale(); len(stale) != 0 {
		t.Fatalf("the entry as recorded must be current, stale = %v", stale)
	}

	release := strings.Replace(before, "/archive/R_2_10_0.tar.gz", "/releases/expat-${version}.tar.xz", 1)
	detached := func(key, fingerprint string) string {
		return strings.Replace(release, "verify: download", "verify: pgp-detached "+host.URL+
			"/releases/expat-${version}.tar.xz.asc "+host.URL+"/keys/"+key+" "+fingerprint, 1)
	}

	for name, tc := range map[string]struct{ edited, sum string }{
		"another url":                  {release, sha256Of(tarball)},
		"another url and verification": {detached("signer.asc", signer.Fingerprint()), sha256Of(tarball)},
		"no source recorded":           {legacy, sha256Of(archive)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if stale := mustParse(t, tc.edited).Stale(); !slices.Equal(stale, []string{"expat"}) {
				t.Fatalf("stale = %v, want [expat]", stale)
			}

			root := writePins(t, tc.edited)

			changed, err := pins.Refresh(t.Context(), root, io.Discard)
			if err != nil || !slices.Equal(changed, []string{"expat"}) {
				t.Fatalf("refresh = %v, %v; want [expat]", changed, err)
			}

			after := string(mustRead(t, filepath.Join(root, pins.File)))

			// The digest of the bytes at the url as it now reads, the source
			// of the entry as it now reads, and not a line more.
			want := pinstest.Recorded(t, strings.Replace(tc.edited, sha256Of(archive), tc.sum, 1), "expat")
			if after != want {
				t.Errorf("after refresh:\n%s\nwant:\n%s", after, want)
			}

			if stale := mustParse(t, after).Stale(); len(stale) != 0 {
				t.Errorf("still stale after refresh: %v", stale)
			}
		})
	}

	t.Run("the new verification is exercised", func(t *testing.T) {
		t.Parallel()

		// The same edit with a key that did not sign: the refresh refuses,
		// where a digest judged by its version alone was left as current.
		root := writePins(t, detached("other.asc", other.Fingerprint()))

		if _, err := pins.Refresh(t.Context(), root, io.Discard); !errors.Is(err, pins.ErrVerify) {
			t.Fatalf("err = %v, want ErrVerify", err)
		}
	})

	t.Run("the same url, spelled otherwise", func(t *testing.T) {
		t.Parallel()

		spelled := strings.Replace(before, "/archive/R_2_10_0.tar.gz", "/archive/R_${major}_10_0.tar.gz", 1)
		if stale := mustParse(t, spelled).Stale(); len(stale) != 0 {
			t.Errorf("a template that resolves to the url the digest was computed from is current, stale = %v", stale)
		}
	})

	t.Run("a source that is no fingerprint", func(t *testing.T) {
		t.Parallel()

		broken := strings.Replace(before, "      source: ", "      source: abc", 1)
		if _, err := pins.Parse([]byte(broken)); !errors.Is(err, pins.ErrEntry) {
			t.Errorf("err = %v, want ErrEntry", err)
		}
	})
}

// TestSourceIsWhatTheManifestsCarry holds the fingerprint to a value computed
// outside limen: every pins.yaml persists it, so a change of its form (the
// separator, the join, the templates taken as written) would leave every
// digest of every repository stale. The entry is forkcloser/go-graphviz's
// expat pin, a templated url and a templated verify argument; the hex is
//
//	printf '%s\n%s' <resolved url> '<method and resolved arguments>' | shasum -a 256
func TestSourceIsWhatTheManifestsCarry(t *testing.T) {
	t.Parallel()

	const text = `pins:
  - name: expat
    renovate: github-tags libexpat/libexpat
    versioning: regex:^R_(?<major>\d+)_(?<minor>\d+)_(?<patch>\d+)$
    version: R_2_10_0
    url: https://github.com/libexpat/libexpat/releases/download/${version}/expat-${major}.${minor}.${patch}.tar.gz
    verify: pgp-detached https://github.com/libexpat/libexpat/releases/download/${version}/expat-${major}.${minor}.${patch}.tar.gz.asc https://github.com/hartwork.gpg CB8DE70A90CFBF6C3BF5CC5696262ACFFBD3AEC6
`

	const (
		resolved = "https://github.com/libexpat/libexpat/releases/download/R_2_10_0/expat-2.10.0.tar.gz\n" +
			"pgp-detached https://github.com/libexpat/libexpat/releases/download/R_2_10_0/expat-2.10.0.tar.gz.asc" +
			" https://github.com/hartwork.gpg CB8DE70A90CFBF6C3BF5CC5696262ACFFBD3AEC6"
		want = "6829130ac4fc17b95e20e1b74f3653ec572205e2cd357f51b8ddc16fd13a1695"
	)

	// The literal is the sha256 of the resolved lines, by this package's own
	// hash and by the hex written down: a typo in either shows.
	if got := sha256Of(resolved); got != want {
		t.Fatalf("the test's own literal: sha256 of the resolved lines = %s, want %s", got, want)
	}

	if got := mustParse(t, text).Entries[0].Source(); got != want {
		t.Errorf("Source() = %s, want %s", got, want)
	}
}

// mustParse parses a manifest the test wrote.
func mustParse(t *testing.T, text string) pins.Manifest {
	t.Helper()

	manifest, err := pins.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}

// TestRefreshWritesAMissingDigest: an entry written without a digest (the
// way a new pin starts) parses as unpinned, which get sha256 refuses; a
// refresh writes its digest block, after its last line or under a bare
// `digest:`, and the entries after it keep every line.
func TestRefreshWritesAMissingDigest(t *testing.T) {
	t.Parallel()

	const tarball = "the tool, version 2"

	host := artifactHost(t, map[string]string{"/releases/tool-2.0.0.tar.gz": tarball})
	pinned := pinstest.Recorded(t, fmt.Sprintf(fixture, host.URL, host.URL, host.URL, host.URL), "kernel")
	toolDigest := "    digest:\n      version: 1.0.0\n      sha256: " + strings.Repeat(
		"0",
		64,
	) + " # computed for 1.0.0\n"

	for name, written := range map[string]string{
		"no digest":   strings.Replace(pinned, toolDigest, "", 1),
		"bare digest": strings.Replace(pinned, toolDigest, "    digest:\n", 1),
	} {
		manifest, err := pins.Parse([]byte(written))
		if err != nil {
			t.Fatalf("%s: an entry without a digest must parse: %v", name, err)
		}

		if _, err := manifest.Get("tool", "sha256"); !errors.Is(err, pins.ErrUnpinned) {
			t.Errorf("%s: get sha256 = %v, want ErrUnpinned", name, err)
		}

		if got := manifest.Unpinned(); !slices.Equal(got, []string{"tool"}) {
			t.Errorf("%s: unpinned = %v, want [tool]", name, got)
		}

		root := writePins(t, written)

		if changed, err := pins.Refresh(t.Context(), root, io.Discard); err != nil ||
			!slices.Equal(changed, []string{"tool"}) {
			t.Fatalf("%s: refresh = %v, %v; want [tool]", name, changed, err)
		}

		after, _ := os.ReadFile(filepath.Join(root, pins.File))
		want := strings.Replace(pinned, toolDigest,
			"    digest:\n      version: 2.0.0\n      source: "+pinstest.Source(t, pinned, "tool")+
				"\n      sha256: "+sha256Of(tarball)+"\n", 1)

		if string(after) != want {
			t.Errorf("%s: after refresh:\n%s\nwant:\n%s", name, after, want)
		}
	}
}

// TestRefreshAllThroughVerifiers: -all recomputes every pin, through gh for
// an attested artifact and through cosign for signed sums, with the
// arguments each entry declared; the signed sums yield the digest without
// the artifact being downloaded.
func TestRefreshAllThroughVerifiers(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	const (
		llvm      = "clang, allegedly"
		kernelSum = "2222222222222222222222222222222222222222222222222222222222222222"
		bundleSum = "3333333333333333333333333333333333333333333333333333333333333333"
	)

	host := artifactHost(t, map[string]string{
		"/releases/download/llvmorg-22.1.8/LLVM-22.1.8.tar.xz": llvm,
		"/pub/v7.x/SHA256SUMS":                                 kernelSum + "  linux-7.2.6.tar.xz\n" + bundleSum + "  app-7.2.6.tar.xz\n",
		"/pub/v7.x/SHA256SUMS.bundle":                          `{"bundle":true}`,
	})

	text := `pins:
  - name: llvm
    renovate: github-releases llvm/llvm-project
    extract-version: ^llvmorg-(?<version>.*)$
    version: 22.1.8
    url: ` + host.URL + `/releases/download/llvmorg-${version}/LLVM-${version}.tar.xz
    verify: github-attestation llvm
    digest:
      version: 22.1.8
      sha256: ` + strings.Repeat("0", 64) + `
  - name: kernel
    renovate: github-tags example/linux
    version: 7.2.6
    url: ` + host.URL + `/pub/v${major}.x/linux-${version}.tar.xz
    verify: cosign-sha256sums ` + host.URL + `/pub/v${major}.x/SHA256SUMS ` + host.URL + `/pub/v${major}.x/SHA256SUMS.bundle ^https://github.com/example/ https://token.actions.githubusercontent.com
    digest:
      version: 7.2.6
      sha256: ` + strings.Repeat("1", 64) + `
  - name: attested
    renovate: github-releases example/app
    version: 22.1.8
    url: ` + host.URL + `/releases/download/llvmorg-${version}/LLVM-${version}.tar.xz
    verify: github-attestation example/app farcloser/limen/.github/workflows/limen-release-apple.yaml
    digest:
      version: 22.1.8
      sha256: ` + strings.Repeat("5", 64) + `
  - name: bundle
    renovate: github-releases example/app
    version: 7.2.6
    url: ` + host.URL + `/pub/v${major}.x/app-${version}.tar.xz
    verify: cosign-sha256sums ` + host.URL + `/pub/v${major}.x/SHA256SUMS ` + host.URL + `/pub/v${major}.x/SHA256SUMS.bundle ^https://github\.com/farcloser/limen/\.github/workflows/limen-release-apple\.yaml@ https://token.actions.githubusercontent.com example/app
    digest:
      version: 7.2.6
      sha256: ` + strings.Repeat("4", 64) + `
`
	root := writePins(t, text)
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv(verifierEnv, log)

	changed, err := pins.RefreshAll(t.Context(), root, io.Discard)
	if err != nil {
		t.Fatalf("refresh -all: %v", err)
	}

	if !slices.Equal(changed, []string{"llvm", "kernel", "attested", "bundle"}) {
		t.Errorf("changed = %v, want all four", changed)
	}

	manifest, err := pins.Parse(mustRead(t, filepath.Join(root, pins.File)))
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := manifest.Get("llvm", "sha256"); got != sha256Of(llvm) {
		t.Errorf("llvm sha256 = %s, want the bytes' %s", got, sha256Of(llvm))
	}

	if got, _ := manifest.Get("kernel", "sha256"); got != kernelSum {
		t.Errorf("kernel sha256 = %s, want the signed sums' line %s", got, kernelSum)
	}

	if got, _ := manifest.Get("bundle", "sha256"); got != bundleSum {
		t.Errorf("bundle sha256 = %s, want the signed sums' line %s", got, bundleSum)
	}

	calls := string(mustRead(t, log))
	for _, want := range []string{
		"gh attestation verify ", "LLVM-22.1.8.tar.xz --owner llvm",
		"LLVM-22.1.8.tar.xz --repo example/app --signer-workflow farcloser/limen/.github/workflows/limen-release-apple.yaml",
		"cosign verify-blob --bundle ", "--certificate-identity-regexp ^https://github.com/example/ --certificate-oidc-issuer https://token.actions.githubusercontent.com ",
		"--certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-github-workflow-repository example/app ",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("verifier calls lack %q:\n%s", want, calls)
		}
	}
}

// TestReleaseAssetTag: the release tag gh is asked for is the version by
// default, and the second argument's template when the project tags with
// a prefix the version does not carry.
func TestReleaseAssetTag(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	host := artifactHost(t, map[string]string{"/v3.32.0/kata.tar.zst": "kata", "/1.2.3/x.tgz": "x"})
	text := "pins:\n" +
		"  - name: kata\n    renovate: github-releases kata-containers/kata-containers\n    version: 3.32.0\n" +
		"    url: " + host.URL + "/v${version}/kata.tar.zst\n    verify: github-release-asset kata-containers/kata-containers v${version}\n" +
		"    digest:\n      version: 0\n      sha256: " + strings.Repeat("0", 64) + "\n" +
		"  - name: x\n    renovate: github-releases example/x\n    version: 1.2.3\n" +
		"    url: " + host.URL + "/${version}/x.tgz\n    verify: github-release-asset example/x\n" +
		"    digest:\n      version: 0\n      sha256: " + strings.Repeat("0", 64) + "\n"
	root := writePins(t, text)
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv(verifierEnv, log)

	if _, err := pins.Refresh(t.Context(), root, io.Discard); err != nil {
		t.Fatal(err)
	}

	calls := string(mustRead(t, log))
	for _, want := range []string{
		"gh release verify-asset v3.32.0 ", "kata.tar.zst --repo kata-containers/kata-containers",
		"gh release verify-asset 1.2.3 ", "x.tgz --repo example/x",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("verifier calls lack %q:\n%s", want, calls)
		}
	}
}

// TestRefreshStopsOnRefusal: a verifier that does not vouch fails the run
// with ErrVerify, and the file is left as it was.
func TestRefreshStopsOnRefusal(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	host := artifactHost(t, map[string]string{"/a-1.tgz": "bytes"})
	text := "pins:\n  - name: a\n    renovate: github-tags x/y\n    version: 1\n    url: " + host.URL + "/a-${version}.tgz\n    verify: github-attestation x\n    digest:\n      version: 0\n      sha256: " + strings.Repeat(
		"0",
		64,
	) + "\n"
	root := writePins(t, text)

	t.Setenv(verifierEnv, "") // the fake gh refuses

	if _, err := pins.Refresh(t.Context(), root, io.Discard); !errors.Is(err, pins.ErrVerify) {
		t.Fatalf("refresh with a refusing verifier: %v, want ErrVerify", err)
	}

	if after := mustRead(t, filepath.Join(root, pins.File)); string(after) != text {
		t.Error("a refused refresh must leave the file untouched")
	}

	// A missing artifact is a refusal too.
	gone := writePins(t, strings.Replace(text, "/a-${version}.tgz", "/missing", 1))
	if _, err := pins.Refresh(t.Context(), gone, io.Discard); !errors.Is(err, pins.ErrVerify) {
		t.Errorf("refresh of a missing artifact: %v, want ErrVerify", err)
	}
}

// TestRefreshNamesTheAnsweringHost: a release URL redirects to an asset host,
// and a refusal from there must name that host, not only the URL asked.
func TestRefreshNamesTheAnsweringHost(t *testing.T) {
	t.Parallel()

	assets := artifactHost(t, map[string]string{})

	release := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, assets.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(release.Close)

	root := writePins(t, "pins:\n  - name: a\n    renovate: github-tags x/y\n    version: 1\n    url: "+
		release.URL+"/a-${version}.tgz\n    verify: download\n    digest:\n      version: 0\n      sha256: "+
		strings.Repeat("0", 64)+"\n")

	_, err := pins.Refresh(t.Context(), root, io.Discard)
	if !errors.Is(err, pins.ErrVerify) {
		t.Fatalf("refresh of an asset the redirect target refuses: %v, want ErrVerify", err)
	}

	assetHost := strings.TrimPrefix(assets.URL, "http://")
	if !strings.Contains(err.Error(), "HTTP 404 from "+assetHost) {
		t.Errorf("the error should name the asset host that answered, got: %v", err)
	}
}

// TestRefreshThroughClearsignedSums: the PGP method runs no tool. The sums
// are clearsigned by a key the test generates, the digest is the signed
// line, and a key other than the pinned one, or a tampered listing, is a
// refusal.
func TestRefreshThroughClearsignedSums(t *testing.T) {
	t.Parallel()

	const kernelSum = "3333333333333333333333333333333333333333333333333333333333333333"

	signer, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	other, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	signed, err := signer.Clearsign(kernelSum+"  linux-7.2.6.tar.xz\n", crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}

	// A sums file two keys signed, the pinned signer second, with every
	// release key in one block.
	both, err := openpgptest.ClearsignBy(kernelSum+"  linux-7.2.6.tar.xz\n", crypto.SHA256, other, signer)
	if err != nil {
		t.Fatal(err)
	}

	host := artifactHost(t, map[string]string{
		"/pub/v7.x/sha256sums.asc": string(signed),
		"/pub/v7.x/both.asc":       string(both),
		"/pub/v7.x/tampered.asc":   strings.Replace(string(signed), "3333", "4444", 1),
		"/keys/signer.asc":         string(signer.PublicKeyBlock()),
		"/keys/other.asc":          string(other.PublicKeyBlock()),
		"/keys/both.asc":           string(openpgptest.KeyBlock(other, signer)),
	})

	manifest := func(sums, key, fingerprint string) string {
		return `pins:
  - name: kernel
    renovate: github-tags example/linux
    version: 7.2.6
    url: ` + host.URL + `/pub/v${major}.x/linux-${version}.tar.xz
    verify: pgp-sha256sums ` + host.URL + `/pub/v${major}.x/` + sums + ` ` + host.URL + `/keys/` + key + ` ` + fingerprint + `
    digest:
      version: 7.2.5
      sha256: ` + strings.Repeat("1", 64) + `
`
	}

	for name, published := range map[string][2]string{
		"one signer":                {"sha256sums.asc", "signer.asc"},
		"pinned signer second":      {"both.asc", "both.asc"},
		"pinned key alone in block": {"both.asc", "signer.asc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := writePins(t, manifest(published[0], published[1], strings.ToLower(signer.Fingerprint())))

			changed, err := pins.Refresh(t.Context(), root, io.Discard)
			if err != nil {
				t.Fatalf("refresh: %v", err)
			}

			if !slices.Equal(changed, []string{"kernel"}) {
				t.Errorf("changed = %v, want kernel", changed)
			}

			parsed, err := pins.Parse(mustRead(t, filepath.Join(root, pins.File)))
			if err != nil {
				t.Fatal(err)
			}

			if got, _ := parsed.Get("kernel", "sha256"); got != kernelSum {
				t.Errorf("kernel sha256 = %s, want the signed line %s", got, kernelSum)
			}
		})
	}

	// Each refusal says why: the reader of a red refresh has the URL and the
	// fingerprints, not the code.
	for name, refused := range map[string]struct{ text, says string }{
		"tampered listing": {manifest("tampered.asc", "signer.asc", signer.Fingerprint()), "does not verify"},
		"block's other key pinned": {
			manifest("sha256sums.asc", "both.asc", other.Fingerprint()),
			"is signed by " + signer.Fingerprint() + ", not by the pinned key " + other.Fingerprint(),
		},
		"two signers, pin not in block": {
			manifest("both.asc", "other.asc", signer.Fingerprint()),
			"is signed by " + other.Fingerprint() + ", not by the pinned key " + signer.Fingerprint(),
		},
		"block without the signer": {
			manifest("sha256sums.asc", "other.asc", signer.Fingerprint()),
			"/keys/other.asc carries no key with the pinned fingerprint " + signer.Fingerprint(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := pins.Refresh(t.Context(), writePins(t, refused.text), io.Discard)
			if !errors.Is(err, pins.ErrVerify) {
				t.Fatalf("err = %v, want ErrVerify", err)
			}

			if !strings.Contains(err.Error(), refused.says) {
				t.Errorf("err = %v, want it to say %q", err, refused.says)
			}
		})
	}
}

func TestRefreshThroughDetachedSignature(t *testing.T) {
	t.Parallel()

	tarball := []byte("\x1f\x8b the xz tarball\n")

	signer, err := openpgptest.NewRSA()
	if err != nil {
		t.Fatal(err)
	}

	other, err := openpgptest.NewEd25519()
	if err != nil {
		t.Fatal(err)
	}

	// The two files upstreams publish: an armored .asc, a binary .sig in
	// GnuPG's old packet format.
	armored, err := signer.SignDetached(tarball, crypto.SHA256, openpgptest.Detached{Armored: true})
	if err != nil {
		t.Fatal(err)
	}

	binary, err := signer.SignDetached(tarball, crypto.SHA512, openpgptest.Detached{OldHeader: true})
	if err != nil {
		t.Fatal(err)
	}

	// A file two keys signed, the pinned signer second, as GnuPG's own
	// tarballs are: two packets in one .sig, every release key in one block.
	otherFirst, err := other.SignDetached(tarball, crypto.SHA512, openpgptest.Detached{OldHeader: true})
	if err != nil {
		t.Fatal(err)
	}

	host := artifactHost(t, map[string]string{
		"/releases/v5.8.4/xz-5.8.4.tar.xz":      string(tarball),
		"/releases/v5.8.4/xz-5.8.4.tar.xz.asc":  string(armored),
		"/releases/v5.8.4/xz-5.8.4.tar.xz.sig":  string(binary),
		"/releases/v5.8.4/xz-5.8.4.tar.xz.both": string(otherFirst) + string(binary),
		"/releases/v5.8.4/tampered.tar.xz":      string(tarball) + "!",
		"/keys/signer.asc":                      string(signer.PublicKeyBlock()),
		"/keys/other.asc":                       string(other.PublicKeyBlock()),
		"/keys/both.asc":                        string(openpgptest.KeyBlock(other, signer)),
	})

	manifest := func(name, sig, key, fingerprint string) string {
		return `pins:
  - name: xz
    renovate: github-releases tukaani-project/xz
    extract-version: ^v(?<version>.*)$
    version: 5.8.4
    url: ` + host.URL + `/releases/v${version}/` + name + `
    verify: pgp-detached ` + host.URL + `/releases/v${version}/xz-${version}.tar.xz` + sig + ` ` + host.URL + `/keys/` + key + ` ` + fingerprint + `
    digest:
      version: 5.8.3
      sha256: ` + strings.Repeat("1", 64) + `
`
	}

	sum := sha256.Sum256(tarball)

	for name, published := range map[string][2]string{
		"armored":                   {".asc", "signer.asc"},
		"binary":                    {".sig", "signer.asc"},
		"pinned signer second":      {".both", "both.asc"},
		"pinned key alone in block": {".both", "signer.asc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := writePins(
				t,
				manifest("xz-${version}.tar.xz", published[0], published[1], strings.ToLower(signer.Fingerprint())),
			)

			changed, err := pins.Refresh(t.Context(), root, io.Discard)
			if err != nil {
				t.Fatalf("refresh: %v", err)
			}

			if !slices.Equal(changed, []string{"xz"}) {
				t.Errorf("changed = %v, want xz", changed)
			}

			parsed, err := pins.Parse(mustRead(t, filepath.Join(root, pins.File)))
			if err != nil {
				t.Fatal(err)
			}

			if got, _ := parsed.Get("xz", "sha256"); got != hex.EncodeToString(sum[:]) {
				t.Errorf("xz sha256 = %s, want the artifact's %x", got, sum)
			}
		})
	}

	// Each refusal says why: the reader of a red refresh has the URL and the
	// fingerprints, not the code.
	for name, refused := range map[string]struct{ text, says string }{
		"tampered artifact": {
			manifest("tampered.tar.xz", ".asc", "signer.asc", signer.Fingerprint()), "does not verify",
		},
		"block's other key pinned": {
			manifest("xz-${version}.tar.xz", ".sig", "both.asc", other.Fingerprint()),
			"is signed by " + signer.Fingerprint() + ", not by the pinned key " + other.Fingerprint(),
		},
		"two signers, pin not in block": {
			manifest("xz-${version}.tar.xz", ".both", "other.asc", signer.Fingerprint()),
			"is signed by " + other.Fingerprint() + ", not by the pinned key " + signer.Fingerprint(),
		},
		"block without the signer": {
			manifest("xz-${version}.tar.xz", ".asc", "other.asc", signer.Fingerprint()),
			"/keys/other.asc carries no key with the pinned fingerprint " + signer.Fingerprint(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := pins.Refresh(t.Context(), writePins(t, refused.text), io.Discard)
			if !errors.Is(err, pins.ErrVerify) {
				t.Fatalf("err = %v, want ErrVerify", err)
			}

			if !strings.Contains(err.Error(), refused.says) {
				t.Errorf("err = %v, want it to say %q", err, refused.says)
			}
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// TestManualPin: a pin no datasource serves says `renovate: manual`, the one
// word; it parses, Renovate's regex does not read it, and a depName beside
// the word is refused.
func TestManualPin(t *testing.T) {
	t.Parallel()

	manual := `pins:
  - name: sde
    renovate: manual
    version: 10.13.1
    url: https://h/sde-external-${version}-lin.tar.xz
    verify: download
`

	parsed, err := pins.Parse([]byte(manual))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !parsed.Entries[0].ManualBump() {
		t.Error("the entry is not read as a manual bump")
	}

	if _, err := pins.Parse(
		[]byte(strings.Replace(manual, "renovate: manual", "renovate: manual intel/sde", 1)),
	); err == nil {
		t.Error("manual with a depName must be refused")
	}

	matcher := regexp.MustCompile(renovatePinsPattern(t))
	if matcher.MatchString("    renovate: manual\n    version: 10.13.1\n    url: https://h/x\n") {
		t.Error("the preset's regex must leave a manual pin alone")
	}
}

// TestRefreshThroughCMS: an artifact with a detached CMS signature beside it
// is hashed once its signature verifies, by a signer matching the regexp,
// chaining to the pinned root; every departure is refused.
func TestRefreshThroughCMS(t *testing.T) {
	t.Parallel()

	artifact := []byte("the sde tarball\n")

	chain, err := cmstest.NewChain(cmstest.Options{RSA: true, Subject: pkix.Name{
		Country: []string{"US"}, Organization: []string{"Intel Corporation"}, CommonName: "Intel Corporation",
	}})
	if err != nil {
		t.Fatal(err)
	}

	signature, err := chain.Sign(artifact, cmstest.SignOptions{OmitRoot: true})
	if err != nil {
		t.Fatal(err)
	}

	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chain.Root.Raw})

	other, err := cmstest.NewChain(cmstest.Options{})
	if err != nil {
		t.Fatal(err)
	}

	host := artifactHost(t, map[string]string{
		"/dl/sde-external-10.13.1-lin.tar.xz":     string(artifact),
		"/dl/sde-external-10.13.1-lin.tar.xz.sig": string(signature),
		"/dl/tampered.tar.xz":                     string(artifact) + "!",
		"/ca/root.pem":                            string(rootPEM),
	})

	manifest := func(name, subject, pin string) string {
		return `pins:
  - name: sde
    renovate: manual
    version: 10.13.1
    url: ` + host.URL + `/dl/` + name + `
    verify: cms ` + host.URL + `/dl/sde-external-${version}-lin.tar.xz.sig ` + subject + ` ` + host.URL + `/ca/root.pem ` + pin + `
`
	}

	root := writePins(
		t,
		manifest(
			"sde-external-${version}-lin.tar.xz",
			`^CN=Intel\sCorporation,O=Intel\sCorporation,`,
			cms.Fingerprint(chain.Root),
		),
	)

	changed, err := pins.Refresh(t.Context(), root, io.Discard)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if !slices.Equal(changed, []string{"sde"}) {
		t.Errorf("changed = %v, want sde", changed)
	}

	parsed, err := pins.Parse(mustRead(t, filepath.Join(root, pins.File)))
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(artifact)
	if got, _ := parsed.Get("sde", "sha256"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("sde sha256 = %s, want the artifact's", got)
	}

	for name, text := range map[string]string{
		"tampered artifact":   manifest("tampered.tar.xz", "^CN=Intel", cms.Fingerprint(chain.Root)),
		"another root pinned": manifest("sde-external-${version}-lin.tar.xz", "^CN=Intel", cms.Fingerprint(other.Root)),
		"signer not the one":  manifest("sde-external-${version}-lin.tar.xz", "^CN=Microsoft", cms.Fingerprint(chain.Root)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := pins.Refresh(t.Context(), writePins(t, text), io.Discard)
			if !errors.Is(err, pins.ErrVerify) {
				t.Errorf("error = %v, want a verification failure", err)
			}
		})
	}
}

// Black-box tests of the pins manifest: parsed, read, and refreshed through
// the exported API, with a local server standing in for the artifact hosts
// and this binary standing in for gh and cosign on PATH.

package pins_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/pins"
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

	fmt.Fprintln(file, strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")+" "+strings.Join(os.Args[1:], " "))

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

	manifest, err := pins.Parse([]byte(fmt.Sprintf(fixture, "https://h", "https://h", "https://h", "https://h")))
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
		got, err := manifest.Get(tc.name, tc.field)
		if err != nil || got != tc.want {
			t.Errorf("Get(%s, %s) = %q, %v; want %q", tc.name, tc.field, got, err, tc.want)
		}
	}

	if _, err := manifest.Get("tool", "digest"); !errors.Is(err, pins.ErrNoSuchField) {
		t.Errorf("an unserved field: %v, want ErrNoSuchField", err)
	}

	if _, err := manifest.Get("nope", "version"); !errors.Is(err, pins.ErrNoSuchPin) {
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
		"no pins section":    {"other:\n  - name: a\n", pins.ErrSyntax},
		"field before pins":  {"  - name: a\npins:\n", pins.ErrSyntax},
		"bad indentation":    {strings.Replace(good, "    url:", "   url:", 1), pins.ErrSyntax},
		"unknown field":      {strings.Replace(good, "    url:", "    where:", 1), pins.ErrEntry},
		"bad name":           {strings.Replace(good, "name: a", "name: A_b", 1), pins.ErrEntry},
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
		"no digest": {
			strings.Replace(good, "    digest:\n      version: 1\n      sha256: "+strings.Repeat("0", 64)+"\n", "", 1),
			pins.ErrEntry,
		},
		"duplicate name": {good + strings.TrimPrefix(good, "pins:\n"), pins.ErrEntry},
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
// bytes at its url, in place — the two digest lines change, comments and
// spacing included nothing else does — and a current pin is left alone.
func TestRefreshDownload(t *testing.T) { // Serial by design: t.Setenv forbids t.Parallel.
	const tarball = "the tool, version 2"

	host := artifactHost(t, map[string]string{"/releases/tool-2.0.0.tar.gz": tarball})
	text := fmt.Sprintf(fixture, host.URL, host.URL, host.URL, host.URL)
	root := writePins(t, text)

	t.Setenv(verifierEnv, filepath.Join(t.TempDir(), "log")) // kernel is current: cosign must not even be asked

	var progress strings.Builder

	changed, err := pins.Refresh(context.Background(), root, &progress)
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
		"      version: 2.0.0\n      sha256: "+sha256Of(tarball)+" # computed for 1.0.0",
		1,
	)
	if string(after) != want {
		t.Errorf("refresh rewrote more than the two digest lines:\n%s", after)
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

	if changed, err := pins.Refresh(context.Background(), root, io.Discard); err != nil || len(changed) != 0 {
		t.Errorf("second refresh: %v, %v; want nothing", changed, err)
	}

	if again, _ := os.ReadFile(filepath.Join(root, pins.File)); string(again) != string(before) {
		t.Error("a refresh with nothing to do rewrote the file")
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
	)

	host := artifactHost(t, map[string]string{
		"/releases/download/llvmorg-22.1.8/LLVM-22.1.8.tar.xz": llvm,
		"/pub/v7.x/SHA256SUMS":                                 kernelSum + "  linux-7.2.6.tar.xz\n",
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
`
	root := writePins(t, text)
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv(verifierEnv, log)

	changed, err := pins.RefreshAll(context.Background(), root, io.Discard)
	if err != nil {
		t.Fatalf("refresh -all: %v", err)
	}

	if !slices.Equal(changed, []string{"llvm", "kernel"}) {
		t.Errorf("changed = %v, want both", changed)
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

	calls := string(mustRead(t, log))
	for _, want := range []string{
		"gh attestation verify ", "LLVM-22.1.8.tar.xz --owner llvm",
		"cosign verify-blob --bundle ", "--certificate-identity-regexp ^https://github.com/example/ --certificate-oidc-issuer https://token.actions.githubusercontent.com ",
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

	if _, err := pins.Refresh(context.Background(), root, io.Discard); err != nil {
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

	if _, err := pins.Refresh(context.Background(), root, io.Discard); !errors.Is(err, pins.ErrVerify) {
		t.Fatalf("refresh with a refusing verifier: %v, want ErrVerify", err)
	}

	if after := mustRead(t, filepath.Join(root, pins.File)); string(after) != text {
		t.Error("a refused refresh must leave the file untouched")
	}

	// A missing artifact is a refusal too.
	gone := writePins(t, strings.Replace(text, "/a-${version}.tgz", "/missing", 1))
	if _, err := pins.Refresh(context.Background(), gone, io.Discard); !errors.Is(err, pins.ErrVerify) {
		t.Errorf("refresh of a missing artifact: %v, want ErrVerify", err)
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

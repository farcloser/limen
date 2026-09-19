// Tests for the update-App automation, through the gh stub. Serial by
// design: they set the process environment (CI, BROWSER, the stub).

package github_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/farcloser/limen/internal/github"
)

const (
	testVariablePath = "GET orgs/test-org/actions/variables/" + "UPDATE_AQUA_CHECKSUM_APP_ID"
	testSecretPath   = "GET orgs/test-org/actions/secrets/" + "UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY"
	testSecretWrite  = "secret set " + "UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY" + " --org test-org"
)

// interactiveRig makes the scenario interactive whatever the runner is (the
// suite itself may run under CI) and points BROWSER at a command that opens
// nothing, so no test ever reaches a real browser.
func interactiveRig(t *testing.T) {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("BROWSER", noopBrowser(t))
}

// noopBrowser writes a command that exits 0 without doing anything: a .cmd
// on Windows (CreateProcess runs it through cmd.exe), a shell script elsewhere.
func noopBrowser(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "browser.cmd")
		if err := os.WriteFile(path, []byte("@exit /b 0\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		return path
	}

	path := filepath.Join(dir, "browser.sh")

	// #nosec G306 -- an executable stub must be executable.
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

// formURLRE finds the manifest form's URL in the progress text — the line
// the code prints for a user without a browser is the contract the test
// drives.
var formURLRE = regexp.MustCompile(`open (http://127\.0\.0\.1:\d+/) yourself`)

// approver is a progress writer that plays the human: when the manifest
// form's URL is printed, it fetches the form (handing the page to check) and
// then plays GitHub redirecting back with the code.
type approver struct {
	t     *testing.T
	check func(page string)
	code  string
	once  sync.Once
	wg    sync.WaitGroup
}

func (a *approver) Write(p []byte) (int, error) {
	if match := formURLRE.FindSubmatch(p); match != nil {
		url := string(match[1])

		a.once.Do(func() {
			a.wg.Add(1)

			go func() {
				defer a.wg.Done()

				a.approve(url)
			}()
		})
	}

	return len(p), nil
}

func (a *approver) approve(url string) {
	form, err := http.Get(url)
	if err != nil {
		a.t.Errorf("fetching the manifest form: %v", err)

		return
	}

	page, err := io.ReadAll(form.Body)
	_ = form.Body.Close()

	if err != nil {
		a.t.Errorf("reading the manifest form: %v", err)

		return
	}

	if a.check != nil {
		a.check(string(page))
	}

	redirect, err := http.Get(url + "callback?code=" + a.code)
	if err != nil {
		a.t.Errorf("playing the redirect: %v", err)

		return
	}

	_ = redirect.Body.Close()
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppAlreadyConfigured(t *testing.T) {
	logPath := stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Body: `{"name":"` + "UPDATE_AQUA_CHECKSUM_APP_ID" + `","value":"42"}`},
		testSecretPath:      {Body: `{"name":"` + "UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY" + `"}`},
		"GET orgs/test-org/installations?per_page=100": {
			Body: `{"installations":[{"app_id":42,"permissions":{"contents":"write","workflows":"write"}}]}`,
		},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusOK {
		t.Fatalf("configured org: %v (%s), want ok", finding.Status, finding.Message)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}

	if strings.Contains(string(calls), "POST") {
		t.Errorf("an already-configured org triggered a write:\n%s", calls)
	}
}

// The App is installed but was never granted workflows: write — the state
// every org registered before that permission joined the manifest is in.
// A limen bump's convergence then touches canonical workflow files and
// GitHub refuses the whole commit; the audit must say so, and say how.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppInstalledWithoutWorkflowsPermission(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Body: `{"value":"42"}`},
		testSecretPath:      {Body: `{}`},
		"GET orgs/test-org/installations?per_page=100": {
			Body: `{"installations":[{"app_id":42,"permissions":{"contents":"write"}}]}`,
		},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("installation without workflows: %v (%s), want advisory", finding.Status, finding.Message)
	}

	for _, want := range []string{"workflows: write", "Permissions & events"} {
		if !strings.Contains(finding.Message, want) {
			t.Errorf("advisory %q does not name %q", finding.Message, want)
		}
	}

	if strings.Contains(finding.Message, "contents: write,") || strings.Contains(finding.Message, "lacks contents") {
		t.Errorf("advisory %q blames the permission that IS granted", finding.Message)
	}
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Fail: true},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusUnverifiable {
		t.Fatalf("unreadable variables: %v (%s), want unverifiable", finding.Status, finding.Message)
	}
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppInstallationUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Body: `{"value":"42"}`},
		testSecretPath:      {Body: `{}`},
		"GET orgs/test-org/installations?per_page=100": {Fail: true},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusUnverifiable {
		t.Fatalf("unreadable installations: %v (%s), want unverifiable", finding.Status, finding.Message)
	}
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppHalfConfigured(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Body: `{"value":"42"}`},
		testSecretPath:      {NotFound: true},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("half-configured org: %v, want advisory", finding.Status)
	}

	if !strings.Contains(finding.Message, "UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY") {
		t.Errorf("advisory does not name the missing secret: %s", finding.Message)
	}
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppNotAnOrg(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {NotFound: true},
	})
	interactiveRig(t)

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("user-account owner: %v, want advisory", finding.Status)
	}
}

func TestEnsureUpdateAppNonInteractive(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
	})
	t.Setenv("CI", "1")

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, io.Discard)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("non-interactive environment: %v (%s), want advisory", finding.Status, finding.Message)
	}

	if !strings.Contains(finding.Message, "browser") {
		t.Errorf("advisory does not explain the browser requirement: %s", finding.Message)
	}
}

//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppCallbackTimeout(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
	})
	interactiveRig(t) // the browser "opens", the user never approves

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	finding := github.EnsureUpdateAquaChecksumApp(ctx, testOrg, io.Discard)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("abandoned browser flow: %v (%s), want advisory", finding.Status, finding.Message)
	}

	if !strings.Contains(finding.Message, "did not complete") {
		t.Errorf("advisory does not name the abandoned registration: %s", finding.Message)
	}
}

// TestEnsureUpdateAppRegisters drives the whole manifest flow: the stubbed
// browser fetches the served form (asserting the manifest it carries), then
// plays GitHub redirecting back with the code; conversion, variable, secret,
// and installation all resolve against the gh stub.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppRegisters(t *testing.T) {
	logPath := stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
		"POST app-manifests/test-code/conversions": {
			Body: `{"id":7,"slug":"limen-test-org","pem":"PRIVATE-KEY-PEM","html_url":"https://github.com/apps/limen-test-org"}`,
		},
		"POST orgs/test-org/actions/variables": {Body: `{}`},
		"GET orgs/test-org/installations?per_page=100": {
			Body: `{"installations":[{"app_id":7,"permissions":{"contents":"write","workflows":"write"}}]}`,
		},
	})

	interactiveRig(t)

	human := &approver{t: t, code: "test-code", check: func(page string) {
		if !strings.Contains(page, "organizations/test-org/settings/apps/new") {
			t.Errorf("form does not target the org's app registration:\n%s", page)
		}

		if !strings.Contains(page, "contents") {
			t.Errorf("manifest does not carry the contents permission:\n%s", page)
		}
	}}
	defer human.wg.Wait()

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, human)

	if finding.Status != github.StatusOK {
		t.Fatalf("full flow: %v (%s), want ok", finding.Status, finding.Message)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}

	if !strings.Contains(string(calls), testSecretWrite+"\nPRIVATE-KEY-PEM\n") {
		t.Errorf("the secret was not stored on the org through gh:\n%s", calls)
	}

	if !strings.Contains(string(calls), `"value":"7"`) {
		t.Errorf("the variable write does not carry the App id:\n%s", calls)
	}
}

// TestEnsureUpdateAppSecretFailure: the key is disclosed exactly once, so a
// failed secret write must say so and point at the manual recovery.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestEnsureUpdateAppSecretFailure(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
		"POST app-manifests/test-code/conversions": {
			Body: `{"id":7,"slug":"limen-test-org","pem":"PRIVATE-KEY-PEM","html_url":"https://github.com/apps/limen-test-org"}`,
		},
		"POST orgs/test-org/actions/variables": {Body: `{}`},
		testSecretWrite:                        {Fail: true},
	})

	interactiveRig(t)

	human := &approver{t: t, code: "test-code"}
	defer human.wg.Wait()

	finding := github.EnsureUpdateAquaChecksumApp(context.Background(), testOrg, human)

	if finding.Status != github.StatusAdvisory {
		t.Fatalf("failed secret write: %v (%s), want advisory", finding.Status, finding.Message)
	}

	if !strings.Contains(finding.Message, "downloadable from the App's settings page") {
		t.Errorf("advisory does not point at the key recovery path: %s", finding.Message)
	}
}

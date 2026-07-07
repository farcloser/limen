// White-box tests for the update-App automation, through the same gh stub
// seam as the audit tests. Serial by design: they mutate package seams.

package github //nolint:testpackage // white-box (see audit_test.go).

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	testVariablePath = "GET orgs/test-org/actions/variables/" + updateAppVariable
	testSecretPath   = "GET orgs/test-org/actions/secrets/" + updateAppSecret
)

// appSeams swaps every interactive seam for the given test doubles and
// restores them on cleanup. Passing nil keeps a seam's previous value.
func appSeams(t *testing.T, browser func(string) error, secret func(string, string, []byte) error) {
	t.Helper()

	previousBrowser := openBrowser
	previousSecret := writeOrgSecret
	previousInteractive := interactiveEnvironment
	previousCallbackWait := callbackWait
	previousInstallWait := installWait
	previousInstallPoll := installPoll

	t.Cleanup(func() {
		openBrowser = previousBrowser
		writeOrgSecret = previousSecret
		interactiveEnvironment = previousInteractive
		callbackWait = previousCallbackWait
		installWait = previousInstallWait
		installPoll = previousInstallPoll
	})

	if browser != nil {
		openBrowser = browser
	}

	if secret != nil {
		writeOrgSecret = secret
	}

	// The suite itself may run under CI; the environment seam answers for
	// the scenario, not for the runner.
	interactiveEnvironment = func() bool { return true }
	callbackWait = 2 * time.Second
	installWait = 2 * time.Second
	installPoll = 10 * time.Millisecond
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppAlreadyConfigured(t *testing.T) {
	logPath := stubGH(t, map[string]stubResponse{
		"GET orgs/test-org":               {Body: `{}`},
		testVariablePath:                  {Body: `{"name":"` + updateAppVariable + `","value":"42"}`},
		testSecretPath:                    {Body: `{"name":"` + updateAppSecret + `"}`},
		"GET orgs/test-org/installations": {Body: `{"installations":[{"app_id":42}]}`},
	})
	appSeams(t, nil, nil)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusOK {
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

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Fail: true},
	})
	appSeams(t, nil, nil)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusUnverifiable {
		t.Fatalf("unreadable variables: %v (%s), want unverifiable", finding.Status, finding.Message)
	}
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppInstallationUnverifiable(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org":               {Body: `{}`},
		testVariablePath:                  {Body: `{"value":"42"}`},
		testSecretPath:                    {Body: `{}`},
		"GET orgs/test-org/installations": {Fail: true},
	})
	appSeams(t, nil, nil)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusUnverifiable {
		t.Fatalf("unreadable installations: %v (%s), want unverifiable", finding.Status, finding.Message)
	}
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppHalfConfigured(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {Body: `{"value":"42"}`},
		testSecretPath:      {NotFound: true},
	})
	appSeams(t, nil, nil)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusAdvisory {
		t.Fatalf("half-configured org: %v, want advisory", finding.Status)
	}

	if !strings.Contains(finding.Message, updateAppSecret) {
		t.Errorf("advisory does not name the missing secret: %s", finding.Message)
	}
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppNotAnOrg(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {NotFound: true},
	})
	appSeams(t, nil, nil)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusAdvisory {
		t.Fatalf("user-account owner: %v, want advisory", finding.Status)
	}
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppNonInteractive(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
	})
	appSeams(t, nil, nil)

	interactiveEnvironment = func() bool { return false }

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusAdvisory {
		t.Fatalf("non-interactive environment: %v (%s), want advisory", finding.Status, finding.Message)
	}

	if !strings.Contains(finding.Message, "browser") {
		t.Errorf("advisory does not explain the browser requirement: %s", finding.Message)
	}
}

//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppCallbackTimeout(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
	})
	appSeams(t, func(string) error { return nil }, nil) // browser "opens", user never approves

	callbackWait = 50 * time.Millisecond

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusAdvisory {
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
//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppRegisters(t *testing.T) {
	logPath := stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
		"POST app-manifests/test-code/conversions": {
			Body: `{"id":7,"slug":"limen-test-org","pem":"PRIVATE-KEY-PEM","html_url":"https://github.com/apps/limen-test-org"}`,
		},
		"POST orgs/test-org/actions/variables": {Body: `{}`},
		"GET orgs/test-org/installations":      {Body: `{"installations":[{"app_id":7}]}`},
	})

	var storedOrg, storedName, storedValue string

	browser := func(url string) error {
		// The install-page call: not ours to answer.
		if !strings.HasSuffix(url, "/") {
			return nil
		}

		form, err := http.Get(url)
		if err != nil {
			return err
		}

		defer form.Body.Close()

		page, err := io.ReadAll(form.Body)
		if err != nil {
			return err
		}

		if !strings.Contains(string(page), "organizations/test-org/settings/apps/new") {
			t.Errorf("form does not target the org's app registration:\n%s", page)
		}

		if !strings.Contains(string(page), "contents") {
			t.Errorf("manifest does not carry the contents permission:\n%s", page)
		}

		redirect, err := http.Get(url + "callback?code=test-code")
		if err != nil {
			return err
		}

		return redirect.Body.Close()
	}

	secret := func(org, name string, value []byte) error {
		storedOrg, storedName, storedValue = org, name, string(value)

		return nil
	}

	appSeams(t, browser, secret)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusOK {
		t.Fatalf("full flow: %v (%s), want ok", finding.Status, finding.Message)
	}

	if storedOrg != testOrg || storedName != updateAppSecret || storedValue != "PRIVATE-KEY-PEM" {
		t.Errorf("secret stored as (%s, %s, %q), want (%s, %s, PRIVATE-KEY-PEM)",
			storedOrg, storedName, storedValue, testOrg, updateAppSecret)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}

	if !strings.Contains(string(calls), `"value":"7"`) {
		t.Errorf("the variable write does not carry the App id:\n%s", calls)
	}
}

// TestEnsureUpdateAppSecretFailure: the key is disclosed exactly once, so a
// failed secret write must say so and point at the manual recovery.
//
//nolint:paralleltest // serial by design: mutates package seams.
func TestEnsureUpdateAppSecretFailure(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org": {Body: `{}`},
		testVariablePath:    {NotFound: true},
		testSecretPath:      {NotFound: true},
		"POST app-manifests/test-code/conversions": {
			Body: `{"id":7,"slug":"limen-test-org","pem":"PRIVATE-KEY-PEM","html_url":"https://github.com/apps/limen-test-org"}`,
		},
		"POST orgs/test-org/actions/variables": {Body: `{}`},
	})

	browser := func(url string) error {
		if !strings.HasSuffix(url, "/") {
			return nil
		}

		redirect, err := http.Get(url + "callback?code=test-code")
		if err != nil {
			return err
		}

		return redirect.Body.Close()
	}

	secret := func(string, string, []byte) error { return errors.New("no admin") }

	appSeams(t, browser, secret)

	finding := EnsureUpdateAquaChecksumApp(testOrg, io.Discard)

	if finding.Status != StatusAdvisory {
		t.Fatalf("failed secret write: %v (%s), want advisory", finding.Status, finding.Message)
	}

	if !strings.Contains(finding.Message, "downloadable from the App's settings page") {
		t.Errorf("advisory does not point at the key recovery path: %s", finding.Message)
	}
}

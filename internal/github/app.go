// app.go — automates the update-aqua-checksum push credential (decision 8 of
// design/LIMEN-GH.md, documented for humans in book/tooling.md).
//
// The update-aqua-checksum workflow needs a push whose commits trigger CI; the
// canonical credential is a per-org GitHub App whose one-hour tokens the
// workflow mints at run time. Registering that App is a browser ceremony —
// GitHub has no unattended API for it — but the app-manifest flow reduces it
// to a single approval click: limen serves a pre-filled manifest form on
// localhost, GitHub redirects back with a one-time code, and converting the
// code returns the App id and private key. The id lands in the org Actions
// variable and the key in the org secret the workflow reads.
//
// The whole flow is idempotent and never fails a bootstrap: every state it
// cannot create or verify — a token without org admin, a headless terminal,
// an abandoned browser tab — degrades to a Finding the caller prints as a
// warning.

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The variable/secret contract shared with update-aqua-checksum.yaml.
const (
	updateAppVariable = "UPDATE_AQUA_CHECKSUM_APP_ID"
	updateAppSecret   = "UPDATE_AQUA_CHECKSUM_APP_PRIVATE_KEY" //nolint:gosec // G101: the secret's NAME, not a credential.
)

// checkUpdateApp labels the finding EnsureUpdateAquaChecksumApp returns.
const checkUpdateApp = "update-app"

// updateAppHomepage fills the App's mandatory homepage field.
const updateAppHomepage = "https://github.com/farcloser/limen"

// appNameLimit is GitHub's cap on App names; the default name is truncated to
// it (the manifest page lets the user edit the name before approving).
const appNameLimit = 34

var (
	errCallbackTimeout = errors.New("timed out waiting for the GitHub redirect (approve the App in the browser)")
	errNotAnOrg        = errors.New(
		"owner is not an organization (App automation is org-only; configure a repository-level variable and secret by hand)",
	)
)

// Test seams, following the ghBin precedent: package vars a test substitutes.
//
//nolint:gochecknoglobals // test seams, like ghBin.
var (
	// openBrowser opens url in the user's browser.
	openBrowser = func(url string) error {
		var name string

		var args []string

		switch runtime.GOOS {
		case "darwin":
			name, args = "open", []string{url}
		case "windows":
			name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
		default:
			name, args = "xdg-open", []string{url}
		}

		// name is from the fixed table above; url is built by this package.
		cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204: see above.
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("opening the browser: %w", err)
		}

		return nil
	}

	// writeOrgSecret stores an org Actions secret through `gh secret set`,
	// which owns the sealed-box encryption the raw API would demand of us.
	// The one sanctioned deviation from the `gh api` shape — still the gh
	// CLI, still gh's credential.
	writeOrgSecret = func(org, name string, value []byte) error {
		args := []string{"secret", "set", name, "--org", org, "--visibility", "all"}

		// ghBin is "gh" outside tests; args are fixed flags plus the org.
		cmd := exec.CommandContext(context.Background(), ghBin, args...) //nolint:gosec // G204: see above.
		cmd.Stdin = strings.NewReader(string(value))

		var stderr strings.Builder

		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh secret set %s --org %s: %w: %s", name, org, err, condenseStderr(stderr.String()))
		}

		return nil
	}

	// interactiveEnvironment reports whether a browser ceremony can happen at
	// all. CI covers GitHub Actions and every mainstream CI system.
	interactiveEnvironment = func() bool { return os.Getenv("CI") == "" }

	// callbackWait bounds the browser round-trip; installWait and installPoll
	// bound the post-registration installation check.
	callbackWait = 3 * time.Minute
	installWait  = 3 * time.Minute
	installPoll  = 5 * time.Second
)

// EnsureUpdateAquaChecksumApp converges the org toward a configured push
// credential and reports the resulting state as a single finding:
//
//	ok           — variable and secret present (created now or found), App installed
//	advisory     — a human must act (half-configured org, abandoned browser flow, user account)
//	unverifiable — the token cannot answer; nothing was changed
//
// It never returns StatusFail: bootstrap treats every non-ok as a warning.
func EnsureUpdateAquaChecksumApp(org string, progress io.Writer) Finding {
	orgAPI := orgClient(org)

	if outcome := orgAPI.api("GET", "", nil); outcome.err != nil || outcome.notFound {
		if outcome.notFound {
			return updateAppFinding(StatusAdvisory, errNotAnOrg.Error())
		}

		return updateAppFinding(StatusUnverifiable,
			fmt.Sprintf("cannot read the organization: %v — an org-admin token is required", outcome.err))
	}

	state, finding := probeUpdateApp(orgAPI)
	if finding != nil {
		return *finding
	}

	switch {
	case state.hasVariable && state.hasSecret:
		return confirmInstallation(orgAPI, state.appID,
			fmt.Sprintf("%s and %s already set", updateAppVariable, updateAppSecret))
	case state.hasVariable != state.hasSecret:
		present, missing := updateAppVariable, updateAppSecret
		if state.hasSecret {
			present, missing = updateAppSecret, updateAppVariable
		}

		return updateAppFinding(StatusAdvisory, fmt.Sprintf(
			"%s is set but %s is missing — a private key cannot be recovered; delete the stale half and rerun, or complete it by hand (book/tooling.md)",
			present,
			missing,
		))
	}

	if !interactiveEnvironment() {
		return updateAppFinding(
			StatusAdvisory,
			"App not configured, and registering one needs a browser — rerun `limen bootstrap` (or follow book/tooling.md) from a workstation",
		)
	}

	return registerUpdateApp(org, orgAPI, progress)
}

// updateAppState is what the two idempotence probes found.
type updateAppState struct {
	hasVariable bool
	appID       string
	hasSecret   bool
}

// probeUpdateApp reads the variable and the secret; a non-404 error on either
// is the "cannot verify" case and short-circuits into a finding.
func probeUpdateApp(orgAPI client) (updateAppState, *Finding) {
	var state updateAppState

	var variable struct {
		Value string `json:"value"`
	}

	outcome := orgAPI.getJSON("/actions/variables/"+updateAppVariable, &variable)
	if outcome.err != nil {
		finding := updateAppFinding(StatusUnverifiable,
			fmt.Sprintf("cannot read org Actions variables: %v — an org-admin token is required", outcome.err))

		return state, &finding
	}

	state.hasVariable = !outcome.notFound
	state.appID = variable.Value

	outcome = orgAPI.api("GET", "/actions/secrets/"+updateAppSecret, nil)
	if outcome.err != nil {
		finding := updateAppFinding(StatusUnverifiable,
			fmt.Sprintf("cannot read org Actions secrets: %v — an org-admin token is required", outcome.err))

		return state, &finding
	}

	state.hasSecret = !outcome.notFound

	return state, nil
}

// confirmInstallation checks that the App behind appID is installed on the
// org and folds the answer into the finding. An unreadable installations list
// downgrades to unverifiable — configured, but the last leg cannot be proven.
func confirmInstallation(orgAPI client, appID, configured string) Finding {
	installed, err := appInstalled(orgAPI, appID)
	if err != nil {
		return updateAppFinding(StatusUnverifiable,
			fmt.Sprintf("%s; installation not verifiable: %v", configured, err))
	}

	if !installed {
		return updateAppFinding(
			StatusAdvisory,
			fmt.Sprintf(
				"%s, but App id %s is not installed on the org — install it from the org's GitHub App settings",
				configured,
				appID,
			),
		)
	}

	return updateAppFinding(StatusOK, fmt.Sprintf("%s; App id %s installed", configured, appID))
}

// appInstallation is one entry of the org's installations list; the app id
// is the only field the check needs.
type appInstallation struct {
	AppID int64 `json:"app_id"`
}

// appInstalled reports whether the org has an installation of the App with
// the given id.
func appInstalled(orgAPI client, appID string) (bool, error) {
	var response struct {
		Installations []appInstallation `json:"installations"`
	}

	outcome := orgAPI.getJSON("/installations", &response)
	if outcome.err != nil || outcome.notFound {
		if outcome.notFound {
			return false, errEndpointNotFound
		}

		return false, outcome.err
	}

	for _, installation := range response.Installations {
		if strconv.FormatInt(installation.AppID, decimalBase) == appID {
			return true, nil
		}
	}

	return false, nil
}

// appConversion is what POST /app-manifests/{code}/conversions returns —
// the only moment GitHub ever discloses the private key.
type appConversion struct {
	ID      int64  `json:"id"`
	Slug    string `json:"slug"`
	PEM     string `json:"pem"`
	HTMLURL string `json:"html_url"`
}

// registerUpdateApp runs the manifest flow end to end: browser approval,
// code conversion, credential storage, installation. Each failure names the
// step so the finding is actionable.
func registerUpdateApp(org string, orgAPI client, progress io.Writer) Finding {
	code, err := manifestApproval(org, progress)
	if err != nil {
		return updateAppFinding(StatusAdvisory, fmt.Sprintf("App registration did not complete: %v", err))
	}

	conversion, err := convertManifestCode(code)
	if err != nil {
		return updateAppFinding(StatusAdvisory, fmt.Sprintf("converting the manifest code: %v", err))
	}

	_, _ = fmt.Fprintf(progress, "limen: App %q registered (id %d)\n", conversion.Slug, conversion.ID)

	appID := strconv.FormatInt(conversion.ID, decimalBase)
	if err := orgAPI.writeJSON("POST", "/actions/variables", map[string]string{
		"name": updateAppVariable, "value": appID, "visibility": "all",
	}); err != nil {
		return updateAppFinding(
			StatusAdvisory,
			fmt.Sprintf(
				"App id %s registered but the org variable was not stored: %v — set %s and %s by hand (book/tooling.md)",
				appID,
				err,
				updateAppVariable,
				updateAppSecret,
			),
		)
	}

	if err := writeOrgSecret(org, updateAppSecret, []byte(conversion.PEM)); err != nil {
		return updateAppFinding(
			StatusAdvisory,
			fmt.Sprintf(
				"App id %s registered and variable stored, but the secret was not: %v — the key is only downloadable from the App's settings page; set %s by hand",
				appID,
				err,
				updateAppSecret,
			),
		)
	}

	_, _ = fmt.Fprintf(progress, "limen: %s and %s set on org %s\n", updateAppVariable, updateAppSecret, org)

	return awaitInstallation(orgAPI, conversion, progress)
}

// manifestApproval serves the pre-filled manifest form on localhost, sends
// the user's browser to it, and waits for GitHub to redirect back with the
// one-time code.
func manifestApproval(org string, progress io.Writer) (string, error) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listening for the GitHub redirect: %w", err)
	}

	codes := make(chan string, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", manifestFormHandler(org, "http://"+listener.Addr().String()+"/callback"))
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, "App approved — return to the terminal.")

		select {
		case codes <- request.URL.Query().Get("code"):
		default:
		}
	})

	const headerTimeout = 10 * time.Second

	server := &http.Server{Handler: mux, ReadHeaderTimeout: headerTimeout}

	go func() { _ = server.Serve(listener) }()

	defer func() { _ = server.Close() }()

	formURL := "http://" + listener.Addr().String() + "/"

	_, _ = fmt.Fprintf(
		progress,
		"limen: registering the update App for org %s — approve it in the browser\n       (no browser? open %s yourself)\n",
		org,
		formURL,
	)

	if err := openBrowser(formURL); err != nil {
		_, _ = fmt.Fprintf(progress, "limen: %v — open the URL above manually\n", err)
	}

	select {
	case code := <-codes:
		if code == "" {
			return "", fmt.Errorf("%w: the redirect carried no code", errCallbackTimeout)
		}

		return code, nil
	case <-time.After(callbackWait):
		return "", errCallbackTimeout
	}
}

// manifestPage is the auto-submitting form that carries the manifest to
// GitHub; html/template escapes the JSON into the attribute.
//
//nolint:gochecknoglobals // compiled once, read-only.
var manifestPage = template.Must(template.New("manifest").Parse(`<!doctype html>
<html><body onload="document.forms[0].submit()">
<form action="https://github.com/organizations/{{.Org}}/settings/apps/new" method="post">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<noscript><button type="submit">Create the GitHub App</button></noscript>
</form></body></html>
`))

// manifestFormHandler renders the manifest form for org, with GitHub sent
// back to redirectURL after the user approves.
func manifestFormHandler(org, redirectURL string) http.HandlerFunc {
	manifest := struct {
		Name               string            `json:"name"`
		URL                string            `json:"url"`
		RedirectURL        string            `json:"redirect_url"`
		Public             bool              `json:"public"`
		HookAttributes     map[string]bool   `json:"hook_attributes"`
		DefaultPermissions map[string]string `json:"default_permissions"`
	}{
		Name:               updateAppName(org),
		URL:                updateAppHomepage,
		RedirectURL:        redirectURL,
		Public:             false,
		HookAttributes:     map[string]bool{"active": false},
		DefaultPermissions: map[string]string{"contents": "write"},
	}

	return func(writer http.ResponseWriter, _ *http.Request) {
		encoded, err := json.Marshal(manifest)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)

			return
		}

		if err := manifestPage.Execute(writer, map[string]string{
			"Org": org, "Manifest": string(encoded),
		}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}
}

// updateAppName is the proposed App name — GitHub App names are globally
// unique, so the org is baked in; the manifest page lets the user edit it if
// the name is nonetheless taken.
func updateAppName(org string) string {
	name := "limen-" + org
	if len(name) > appNameLimit {
		name = name[:appNameLimit]
	}

	return name
}

// convertManifestCode exchanges the one-time code for the App's identity and
// private key.
func convertManifestCode(code string) (appConversion, error) {
	var conversion appConversion

	converter := client{base: "app-manifests/" + code + "/conversions"}

	outcome := converter.api("POST", "", nil)
	if outcome.err != nil || outcome.notFound {
		if outcome.notFound {
			return conversion, errEndpointNotFound
		}

		return conversion, outcome.err
	}

	if err := json.Unmarshal(outcome.body, &conversion); err != nil {
		return conversion, fmt.Errorf("decoding the conversion response: %w", err)
	}

	return conversion, nil
}

// awaitInstallation sends the browser to the App's install page and polls
// until the installation appears (installing is the one step GitHub reserves
// for the UI). Timing out is an advisory, not a failure: the credential is
// stored, only the click is missing.
func awaitInstallation(orgAPI client, conversion appConversion, progress io.Writer) Finding {
	installURL := conversion.HTMLURL + "/installations/new"

	_, _ = fmt.Fprintf(
		progress,
		"limen: install the App on the org (all repositories) — approve it in the browser\n       (no browser? open %s yourself)\n",
		installURL,
	)

	if err := openBrowser(installURL); err != nil {
		_, _ = fmt.Fprintf(progress, "limen: %v — open the URL above manually\n", err)
	}

	appID := strconv.FormatInt(conversion.ID, decimalBase)
	deadline := time.Now().Add(installWait)

	for {
		installed, err := appInstalled(orgAPI, appID)
		if err != nil {
			return updateAppFinding(StatusUnverifiable,
				fmt.Sprintf("App id %s registered and credentials stored; installation not verifiable: %v", appID, err))
		}

		if installed {
			return updateAppFinding(StatusOK,
				fmt.Sprintf("App %q registered, credentials stored, installed (id %s)", conversion.Slug, appID))
		}

		if time.Now().After(deadline) {
			return updateAppFinding(
				StatusAdvisory,
				fmt.Sprintf(
					"App id %s registered and credentials stored, but no installation appeared — finish at %s",
					appID,
					installURL,
				),
			)
		}

		time.Sleep(installPoll)
	}
}

// updateAppFinding builds the one finding this file reports.
func updateAppFinding(status Status, message string) Finding {
	return Finding{Check: checkUpdateApp, Status: status, Message: message}
}

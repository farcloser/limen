// Package github audits a repository's GitHub settings against the canonical
// baseline of design/LIMEN-GITHUB.md and remediates what is safe to remediate.
//
// The baseline is a floor: a repository may be stricter than it, never looser.
// Every check yields one of four verdicts: ok, fail (below the floor and
// auto-fixable), advisory (below the floor but never auto-fixed — people,
// credentials, and anything whose change could lock someone out), and
// unverifiable (the API cannot answer under the current token — reported
// distinctly, never counted as ok: what cannot be verified does not pass).
//
// The API is called in-process over HTTPS, every call on one connection
// pool; gh owns the identity (the token is GH_TOKEN or GITHUB_TOKEN from the
// environment, as CI sets it, else what `gh auth token` prints), so limen
// stores no credential and the same run works on a laptop and in CI.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status is the verdict of one settings check.
type Status string

// The four verdicts (see the package documentation).
const (
	StatusOK           Status = "ok"
	StatusFail         Status = "fail"
	StatusAdvisory     Status = "advisory"
	StatusUnverifiable Status = "unverifiable"
)

// Finding is the result of one settings check against one repository.
type Finding struct {
	// Target names what was audited, and is set only by runs that audit
	// more than one thing (AuditMany): a single-target report has nothing
	// to disambiguate, and stamping it would change that report's JSON.
	Target  string `json:"target,omitempty"`
	Check   string `json:"check"`
	Status  Status `json:"status"`
	Current string `json:"current,omitempty"`
	Desired string `json:"desired,omitempty"`
	Message string `json:"message"`
}

// OK reports whether the finding needs no attention.
func (f Finding) OK() bool { return f.Status == StatusOK }

// AllOK reports whether every finding passed: any fail, advisory, or
// unverifiable verdict counts as non-compliant.
func AllOK(findings []Finding) bool {
	for _, finding := range findings {
		if !finding.OK() {
			return false
		}
	}

	return true
}

// client calls the GitHub API for one target, a repository or an
// organization.
type client struct {
	// base is the API root every path is built from: "repos/owner/name" for a
	// repository, "orgs/owner" for an organization.
	base string
}

// repoClient targets one repository ("owner/name" slug).
func repoClient(slug string) client { return client{base: "repos/" + slug} }

// orgClient targets one organization.
func orgClient(org string) client { return client{base: "orgs/" + org} }

// apiOutcome classifies one API call. notFound distinguishes the endpoints
// that answer through their status code (HTTP 404 = feature off) from real
// errors, which land in err.
type apiOutcome struct {
	err  error
	body []byte
	// notFound is the endpoints that answer through their status code
	// (HTTP 404 = feature off).
	notFound bool
	// orgEnforced is the write GitHub refused because an organization code
	// security configuration owns the setting: not a failure to apply, but a
	// statement that the repository endpoint is the wrong place to apply it.
	orgEnforced bool
}

// The transport: GitHub's REST API, versioned, over one HTTP client.
const (
	defaultAPIURL = "https://api.github.com"
	apiVersion    = "2022-11-28"
	// apiURLEnv names the API root. GitHub Actions sets it to the default;
	// the tests point it at their own server.
	apiURLEnv = "GITHUB_API_URL"
	methodGet = "GET"
	userAgent = "limen"
	// maxPages bounds a listing at ten thousand entries, the largest page a
	// hundred times: no organization, team or ruleset of the fleet comes
	// near, and a longer list is an error, never a truncated inventory.
	maxPages   = 100
	perPage    = 100
	maxRetries = 1
	// retryWait bounds how long a rate limit's Retry-After is honoured.
	retryWait        = 60 * time.Second
	defaultRetryWait = 2 * time.Second
	requestTimeout   = 60 * time.Second
	httpTooMany      = http.StatusTooManyRequests
)

// The one refusal that is not a failure to fix but a statement about who
// owns the setting: an organization code security configuration governs it,
// and the repository endpoint will refuse every attempt until the
// organization's own object changes. Matched on GitHub's prose because the
// status code alone does not distinguish it from the other 422s the settings
// endpoints return.
const enforcedConfigMarker = "enforced security configuration"

// errOrgEnforced marks a write the repository cannot win: the setting belongs
// to an enforced organization code security configuration.
var errOrgEnforced = errors.New("setting is owned by an enforced organization code security configuration")

// errPageWithoutField is an object-wrapped listing whose page lacks the
// field the caller collects — a shape the API does not answer with.
var errPageWithoutField = errors.New("a page carries no such field")

// errNoToken is a run with no credential to carry.
var errNoToken = errors.New("no GitHub token")

// errAPI is a call GitHub answered outside 2xx, other than the two statuses
// read as answers.
var errAPI = errors.New("github api")

// errTooManyPages is a list longer than the audit follows: a page is not an
// inventory, and neither is a hundred of them cut short, so the list is an
// error rather than a truncated answer.
var errTooManyPages = errors.New("the list runs past the pages followed")

// httpClient is the one pool every call shares: a repository audit is some
// forty reads, and a connection per read was most of its time.
var httpClient = &http.Client{Timeout: requestTimeout} //nolint:gochecknoglobals // one pool for the process.

// The token, resolved once for the process.
var (
	tokenOnce  sync.Once //nolint:gochecknoglobals // resolved once for the process.
	tokenValue string    //nolint:gochecknoglobals // resolved once for the process.
	errToken   error     //nolint:gochecknoglobals // resolved once for the process.
	linkNextRE = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)
)

// token is the credential every call carries: GH_TOKEN or GITHUB_TOKEN from
// the environment, what CI sets, read on every call; else what `gh auth
// token` prints, the gh on PATH owning the laptop's identity, asked once for
// the process. limen stores none.
func token(ctx context.Context) (string, error) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, nil
		}
	}

	tokenOnce.Do(func() { tokenValue, errToken = ghToken(ctx) })

	return tokenValue, errToken
}

// ghToken asks the gh on PATH for the token of the account it is signed in as.
func ghToken(ctx context.Context) (string, error) {
	var stdout, stderr bytes.Buffer

	// gh on the hermetic PATH; fixed arguments.
	cmd := exec.CommandContext(ctx, "gh", "auth", "token") // #nosec G204 -- see above.
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := condenseStderr(stderr.String())
		if detail != "" {
			detail = ": " + detail
		}

		return "", fmt.Errorf("%w: set GH_TOKEN, or sign in with `gh auth login` (gh auth token: %w%s)",
			errNoToken, err, detail)
	}

	value := strings.TrimSpace(stdout.String())
	if value == "" {
		return "", fmt.Errorf("%w: `gh auth token` printed nothing; sign in with `gh auth login`", errNoToken)
	}

	return value, nil
}

// Preflight resolves the token once, before the first call, so a run with
// no credential fails with the cause rather than reporting every check
// unverifiable.
func Preflight(ctx context.Context) error {
	_, err := token(ctx)

	return err
}

// apiURL is the API root: the environment's, else GitHub's.
func apiURL() string {
	if root := strings.TrimRight(os.Getenv(apiURLEnv), "/"); root != "" {
		return root
	}

	return defaultAPIURL
}

// orgOf returns the organization a client's base path belongs to, for
// phrasing an error that has to point somewhere else.
func (c client) orgOf() string {
	base := strings.TrimPrefix(c.base, "repos/")
	base = strings.TrimPrefix(base, "orgs/")
	owner, _, _ := strings.Cut(base, "/")

	return owner
}

// api performs one call with the given method, repo-relative path, and
// optional JSON payload, and classifies the outcome.
func (c client) api(ctx context.Context, method, path string, payload []byte) apiOutcome {
	return c.classify(request(ctx, method, c.base+path, payload))
}

// classify phrases the refusals that need to name where the setting actually
// lives — request only detects them, having no client to ask.
func (c client) classify(outcome apiOutcome) apiOutcome {
	if !outcome.orgEnforced {
		return outcome
	}

	org := c.orgOf()
	outcome.err = fmt.Errorf(
		"%w: run `limen github fix -org %s` — the repository endpoint refuses every attempt while the configuration stands",
		errOrgEnforced,
		org,
	)

	return outcome
}

// request performs one call to fullPath under the API root and classifies
// its status: a 404 is the endpoint's own answer, a 422 naming an enforced
// configuration is the organization's, anything else outside 2xx is an error
// carrying GitHub's message.
func request(ctx context.Context, method, fullPath string, payload []byte) apiOutcome {
	outcome, _ := fetchPage(ctx, method, apiURL()+"/"+fullPath, payload)

	return outcome
}

// fetchPage performs one HTTP request and returns its outcome and, for a
// list, the URL of the next page from the Link header.
func fetchPage(ctx context.Context, method, target string, payload []byte) (apiOutcome, string) {
	credential, err := token(ctx)
	if err != nil {
		return apiOutcome{err: err}, ""
	}

	for attempt := 0; ; attempt++ {
		answer, err := send(ctx, method, target, payload, credential)
		if err != nil {
			return apiOutcome{err: fmt.Errorf("%s %s: %w", method, target, err)}, ""
		}

		if wait, retry := retryAfter(answer); retry && attempt < maxRetries {
			if err := pause(ctx, wait); err != nil {
				return apiOutcome{err: fmt.Errorf("%s %s: %w", method, target, err)}, ""
			}

			continue
		}

		switch {
		case answer.status == http.StatusNotFound:
			return apiOutcome{notFound: true}, ""
		case answer.status == http.StatusUnprocessableEntity && strings.Contains(string(answer.body), enforcedConfigMarker):
			return apiOutcome{orgEnforced: true}, ""
		case answer.status < http.StatusOK || answer.status >= http.StatusMultipleChoices:
			return apiOutcome{err: fmt.Errorf("%w: %s %s: HTTP %d: %s",
				errAPI, method, target, answer.status, apiMessage(answer.body))}, ""
		}

		return apiOutcome{body: answer.body}, nextPage(answer.header.Get("Link"))
	}
}

// pause waits for the rate limit's duration, or until the run is cancelled.
func pause(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller names the call.
	}
}

// answer is what one exchange returned, the body read and closed.
type answer struct {
	header http.Header
	body   []byte
	status int
}

// send performs the HTTP exchange and reads the body.
func send(ctx context.Context, method, target string, payload []byte, credential string) (answer, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return answer{}, err //nolint:wrapcheck // the caller names the call.
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("User-Agent", userAgent)

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return answer{}, err //nolint:wrapcheck // the caller names the call.
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return answer{}, fmt.Errorf("reading the response: %w", err)
	}

	return answer{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// retryAfter reports whether an answer is a rate limit (a 429, or the 403
// GitHub's secondary limit answers with, named by Retry-After or an exhausted
// X-RateLimit-Remaining) worth one more attempt, and how long GitHub asks to
// wait, bounded. A server fault is not retried: it is reported, as the
// finding it makes unverifiable.
func retryAfter(got answer) (time.Duration, bool) {
	limited := got.status == httpTooMany ||
		(got.status == http.StatusForbidden && got.header.Get("Retry-After") != "") ||
		(got.status == http.StatusForbidden && got.header.Get("X-Ratelimit-Remaining") == "0")
	if !limited {
		return 0, false
	}

	wait := defaultRetryWait

	if seconds, err := strconv.Atoi(got.header.Get("Retry-After")); err == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	} else if reset, err := strconv.ParseInt(got.header.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
		if until := time.Until(time.Unix(reset, 0)); until > 0 {
			wait = until
		}
	}

	return min(wait, retryWait), true
}

// apiMessage reduces an error body to GitHub's message, capped: a finding
// message is a sentence, not a log dump.
func apiMessage(body []byte) string {
	const maxErrLen = 200

	var parsed struct {
		Message string `json:"message"`
	}

	message := strings.TrimSpace(string(body))
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		message = parsed.Message
	}

	if len(message) > maxErrLen {
		message = message[:maxErrLen] + "…"
	}

	return message
}

// nextPage is the URL the Link header names as the next page, or "".
func nextPage(link string) string {
	match := linkNextRE.FindStringSubmatch(link)
	if match == nil {
		return ""
	}

	return match[1]
}

// condenseStderr reduces gh's stderr to a single readable line: aqua's
// lazy-install log lines (the shim may be downloading gh itself) are dropped,
// the rest is joined, and the result is capped.
func condenseStderr(raw string) string {
	const maxErrLen = 200

	var kept []string

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "program=aqua") || strings.Contains(line, `"level":"WARN"`) {
			continue
		}

		kept = append(kept, line)
	}

	message := strings.Join(kept, " · ")
	if len(message) > maxErrLen {
		message = message[:maxErrLen] + "…"
	}

	return message
}

// Every list endpoint is read whole. The REST API pages its lists (30 entries
// by default, 100 at most), and a page is not an inventory: an owner, a team,
// a ruleset, or an installed App on the second page is exactly as real as one
// on the first, and an audit that judged the first page alone would report
// the rest as absent — creating a ruleset that exists, flagging a grant that
// was made, missing an owner nobody declared. So no list is fetched with
// getJSON: arrays go through getJSONAllPages, object-wrapped lists through
// listPages, and both ask for the largest page (per_page=100 in the path) and
// follow the Link headers to the last.

// pages fetches every page of a list, following the Link headers.
func (c client) pages(ctx context.Context, path string) ([][]byte, apiOutcome) {
	target := apiURL() + "/" + c.base + path

	var bodies [][]byte

	for target != "" {
		if len(bodies) == maxPages {
			return nil, apiOutcome{
				err: fmt.Errorf("GET %s: %w (%d of %d entries each)", path, errTooManyPages, maxPages, perPage),
			}
		}

		outcome, next := fetchPage(ctx, methodGet, target, nil)
		if outcome.err != nil || outcome.notFound {
			return nil, c.classify(outcome)
		}

		bodies = append(bodies, outcome.body)
		target = next
	}

	return bodies, apiOutcome{}
}

// getJSONAllPages fetches every page of a list endpoint whose response is a
// JSON array and decodes the merged array into out.
func (c client) getJSONAllPages(ctx context.Context, path string, out any) apiOutcome {
	bodies, outcome := c.pages(ctx, path)
	if outcome.err != nil || outcome.notFound {
		return outcome
	}

	var merged []json.RawMessage

	for _, body := range bodies {
		var page []json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			outcome.err = fmt.Errorf("GET %s: decoding a page: %w", path, err)

			return outcome
		}

		merged = append(merged, page...)
	}

	all, err := json.Marshal(merged)
	if err != nil {
		outcome.err = fmt.Errorf("GET %s: merging pages: %w", path, err)

		return outcome
	}

	outcome.body = all

	if err := json.Unmarshal(all, out); err != nil {
		outcome.err = fmt.Errorf("GET %s: decoding response: %w", path, err)
	}

	return outcome
}

// listPages fetches every page of a list endpoint whose response is an object
// wrapping the list — {"total_count": n, "<field>": [...]}, the shape of the
// installations, Actions secrets, and runners endpoints — and returns the
// field's entries from all pages, in order.
func listPages[T any](ctx context.Context, c client, path, field string) ([]T, apiOutcome) {
	bodies, outcome := c.pages(ctx, path)
	if outcome.err != nil || outcome.notFound {
		return nil, outcome
	}

	var entries []T

	for _, body := range bodies {
		var page map[string]json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			outcome.err = fmt.Errorf("GET %s: decoding a page: %w", path, err)

			return nil, outcome
		}

		raw, present := page[field]
		if !present {
			outcome.err = fmt.Errorf("GET %s: %w: %q", path, errPageWithoutField, field)

			return nil, outcome
		}

		var pageEntries []T
		if err := json.Unmarshal(raw, &pageEntries); err != nil {
			outcome.err = fmt.Errorf("GET %s: decoding %q: %w", path, field, err)

			return nil, outcome
		}

		entries = append(entries, pageEntries...)
	}

	return entries, outcome
}

// getJSON fetches a repo-relative path and decodes the JSON response into out.
// For single objects only — never a list endpoint (see pages).
func (c client) getJSON(ctx context.Context, path string, out any) apiOutcome {
	outcome := c.api(ctx, methodGet, path, nil)
	if outcome.err != nil || outcome.notFound {
		return outcome
	}

	if err := json.Unmarshal(outcome.body, out); err != nil {
		outcome.err = fmt.Errorf("GET %s: decoding response: %w", path, err)
	}

	return outcome
}

// deleteResource issues DELETE on a repo-relative path and reports the error,
// if any. A 404 is an error here, as in writeJSON: the caller only deletes
// what the audit just observed, so "not found" means the endpoint, not the
// setting, is missing.
func (c client) deleteResource(ctx context.Context, path string) error {
	outcome := c.api(ctx, http.MethodDelete, path, nil)
	if outcome.notFound {
		return fmt.Errorf("DELETE %s: %w", path, errEndpointNotFound)
	}

	return outcome.err
}

// writeJSON sends payload (marshaled) to a repo-relative path with the given
// method and reports the error, if any.
func (c client) writeJSON(ctx context.Context, method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding %s %s payload: %w", method, path, err)
	}

	outcome := c.api(ctx, method, path, body)
	if outcome.notFound {
		return fmt.Errorf("%s %s: %w", method, path, errEndpointNotFound)
	}

	return outcome.err
}

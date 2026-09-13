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
// All GitHub access goes through the gh CLI (`gh api …`): gh owns
// authentication, limen never sees a credential, and the same invocation works
// on a laptop and in CI.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ghBin is the GitHub CLI executable the package shells out to; a package var
// so tests can substitute a stub.
var ghBin = "gh" //nolint:gochecknoglobals // test seam: tests substitute a stub binary.

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

// client calls the GitHub API for one target — a repository or an
// organization — through the gh CLI.
type client struct {
	// base is the API root every path is built from: "repos/owner/name" for a
	// repository, "orgs/owner" for an organization.
	base string
}

// repoClient targets one repository ("owner/name" slug).
func repoClient(slug string) client { return client{base: "repos/" + slug} }

// orgClient targets one organization.
func orgClient(org string) client { return client{base: "orgs/" + org} }

// apiOutcome classifies one gh api invocation. notFound distinguishes the
// endpoints that answer through their status code (HTTP 404 = feature off)
// from real errors, which land in err.
type apiOutcome struct {
	err      error
	body     []byte
	notFound bool
}

// httpNotFoundMarker is how gh reports a 404 on stderr.
const httpNotFoundMarker = "(HTTP 404)"

// methodGet is the one method every read shares.
const methodGet = "GET"

// errPageWithoutField is an object-wrapped listing whose page lacks the
// field the caller collects — a shape the API does not answer with.
var errPageWithoutField = errors.New("a page carries no such field")

// api runs `gh api` with the given method, repo-relative path, and optional
// JSON payload (sent via --input -), and classifies the outcome.
func (c client) api(method, path string, payload []byte) apiOutcome {
	fullPath := c.base + path

	args := []string{"api", "--method", method, fullPath}
	if payload != nil {
		args = append(args, "--input", "-")
	}

	return runGH(args, method, fullPath, payload)
}

// Every list endpoint is read whole. The REST API pages its lists (30 entries
// by default, 100 at most), and a page is not an inventory: an owner, a team,
// a ruleset, or an installed App on the second page is exactly as real as one
// on the first, and an audit that judged the first page alone would report
// the rest as absent — creating a ruleset that exists, flagging a grant that
// was made, missing an owner nobody declared. So no list is fetched with
// getJSON: arrays go through getJSONAllPages, object-wrapped lists through
// listPages, and both ask for the largest page (per_page=100 in the path) so
// gh follows as few Link headers as possible.

// apiAllPages is api's paginating GET: gh follows the Link headers and merges
// the pages of an array response into one array, so callers decode exactly
// what they decode from a single page. Object-wrapped lists add --slurp (see
// listPages).
//
// --paginate and --slurp go LAST. The test stub reads the method and path off
// fixed argv positions, and a flag inserted ahead of them shifts every key.
func (c client) apiAllPages(path string, flags ...string) apiOutcome {
	fullPath := c.base + path

	args := append([]string{"api", "--method", methodGet, fullPath, "--paginate"}, flags...)

	return runGH(args, methodGet, fullPath, nil)
}

// runGH executes one gh invocation and classifies the outcome; method and
// fullPath are carried only to phrase the error.
func runGH(args []string, method, fullPath string, payload []byte) apiOutcome {
	// The rules API carries no context; Background is the honest choice.
	// ghBin is "gh" outside tests (a package seam, not user input), and every
	// argument is a fixed API path built above.
	cmd := exec.CommandContext(context.Background(), ghBin, args...) // #nosec G204 -- see above.
	if payload != nil {
		cmd.Stdin = bytes.NewReader(payload)
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), httpNotFoundMarker) {
			return apiOutcome{notFound: true}
		}

		return apiOutcome{
			err: fmt.Errorf("gh api %s %s: %w: %s", method, fullPath, err, condenseStderr(stderr.String())),
		}
	}

	return apiOutcome{body: stdout.Bytes()}
}

// condenseStderr reduces gh's stderr to a single readable line: aqua's
// lazy-install log lines (the shim may be downloading gh itself) are dropped,
// the rest is joined, and the result is capped — a finding message is a
// sentence, not a log dump.
func condenseStderr(raw string) string {
	const maxErrLen = 200

	var kept []string

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "program=aqua") {
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

// getJSONAllPages fetches every page of a list endpoint whose response is a
// JSON array and decodes the merged array into out.
func (c client) getJSONAllPages(path string, out any) apiOutcome {
	outcome := c.apiAllPages(path)
	if outcome.err != nil || outcome.notFound {
		return outcome
	}

	if err := json.Unmarshal(outcome.body, out); err != nil {
		outcome.err = fmt.Errorf("gh api GET %s (paginated): decoding response: %w", path, err)
	}

	return outcome
}

// listPages fetches every page of a list endpoint whose response is an object
// wrapping the list — {"total_count": n, "<field>": [...]}, the shape of the
// installations, Actions secrets, and runners endpoints — and returns the
// field's entries from all pages, in order. gh merges array pages only;
// --slurp hands object pages back as one array of page objects, and the
// field is collected from each.
func listPages[T any](c client, path, field string) ([]T, apiOutcome) {
	outcome := c.apiAllPages(path, "--slurp")
	if outcome.err != nil || outcome.notFound {
		return nil, outcome
	}

	var pages []map[string]json.RawMessage

	if err := json.Unmarshal(outcome.body, &pages); err != nil {
		outcome.err = fmt.Errorf("gh api GET %s (paginated): decoding pages: %w", path, err)

		return nil, outcome
	}

	var entries []T

	for _, page := range pages {
		raw, present := page[field]
		if !present {
			outcome.err = fmt.Errorf("gh api GET %s (paginated): %w: %q", path, errPageWithoutField, field)

			return nil, outcome
		}

		var pageEntries []T

		if err := json.Unmarshal(raw, &pageEntries); err != nil {
			outcome.err = fmt.Errorf("gh api GET %s (paginated): decoding %q: %w", path, field, err)

			return nil, outcome
		}

		entries = append(entries, pageEntries...)
	}

	return entries, outcome
}

// getJSON fetches a repo-relative path and decodes the JSON response into out.
// For single objects only — never a list endpoint (see apiAllPages).
func (c client) getJSON(path string, out any) apiOutcome {
	outcome := c.api(methodGet, path, nil)
	if outcome.err != nil || outcome.notFound {
		return outcome
	}

	if err := json.Unmarshal(outcome.body, out); err != nil {
		outcome.err = fmt.Errorf("gh api GET %s: decoding response: %w", path, err)
	}

	return outcome
}

// deleteResource issues DELETE on a repo-relative path and reports the error,
// if any. A 404 is an error here, as in writeJSON: the caller only deletes
// what the audit just observed, so "not found" means the endpoint, not the
// setting, is missing.
func (c client) deleteResource(path string) error {
	outcome := c.api("DELETE", path, nil)
	if outcome.notFound {
		return fmt.Errorf("gh api DELETE %s: %w", path, errEndpointNotFound)
	}

	return outcome.err
}

// writeJSON sends payload (marshaled) to a repo-relative path with the given
// method and reports the error, if any.
func (c client) writeJSON(method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding %s %s payload: %w", method, path, err)
	}

	outcome := c.api(method, path, body)
	if outcome.notFound {
		return fmt.Errorf("gh api %s %s: %w", method, path, errEndpointNotFound)
	}

	return outcome.err
}

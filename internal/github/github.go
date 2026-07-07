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

// api runs `gh api` with the given method, repo-relative path, and optional
// JSON payload (sent via --input -), and classifies the outcome.
func (c client) api(method, path string, payload []byte) apiOutcome {
	fullPath := c.base + path

	args := []string{"api", "--method", method, fullPath}
	if payload != nil {
		args = append(args, "--input", "-")
	}

	// The rules API carries no context; Background is the honest choice.
	// ghBin is "gh" outside tests (a package seam, not user input), and every
	// argument is a fixed API path built above.
	cmd := exec.CommandContext(context.Background(), ghBin, args...) //nolint:gosec // G204: see above.
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

// getJSON fetches a repo-relative path and decodes the JSON response into out.
func (c client) getJSON(path string, out any) apiOutcome {
	outcome := c.api("GET", path, nil)
	if outcome.err != nil || outcome.notFound {
		return outcome
	}

	if err := json.Unmarshal(outcome.body, out); err != nil {
		outcome.err = fmt.Errorf("gh api GET %s: decoding response: %w", path, err)
	}

	return outcome
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

// identity.go — resolves the commit identity of an organization's
// update-aqua-checksum App, so the tree rules can keep it in every
// repository's renovate.json5 gitIgnoredAuthors (book/tooling.md).
//
// The workflow commits through the createCommitOnBranch mutation, which
// attributes the commit to the token's identity: with the App configured that
// is the App's bot user, "<slug>[bot]", whose noreply address is
// "<user-id>+<slug>[bot]@users.noreply.github.com". Renovate must be told to
// ignore that author or it treats every fix-up as a human edit and stops
// rebasing the branch — the failure mode this file exists to prevent.
//
// Two entry points, deliberately, because the two limen commands that need
// the identity have different contracts:
//
//   - ResolveUpdateAppIdentity is for `limen check`: DETERMINISTIC. It uses
//     nothing but the public users endpoint and the naming convention
//     (updateAppName), so a laptop with an admin gh token and a CI runner with
//     no token reach the same verdict — "CI green means the same as local
//     green" is the whole point of check, and a verdict that depended on the
//     caller's credentials broke it (local red, CI green, on the same tree).
//   - DiscoverUpdateAppIdentity is for `limen fix` and `bootstrap`: the
//     human, mutating, credentialed step. With an org-admin gh token it reads
//     the App back from the org (the variable UPDATE_AQUA_CHECKSUM_APP_ID
//     names it, the installation list gives its slug — so a renamed App is
//     found), and writes the truth into the tree; without one it falls back to
//     the convention. Once fix has written the identity, check has nothing to
//     resolve on that point: it verifies the file, and a renamed App simply
//     reads as "unknown" to check rather than as a failure.
//
// Either way the bot user id comes from the public users endpoint, which
// needs no token at all.

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Test seams, following the ghBin precedent.
var (
	// usersAPIBase is where the public users endpoint lives; tests point it at
	// an httptest server.
	usersAPIBase = "https://api.github.com" //nolint:gochecknoglobals // test seam.
	// usersHTTPClient makes the one public request; short timeout so an
	// offline `limen check` degrades to "unresolved" quickly rather than hang.
	usersHTTPClient = &http.Client{Timeout: 5 * time.Second} //nolint:gochecknoglobals // test seam.
)

// ErrUpdateAppUnknown means the organization has no update-App identity that
// could be resolved: no App registered under the expected name, or the
// endpoint could not be reached. Callers treat it as "not enforced".
var ErrUpdateAppUnknown = errors.New("update-app identity could not be resolved")

// UpdateAppIdentity is the App's commit identity as Renovate sees it.
type UpdateAppIdentity struct {
	// Slug is the App's slug, e.g. "limen-ci-forkcloser".
	Slug string
	// UserID is the numeric id of the "<slug>[bot]" user.
	UserID int64
}

// Email is the noreply address GitHub attributes the App's commits to — the
// exact string that belongs in renovate.json5's gitIgnoredAuthors.
func (i UpdateAppIdentity) Email() string {
	return strconv.FormatInt(i.UserID, decimalBase) + "+" + i.Slug + "[bot]@users.noreply.github.com"
}

// ResolveUpdateAppIdentity finds the update-App identity for org
// deterministically: the convention slug (updateAppName), the public users
// endpoint, nothing that depends on the caller's credentials. For `limen
// check`. It never registers anything; a missing App is ErrUpdateAppUnknown,
// and so is any transport failure (wrapped, so the cause is printable).
func ResolveUpdateAppIdentity(org string) (UpdateAppIdentity, error) {
	return identityFor(updateAppName(org))
}

// DiscoverUpdateAppIdentity finds the update-App identity for org using every
// means available: the App read back from the org through gh when the token
// can (org variable → installation list, so a renamed App is found), the
// convention otherwise. For `limen fix` and `bootstrap`, which write the
// answer into the tree — never for check, whose verdict must not depend on
// who runs it.
func DiscoverUpdateAppIdentity(org string) (UpdateAppIdentity, error) {
	return identityFor(discoveredUpdateAppSlug(org))
}

// identityFor completes an identity from a slug: the bot user's id.
func identityFor(slug string) (UpdateAppIdentity, error) {
	userID, err := botUserID(slug)
	if err != nil {
		return UpdateAppIdentity{}, err
	}

	return UpdateAppIdentity{Slug: slug, UserID: userID}, nil
}

// discoveredUpdateAppSlug is the App's slug read back from the org through
// gh when the token can (org variable → installation list), the limen naming
// convention otherwise. Every failure on the authed path is silent by design
// — a laptop without gh, a CI runner without a token, a token without org
// admin — because the convention is the right answer for every App limen
// registered under its default name.
func discoveredUpdateAppSlug(org string) string {
	orgAPI := orgClient(org)

	var variable struct {
		Value string `json:"value"`
	}

	if outcome := orgAPI.getJSON("/actions/variables/"+updateAppVariable, &variable); outcome.err != nil ||
		outcome.notFound || variable.Value == "" {
		return updateAppName(org)
	}

	var installations struct {
		Installations []orgAppInstallationRef `json:"installations"`
	}

	if outcome := orgAPI.getJSON("/installations", &installations); outcome.err != nil || outcome.notFound {
		return updateAppName(org)
	}

	for _, installation := range installations.Installations {
		if strconv.FormatInt(installation.AppID, decimalBase) == variable.Value && installation.AppSlug != "" {
			return installation.AppSlug
		}
	}

	return updateAppName(org)
}

// orgAppInstallationRef is the installation-list subset the slug lookup
// reads: which App (by id) is installed under which slug.
type orgAppInstallationRef struct {
	AppID   int64  `json:"app_id"`
	AppSlug string `json:"app_slug"`
}

// botUserID looks up the "<slug>[bot]" user through the public users
// endpoint. Unauthenticated works (60 requests/hour/IP); a GH_TOKEN or
// GITHUB_TOKEN in the environment is sent when present, for the rate limit
// only — the endpoint returns the same public record either way.
func botUserID(slug string) (int64, error) {
	endpoint := usersAPIBase + "/users/" + url.PathEscape(slug+"[bot]")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrUpdateAppUnknown, err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	if token := firstEnv("GH_TOKEN", "GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := usersHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrUpdateAppUnknown, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("%w: no GitHub App user %q — the App is not registered under that name",
			ErrUpdateAppUnknown, slug+"[bot]")
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: GET %s: HTTP %d", ErrUpdateAppUnknown, endpoint, resp.StatusCode)
	}

	var user struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, fmt.Errorf("%w: reading %s: %w", ErrUpdateAppUnknown, endpoint, err)
	}

	if err := json.Unmarshal(body, &user); err != nil {
		return 0, fmt.Errorf("%w: decoding %s: %w", ErrUpdateAppUnknown, endpoint, err)
	}

	if user.ID == 0 || user.Type != "Bot" {
		return 0, fmt.Errorf("%w: %q is not a GitHub App bot user", ErrUpdateAppUnknown, slug+"[bot]")
	}

	return user.ID, nil
}

// firstEnv returns the first non-empty environment variable among names.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}

	return ""
}

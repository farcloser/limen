// Tests for the update-App identity resolver: the public users endpoint
// through an httptest server named by GITHUB_API_URL, the authed slug lookup
// through the gh stub.

package github_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/farcloser/limen/internal/github"
)

// usersServer serves the public users endpoint for the given bot logins.
func usersServer(t *testing.T, users map[string]string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, found := users[r.URL.Path]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))

			return
		}

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
}

// TestResolveUpdateAppIdentityConvention: without an authed gh (every org
// endpoint fails), the slug is limen's naming convention and the id comes
// from the users endpoint. The email is the exact noreply form.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestResolveUpdateAppIdentityConvention(t *testing.T) {
	stubGH(t, map[string]stubResponse{}) // nothing answers: unauthenticated laptop
	usersServer(t, map[string]string{
		"/users/limen-ci-test-org[bot]": `{"id": 317468017, "login": "limen-ci-test-org[bot]", "type": "Bot"}`,
	})

	identity, err := github.ResolveUpdateAppIdentity("test-org")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if identity.Slug != "limen-ci-test-org" || identity.UserID != 317468017 {
		t.Errorf("identity = %+v", identity)
	}

	if got, want := identity.Email(), "317468017+limen-ci-test-org[bot]@users.noreply.github.com"; got != want {
		t.Errorf("email = %q, want %q", got, want)
	}
}

// TestDiscoverUpdateAppIdentityRenamed: with an org-admin token, Discover
// reads the App back from the org — the variable names its id, the
// installation list its slug — so a renamed App is found. Resolve, on the
// same org with the same token, deliberately does NOT see it: check's answer
// must not depend on who runs it, so it knows only the convention name (which
// here does not exist) and reports unknown.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestDiscoverUpdateAppIdentityRenamed(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org/actions/variables/UPDATE_AQUA_CHECKSUM_APP_ID": {
			Body: `{"name": "UPDATE_AQUA_CHECKSUM_APP_ID", "value": "4242"}`,
		},
		"GET orgs/test-org/installations?per_page=100": {
			Body: `{"total_count": 2, "installations": [
				{"app_id": 1, "app_slug": "renovate"},
				{"app_id": 4242, "app_slug": "our-ci-pusher"}]}`,
		},
	})
	usersServer(t, map[string]string{
		"/users/our-ci-pusher[bot]": `{"id": 99, "login": "our-ci-pusher[bot]", "type": "Bot"}`,
	})

	identity, err := github.DiscoverUpdateAppIdentity("test-org")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if identity.Slug != "our-ci-pusher" || identity.UserID != 99 {
		t.Errorf("identity = %+v, want the renamed App", identity)
	}

	if _, err := github.ResolveUpdateAppIdentity("test-org"); !errors.Is(err, github.ErrUpdateAppUnknown) {
		t.Errorf("resolve with a renamed App: %v, want github.ErrUpdateAppUnknown (check stays credential-independent)",
			err)
	}
}

// TestDiscoverUpdateAppIdentityFallsBack: without a usable token, Discover
// gives the same answer as Resolve — the convention.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestDiscoverUpdateAppIdentityFallsBack(t *testing.T) {
	stubGH(t, map[string]stubResponse{})
	usersServer(t, map[string]string{
		"/users/limen-ci-test-org[bot]": `{"id": 317468017, "login": "limen-ci-test-org[bot]", "type": "Bot"}`,
	})

	discovered, err := github.DiscoverUpdateAppIdentity("test-org")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	resolved, err := github.ResolveUpdateAppIdentity("test-org")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if discovered != resolved {
		t.Errorf("discover %+v != resolve %+v without a token", discovered, resolved)
	}
}

// TestResolveUpdateAppIdentityUnknown: no App under the expected name is
// github.ErrUpdateAppUnknown — the callers' "not enforced" signal — and so is a
// record that is not a Bot.
//
//nolint:paralleltest // serial by design: sets the process environment.
func TestResolveUpdateAppIdentityUnknown(t *testing.T) {
	stubGH(t, map[string]stubResponse{})
	usersServer(t, map[string]string{
		"/users/limen-ci-human[bot]": `{"id": 7, "login": "limen-ci-human[bot]", "type": "User"}`,
	})

	if _, err := github.ResolveUpdateAppIdentity("nobody"); !errors.Is(err, github.ErrUpdateAppUnknown) {
		t.Errorf("unregistered App: %v, want github.ErrUpdateAppUnknown", err)
	}

	if _, err := github.ResolveUpdateAppIdentity("human"); !errors.Is(err, github.ErrUpdateAppUnknown) {
		t.Errorf("non-bot record: %v, want github.ErrUpdateAppUnknown", err)
	}
}

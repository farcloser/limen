// White-box tests for the update-App identity resolver: the public users
// endpoint through an httptest server, the authed slug lookup through the gh
// stub seam.

package github //nolint:testpackage // white-box (see audit_test.go).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

	previous := usersAPIBase
	usersAPIBase = server.URL

	t.Cleanup(func() { usersAPIBase = previous })
}

// TestResolveUpdateAppIdentityConvention: without an authed gh (every org
// endpoint fails), the slug is limen's naming convention and the id comes
// from the users endpoint. The email is the exact noreply form.
//
//nolint:paralleltest // serial by design: mutates package-level seams.
func TestResolveUpdateAppIdentityConvention(t *testing.T) {
	stubGH(t, map[string]stubResponse{}) // nothing answers: unauthenticated laptop
	usersServer(t, map[string]string{
		"/users/limen-ci-test-org[bot]": `{"id": 317468017, "login": "limen-ci-test-org[bot]", "type": "Bot"}`,
	})

	identity, err := ResolveUpdateAppIdentity("test-org")
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

// TestResolveUpdateAppIdentityRenamed: with an org-admin token the App is
// read back from the org — the variable names its id, the installation list
// its slug — so a renamed App still resolves.
//
//nolint:paralleltest // serial by design: mutates package-level seams.
func TestResolveUpdateAppIdentityRenamed(t *testing.T) {
	stubGH(t, map[string]stubResponse{
		"GET orgs/test-org/actions/variables/UPDATE_AQUA_CHECKSUM_APP_ID": {
			Body: `{"name": "UPDATE_AQUA_CHECKSUM_APP_ID", "value": "4242"}`,
		},
		"GET orgs/test-org/installations": {
			Body: `{"total_count": 2, "installations": [
				{"app_id": 1, "app_slug": "renovate"},
				{"app_id": 4242, "app_slug": "our-ci-pusher"}]}`,
		},
	})
	usersServer(t, map[string]string{
		"/users/our-ci-pusher[bot]": `{"id": 99, "login": "our-ci-pusher[bot]", "type": "Bot"}`,
	})

	identity, err := ResolveUpdateAppIdentity("test-org")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if identity.Slug != "our-ci-pusher" || identity.UserID != 99 {
		t.Errorf("identity = %+v, want the renamed App", identity)
	}
}

// TestResolveUpdateAppIdentityUnknown: no App under the expected name is
// ErrUpdateAppUnknown — the callers' "not enforced" signal — and so is a
// record that is not a Bot.
//
//nolint:paralleltest // serial by design: mutates package-level seams.
func TestResolveUpdateAppIdentityUnknown(t *testing.T) {
	stubGH(t, map[string]stubResponse{})
	usersServer(t, map[string]string{
		"/users/limen-ci-human[bot]": `{"id": 7, "login": "limen-ci-human[bot]", "type": "User"}`,
	})

	if _, err := ResolveUpdateAppIdentity("nobody"); !errors.Is(err, ErrUpdateAppUnknown) {
		t.Errorf("unregistered App: %v, want ErrUpdateAppUnknown", err)
	}

	if _, err := ResolveUpdateAppIdentity("human"); !errors.Is(err, ErrUpdateAppUnknown) {
		t.Errorf("non-bot record: %v, want ErrUpdateAppUnknown", err)
	}
}

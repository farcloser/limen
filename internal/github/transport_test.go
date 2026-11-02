// Tests for the transport's own behaviour: the rate limit retried once,
// bounded; a server fault reported, not retried.

package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/farcloser/limen/internal/github"
)

// rateLimitedServer answers the organization's repository listing with the
// given status and headers once, then with the listing.
func rateLimitedServer(t *testing.T, status int, headers map[string]string) *atomic.Int32 {
	t.Helper()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			for name, value := range headers {
				w.Header().Set(name, value)
			}

			http.Error(w, `{"message":"slow down"}`, status)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"alpha","archived":false}]`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	return &calls
}

// TestRateLimitRetriedOnce: a 429, and the 403 GitHub's secondary limit
// answers with, are retried once after the wait GitHub names. Serial: each
// case points the process at its own server.
func TestRateLimitRetriedOnce(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	for name, tc := range map[string]struct {
		status  int
		headers map[string]string
	}{
		"429 with Retry-After":         {http.StatusTooManyRequests, map[string]string{"Retry-After": "1"}},
		"403 with Retry-After":         {http.StatusForbidden, map[string]string{"Retry-After": "1"}},
		"403 with the limit exhausted": {http.StatusForbidden, map[string]string{"X-Ratelimit-Remaining": "0", "X-Ratelimit-Reset": "1"}},
	} {
		calls := rateLimitedServer(t, tc.status, tc.headers)

		repos, err := github.OrgRepos(t.Context(), testOrg)
		if err != nil {
			t.Fatalf("%s: OrgRepos: %v", name, err)
		}

		if len(repos) != 1 || calls.Load() != 2 {
			t.Errorf("%s: repos %v after %d calls, want the listing after one retry", name, repos, calls.Load())
		}
	}
}

// TestServerFaultIsReported: a 500 is the finding's error, not a retry, and
// a 403 without a rate-limit header is a refusal, carrying GitHub's message.
func TestServerFaultIsReported(t *testing.T) { //nolint:paralleltest // serial: sets the process environment.
	for name, status := range map[string]int{"500": http.StatusInternalServerError, "403": http.StatusForbidden} {
		calls := rateLimitedServer(t, status, nil)

		_, err := github.OrgRepos(t.Context(), testOrg)
		if err == nil || !strings.Contains(err.Error(), "slow down") || calls.Load() != 1 {
			t.Errorf("%s: err %v after %d calls, want GitHub's message after one call", name, err, calls.Load())
		}
	}
}

// TestRateLimitRetriedOnceOnly: a limit that does not lift is reported after
// the one retry, as GitHub's status, not waited on again.
func TestRateLimitRetriedOnceOnly(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"message":"slow down"}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	_, err := github.OrgRepos(t.Context(), testOrg)
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") || calls.Load() != 2 {
		t.Errorf("err %v after %d calls, want HTTP 429 after exactly two", err, calls.Load())
	}
}

// TestRateLimitWaitIsCancellable: a long Retry-After is waited out only while
// the run lives; a cancelled context ends the wait at once.
func TestRateLimitWaitIsCancellable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		http.Error(w, `{"message":"slow down"}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()

	_, err := github.OrgRepos(ctx, testOrg)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Errorf("err %v after %s, want the context's deadline within a moment", err, time.Since(started))
	}
}

// TestRequestHeaders: every call carries the token as a bearer, the versioned
// JSON media type and limen's user agent; GH_TOKEN wins over GITHUB_TOKEN, as
// for gh.
func TestRequestHeaders(t *testing.T) {
	var seen http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "from-gh")
	t.Setenv("GITHUB_TOKEN", "from-actions")

	if _, err := github.OrgRepos(t.Context(), testOrg); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"Authorization":        "Bearer from-gh",
		"Accept":               "application/vnd.github+json",
		"X-Github-Api-Version": "2022-11-28",
		"User-Agent":           "limen",
	} {
		if got := seen.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestRefusalMessageIsCapped: GitHub's message rides in the error, capped so
// a finding stays a sentence.
func TestRefusalMessageIsCapped(t *testing.T) {
	long := strings.Repeat("x", 300)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"`+long+`"}`, http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	_, err := github.OrgRepos(t.Context(), testOrg)
	if err == nil || !strings.Contains(err.Error(), "…") || strings.Contains(err.Error(), long) {
		t.Errorf("err %v, want the message cut with an ellipsis", err)
	}
}

// TestListPastTheCapIsAnError: a list that keeps naming a next page past the
// hundred followed is an error, never a truncated inventory.
func TestListPastTheCapIsAnError(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Link", "<http://"+r.Host+"/orgs/test-org/repos?per_page=100&type=all&page="+
			strconv.Itoa(int(calls.Load())+1)+`>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"r","archived":false}]`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	_, err := github.OrgRepos(t.Context(), testOrg)
	if err == nil || !strings.Contains(err.Error(), "runs past the pages followed") || calls.Load() != 100 {
		t.Errorf("err %v after %d calls, want the cap named after a hundred pages", err, calls.Load())
	}
}

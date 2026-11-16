// Black-box tests of `limen github watch`, through cli.Run, against an API
// server that keeps an account's subscriptions and every write it is sent.

package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The three states GitHub keeps of an account and a repository.
const (
	subscribedNot     = ""
	subscribedWatched = "watched"
	subscribedIgnored = "ignored"
)

// A watched is the stand-in for GitHub: organizations with their
// repositories, the account's subscription to each, and what was written.
type watched struct {
	t *testing.T

	mu sync.Mutex
	// login is the account the token belongs to.
	login string
	// orgs lists each organization's repositories by name; archived names
	// the ones the listing reports archived.
	orgs     map[string][]string
	archived map[string]bool
	// state is the account's subscription, by "org/name".
	state map[string]string
	// refuse names a repository whose PUT answers 500; noScope makes every
	// PUT answer 404, as GitHub does to a token without the scope.
	refuse  string
	noScope bool
	// writes is every write the API was sent, "METHOD path body" each.
	writes []string
	// reads counts the reads.
	reads int
}

func newWatched(t *testing.T) *watched {
	t.Helper()

	rig := &watched{
		t: t, login: "apostasie",
		orgs: map[string][]string{}, archived: map[string]bool{}, state: map[string]string{},
	}

	server := httptest.NewServer(http.HandlerFunc(rig.serve))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	return rig
}

func (w *watched) serve(writer http.ResponseWriter, request *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path := strings.TrimPrefix(request.URL.Path, "/")
	parts := strings.Split(path, "/")

	if request.Method == http.MethodGet {
		w.reads++
	} else {
		body, _ := io.ReadAll(request.Body)
		w.writes = append(w.writes, request.Method+" "+path+" "+string(body))
	}

	const subscriptionParts = 4

	switch {
	case request.Method == http.MethodGet && path == "user":
		answer(writer, http.StatusOK, map[string]string{"login": w.login})
	case request.Method == http.MethodGet && len(parts) == 3 && parts[0] == "orgs" && parts[2] == "repos":
		listed := []map[string]any{}
		for _, name := range w.orgs[parts[1]] {
			listed = append(listed, map[string]any{"name": name, "archived": w.archived[parts[1]+"/"+name]})
		}

		answer(writer, http.StatusOK, listed)
	case len(parts) == subscriptionParts && parts[0] == "repos" && parts[3] == "subscription":
		w.subscription(writer, request.Method, parts[1]+"/"+parts[2])
	default:
		w.t.Errorf(
			"the command made a call the stand-in does not know: %s %s",
			request.Method,
			request.URL.RequestURI(),
		)
		answer(writer, http.StatusInternalServerError, map[string]string{"message": "unknown call"})
	}
}

// subscription answers the account's subscription to one repository as
// GitHub does: a 404 for none, the two booleans otherwise; and takes a PUT.
func (w *watched) subscription(writer http.ResponseWriter, method, repo string) {
	missing := map[string]string{"message": "Not Found"}

	switch {
	case method == http.MethodGet && w.state[repo] == subscribedNot:
		answer(writer, http.StatusNotFound, missing)
	case method == http.MethodGet:
		answer(writer, http.StatusOK, map[string]bool{
			"subscribed": w.state[repo] == subscribedWatched, "ignored": w.state[repo] == subscribedIgnored,
		})
	case method == http.MethodPut && w.noScope:
		answer(writer, http.StatusNotFound, missing)
	case method == http.MethodPut && repo == w.refuse:
		answer(writer, http.StatusInternalServerError, map[string]string{"message": "Server Error"})
	case method == http.MethodPut:
		w.state[repo] = subscribedWatched

		answer(writer, http.StatusOK, map[string]bool{"subscribed": true, "ignored": false})
	default:
		w.t.Errorf("the command sent %s to a subscription", method)
		answer(writer, http.StatusInternalServerError, missing)
	}
}

// watch runs the command and returns its exit code and both streams.
func watch(args ...string) (int, string) {
	var stdout, stderr bytes.Buffer

	code := run(append([]string{"github", "watch"}, args...), &stdout, &stderr)

	return code, stdout.String() + stderr.String()
}

// puts is the repositories the API was sent a subscription for, in order,
// each checked to carry what watching is.
func (w *watched) puts() []string {
	w.t.Helper()

	var repos []string

	for _, write := range w.writes {
		fields := strings.SplitN(write, " ", 3)
		repo := strings.TrimSuffix(strings.TrimPrefix(fields[1], "repos/"), "/subscription")

		var sent map[string]bool
		if err := json.Unmarshal([]byte(fields[2]), &sent); err != nil || fields[0] != http.MethodPut ||
			!sent["subscribed"] || sent["ignored"] {
			w.t.Errorf("a write that is not a watch: %s", write)
		}

		repos = append(repos, repo)
	}

	return repos
}

// TestWatchSubscribesWhatIsNotWatched: across two organizations, the account
// is subscribed to the repositories it has no subscription to, and to no
// other: one already watched is counted and left, one it ignores is named
// and left, an archived one is not looked at. The run says whose account it
// is. A second run writes nothing. Serial: sets the process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestWatchSubscribesWhatIsNotWatched(t *testing.T) {
	rig := newWatched(t)
	rig.orgs["acme"] = []string{"alpha", "beta", "gamma", "old"}
	rig.orgs["zeta"] = []string{"one"}
	rig.archived["acme/old"] = true
	rig.state["acme/beta"] = subscribedWatched
	rig.state["acme/gamma"] = subscribedIgnored

	code, said := watch("-org", "acme", "-org", "zeta", "-yes")
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, said)
	}

	if got, want := rig.puts(), []string{"acme/alpha", "zeta/one"}; !slices.Equal(got, want) {
		t.Errorf("subscribed to %v, want %v", got, want)
	}

	for _, want := range []string{
		"as apostasie", "1 of 4 repositories already watched", "ignored: left as it is",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the run does not say %q:\n%s", want, said)
		}
	}

	if !lineSays(said, "acme/gamma", "ignored") || strings.Contains(said, "acme/old") ||
		strings.Contains(said, "acme/beta") {
		t.Errorf("the ignored repository is to be named, the archived and the watched ones not:\n%s", said)
	}

	wrote := len(rig.writes)

	code, said = watch("-org", "acme", "-org", "zeta", "-yes")
	if code != 0 || len(rig.writes) != wrote || !strings.Contains(said, "3 of 4 repositories already watched") {
		t.Errorf("a second run: exit %d, %d more writes:\n%s", code, len(rig.writes)-wrote, said)
	}
}

// TestWatchRefusals: a run told whose account it is for refuses any other,
// before a repository is read; a token that may not touch subscriptions
// ends the run at its first write, with what the token lacks; a repository
// GitHub refuses is reported and the others are still watched. A run with
// no organization, or with an argument, is a usage error that reads nothing.
// Serial: sets the process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestWatchRefusals(t *testing.T) {
	t.Run("another account's token", func(t *testing.T) {
		rig := newWatched(t)
		rig.login = "closer-claudio"
		rig.orgs["acme"] = []string{"alpha"}

		code, said := watch("-org", "acme", "-as", "apostasie", "-yes")
		if code != 2 || !strings.Contains(said, "the token is closer-claudio's") ||
			len(rig.writes) != 0 || rig.reads != 1 {
			t.Errorf("exit %d, %d writes, %d reads, want 2, none, and the account alone read:\n%s",
				code, len(rig.writes), rig.reads, said)
		}

		if code, said := watch("-org", "acme", "-as", "Closer-Claudio", "-yes"); code != 0 {
			t.Errorf("the account named in another case: exit %d\n%s", code, said)
		}
	})

	t.Run("a token without the scope", func(t *testing.T) {
		rig := newWatched(t)
		rig.orgs["acme"] = []string{"alpha", "beta", "gamma"}
		rig.noScope = true

		code, said := watch("-org", "acme", "-yes")
		if code != 1 || !strings.Contains(said, "the token lacks the notifications scope") ||
			!strings.Contains(said, "3 of 3 repositories were not watched") || len(rig.writes) != 1 {
			t.Errorf("exit %d, %d writes, want 1, the scope named, and one write:\n%s", code, len(rig.writes), said)
		}
	})

	t.Run("one repository GitHub refuses", func(t *testing.T) {
		rig := newWatched(t)
		rig.orgs["acme"] = []string{"alpha", "beta", "gamma"}
		rig.refuse = "acme/beta"

		code, said := watch("-org", "acme", "-yes")
		if code != 1 || !strings.Contains(said, "1 of 3 repositories were not watched") ||
			rig.state["acme/alpha"] != subscribedWatched || rig.state["acme/gamma"] != subscribedWatched {
			t.Errorf("exit %d, want 1 and the two others watched (%v):\n%s", code, rig.state, said)
		}
	})

	t.Run("usage", func(t *testing.T) {
		rig := newWatched(t)

		for name, args := range map[string][]string{
			"no organization": {"-yes"},
			"an argument":     {"-org", "acme", "extra"},
		} {
			if code, said := watch(args...); code != 2 {
				t.Errorf("%s: exit %d, want 2:\n%s", name, code, said)
			}
		}

		if rig.reads != 0 || len(rig.writes) != 0 {
			t.Errorf("a usage error read %d times and wrote %d", rig.reads, len(rig.writes))
		}
	})
}

// Black-box tests of the downloader against a local server.

package fetch_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/farcloser/limen/internal/fetch"
)

// TestNamesItself: the request says who is asking. A host that screens
// automated clients by Go's default agent, as tukaani.org does, refuses that
// one and serves this one; a redirect carries the name along.
func TestNamesItself(t *testing.T) {
	t.Parallel()

	const body = "the key block\n"

	var (
		lock   sync.Mutex
		agents []string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/key.asc", http.StatusFound)
	})
	mux.HandleFunc("/key.asc", func(w http.ResponseWriter, r *http.Request) {
		agent := r.Header.Get("User-Agent")

		lock.Lock()

		agents = append(agents, agent)
		lock.Unlock()

		if agent == "" || strings.HasPrefix(agent, "Go-http-client/") {
			http.Error(w, "automated clients are not served", http.StatusForbidden)

			return
		}

		_, _ = io.WriteString(w, body)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	want := sha256.Sum256([]byte(body))

	for _, path := range []string{"/key.asc", "/moved"} {
		file := filepath.Join(t.TempDir(), "key.asc")

		sum, err := fetch.File(t.Context(), server.URL+path, file)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		if sum != hex.EncodeToString(want[:]) {
			t.Errorf("%s: sha256 = %s, want the body's", path, sum)
		}

		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != body {
			t.Errorf("%s: wrote %q, want %q", path, got, body)
		}
	}

	lock.Lock()
	defer lock.Unlock()

	for _, agent := range agents {
		if !strings.HasPrefix(agent, "limen ") {
			t.Errorf("User-Agent = %q, want one that names limen", agent)
		}
	}
}

// TestRefusalIsFinal: a definitive status is not retried and names itself.
func TestRefusalIsFinal(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	_, err := fetch.File(t.Context(), server.URL+"/gone", filepath.Join(t.TempDir(), "gone"))
	if !errors.Is(err, fetch.ErrFetch) || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %v, want ErrFetch naming HTTP 404", err)
	}

	if asked := calls.Load(); asked != 1 {
		t.Errorf("the server was asked %d times, want once", asked)
	}
}

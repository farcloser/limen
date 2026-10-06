package vulndb_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/fetch"
	"github.com/farcloser/limen/internal/vulndb"
)

// archive builds a zip holding the named files.
func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := zip.NewWriter(&buf)

	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}

		if _, err = entry.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

// serve answers every request with body, or with status when body is nil.
func serve(t *testing.T, status int, body []byte) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if body == nil {
			w.WriteHeader(status)

			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server.URL + "/vulndb.zip"
}

func TestFetchUnpacksAndReplaces(t *testing.T) {
	t.Parallel()

	source := serve(t, http.StatusOK, archive(t, map[string]string{
		"index/db.json":        `{"modified":"2026-10-01T00:00:00Z"}`,
		"index/modules.json":   `[]`,
		"ID/GO-2020-0001.json": `{}`,
	}))

	dir := filepath.Join(t.TempDir(), "build", "vulndb")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	stale := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := vulndb.Fetch(t.Context(), source, dir)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if !strings.HasPrefix(db, "file:///") || !strings.HasSuffix(db, "/build/vulndb") {
		t.Errorf("db URL %q, want a file URL of the directory", db)
	}

	if _, err = os.Stat(filepath.Join(dir, "ID", "GO-2020-0001.json")); err != nil {
		t.Errorf("an entry was not unpacked: %v", err)
	}

	if _, err = os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("what the directory held before should be gone, stat: %v", err)
	}
}

func TestFetchRefusesAnEscapingEntry(t *testing.T) {
	t.Parallel()

	source := serve(t, http.StatusOK, archive(t, map[string]string{
		"index/modules.json": `[]`,
		"../escaped.json":    `{}`,
	}))

	_, err := vulndb.Fetch(t.Context(), source, filepath.Join(t.TempDir(), "vulndb"))
	if !errors.Is(err, vulndb.ErrArchive) {
		t.Fatalf("an entry outside the directory: %v, want ErrArchive", err)
	}
}

func TestFetchRefusesAnArchiveWithoutTheIndex(t *testing.T) {
	t.Parallel()

	source := serve(t, http.StatusOK, archive(t, map[string]string{"ID/GO-2020-0001.json": `{}`}))

	dir := filepath.Join(t.TempDir(), "vulndb")

	_, err := vulndb.Fetch(t.Context(), source, dir)
	if !errors.Is(err, vulndb.ErrArchive) {
		t.Fatalf("no index/modules.json: %v, want ErrArchive", err)
	}

	if _, err = os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused archive must leave no directory behind, stat: %v", err)
	}
}

func TestFetchReportsARefusedDownload(t *testing.T) {
	t.Parallel()

	source := serve(t, http.StatusNotFound, nil)

	_, err := vulndb.Fetch(t.Context(), source, filepath.Join(t.TempDir(), "vulndb"))
	if !errors.Is(err, fetch.ErrFetch) {
		t.Fatalf("a 404: %v, want fetch.ErrFetch", err)
	}
}

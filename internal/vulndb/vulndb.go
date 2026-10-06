// Package vulndb fetches the Go vulnerability database as one archive and
// unpacks it where govulncheck's -db flag reads it, so a scan makes one
// retried download instead of a request per module per platform.
package vulndb

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/farcloser/limen/internal/fetch"
)

// Source is the archive vuln.go.dev publishes alongside the per-entry files.
const Source = "https://vuln.go.dev/vulndb.zip"

// ErrArchive is an archive that is not a vulnerability database govulncheck
// can read, or that tries to write outside its directory.
var ErrArchive = errors.New("not a usable vulnerability database archive")

const (
	dirPermissions  = 0o700
	filePermissions = 0o600
	// maxEntry bounds one unpacked file: the largest, index/modules.json, is
	// well under a megabyte, so a bigger entry is not the database.
	maxEntry = 64 << 20
	// modulesIndex is the file govulncheck checks for before it trusts a
	// directory to follow the v1 schema.
	modulesIndex = "index/modules.json"
)

// Fetch downloads the archive at source, retrying transient failures, and
// unpacks it into dir, replacing what dir held. It returns the file URL
// govulncheck's -db flag takes.
func Fetch(ctx context.Context, source, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}

	parent := filepath.Dir(abs)
	if err = os.MkdirAll(parent, dirPermissions); err != nil {
		return "", fmt.Errorf("%s: %w", parent, err)
	}

	// The staging directory sits beside dir, not under the system temporary
	// directory: the final rename must not cross a volume.
	staging, err := os.MkdirTemp(parent, ".vulndb-")
	if err != nil {
		return "", fmt.Errorf("staging directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(staging) }()

	archive := filepath.Join(staging, "vulndb.zip")
	if _, err = fetch.File(ctx, source, archive); err != nil {
		return "", fmt.Errorf("vulnerability database: %w", err)
	}

	unpacked := filepath.Join(staging, "db")
	if err = unpack(archive, unpacked); err != nil {
		return "", err
	}

	if _, err = os.Stat(filepath.Join(unpacked, filepath.FromSlash(modulesIndex))); err != nil {
		return "", fmt.Errorf("%w: %s has no %s", ErrArchive, source, modulesIndex)
	}

	if err = os.RemoveAll(abs); err != nil {
		return "", fmt.Errorf("%s: %w", abs, err)
	}

	if err = os.Rename(unpacked, abs); err != nil {
		return "", fmt.Errorf("%s: %w", abs, err)
	}

	return fileURL(abs), nil
}

// unpack writes every file of the zip archive under dir, refusing a name that
// would land outside it.
func unpack(archive, dir string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = reader.Close() }()

	for _, entry := range reader.File {
		if !filepath.IsLocal(entry.Name) {
			return fmt.Errorf("%w: entry %q escapes the directory", ErrArchive, entry.Name)
		}

		if entry.FileInfo().IsDir() {
			continue
		}

		if err = unpackFile(entry, filepath.Join(dir, filepath.FromSlash(entry.Name))); err != nil {
			return err
		}
	}

	return nil
}

func unpackFile(entry *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), dirPermissions); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}

	src, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrArchive, entry.Name, err)
	}

	defer func() { _ = src.Close() }()

	// #nosec G304 -- target is under the staging directory, checked by IsLocal.
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePermissions)
	if err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}

	written, copyErr := io.CopyN(out, src, maxEntry+1)
	closeErr := out.Close()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return fmt.Errorf("%w: %s: %w", ErrArchive, entry.Name, copyErr)
	}

	if written > maxEntry {
		return fmt.Errorf("%w: entry %q is larger than %d bytes", ErrArchive, entry.Name, maxEntry)
	}

	if closeErr != nil {
		return fmt.Errorf("%s: %w", target, closeErr)
	}

	return nil
}

// fileURL is the file URL of an absolute path. On Windows the drive letter
// needs a leading slash ("file:///C:/…"), which govulncheck requires.
func fileURL(abs string) string {
	path := filepath.ToSlash(abs)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return (&url.URL{Scheme: "file", Path: path}).String()
}

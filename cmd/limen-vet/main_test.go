package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var diagnostic = regexp.MustCompile(
	`^(?:\.[/\\])?(\S+\.go):(\d+):\d+: (.*?)(?: \(take an io\.Reader.*\)|: version-conditional.*|: //limen:allow.*)?$`,
)

func TestVettool(t *testing.T) {
	t.Parallel()

	// The module go vet runs on: a file per case. Each go1.N term in a header is
	// a finding, on every platform, built or not.
	module := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.26.0\n",
		"ok.go":  "package m\n",
		// Never built: the term is always true under the go line.
		"old.go": "//go:build !go1.18\n\npackage m\n",
		// Built on linux only; a finding on every platform.
		"new_linux.go": "//go:build go1.27 && linux\n\npackage m\n",
		"plus.go":      "// +build go1.10\n\npackage m\n",
		"allowed.go":   "//limen:allow buildversion -- needs the iterator API\n//go:build go1.27\n\npackage m\n",
		"noreason.go":  "//limen:allow buildversion\n//go:build go1.26\n\npackage m\n",
		// Not a header: a constraint after the package clause is a comment.
		"body.go": "package m\n\n//go:build go1.20\n",
		"helper/helper.go": `package helper

import (
	"io"
	"os"
)

func LoadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}
`,
		"lib/lib.go": `package lib

import "example.com/m/helper"

func Load() ([]byte, error) { return helper.LoadFile("x") }
`,
		// Sanctioned in its source: its own finding goes, and so does
		// storefront's call into it; storefront's own os.ReadFile stays.
		"store/doc.go": "// Package store keeps blobs on disk.\n//\n//limen:allow fsaccess -- an on-disk store\npackage store\n",
		"store/store.go": `package store

import "os"

func Load() ([]byte, error) { return os.ReadFile("x") }
`,
		"store/cache/cache.go": `package cache

import "os"

func Clear() error { return os.RemoveAll("x") }
`,
		"storefront/front.go": `package storefront

import (
	"os"

	"example.com/m/store"
)

func Page() ([]byte, error) { return os.ReadFile("x") }

func Stock() ([]byte, error) { return store.Load() }
`,
		"cmd/tool/main.go": `package main

import "example.com/m/helper"

func main() { _, _ = helper.LoadFile("x") }
`,
	}

	expected := []string{
		"helper/helper.go:9: library code touches the file system: os.Open",
		"lib/lib.go:5: library code calls helper.LoadFile, which touches the file system through os.Open",
		"new_linux.go:1: build constraint tests the Go release (go1.27)",
		"noreason.go:1: limen:allow buildversion needs a reason",
		"noreason.go:2: build constraint tests the Go release (go1.26)",
		"old.go:1: build constraint tests the Go release (go1.18)",
		"plus.go:1: build constraint tests the Go release (go1.10)",
		"store/cache/cache.go:5: library code touches the file system: os.RemoveAll",
		"storefront/front.go:9: library code touches the file system: os.ReadFile",
	}

	// go build -o writes the name as given; Windows runs no file without .exe.
	bin := filepath.Join(t.TempDir(), "limen-vet")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")

	build.Env = append(os.Environ(), "GOOS=", "GOARCH=")

	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	dir := t.TempDir()

	for name, content := range module {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			t.Parallel()

			vet := exec.CommandContext(
				t.Context(),
				"go",
				"vet",
				"-vettool="+bin,
				"./...",
			)
			vet.Dir = dir

			vet.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "GOWORK=off", "GOFLAGS=")

			out, err := vet.CombinedOutput()
			if err == nil {
				t.Fatalf("go vet passed; want findings\n%s", out)
			}

			var got []string

			for line := range strings.SplitSeq(string(out), "\n") {
				if m := diagnostic.FindStringSubmatch(line); m != nil {
					got = append(got, filepath.ToSlash(m[1])+":"+m[2]+": "+m[3])
				} else if line != "" && !strings.HasPrefix(line, "#") {
					t.Errorf("unexpected output: %s", line)
				}
			}

			slices.Sort(got)
			got = slices.Compact(got)

			if !slices.Equal(got, expected) {
				t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(expected, "\n"))
			}
		})
	}
}

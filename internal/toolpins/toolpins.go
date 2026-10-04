// Package toolpins carries the go.mod files of limen's own tools modules into
// a release binary: tools/go.mod, and tools/<name>/go.mod for each isolated
// tool. go:embed cannot reach them where they live (each is a module of its
// own), so the release's goreleaser hook copies them into release/ before the
// build. The copies are gitignored, and a build reads them only when the
// release stamped it: a development build, even one made after a local dry
// run left the copies behind, carries no pins, and fix then refuses to seed
// a tool rather than guess a version.
package toolpins

import (
	"embed"
	"io/fs"
	"strings"
)

// sharedFile is the copy of tools/go.mod; every other <name>.go.mod is the
// copy of tools/<name>/go.mod.
const sharedFile = "tools.go.mod"

//go:embed all:release
var release embed.FS

// stamp is "release" in a binary the release built (-ldflags -X, see
// .release-go.yaml), "" otherwise.
//
//nolint:gochecknoglobals // set by the linker; -X can only reach a package variable.
var stamp string

// GoMods returns the embedded tools/go.mod and, keyed by tool name, each
// tools/<name>/go.mod: empty unless the release built this binary.
func GoMods() (shared string, isolated map[string]string) {
	isolated = map[string]string{}
	if stamp != "release" {
		return "", isolated
	}

	entries, err := fs.ReadDir(release, "release")
	if err != nil {
		return "", isolated
	}

	for _, entry := range entries {
		name, isMod := strings.CutSuffix(entry.Name(), ".go.mod")
		if !isMod || entry.IsDir() {
			continue
		}

		data, err := fs.ReadFile(release, "release/"+entry.Name())
		if err != nil {
			continue
		}

		if entry.Name() == sharedFile {
			shared = string(data)
		} else {
			isolated[name] = string(data)
		}
	}

	return shared, isolated
}

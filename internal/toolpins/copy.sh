#!/bin/sh
# Copies the tools modules' go.mod files into release/, for the build to
# embed (see toolpins.go). Run by the release's goreleaser hook, from the
# repository root; a tools module that cannot be copied fails the release.
set -eu

dest=internal/toolpins/release

cp tools/go.mod "$dest/tools.go.mod"

for mod in tools/*/go.mod; do
    name=${mod#tools/}
    cp "$mod" "$dest/${name%/go.mod}.go.mod"
done

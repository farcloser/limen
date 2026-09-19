// Command limen verifies a repository against Farcloser's engineering rules.
//
// Usage:
//
//	limen check [-json] [path]
//
// It exits 0 when the repository complies and 1 when any rule fails, so it can
// be dropped into pre-commit, CI, or an agent's workflow unchanged. Everything
// but the exit lives in internal/cli.
package main

import (
	"os"

	"github.com/farcloser/limen/internal/cli"
)

// version is stamped at build time via -X main.version: goreleaser injects the
// release version, `just build go` injects `git describe`; a plain `go build`
// reports "dev".
var version = "dev"

func main() {
	os.Exit(cli.Run(version, os.Args[1:], os.Stdout, os.Stderr))
}

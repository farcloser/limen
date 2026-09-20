// limen-lint-go renders the Go lint baseline with a project's carve-outs into
// the configuration the lint recipes run, checks the pinned golangci-lint
// against the baseline's floor, and prints the go-licenses flags. It ships in
// limen's release, beside limen; the baseline is baseline.yml beside this
// file, embedded, and the carve-outs the project's root .lint-go.yaml
// (book/per-language.md, "one baseline, per-project carve-outs").
package main

import (
	_ "embed"
	"os"

	"github.com/farcloser/limen/cmd/limen-lint-go/internal/lintgo"
)

// The Go lint baseline: the golangci-lint configuration every repository
// starts from, the go-licenses allow list, and the golangci-lint release its
// linter names were written for.
//
//go:embed baseline.yml
var baseline []byte

func main() {
	os.Exit(lintgo.Run(os.Args[1:], baseline, os.Stdout, os.Stderr))
}

// limen-lint-go renders the Go lint baseline with a project's carve-outs into
// the configuration the lint recipes run, checks the pinned golangci-lint
// against the baseline's floor, and prints the go-licenses flags. It ships in
// limen's release, beside limen; the baseline is the repository's
// .limen/lint-go.yaml, content-pinned by limen, and the carve-outs the
// project's root .lint-go.yaml (book/per-language.md, "one baseline,
// per-project carve-outs").
package main

import (
	"os"

	"github.com/farcloser/limen/cmd/limen-lint-go/internal/lintgo"
)

func main() {
	os.Exit(lintgo.Run(os.Args[1:], os.Stdout, os.Stderr))
}

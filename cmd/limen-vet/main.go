// limen-vet is the analyzers `go vet -vettool` runs on top of vet's own:
//
//   - buildversion: no build constraint tests the Go release (go1.N);
//   - fsaccess: library code takes readers and writers, and does not reach the
//     file system itself.
//
// A finding is silenced on its line, or the line above, with
// `//limen:allow <analyzer> -- <reason>`.
package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/farcloser/limen/cmd/limen-vet/internal/buildversion"
	"github.com/farcloser/limen/cmd/limen-vet/internal/fsaccess"
)

func main() {
	unitchecker.Main(buildversion.Analyzer, fsaccess.Analyzer)
}

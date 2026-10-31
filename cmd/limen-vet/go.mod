// limen-vet's own module: the analyzers `go vet -vettool` runs on top of vet's
// own. A module of its own so golang.org/x/tools never enters limen's
// zero-dependency go.mod.
module github.com/farcloser/limen/cmd/limen-vet

go 1.26.0

require golang.org/x/tools v0.52.0

require (
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)

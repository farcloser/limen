// limen-lint-go's own module: the Go lint driver limen's release ships
// beside limen (book/linting-go.md). A module of its own so its one
// dependency, the YAML parser, never enters limen's zero-dependency go.mod.
module github.com/farcloser/limen/cmd/limen-lint-go

go 1.26.0

require go.yaml.in/yaml/v4 v4.0.0

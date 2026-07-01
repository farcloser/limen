# project.just — this project's own recipes. Not part of the shared baseline;
# limen does not check or overwrite this file. Add whatever this repository needs here.

export LINT_GO_LICENSES_FLAGS := "--ignore gotest.tools"

# The canonical `lint limen` / `fix limen` recipes must judge this repository
# by its own working tree, not by the (always older) released pin: `go run`
# compiles the current tree on every invocation, so there is nothing to build
# first and nothing stale to trust.
export LIMEN_BIN := 'go run ./cmd/limen'

lint-all: lint::go::default lint::default

test-all: test::go::default

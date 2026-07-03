# This file is the project's own — add recipes below. Keep the import: it
# mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

export LINT_GO_LICENSES_FLAGS := "--ignore gotest.tools"

# The canonical `lint limen` / `fix limen` recipes must judge this repository
# by its own working tree, not by the (always older) released pin: `go run`
# compiles the current tree on every invocation, so there is nothing to build
# first and nothing stale to trust.
export LIMEN_BIN := 'go run ./cmd/limen'

# Bare `just` lists; `lint` and `test` below are what CI runs.
default:
    @just --list

lint: do::lint::default do::lint::go::default

test: do::test::go::default

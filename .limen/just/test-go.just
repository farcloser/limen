# DO NOT EDIT MANUALLY.
# This file provides shared tasks common to all projects and managed by limen.
# Project recipes live in the root .justfile, below the shared-baseline import.

set working-directory := '../..'

# Silence just's per-line command echo; recipes announce themselves via _banner.
set quiet

# Shared private _banner recipe (see lib.just).
import 'lib.just'

default: unit

# `go test` runs through gotestsum (same exit semantics; GOTESTSUM_FORMAT picks
# the output). -count=1 defeats the result cache. TEST_GO_TIMEOUT overrides the
# timeout from the root .justfile.
#
# A PASSING test's output is hidden twice over (go test drops it without -v;
# gotestsum's default format hides it), so debug prints look like they never
# ran. To see them: GOTESTSUM_FORMAT=standard-verbose just do test go
#
# CGO_ENABLED resolves through lib.just's go_cgo, so a cgo project tests the
# artifact it ships; `race` forces 1 (the detector injects runtime/cgo).

# A compile that needs a generated input (an embedded build product, codegen)
# gets it from the project's `_test-prepare` recipe in the root .justfile,
# run before every shared Go test recipe when it exists: the CI lanes call
# these recipes directly (`just do test go race`), never the project's `test`
# aggregate, so the aggregate's own prerequisites would not run there. A root
# .justfile without the recipe is unchanged. `just --show` sees a private
# recipe where `--summary` does not.
_prepare:
    #!/usr/bin/env bash
    set -euo pipefail
    if just --show _test-prepare >/dev/null 2>&1; then
        just _test-prepare
    fi

# Unit tests.
unit: (_banner "test go" "unit") _prepare (_go-tool "gotestsum" "gotest.tools/gotestsum")
    CGO_ENABLED="{{ go_cgo }}" \
        "$LIMEN_BUILD/tools/gotestsum" -- -count=1 -timeout "${TEST_GO_TIMEOUT:-10m}" ./...

# Unit tests under the race detector. External linkmode: the detector injects
# runtime/cgo, and hardened CGO_CFLAGS make those objects reference libc
# symbols the internal linker cannot resolve on Linux (golang/go#52690,
# #54313, #58619). macOS ld's "malformed LC_DYSYMTAB" warnings on race builds
# are cosmetic (golang/go#61229).
#
# The detector is a prebuilt runtime Go vendors per platform, absent on
# windows/arm64 among others. Ask the toolchain rather than keep a platform
# list: a probe build of `unsafe` answers in milliseconds. Only the exact
# "-race is not supported" refusal skips — loudly, and passes, since the other
# legs run the detector natively; any other probe failure is real. No go.mod
# at all: says so and passes, like fuzz, since CI runs this in every
# repository.
race: (_banner "test go" "race") _prepare (_go-tool "gotestsum" "gotest.tools/gotestsum" "go.mod")
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f go.mod ]; then
        echo "no go.mod — not a Go module, nothing to run under the detector (skipped, not failed)."
        exit 0
    fi
    export CGO_ENABLED=1
    if ! probe="$(go build -race -o /dev/null unsafe 2>&1)"; then
        if printf '%s\n' "$probe" | grep -q -- '-race is not supported'; then
            echo "race: SKIPPED — no race detector for $(go env GOOS)/$(go env GOARCH); ${probe}" >&2
            exit 0
        fi
        printf '%s\n' "$probe" >&2
        exit 1
    fi
    "$LIMEN_BUILD/tools/gotestsum" -- -count=1 -timeout "${TEST_GO_TIMEOUT:-10m}" -ldflags=-linkmode=external -race ./...

# Every Fuzz* target for a short budget (TEST_GO_FUZZ_TIME, default 10s).
# Driven by `go test -fuzz`, one target per invocation — only that build
# compiles in the coverage counters. Targets come from -list, not grep, so
# build tags stay Go's call.
#
# The verdict is the crasher, not the exit code: a real finding writes under
# testdata/fuzz/<Target>/ (commit it); the coordinator can also exit 1 with
# "context deadline exceeded" as fuzztime expires mid-iteration, so exit 1
# without a new file is retried once. No Fuzz* targets, or no go.mod at all:
# says so and passes.
#
# A fuzz run is bounded in memory as well as in time. `go test -fuzz` starts
# one worker process a core and bounds none of them: a target that allocates
# on its input's say takes the machine's memory, several runs at once all of
# it. So the workers are counted (TEST_GO_FUZZ_WORKERS, default 4, or the
# machine's processors when it has fewer), and each
# run goes under `limen guard`, which ends any process of it that holds more
# than TEST_GO_FUZZ_MEMORY (MiB a process, default 2048). The lane's ceiling
# is the two multiplied, plus the coordinator. A worker ended so is a finding
# like any other: the coordinator writes the input it was on under
# testdata/fuzz/. The guard reads a process group's memory on macOS and
# Linux; where it cannot (Windows), nothing is fuzzed, and the lane says so
# and passes, as `race` does without a detector: CI fuzzes on Linux.
#
# What runs under the guard is the test binary, not `go test`. A package's
# binary is built first, outside the guard: the compiler and the linker are
# not the run, and the first fuzz build of a toolchain compiles the standard
# library again, `runtime` with it, which holds more than a limit set low
# for a small target allows. The binary is then started as `go test` starts
# it, in the package's directory, with the corpus cache `go test` gives it.
fuzz: (_banner "test go" "fuzz") _prepare
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f go.mod ]; then
        echo "no go.mod — not a Go module, nothing to fuzz (skipped, not failed)."
        exit 0
    fi
    if [ "$(go env GOHOSTOS)" = windows ]; then
        echo "fuzz: SKIPPED — limen guard cannot bound a process's memory on windows, and nothing is fuzzed unguarded." >&2
        exit 0
    fi
    export CGO_ENABLED="{{ go_cgo }}"
    fuzztime="${TEST_GO_FUZZ_TIME:-10s}"
    # Never more workers by default than the machine has processors.
    processors=$(getconf _NPROCESSORS_ONLN)
    default_workers=4
    if [ "$processors" -lt "$default_workers" ]; then default_workers=$processors; fi
    workers="${TEST_GO_FUZZ_WORKERS:-$default_workers}"
    memory="${TEST_GO_FUZZ_MEMORY:-2048}"
    # Fail CLOSED on a malformed bound, as the coverage minimum does: a typo
    # must not leave the run unbounded.
    for bound in "TEST_GO_FUZZ_WORKERS=$workers" "TEST_GO_FUZZ_MEMORY=$memory"; do
        case "${bound#*=}" in
            ''|*[!0-9]*|0)
                echo "${bound%%=*} must be a whole number above zero (got '${bound#*=}')." >&2
                exit 2
                ;;
        esac
    done
    # Crasher count for a target: the directory does not exist until the first
    # finding, and a missing directory is zero, not an error (pipefail).
    crashers() {
        if [ -d "$1" ]; then find "$1" -type f | wc -l; else echo 0; fi
    }
    found=0
    fail=0
    built=0
    # Where `go test -fuzz` keeps what it found for a package, by import path.
    corpus="$(go env GOCACHE)/fuzz"
    # Listed in a statement of its own: a `go list` that fails inside the
    # loop's own substitution lists nothing, and the lane would pass on "no
    # Fuzz* targets".
    packages=$(go list ./...)
    for pkg in $packages; do
        targets=$(go test -count=1 -run '^$' -list '^Fuzz' "$pkg" | grep '^Fuzz' || true)
        [ -n "$targets" ] || continue
        dir=$(go list -f '{{{{.Dir}}' "$pkg")
        # The package's fuzz binary, built once for its targets.
        built=$((built + 1))
        binary="$LIMEN_BUILD/fuzz/$built/fuzz.test"
        mkdir -p "${binary%/*}"
        go test -c -fuzz '^Fuzz' -o "$binary" "$pkg"
        for target in $targets; do
            found=1
            echo "fuzzing $pkg $target for $fuzztime"
            before=$(crashers "$dir/testdata/fuzz/$target")
            ok=0
            for attempt in 1 2; do
                if ${LIMEN_BIN:-limen} guard -memory "$memory" -dir "$dir" -- \
                    "$binary" -test.paniconexit0 -test.fuzzcachedir="$corpus/$pkg" \
                    -test.count=1 -test.timeout="${TEST_GO_TIMEOUT:-10m}" -test.parallel="$workers" \
                    -test.run="^${target}\$" -test.fuzz="^${target}\$" -test.fuzztime="$fuzztime"; then
                    ok=1
                    break
                fi
                after=$(crashers "$dir/testdata/fuzz/$target")
                if [ "$after" -gt "$before" ]; then
                    echo "$target: new crasher written under $dir/testdata/fuzz/$target — commit it as a regression test" >&2
                    break
                fi
                echo "$target: exit without a crasher (attempt $attempt) — coordinator shutdown hiccup, retrying" >&2
            done
            if [ "$ok" -eq 1 ]; then
                echo "PASS: $target"
            else
                fail=1
            fi
        done
    done
    if [ "$found" -eq 0 ]; then
        echo "no Fuzz* targets in this module — nothing to fuzz"
    fi
    exit "$fail"

# Coverage: per-function summary, an HTML report under .build/coverage/, and an
# optional minimum gate — export TEST_GO_COVER_MIN := '80' (integer percent)
# from the root .justfile to enforce a floor; unset or 0 reports without gating.
cover: (_banner "test go" "cover") _prepare (_go-tool "gotestsum" "gotest.tools/gotestsum")
    #!/usr/bin/env bash
    set -euo pipefail
    export CGO_ENABLED="{{ go_cgo }}"
    dir="$LIMEN_BUILD/coverage"
    mkdir -p "$dir"
    "$LIMEN_BUILD/tools/gotestsum" -- -count=1 -timeout "${TEST_GO_TIMEOUT:-10m}" -coverprofile="$dir/coverage.out" ./...
    go tool cover -func="$dir/coverage.out"
    go tool cover -html="$dir/coverage.out" -o "$dir/coverage.html"
    echo "HTML report: $dir/coverage.html"
    min="${TEST_GO_COVER_MIN:-0}"
    # Fail CLOSED on a malformed threshold. `[ "$min" -gt 0 ]` errors on a
    # non-integer (a float like 80.0, a typo like 8O), and swallowing that error
    # would silently skip the whole gate — a mistyped minimum must not disable
    # enforcement. Reject anything that is not a whole number, loudly.
    case "$min" in
        ''|*[!0-9]*)
            echo "TEST_GO_COVER_MIN must be a whole-number percentage (got '$min')." >&2
            exit 2
            ;;
    esac
    if [ "$min" -gt 0 ]; then
        total=$(go tool cover -func="$dir/coverage.out" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')
        if [ "${total%%.*}" -lt "$min" ]; then
            echo "coverage ${total}% is below the ${min}% minimum" >&2
            exit 1
        fi
        echo "coverage ${total}% meets the ${min}% minimum"
    fi

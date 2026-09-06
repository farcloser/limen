# DO NOT EDIT MANUALLY.
# This file provides shared tasks common to all projects and managed by limen.
# Project recipes live in the root Justfile, below the shared-baseline import.

set working-directory := '../..'

# Silence just's per-line command echo; recipes announce themselves via _banner.
set quiet

# Shared private _banner recipe (see lib.just).
import 'lib.just'

default: code mod vuln licenses

# golangci-lint, once per supported platform (see _per-goos in lib.just:
# platform-specific files are only analyzed for the GOOS that builds them).
# The formatter is build-graph-independent and runs once. The last step bans
# blanket revive suppressions: nolintlint polices golangci directives, and
# revive findings proved environment-nondeterministic across the per-GOOS
# legs — the suppression itself then flakes as "unused". revive's own
# selective directives are invisible to nolintlint, hence stable.
code: (_banner "lint go" "go") (_per-goos "golangci-lint run")
    golangci-lint fmt --diff
    @if git grep -nE 'nolint:[a-z, ]*revive' -- '*.go'; then echo 'blanket revive suppression is banned — use //revive:disable-next-line:<rule> (or a disable/enable block) instead' >&2; exit 1; fi

# go.mod/go.sum tidiness — fails printing the diff; `just do fix go mod` repairs.
mod: (_banner "lint go" "mod")
    go mod tidy -diff

# Known-vulnerability scan against the Go vulnerability database (network:
# govulncheck fetches vuln.go.dev), once per supported platform (see _per-goos
# in lib.just: call paths and dependencies differ by GOOS).
vuln: (_banner "lint go" "vuln") (_go-tool "govulncheck" "golang.org/x/vuln/cmd/govulncheck") (_per-goos "build/tools/govulncheck ./...")

# Dependency license compliance, once per supported platform (see _per-goos in
# lib.just: each GOOS pulls its own dependencies). A project that needs extra
# flags — typically --ignore=<module> for the module-layout false positives of
# https://github.com/google/go-licenses/issues/186 — exports them from its
# root Justfile, the home of all project customization (the variable name
# mirrors the task path `lint go licenses`):
#     export LINT_GO_LICENSES_FLAGS := '--ignore=gotest.tools/v3'
# For a one-off, set it on the invocation instead:
# LINT_GO_LICENSES_FLAGS='--ignore=x' just do lint go licenses
#
# GOROOT is passed explicitly because go-licenses recognizes stdlib packages by
# their location under GOROOT, reading its default from its own build. _go-tool
# compiles it with the pinned toolchain, so the two agree by construction; the
# explicit value keeps that true even when a project overrides GOROOT.
licenses: (_banner "lint go" "licenses") (_go-tool "go-licenses" "github.com/google/go-licenses/v2") (_per-goos 'GOROOT="$(go env GOROOT)" build/tools/go-licenses check --include_tests --allowed_licenses=Apache-2.0,BSD-2-Clause,BSD-3-Clause,MIT ${LINT_GO_LICENSES_FLAGS:-} ./...')

# Bounds-check-elimination report: every bounds check the compiler could NOT
# eliminate, for performance tuning. Informational — it never fails, so it is
# not in the default set; and it reports for the native architecture only,
# since BCE results are arch-specific and this is a report for the human
# reading it, not a gate.
bce: (_banner "lint go" "bce")
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Bounds Check Elimination Report"
    echo "================================"
    echo ""
    # Keep compiler diagnostics only (file.go:line:col:): the build's other
    # chatter — package banners, a CGO link's `ld: warning` — must not count.
    output=$(go build -gcflags='-d=ssa/check_bce/debug=1' ./... 2>&1 | grep -E '\.go:[0-9]+:[0-9]+: ' || true)
    # Generic instantiations compile in the CALLER's package, so the compiler
    # also reports positions inside GOROOT (slices.SortFunc & co) and the module
    # cache (a generic dependency). Real checks in the binary, but not this
    # module's code to change — keep the module's own files only. Positions are
    # relative to the module root, so anything outside it starts with "../" (or
    # is absolute).
    external=$(echo "$output" | grep -cE '^(\.\./|/)' || true)
    output=$(echo "$output" | grep -vE '^(\.\./|/)' || true)
    if [ "$external" != 0 ]; then
        echo "(${external} in generic instantiations from GOROOT/module cache omitted — not this module's code)"
        echo ""
    fi
    if [ -z "$output" ]; then
        echo "No bounds checks detected (BCE fully eliminated)."
    else
        total=$(echo "$output" | wc -l | tr -d ' ')
        echo "Total: $total bounds checks"
        echo ""
        echo "By file:"
        echo "$output" | sed 's/:.*$//' | sort | uniq -c | sort -rn
        echo ""
        echo "Details:"
        echo "$output" | sort
    fi

# Escape-analysis and inlining report — both come from the same compiler pass
# (-gcflags=-m): values the compiler moved to the heap (the prime source of GC
# pressure) and functions it refused to inline, with the reason. Informational
# like bce: never fails, native architecture only, not in the default set.
escape: (_banner "lint go" "escape")
    #!/usr/bin/env bash
    set -euo pipefail
    # Same filters as bce: compiler diagnostics only, then drop positions
    # inside GOROOT / the module cache (generic instantiations compiled into
    # this module's packages).
    output=$(go build -gcflags='-m' ./... 2>&1 | grep -E '\.go:[0-9]+:[0-9]+: ' || true)
    external=$(echo "$output" | grep -cE '^(\.\./|/)' || true)
    output=$(echo "$output" | grep -vE '^(\.\./|/)' || true)
    heap=$(echo "$output" | grep -E 'escapes to heap|moved to heap' || true)
    noinline=$(echo "$output" | grep 'cannot inline' || true)
    echo "Escape Analysis Report"
    echo "======================"
    if [ "$external" != 0 ]; then
        echo "(${external} lines in generic instantiations from GOROOT/module cache omitted — not this module's code)"
        echo ""
    fi
    if [ -z "$heap" ]; then
        echo "No heap escapes."
    else
        echo "Total: $(echo "$heap" | wc -l | tr -d ' ') heap escapes"
        echo ""
        echo "By file:"
        echo "$heap" | sed 's/:.*$//' | sort | uniq -c | sort -rn
        echo ""
        echo "$heap" | sort
    fi
    echo ""
    echo "Inlining Failures"
    echo "================="
    if [ -z "$noinline" ]; then
        echo "None."
    else
        echo "Total: $(echo "$noinline" | wc -l | tr -d ' ') functions the compiler could not inline"
        echo ""
        echo "$noinline" | sort
    fi

# Unreachable functions, by whole-program call-graph analysis (deeper than the
# per-package `unused` linter in golangci). -test keeps library-only repos
# analyzable (test binaries serve as roots) and stops dead code from hiding
# behind test-only callers. Informational: native only, not in the default set.
deadcode: (_banner "lint go" "deadcode") (_go-tool "deadcode" "golang.org/x/tools/cmd/deadcode")
    build/tools/deadcode -test ./...

# The Go-source analyzers — deadcode, govulncheck, go-licenses — are go.mod
# `tool` directives, NOT aqua packages (see book/tooling.md, "Go-source
# analyzers are go.mod tools"): each embeds Go's own source loader, so it must
# be compiled by the same toolchain that compiles the project. aqua builds a
# go_install package once per tool version, with whichever project's pinned go
# ran it first, and then shares that binary across projects — an analyzer built
# by go1.N cannot read a module that declares go1.N+1.
#
# The per-GOOS legs cannot run `go tool <name>` directly: `go tool` honours
# GOOS/GOARCH and would cross-compile the analyzer itself (exec format error).
# So the analyzer is built natively ONCE, into build/tools/, from the pinned
# go.mod entry with the pinned go (cached per toolchain), and the legs run that
# binary. A go.mod without the directive fails here with the exact command to
# add it; `limen check` reports the same (the gotools rule).
_go-tool name pkg:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! grep -qE '^(tool[[:space:]]+)?[[:space:]]*{{ pkg }}([[:space:]]|$)' go.mod; then
        echo "go.mod lacks 'tool {{ pkg }}' — add it: go get -tool {{ pkg }}@<version> && go mod tidy (see book/tooling.md)" >&2
        exit 1
    fi
    mkdir -p build/tools
    GOOS='' GOARCH='' go build -o 'build/tools/{{ name }}' '{{ pkg }}'

# DO NOT EDIT MANUALLY.
# This file provides shared tasks common to all projects and managed by limen.
# Project recipes live in the root Justfile, below the shared-baseline import.

# Per-recipe banner, imported (a `mod` cannot be depended on). Depended on WITH
# arguments so plain and shebang recipes print it identically.
_banner mod name:
    @echo "▶ {{ mod }}: {{ name }}"

# go_cgo: "does THIS project build with cgo?", shared by build, test and the
# per-GOOS legs.
#
# It is a SHELL expansion, not a just value, on purpose: a child module cannot
# see its parent's variables and env() reads just's process environment, so
# `export CGO_ENABLED := env('CGO_ENABLED', '0')` here would silently discard a
# project's GO_CGO.
#
# Precedence: CGO_ENABLED on the invocation, then the project's GO_CGO, then
# off — go's own default is cgo on wherever a C toolchain happens to exist.
go_cgo := '${CGO_ENABLED:-${GO_CGO:-0}}'

# Run CMD once per supported platform (the list below mirrors the canonical
# checksum.supported_envs in aqua.yaml) with CGO disabled: the Go build/dependency graph
# differs per GOOS, so a single native run misses the other platforms' files
# and dependencies. CGO prevents exactly that — loading packages for a foreign
# GOOS needs that platform's C toolchain — so a project that genuinely needs
# cgo declares it (`export GO_CGO := '1'`, or CGO_ENABLED=1 for one invocation)
# and gets one native run only: reduced coverage, announced loudly.
_per-goos +cmd:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{ go_cgo }}" = "1" ]; then
        echo "CGO_ENABLED=1: cross-platform runs are impossible (foreign-GOOS package" >&2
        echo "loading needs that platform's C toolchain) — native platform only." >&2
        {{ cmd }}
    else
        # Native platform FIRST, always: go_install tools build lazily on
        # first use, and `go install` refuses to cross-compile when GOBIN is
        # set (which is how aqua builds Go tools) — so a cold start must
        # trigger that build on the native leg; the foreign legs then merely
        # run the already-built binary.
        # Extend this list together with checksum.supported_envs in aqua.yaml.
        platforms=(darwin linux windows)
        native="$(go env GOHOSTOS)"
        ordered=("$native")
        for goos in "${platforms[@]}"; do
            [ "$goos" = "$native" ] || ordered+=("$goos")
        done
        for goos in "${ordered[@]}"; do
            echo "→ GOOS=$goos"
            CGO_ENABLED=0 GOOS="$goos" {{ cmd }}
        done
    fi

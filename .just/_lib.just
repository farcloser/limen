# DO NOT EDIT MANUALLY.
# This file provides shared tasks common to all projects and managed by limen.
# If you want to add project specific tasks, you can do so in the `project.just` file.

# Uniform per-recipe banner, shared across the lint/fix/tools modules via `import`
# (a `mod` can't be depended on, and modules don't inherit the parent's recipes).
# Private → hidden from --list. The module name is passed in because just has no
# current-module variable; depended on with arguments so it prints identically for
# plain *and* shebang recipes, which just otherwise echoes inconsistently (plain
# recipes echo their lines; shebang recipes do not).
_banner mod name:
    @echo "▶ {{ mod }}: {{ name }}"

# Run CMD once per supported platform (darwin, linux — mirroring the canonical
# checksum.supported_envs) with CGO disabled: the Go build/dependency graph
# differs per GOOS, so a single native run misses the other platforms' files
# and dependencies. CGO prevents exactly that — loading packages for a foreign
# GOOS needs that platform's C toolchain — so a project that genuinely needs
# cgo must export CGO_ENABLED=1 and gets one native run only: reduced
# coverage, announced loudly.
_per-goos +cmd:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "${CGO_ENABLED:-0}" = "1" ]; then
        echo "CGO_ENABLED=1: cross-platform runs are impossible (foreign-GOOS package" >&2
        echo "loading needs that platform's C toolchain) — native platform only." >&2
        {{ cmd }}
    else
        # Native platform FIRST, always: go_install tools build lazily on
        # first use, and `go install` refuses to cross-compile when GOBIN is
        # set (which is how aqua builds Go tools) — so a cold start must
        # trigger that build on the native leg; the foreign legs then merely
        # run the already-built binary.
        if [ "$(uname -s)" = "Darwin" ]; then set -- darwin linux; else set -- linux darwin; fi
        for goos in "$@"; do
            echo "→ GOOS=$goos"
            CGO_ENABLED=0 GOOS="$goos" {{ cmd }}
        done
    fi

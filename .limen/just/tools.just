# DO NOT EDIT MANUALLY.
# This file provides shared tasks common to all projects and managed by limen.
# Project recipes live in the root Justfile, below the shared-baseline import.

set working-directory := '../..'

# Silence just's per-line command echo; recipes announce themselves via _banner.
set quiet

# Shared private _banner recipe and go_tool_build (see lib.just).
import 'lib.just'

# --- Project tooling (see book/tooling.md). Every recipe takes the tool as it
#     is pinned: an aqua slug exactly as in aqua.yaml (golangci/golangci-lint),
#     or a Go package path exactly as in tools/go.mod
#     (golang.org/x/tools/cmd/deadcode). ---
#
# The two are told apart by the first path element: a Go module root is a
# host and always carries a dot; a GitHub owner never can. aqua.yaml holds no
# go_install package any more (book/tooling.md), so there is no overlap.
#
# The mutating recipes end by really installing the tool, never by linking:
# a FULL `aqua install` for an aqua package (links verify nothing — lazy pulls
# are a CI economy, wrong at the moment a pin changes; on a warm machine the
# full install is incremental), and a native build into build/tools/ for a
# Go tool (the same build _go-tool does before a recipe runs it). Either way a
# bad pin fails here, not at first use.

# Add a new tool at its latest version, e.g. `just do tools add junegunn/fzf`.
add pkg: (_banner "tools" "add")
    #!/usr/bin/env bash
    set -euo pipefail
    # Regex-escape the name: slugs and package paths carry dots, and an
    # unescaped '.' would match a sibling (`foo/li.r` vs `foo/liar`). ']' and
    # '\' are absent from both and trip BSD sed.
    esc=$(printf '%s' '{{ pkg }}' | sed 's/[.^$*+?()[{}|]/\\&/g')
    first='{{ pkg }}'; first=${first%%/*}
    if [[ $first == *.* ]]; then
        if grep -qE "^(tool[[:space:]]+)?[[:space:]]*${esc}([[:space:]]|$)" tools/go.mod 2>/dev/null; then
            echo "{{ pkg }} is already a tool directive in tools/go.mod — use 'just do tools update <command>' or 'just do tools set {{ pkg }} <version>'." >&2
            exit 1
        fi
        go -C tools get -tool '{{ pkg }}@latest'   # the directive, at the latest release
        go -C tools mod tidy
        {{ go_tool_build }} '{{ pkg }}'           # REAL build: a bad pin fails here, not at first use
        exit 0
    fi
    if grep -qE "^[[:space:]]*-[[:space:]]*name:[[:space:]]*${esc}([[:space:]]|@|$)" aqua.yaml; then
        echo "{{ pkg }} is already in aqua.yaml — use 'just do tools update {{ pkg }}' or 'just do tools set {{ pkg }} <version>'." >&2
        exit 1
    fi
    aqua generate -i "{{ pkg }}"      # append the latest version to aqua.yaml
    aqua update-checksum --prune      # record its checksum (and drop stale ones)
    aqua install                      # REAL install: a bad pin fails here, not at first use

# Set an existing tool to an exact version, e.g. `just do tools set golangci/golangci-lint v1.55.3`.
set pkg version: (_banner "tools" "set")
    #!/usr/bin/env bash
    set -euo pipefail
    # See `add` for the escaping.
    esc=$(printf '%s' '{{ pkg }}' | sed 's/[.^$*+?()[{}|]/\\&/g')
    first='{{ pkg }}'; first=${first%%/*}
    if [[ $first == *.* ]]; then
        if ! grep -qE "^(tool[[:space:]]+)?[[:space:]]*${esc}([[:space:]]|$)" tools/go.mod 2>/dev/null; then
            echo "{{ pkg }} is not a tool directive in tools/go.mod — add it with 'just do tools add {{ pkg }}'." >&2
            exit 1
        fi
        go -C tools get -tool '{{ pkg }}@{{ version }}'   # moves the pin
        go -C tools mod tidy
        {{ go_tool_build }} '{{ pkg }}'                   # REAL build: a bad pin fails here, not at first use
        exit 0
    fi
    if ! grep -qE "^[[:space:]]*-[[:space:]]*name:[[:space:]]*${esc}@" aqua.yaml; then
        echo "{{ pkg }} is not pinned in aqua.yaml — add it with 'just do tools add {{ pkg }}'." >&2
        exit 1
    fi
    # Explicit sibling template: macOS mktemp ignores $TMPDIR, and a sibling
    # makes the mv an atomic same-filesystem rename.
    tmp=$(mktemp aqua.yaml.XXXXXX)
    trap 'rm -f "$tmp"' EXIT
    sed -E "s#(^[[:space:]]*-[[:space:]]*name:[[:space:]]*${esc})@[^[:space:]]*#\1@{{ version }}#" aqua.yaml > "$tmp"
    mv "$tmp" aqua.yaml
    aqua update-checksum --prune      # new checksum in, replaced version's out
    aqua install                      # REAL install: a bad pin fails here, not at first use

# Takes the COMMAND name (`just`, `limen`, `deadcode`), not the package: `aqua
# update` resolves a command to its pinned package, local registry included,
# and a tool directive is found by the command it builds as. (The blank line
# below keeps this paragraph out of `just --list`; only the line right above
# the recipe is its doc.)

# Update an existing tool to its latest version, e.g. `just do tools update golangci-lint`.
update command: (_banner "tools" "update")
    #!/usr/bin/env bash
    set -euo pipefail
    # A directive builds as its last path element, except that go strips a /vN
    # major suffix (github.com/google/go-licenses/v2 builds as go-licenses —
    # see _go-tool). A `tool` line and a `tool (…)` block are both walked;
    # a `//` comment line inside the block is not a directive.
    pkg=$(awk -v cmd='{{ command }}' '
        function name(p,  n, a) { n = split(p, a, "/"); if (n > 1 && a[n] ~ /^v[0-9]+$/) return a[n - 1]; return a[n] }
        { p = "" }
        /^tool[[:blank:]]*\(/ { block = 1; next }
        block && /^\)/ { block = 0; next }
        /^tool[[:blank:]]/ { p = $2 }
        block && NF && $1 !~ /^\/\// { p = $1 }
        p != "" && name(p) == cmd { print p; exit }
    ' tools/go.mod 2>/dev/null || true)
    if [ -n "$pkg" ]; then
        go -C tools get -tool "${pkg}@latest"   # bump tools/go.mod to the latest release
        go -C tools mod tidy
        {{ go_tool_build }} "$pkg"              # REAL build: a bad pin fails here, not at first use
        exit 0
    fi
    aqua update "{{ command }}"       # bump aqua.yaml to the latest release
    aqua update-checksum --prune      # new checksum in, replaced version's out
    aqua install                      # REAL install: a bad pin fails here, not at first use

# Remove a tool entirely, e.g. `just do tools remove junegunn/fzf`.
remove pkg: (_banner "tools" "remove")
    #!/usr/bin/env bash
    set -euo pipefail
    # See `add` for the escaping.
    esc=$(printf '%s' '{{ pkg }}' | sed 's/[.^$*+?()[{}|]/\\&/g')
    first='{{ pkg }}'; first=${first%%/*}
    if [[ $first == *.* ]]; then
        if ! grep -qE "^(tool[[:space:]]+)?[[:space:]]*${esc}([[:space:]]|$)" tools/go.mod 2>/dev/null; then
            echo "{{ pkg }} is not a tool directive in tools/go.mod — nothing to remove." >&2
            exit 1
        fi
        go -C tools get -tool '{{ pkg }}@none'   # drops the directive
        go -C tools mod tidy                     # and the requirements only it needed
        # The built binary, named as go names it: last element, /vN stripped.
        pkg='{{ pkg }}'; name=${pkg##*/}
        if [[ $name =~ ^v[0-9]+$ ]]; then name=${pkg%/*}; name=${name##*/}; fi
        rm -f "build/tools/${name}" "build/tools/${name}.exe"
        exit 0
    fi
    if ! grep -qE "^[[:space:]]*-[[:space:]]*name:[[:space:]]*${esc}([[:space:]]|@|$)" aqua.yaml; then
        echo "{{ pkg }} is not in aqua.yaml — nothing to remove." >&2
        exit 1
    fi
    # See `set` for why mktemp gets an explicit sibling-path template.
    tmp=$(mktemp aqua.yaml.XXXXXX)
    trap 'rm -f "$tmp"' EXIT
    # Exact string compare on the parsed slug, not a regex (a dot would be a
    # wildcard, and escaping for awk is unportable: -v eats backslashes). The
    # entry's extent is decided by indentation, as YAML decides it: deeper lines
    # belong to it, a line at its own depth or shallower ends it — so a
    # `# --- section ---` header at item depth stays with what follows.
    awk -v pkg="{{ pkg }}" '
        function indent(l) { match(l, /^[[:blank:]]*/); return RLENGTH }
        {
            if (skip) { if ($0 !~ /^[[:blank:]]*$/ && indent($0) > depth) next; skip = 0 }
            if (match($0, /^[[:blank:]]*-[[:blank:]]*name:[[:blank:]]*/)) {
                slug = substr($0, RLENGTH + 1)
                sub(/[[:blank:]].*$/, "", slug)   # drop trailing spaces / comment
                sub(/@.*$/, "", slug)             # drop @version
                if (slug == pkg) { skip = 1; depth = indent($0); next }
            }
            print
        }
    ' aqua.yaml > "$tmp"
    mv "$tmp" aqua.yaml
    aqua remove "{{ pkg }}" || true   # uninstall the binary
    aqua update-checksum --prune      # drop the now-unused checksum

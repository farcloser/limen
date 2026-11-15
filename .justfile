# This file is the project's own — add recipes below. Keep the import: it
# mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# Judge this repository by its own working tree, not the released pin: `go run`
# compiles the current tree on every invocation; limen-lint-go, a module of
# its own under cmd/, is built into $LIMEN_BUILD/tools/ first (build-lint-go).
export LIMEN_BIN := 'go run ./cmd/limen'
export LIMEN_LINT_GO_BIN := LIMEN_BUILD / 'tools/limen-lint-go'

# Bare `just` lists; `lint`, `test` and `security` below are what CI runs.
default:
    @just --list

lint: build-lint-go do::lint::default do::lint::go::default do::lint::go::deadcode
fix: build-lint-go do::fix::default do::fix::go::default
test: do::test::go::default test-lint-go test-lint-converge
security: do::security::default

# limen-lint-go is the second binary of limen's release (see .release-go.yaml),
# a nested module so its YAML parser never enters limen's own go.mod. The
# shared Go lint lanes enter it as they enter every command that is a module
# of its own, on the baseline rendered with its own .lint-go.yaml; this
# repository builds it first, since the lanes render through it, and runs its
# tests itself, through the gotestsum the test go lane built, which stops at
# the root module.
build-lint-go:
    mkdir -p "$LIMEN_BUILD/tools" && GOOS='' GOARCH='' go -C cmd/limen-lint-go build -o "$LIMEN_BUILD/tools/" .

test-lint-go:
    cd cmd/limen-lint-go && "$LIMEN_BUILD/tools/gotestsum" -- -count=1 -timeout "${TEST_GO_TIMEOUT:-10m}" ./...

# The `lint converge` lane (.limen/just/lint.just), run for real. limen
# declares no derived file of its own, so its lint passes the lane vacuously
# and nothing else in this repository runs it: a scratch repository with a
# copy of the shared recipes, a declared file, a declared directory and a
# `converge` recipe, on every platform the tests run on. On Windows the lane reaches `just` and the
# project's recipe through git-bash, which is where it once could not.
test-lint-converge:
    #!/usr/bin/env bash
    set -euo pipefail
    # The pins are this checkout's, wherever the lane runs from.
    export HERMETIC_CONFIG="${HERMETIC_CONFIG:-$PWD/.hermetic/hermetic.yaml}"
    scratch=$(mktemp -d "${TMPDIR:-/tmp}/lint-converge.XXXXXX")
    out="$scratch.out"
    # Out of the scratch before it goes: Windows does not remove a directory
    # a process still stands in.
    here="$PWD"
    trap 'cd "$here" && rm -rf "$scratch" "$out"' EXIT
    mkdir -p "$scratch/.limen"
    cp -R .limen/just "$scratch/.limen/just"
    cd "$scratch"
    git init -q .
    git config core.autocrlf false
    printf 'derived, as converged\n' > derived.lock
    printf 'other\n' > other.txt
    mkdir -p gen/sub generated locks elsewhere
    printf 'one\n' > gen/one.txt
    printf 'two\n' > gen/sub/two.txt
    printf 'beside\n' > generated/beside.txt
    printf 'locked\n' > locks/deep.lock
    printf 'kept\n' > elsewhere/two.txt
    printf 'kept\n' > elsewhere/deep.lock
    printf '# the derived files\nderived.lock\nlocks/deep.lock\ngen/\n' > .converge
    {
        printf '%s\n' "import '.limen/just/main.just'" '' 'converge:'
        printf '%s\n' "    printf 'derived, %s\\n' \"\${CONVERGE_SAYS:-as converged}\" > derived.lock"
        # shellcheck disable=SC2016 # the recipe's own text: expanded when it runs, not here
        printf '%s\n' '    if [ -n "${CONVERGE_TOUCHES:-}" ]; then echo touched >> other.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = change ]; then echo moved > gen/one.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = add ]; then mkdir -p gen/new && echo three > gen/new/three.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = drop ]; then rm gen/sub/two.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = beside ]; then echo touched >> generated/beside.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = spaced ]; then echo spaced > "gen/new file.txt"; fi' \
            '    if [ "${CONVERGE_DIR:-}" = link ]; then rm gen/one.txt && ln -s ../other.txt gen/one.txt; fi' \
            '    if [ "${CONVERGE_DIR:-}" = filelink ]; then rm derived.lock && ln -s other.txt derived.lock; fi' \
            '    if [ "${CONVERGE_DIR:-}" = dirlink ]; then rm -r gen/sub && ln -s ../elsewhere gen/sub; fi' \
            '    if [ "${CONVERGE_DIR:-}" = parentlink ]; then rm -r locks && ln -s elsewhere locks; fi'
    } > .justfile
    git add -A
    lane() { just do lint converge > "$out" 2>&1; }
    refused() {
        if lane; then
            echo "lint converge passed, want it to fail: $1" >&2
            cat "$out" >&2
            exit 1
        fi
        if ! grep -qF -- "$2" "$out"; then
            echo "lint converge failed without saying \"$2\": $1" >&2
            cat "$out" >&2
            exit 1
        fi
    }
    if ! lane; then
        echo "lint converge failed on files that are what the recipe makes:" >&2
        cat "$out" >&2
        exit 1
    fi
    CONVERGE_SAYS=moved refused "a declared file the recipe would change" "derived.lock is not what 'just converge' makes"
    if [ "$(cat derived.lock)" != 'derived, as converged' ]; then
        echo "lint converge left derived.lock as the recipe wrote it: $(cat derived.lock)" >&2
        exit 1
    fi
    CONVERGE_TOUCHES=1 refused "a file the recipe touches and does not declare" "changed other.txt, which .converge does not declare"
    printf 'other\n' > other.txt
    CONVERGE_DIR=change refused "a file under a declared directory the recipe would change" "gen/one.txt is not what 'just converge' makes"
    CONVERGE_DIR=add refused "a file the recipe makes under a declared directory and the tree lacks" "gen/new/three.txt is made by 'just converge' and is not in this tree"
    CONVERGE_DIR=drop refused "a file under a declared directory the recipe no longer makes" "gen/sub/two.txt is no longer made by 'just converge'"
    if [ "$(cat gen/one.txt)" != one ] || [ "$(cat gen/sub/two.txt)" != two ] || [ -e gen/new ]; then
        echo "lint converge left the declared directory as the recipe made it:" >&2
        find gen >&2
        exit 1
    fi
    CONVERGE_DIR=beside refused "a directory whose name starts as a declared one does" "changed generated/beside.txt, which .converge does not declare"
    printf 'beside\n' > generated/beside.txt
    CONVERGE_DIR=spaced refused "a new file under a declared directory, a space in its name" "gen/new file.txt is made by 'just converge' and is not in this tree"
    if grep -qF "does not declare" "$out"; then
        echo "lint converge called a file under a declared directory undeclared:" >&2
        cat "$out" >&2
        exit 1
    fi
    # A link where a file was: the lane says so, and puts the file back
    # without writing through the link. Not on Windows, where git-bash's ln
    # copies.
    if [ "{{ os() }}" != windows ]; then
        CONVERGE_DIR="link" refused "a link the recipe leaves under a declared directory" "made gen/one.txt, which is not a regular file"
        CONVERGE_DIR=filelink refused "a link the recipe leaves where a declared file was" "derived.lock is not what 'just converge' makes"
        if [ -L gen/one.txt ] || [ -L derived.lock ] || [ "$(cat gen/one.txt)" != one ] || [ "$(cat other.txt)" != other ] \
            || [ "$(cat derived.lock)" != 'derived, as converged' ]; then
            echo "lint converge restored through a link, or left one:" >&2
            ls -l gen derived.lock >&2
            cat other.txt >&2
            exit 1
        fi
        # A link where a directory was, above the files to restore: under the
        # declared directory, and above a declared file.
        CONVERGE_DIR=dirlink refused "a link the recipe leaves where a directory under a declared one was" "made gen/sub, which is not a regular file"
        CONVERGE_DIR=parentlink refused "a link the recipe leaves where a declared file's directory was" "locks/deep.lock is not what 'just converge' makes"
        if [ -L gen/sub ] || [ -L locks ] || [ "$(cat gen/sub/two.txt)" != two ] || [ "$(cat locks/deep.lock)" != locked ] \
            || [ "$(cat elsewhere/two.txt)" != kept ] || [ "$(cat elsewhere/deep.lock)" != kept ]; then
            echo "lint converge restored through a link to a directory, or left one:" >&2
            ls -l gen locks elsewhere >&2
            cat elsewhere/two.txt elsewhere/deep.lock >&2
            exit 1
        fi
    fi
    printf './derived.lock\n' > .converge
    refused "a path spelled otherwise than git names it" "is not a tracked regular file under that exact name"
    printf './gen/\n' > .converge
    refused "a directory spelled otherwise than git names it" "is not a directory of tracked files under that exact name"
    printf '.github/\n' > .converge
    refused "a directory that is GitHub's" "is not a path a derived file may have"
    echo "lint converge: in sync, stale and restored, undeclared, misspelled; a directory changed, short, long and restored, ok"

# Host-side helper, not baseline: presupposes macOS, UTM and a provisioned
# Windows VM (book/vm_testing.md). The VM mounts this repository's parent as
# Z:\. `utmctl exec` relays neither output nor exit code, so the guest writes a
# log and an exit marker at the share root and we poll. Arguments travel as
# argv end to end; no env is passed, since qemu-ga replaces the guest
# environment wholesale (which strips APPDATA and breaks aqua). Tunables:
# VM_NAME, VM_TIMEOUT.
vm +args:
    #!/usr/bin/env bash
    set -euo pipefail
    vm_name="${VM_NAME:-Windows}"
    timeout="${VM_TIMEOUT:-1800}"
    # Absolute path: the hermetic PATH excludes homebrew, and utmctl must be
    # reachable at /Applications/UTM.app anyway (the binary hardcodes it).
    utmctl="${UTMCTL:-/Applications/UTM.app/Contents/MacOS/utmctl}"
    # The VM shares the parent of this repository; the guest sees the repo at
    # Z:/<its basename>. Markers land at the share root — outside the repo, so
    # nothing pollutes the working tree.
    host_repo="{{ justfile_directory() }}"
    share_root="$(dirname "$host_repo")"
    guest_dir="Z:/$(basename "$host_repo")"
    runid="__vm-$(date +%s)-$$"
    # Read first, searched after: utmctl piped into grep -q can be killed by
    # SIGPIPE at the match, which pipefail reads as the vm not running.
    status="$("$utmctl" status "$vm_name")"
    printf '%s\n' "$status" | grep -q started \
        || { echo "vm '$vm_name' is not running (utmctl status: $status)" >&2; exit 1; }
    # shellcheck disable=SC2016 # the template expands in the GUEST bash, by design
    "$utmctl" exec "$vm_name" --cmd "C:/Program Files/Git/bin/bash.exe" -l -c \
        'cd "$1" && just "${@:3}" > "Z:/$2.log" 2>&1; echo $? > "Z:/$2.exit"' \
        bash "$guest_dir" "$runid" {{ args }}
    # Stream the guest log as it grows (line-count bookkeeping — no background
    # tail to babysit), so a slow run looks slow instead of stuck.
    log="$share_root/$runid.log"
    printed=0
    drain() {
        [ -f "$log" ] || return 0
        total="$(wc -l < "$log")"
        if [ "$total" -gt "$printed" ]; then
            tail -n "+$((printed + 1))" "$log"
            printed="$total"
        fi
    }
    deadline=$(( $(date +%s) + timeout ))
    while [ ! -f "$share_root/$runid.exit" ]; do
        drain
        if [ "$(date +%s)" -ge "$deadline" ]; then
            echo "timed out after ${timeout}s — the guest may still be running ($log left behind)" >&2
            exit 124
        fi
        sleep 2
    done
    drain
    status="$(tr -d '[:space:]' < "$share_root/$runid.exit")"
    rm -f "$log" "$share_root/$runid.exit"
    exit "$status"

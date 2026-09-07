# This file is the project's own — add recipes below. Keep the import: it
# mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

export LINT_GO_LICENSES_FLAGS := "--ignore gotest.tools"

# Judge this repository by its own working tree, not the released pin: `go run`
# compiles the current tree on every invocation.
export LIMEN_BIN := 'go run ./cmd/limen'

# Bare `just` lists; `lint` and `test` below are what CI runs.
default:
    @just --list

lint: do::lint::default do::lint::go::default do::lint::go::deadcode
fix: do::fix::default do::fix::go::default
test: do::test::go::default

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
    "$utmctl" status "$vm_name" | grep -q started \
        || { echo "vm '$vm_name' is not running (utmctl status)" >&2; exit 1; }
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

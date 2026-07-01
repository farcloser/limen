package github

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OverridePath is the committed per-repository exceptions file: a delta
// against the baseline, exceptions only, never a full settings copy. Each
// line names a check identifier and the reason it is exempt:
//
//	wiki: hosts the operations runbook
//
// An exempted check reports ok (with the reason), visibly — the escape hatch
// lives in review, never in a UI click. Unknown identifiers and empty reasons
// fail the file itself.
const OverridePath = ".github/limen-github.yaml"

var (
	errUnknownCheck = errors.New("unknown check identifier")
	errEmptyReason  = errors.New("an exception requires a reason")
)

// LoadOverrides reads the exceptions file under dir. A missing file means no
// exceptions; a malformed one is an error (a broken escape hatch must not
// silently exempt nothing — or everything).
func LoadOverrides(dir string) (map[string]string, error) {
	path := filepath.Join(dir, filepath.FromSlash(OverridePath))

	data, err := os.ReadFile(path) //nolint:gosec // G304: caller-designated repository, the tool's contract.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}

		return nil, fmt.Errorf("reading %s: %w", OverridePath, err)
	}

	known := knownChecks()
	overrides := map[string]string{}

	for lineNumber, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, reason, found := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		reason = strings.TrimSpace(reason)

		if !found || reason == "" {
			return nil, fmt.Errorf("%s:%d: %w", OverridePath, lineNumber+1, errEmptyReason)
		}

		if !known[key] {
			return nil, fmt.Errorf("%s:%d: %w: %q", OverridePath, lineNumber+1, errUnknownCheck, key)
		}

		overrides[key] = reason
	}

	return overrides, nil
}

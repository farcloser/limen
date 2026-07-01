package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// InferRepo derives the "owner/name" slug from the origin remote of the git
// repository at dir, accepting both SSH (git@github.com:owner/name.git) and
// HTTPS (https://github.com/owner/name) remote forms.
func InferRepo(dir string) (string, error) {
	gitArgs := []string{"-C", dir, "remote", "get-url", "origin"}
	cmd := exec.CommandContext(context.Background(), "git", gitArgs...) //nolint:gosec // G204: fixed argument list.

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w (git remote get-url origin: %w)", errNoRepo, err)
	}

	remote := strings.TrimSpace(string(out))

	var slug string

	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		slug = strings.TrimPrefix(remote, "git@github.com:")
	case strings.Contains(remote, "github.com/"):
		_, slug, _ = strings.Cut(remote, "github.com/")
	default:
		return "", fmt.Errorf("%w: origin %q is not a github.com remote", errNoRepo, remote)
	}

	slug = strings.TrimSuffix(slug, ".git")
	slug = strings.Trim(slug, "/")

	const slugParts = 2
	if parts := strings.Split(slug, "/"); len(parts) != slugParts || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("%w: cannot parse %q into owner/name", errNoRepo, remote)
	}

	return slug, nil
}

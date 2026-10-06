// Package fetch downloads a URL to a file, retrying what is worth retrying.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ErrFetch is a download that did not land: a definitive refusal, or
// transient failures that outlasted every attempt.
var ErrFetch = errors.New("download failed")

// Transient failures are retried the way every curl in the rig retries, so a
// hiccup does not cost a run.
const (
	attempts = 5
	backoff  = 3 * time.Second
)

// errTransient is a failure worth another attempt: no answer, or a 5xx.
var errTransient = errors.New("retryable")

// File fetches url into file and returns the sha256 of what it wrote,
// retrying transient failures with a growing pause. No whole-request timeout:
// an artifact can be gigabytes; ctx is the bound.
func File(ctx context.Context, url, file string) (string, error) {
	var lastErr error

	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("%w: %s: %w", ErrFetch, url, ctx.Err())
			case <-time.After(backoff * time.Duration(attempt)):
			}
		}

		sum, err := once(ctx, url, file)
		if err == nil {
			return sum, nil
		}

		if !errors.Is(err, errTransient) {
			return "", err
		}

		lastErr = err
	}

	return "", fmt.Errorf("%w: gave up after %d attempts: %w", ErrFetch, attempts, lastErr)
}

// once is one attempt: a status worth retrying is errTransient, a definitive
// refusal is ErrFetch.
func once(ctx context.Context, url, file string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrFetch, url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", errTransient, url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("%w: GET %s: HTTP %d%s", errTransient, url, resp.StatusCode, answeredBy(req, resp))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: GET %s: HTTP %d%s", ErrFetch, url, resp.StatusCode, answeredBy(req, resp))
	}

	out, err := os.Create(file) // #nosec G304 -- a path the caller built under its own temporary directory.
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrFetch, file, err)
	}

	hash := sha256.New()

	_, copyErr := io.Copy(io.MultiWriter(out, hash), resp.Body)
	closeErr := out.Close()

	if copyErr != nil {
		return "", fmt.Errorf("%w: downloading %s: %w", errTransient, url, copyErr)
	}

	if closeErr != nil {
		return "", fmt.Errorf("%w: writing %s: %w", ErrFetch, file, closeErr)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// answeredBy names the host that sent the status when a redirect moved the
// request off the one asked: a release URL answers with a redirect, and the
// status worth reporting may be the asset host's, not GitHub's.
func answeredBy(req *http.Request, resp *http.Response) string {
	if resp.Request == nil || resp.Request.URL.Host == req.URL.Host {
		return ""
	}

	return " from " + resp.Request.URL.Host + " (redirected)"
}

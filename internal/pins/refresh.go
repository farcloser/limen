package pins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/farcloser/limen/internal/verify/openpgp"
)

// ErrVerify is an artifact the declared method would not vouch for.
var ErrVerify = errors.New("verification failed")

// filePermissions is owner read-write, like every file limen writes.
const filePermissions = 0o600

// Every download lands under a temporary directory of its own.
const (
	tempPrefix = "limen-pins"
	errTempDir = "temporary directory: %w"
)

// errAt prefixes an error with a sentinel and what it is about.
const errAt = "%w: %s: %w"

// Load reads the repository's pins.yaml. A missing file is (Manifest{}, false, nil).
func Load(root string) (Manifest, bool, error) {
	// #nosec G304 -- caller-designated repository, the tool's contract.
	data, err := os.ReadFile(filepath.Join(root, File))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, false, nil
		}

		return Manifest{}, false, fmt.Errorf("reading %s: %w", File, err)
	}

	manifest, err := Parse(data)
	if err != nil {
		return Manifest{}, true, err
	}

	return manifest, true, nil
}

// Refresh recomputes the digest of every stale entry through the method
// each declares, rewrites those digests in place, and returns the names it
// changed. The first failure stops the run and the file is left as it was,
// digests already computed included: a half-refreshed manifest is worse
// than a stale one, since check can no longer tell which half.
func Refresh(ctx context.Context, root string, progress io.Writer) ([]string, error) {
	return refreshDue(ctx, root, progress, Entry.Stale)
}

// RefreshAll recomputes every digest, stale or not: the check after a
// changed verifier, or a doubt.
func RefreshAll(ctx context.Context, root string, progress io.Writer) ([]string, error) {
	return refreshDue(ctx, root, progress, func(Entry) bool { return true })
}

// errFormat prefixes an error with what it is about.
const errFormat = "%s: %w"

// refreshDue recomputes the entries due tells it to.
func refreshDue(ctx context.Context, root string, progress io.Writer, due func(Entry) bool) ([]string, error) {
	manifest, found, err := Load(root)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, fmt.Errorf("%w: no %s in %s", os.ErrNotExist, File, root)
	}

	var changed []string

	for index, entry := range manifest.Entries {
		if !due(entry) {
			continue
		}

		sum, err := digest(ctx, root, entry)
		if err != nil {
			return changed, fmt.Errorf(errFormat, entry.Name, err)
		}

		if sum == entry.SHA256 && !entry.Stale() {
			_, _ = fmt.Fprintf(progress, "%s: %s unchanged\n", entry.Name, entry.Version)

			continue
		}

		manifest = manifest.withDigest(index, sum)

		changed = append(changed, entry.Name)

		_, _ = fmt.Fprintf(progress, "%s: %s → sha256 %s\n", entry.Name, entry.Version, sum)
	}

	if len(changed) == 0 {
		return nil, nil
	}

	if err := os.WriteFile(filepath.Join(root, File), []byte(manifest.Text()), filePermissions); err != nil {
		return changed, fmt.Errorf("writing %s: %w", File, err)
	}

	return changed, nil
}

// digest obtains the artifact's sha256 the way the entry says it must be.
func digest(ctx context.Context, root string, entry Entry) (string, error) {
	args := entry.VerifyArgs()

	switch entry.Method() {
	case VerifyDownload:
		return hashDownload(ctx, entry.ResolvedURL(), nil)
	case VerifyGitHubAttestation:
		return hashDownload(ctx, entry.ResolvedURL(), func(file string) error {
			return run(ctx, root, "gh", "attestation", "verify", file, "--owner", args[0])
		})
	case VerifyGitHubReleaseAsset:
		tag := entry.Version
		if len(args) > 1 {
			tag = args[1]
		}

		return hashDownload(ctx, entry.ResolvedURL(), func(file string) error {
			return run(ctx, root, "gh", "release", "verify-asset", tag, file, "--repo", args[0])
		})
	case VerifyCosignSums:
		return hashFromSignedSums(ctx, root, entry, args)
	case VerifyPGPSums:
		return hashFromClearsignedSums(ctx, entry, args)
	default:
		return "", fmt.Errorf("%w: unknown method %q", ErrEntry, entry.Method())
	}
}

// hashDownload fetches url to a temporary file, has verify (when given)
// accept that file, and returns its sha256. The bytes hashed are the bytes
// verified: one download, one file.
func hashDownload(ctx context.Context, url string, verify func(file string) error) (string, error) {
	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		return "", fmt.Errorf(errTempDir, err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	// The verifiers judge the file by its name too (gh matches a release
	// asset by basename), so the download keeps the artifact's own name.
	file := filepath.Join(dir, path.Base(url))

	sum, err := download(ctx, url, file)
	if err != nil {
		return "", err
	}

	if verify != nil {
		if err := verify(file); err != nil {
			return "", err
		}
	}

	return sum, nil
}

// hashFromSignedSums takes the artifact's line from a SHA256SUMS file that
// cosign vouches for: the sums and their bundle are fetched, verified as a
// pair, and only then read.
func hashFromSignedSums(ctx context.Context, root string, entry Entry, args []string) (string, error) {
	sumsURL, bundleURL, identity, issuer := args[0], args[1], args[2], args[3]

	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		return "", fmt.Errorf(errTempDir, err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	sums := filepath.Join(dir, "SHA256SUMS")
	bundle := filepath.Join(dir, "SHA256SUMS.bundle")

	if _, err = download(ctx, sumsURL, sums); err != nil {
		return "", err
	}

	if _, err = download(ctx, bundleURL, bundle); err != nil {
		return "", err
	}

	if err = run(ctx, root, "cosign", "verify-blob", "--bundle", bundle,
		"--certificate-identity-regexp", identity, "--certificate-oidc-issuer", issuer, sums); err != nil {
		return "", err
	}

	data, err := os.ReadFile(sums) // #nosec G304 -- a file this function just wrote.
	if err != nil {
		return "", fmt.Errorf("reading the signed sums: %w", err)
	}

	return sumFor(data, path.Base(entry.ResolvedURL()), sumsURL)
}

// hashFromClearsignedSums takes the artifact's line from a PGP-clearsigned
// sums file: the file and the signer's public key block are fetched, the
// signature is verified in-process, the signing key must be the one the
// entry pins by fingerprint, and only the text the signature vouches for is
// read. The key's URL is transport — a key from anywhere is accepted if and
// only if it is the pinned one.
func hashFromClearsignedSums(ctx context.Context, entry Entry, args []string) (string, error) {
	sumsURL, keyURL, fingerprint := args[0], args[1], strings.ToUpper(args[2])

	dir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		return "", fmt.Errorf(errTempDir, err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	sums := filepath.Join(dir, "SHA256SUMS.asc")
	key := filepath.Join(dir, "key.asc")

	if _, err = download(ctx, sumsURL, sums); err != nil {
		return "", err
	}

	if _, err = download(ctx, keyURL, key); err != nil {
		return "", err
	}

	keyData, err := os.ReadFile(key) // #nosec G304 -- a file this function just wrote.
	if err != nil {
		return "", fmt.Errorf("reading the key: %w", err)
	}

	keys, err := openpgp.ParseKeys(keyData)
	if err != nil {
		return "", fmt.Errorf(errAt, ErrVerify, keyURL, err)
	}

	sumsData, err := os.ReadFile(sums) // #nosec G304 -- a file this function just wrote.
	if err != nil {
		return "", fmt.Errorf("reading the signed sums: %w", err)
	}

	verified, err := openpgp.VerifyClearsigned(sumsData, keys)
	if err != nil {
		return "", fmt.Errorf(errAt, ErrVerify, sumsURL, err)
	}

	if string(verified.Signer) != fingerprint {
		return "", fmt.Errorf("%w: %s is signed by %s, not by the pinned key %s",
			ErrVerify, sumsURL, verified.Signer, fingerprint)
	}

	return sumFor(verified.Text, path.Base(entry.ResolvedURL()), sumsURL)
}

// sumFor takes the sha256 of one file out of a `<sha256>  <name>` listing;
// a name it does not carry, or carries with something other than a sha256,
// is a verification failure, since the listing is what was vouched for.
func sumFor(data []byte, want, sumsURL string) (string, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == want {
			if !sha256RE.MatchString(fields[0]) {
				return "", fmt.Errorf("%w: %s carries no sha256 for %s", ErrVerify, sumsURL, want)
			}

			return fields[0], nil
		}
	}

	return "", fmt.Errorf("%w: no line for %s in the signed sums at %s", ErrVerify, want, sumsURL)
}

// Transient failures are retried the way every curl in the rig retries,
// so a hiccup does not cost a Renovate branch its workflow run.
const (
	downloadAttempts = 5
	downloadBackoff  = 3 * time.Second
)

// errTransient is a failure worth another attempt: no answer, or a 5xx.
var errTransient = errors.New("retryable")

// download fetches url into file and returns the sha256 of what it wrote,
// retrying transient failures. No whole-request timeout: an artifact can be
// gigabytes; ctx is the bound.
func download(ctx context.Context, url, file string) (string, error) {
	var lastErr error

	for attempt := range downloadAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("%s: %w", url, ctx.Err())
			case <-time.After(downloadBackoff * time.Duration(attempt)):
			}
		}

		sum, err := downloadOnce(ctx, url, file)
		if err == nil {
			return sum, nil
		}

		if !errors.Is(err, errTransient) {
			return "", err
		}

		lastErr = err
	}

	return "", fmt.Errorf("%w: gave up after %d attempts: %w", ErrVerify, downloadAttempts, lastErr)
}

// downloadOnce is one attempt: a status worth retrying is errTransient, a
// definitive refusal is ErrVerify.
func downloadOnce(ctx context.Context, url, file string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf(errFormat, url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(errAt, errTransient, url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("%w: GET %s: HTTP %d", errTransient, url, resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: GET %s: HTTP %d", ErrVerify, url, resp.StatusCode)
	}

	out, err := os.Create(file) // #nosec G304 -- a path this package built under its own temporary directory.
	if err != nil {
		return "", fmt.Errorf(errFormat, file, err)
	}

	hash := sha256.New()

	_, copyErr := io.Copy(io.MultiWriter(out, hash), resp.Body)
	closeErr := out.Close()

	if copyErr != nil {
		return "", fmt.Errorf("%w: downloading %s: %w", errTransient, url, copyErr)
	}

	if closeErr != nil {
		return "", fmt.Errorf("writing %s: %w", file, closeErr)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// run executes a pinned verifier from the repository root (the hermetic PATH
// resolves from there) and folds a refusal into ErrVerify with the tool's
// last words.
func run(ctx context.Context, root, name string, args ...string) error {
	// name is one of the two verifiers this package calls; args are the
	// entry's own declared values plus a file this package wrote.
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- see above.
	cmd.Dir = root

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s %s: %w: %s", ErrVerify, name, strings.Join(args, " "), err, lastLine(string(out)))
	}

	return nil
}

// lastLine is the last non-empty line of a tool's output, capped.
func lastLine(out string) string {
	const maxLen = 200

	lines := strings.Split(strings.TrimSpace(out), "\n")

	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > maxLen {
		last = last[:maxLen] + "…"
	}

	return last
}

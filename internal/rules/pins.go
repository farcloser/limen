package rules

import (
	"slices"
	"strings"

	"github.com/farcloser/limen/internal/pins"
)

// verifierPackages maps each verification method that shells out to the
// aqua package of its tool; a method verified in-process (pgp-sha256sums)
// has no entry. Unpinned, aqua's proxy falls through to whatever gh or
// cosign the machine has, and a digest would be vouched for by a binary
// nobody chose; the rule requires the pin, offline.
//
//nolint:gochecknoglobals // immutable table.
var verifierPackages = map[string]string{
	pins.VerifyGitHubAttestation:  "cli/cli",
	pins.VerifyGitHubReleaseAsset: "cli/cli",
	pins.VerifyCosignSums:         "sigstore/cosign",
}

// checkPins judges pins.yaml when the repository carries one: the file has
// the shape, every entry is complete with a known verification method, the
// tool each method shells out to is pinned in aqua.yaml, and no digest is
// stale — computed for a version other than the one pinned, which is what a
// Renovate bump leaves until `limen pins refresh` runs. A repository without
// the file has no pins, and no finding. Offline, like every check: whether a
// digest is right is the refresh's job, whether it is current is this rule's.
func checkPins(root string) (Finding, bool) {
	const rule = "pins"

	manifest, found, err := pins.Load(root)
	if !found && err == nil {
		return Finding{}, false
	}

	if err != nil {
		return fail(rule, pins.File, err.Error()), true
	}

	if missing := unpinnedVerifiers(root, manifest); len(missing) > 0 {
		return fail(rule, pins.File,
			"the verifiers pins.yaml relies on must be pinned in aqua.yaml (`just do tools add`): "+
				strings.Join(missing, ", ")), true
	}

	if stale := manifest.Stale(); len(stale) > 0 {
		return fail(rule, pins.File,
			"digest computed for another version than the one pinned: "+strings.Join(stale, ", ")+
				" — run `limen pins refresh` (the checksum workflow does, on Renovate branches)"), true
	}

	return Finding{
		Rule: rule, Status: StatusOK, Path: pins.File,
		Message: manifest.String() + " declared, every digest current",
	}, true
}

// unpinnedVerifiers names the aqua packages the manifest's methods need and
// aqua.yaml does not declare, sorted, each once.
func unpinnedVerifiers(root string, manifest pins.Manifest) []string {
	var declared []string

	if name, ok := findFirst(root, "aqua.yaml", "aqua.yml"); ok {
		if data, err := readRepoFile(root, name); err == nil {
			if aqua, parsed := parseAquaManifest(string(data)); parsed {
				for _, pkg := range aqua.pkgs {
					declared = append(declared, pkg.name)
				}
			}
		}
	}

	var missing []string

	for _, entry := range manifest.Entries {
		pkg, needed := verifierPackages[entry.Method()]
		if needed && !slices.Contains(declared, pkg) && !slices.Contains(missing, pkg) {
			missing = append(missing, pkg)
		}
	}

	slices.Sort(missing)

	return missing
}

// Package pins is the one place a repository declares the artifacts it
// fetches by hand — a kernel tarball, a toolchain release, a source archive —
// pinned by digest, the way aqua.yaml declares tools. The file is data the
// build reads back (`limen pins get`), Renovate moves (the shared preset
// watches every pins.yaml), and limen keeps honest (`limen pins refresh`
// recomputes a digest after a version bump, through the verifier the entry
// declares; the `pins` rule fails a digest that no longer matches its
// version). Nothing about a pin lives anywhere else, so nothing drifts.
//
// The format is a fixed YAML shape, parsed by hand like aqua.yaml so limen
// keeps zero dependencies:
//
//	pins:
//	  - name: llvm
//	    renovate: github-releases llvm/llvm-project
//	    extract-version: ^llvmorg-(?<version>.*)$
//	    version: 22.1.8
//	    url: https://github.com/llvm/llvm-project/releases/download/llvmorg-${version}/LLVM-${version}.tar.xz
//	    verify: github-attestation llvm
//	    digest:
//	      version: 22.1.8
//	      sha256: 805efad2bb91cb4967fa569e0881d10c0f69c04461cf671cccbae19f547acc34
//
// Two-space indentation, one scalar per line, comments and blank lines
// anywhere. `version` is what moves; `digest.version` records what the
// sha256 was computed for, so a stale digest is visible without the network.
// The `renovate` line, the optional `extract-version` and `versioning` lines,
// and the `version` line come in that order with nothing else between them:
// that is the block the shared preset's regex reads.
package pins

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// File is the manifest's path, at the repository root.
const File = "pins.yaml"

// The fields of one entry, in the order the file carries them.
const (
	fieldName           = "name"
	fieldRenovate       = "renovate"
	fieldExtractVersion = "extract-version"
	fieldVersioning     = "versioning"
	fieldVersion        = "version"
	fieldURL            = "url"
	fieldVerify         = "verify"
	fieldDigest         = "digest"
	fieldSHA256         = "sha256"
)

// Verification methods: how a digest is obtained for the artifact at url.
const (
	// VerifyDownload hashes the bytes at url; TLS is the whole guarantee.
	VerifyDownload = "download"
	// VerifyGitHubAttestation downloads the artifact and has `gh attestation
	// verify --owner <owner>` accept it before hashing it.
	VerifyGitHubAttestation = "github-attestation"
	// VerifyGitHubReleaseAsset downloads the artifact and has `gh release
	// verify-asset <tag> --repo <owner/repo>` accept it before hashing it
	// (GitHub's own attestation of an immutable release, which `gh
	// attestation verify` does not see). The tag is ${version} unless a
	// second argument templates it (`v${version}`).
	VerifyGitHubReleaseAsset = "github-release-asset"
	// VerifyCosignSums fetches a SHA256SUMS file and its cosign bundle, has
	// `cosign verify-blob` accept the pair for the given identity and issuer,
	// and takes the artifact's line; the artifact itself is not downloaded.
	VerifyCosignSums = "cosign-sha256sums"
)

// arity is how many arguments a method takes after its name.
type arity struct{ min, max int }

// verifyArity is each method's arity.
//
//nolint:gochecknoglobals // immutable table.
var verifyArity = map[string]arity{
	VerifyDownload:           {0, 0},
	VerifyGitHubAttestation:  {1, 1},
	VerifyGitHubReleaseAsset: {1, 2}, //nolint:mnd // owner/repo, optional tag template.
	VerifyCosignSums:         {4, 4}, //nolint:mnd // sums url, bundle url, identity regexp, issuer.
}

var (
	// ErrSyntax is a file that does not have the shape.
	ErrSyntax = errors.New("pins.yaml: not the pins shape")
	// ErrEntry is an entry with a missing, malformed, or unknown field.
	ErrEntry = errors.New("pins.yaml: invalid entry")
	// ErrNoSuchPin is a name the manifest does not carry.
	ErrNoSuchPin = errors.New("no such pin")
	// ErrNoSuchField is a field `limen pins get` does not serve.
	ErrNoSuchField = errors.New("no such field")
)

var (
	nameRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Entry is one pinned artifact.
type Entry struct {
	Name    string
	Version string
	// Renovate is the datasource and the depName, as the shared preset
	// reads them; ExtractVersion and Versioning are its optional companions.
	Renovate       []string
	ExtractVersion string
	Versioning     string
	// URL is the template, with ${version} and ${major} unexpanded.
	URL string
	// Verify is the method followed by its arguments, templates unexpanded.
	Verify []string
	// DigestVersion is the version the SHA256 was computed for.
	DigestVersion string
	SHA256        string

	// Line indexes into the manifest: the digest's, for the in-place
	// rewrite; the Renovate block's, to hold it to the order the preset reads.
	digestVersionLine  int
	sha256Line         int
	renovateLine       int
	extractVersionLine int
	versioningLine     int
	versionLine        int
}

// Stale reports whether the digest was computed for another version than the
// one pinned now — the state a Renovate bump leaves behind until a refresh.
func (e Entry) Stale() bool { return e.DigestVersion != e.Version }

// ResolvedURL is the url with the version substituted.
func (e Entry) ResolvedURL() string { return e.expand(e.URL) }

// VerifyArgs are the method's arguments with the version substituted.
func (e Entry) VerifyArgs() []string {
	out := make([]string, 0, len(e.Verify)-1)
	for _, arg := range e.Verify[1:] {
		out = append(out, e.expand(arg))
	}

	return out
}

// Method is the verification method's name.
func (e Entry) Method() string { return e.Verify[0] }

// expand substitutes ${version} and ${major} (the version up to its first
// dot, the way kernel.org names its series directories).
func (e Entry) expand(s string) string {
	major, _, _ := strings.Cut(e.Version, ".")

	return strings.NewReplacer("${version}", e.Version, "${major}", major).Replace(s)
}

// Manifest is a parsed pins.yaml: the entries and the lines they came from.
type Manifest struct {
	Entries []Entry
	lines   []string
}

// Parse reads a pins.yaml. Every entry is validated on the way in: a file
// that parses is a file every command can act on.
func Parse(data []byte) (Manifest, error) {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	pass := parser{manifest: Manifest{lines: lines}}

	for index, raw := range lines {
		if err := pass.line(index, raw); err != nil {
			return Manifest{}, err
		}
	}

	if !pass.inPins {
		return Manifest{}, fmt.Errorf("%w: no `pins:` section", ErrSyntax)
	}

	seen := map[string]bool{}

	for _, entry := range pass.manifest.Entries {
		if err := entry.validate(); err != nil {
			return Manifest{}, err
		}

		if err := pass.manifest.blockIsContiguous(entry); err != nil {
			return Manifest{}, err
		}

		if seen[entry.Name] {
			return Manifest{}, fmt.Errorf("%w: %q is declared twice", ErrEntry, entry.Name)
		}

		seen[entry.Name] = true
	}

	return pass.manifest, nil
}

// parser is the state of one pass over the lines: inside `pins:` or not,
// inside an entry's `digest:` mapping or not, and the entry being filled.
type parser struct {
	manifest Manifest
	current  *Entry
	inPins   bool
	inDigest bool
}

// The indentation columns of the shape: entries, their fields, the digest's.
const (
	entryIndent  = 2
	fieldIndent  = 4
	digestIndent = 6
)

// line consumes one line of the file.
func (p *parser) line(index int, raw string) error {
	line := strings.TrimRight(raw, " \t")
	trimmed := strings.TrimSpace(line)

	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}

	indent := len(line) - len(strings.TrimLeft(line, " "))

	switch {
	case indent == 0:
		return p.top(index, trimmed)
	case !p.inPins:
		return fmt.Errorf("%w: line %d: content before `pins:`", ErrSyntax, index+1)
	case indent == entryIndent && strings.HasPrefix(trimmed, "- "):
		p.manifest.Entries = append(p.manifest.Entries, newEntry())
		p.current = &p.manifest.Entries[len(p.manifest.Entries)-1]
		p.inDigest = false

		return setField(p.current, strings.TrimPrefix(trimmed, "- "), index)
	case p.current == nil:
		return fmt.Errorf("%w: line %d: field outside an entry", ErrSyntax, index+1)
	case indent == fieldIndent:
		p.inDigest = trimmed == fieldDigest+":"
		if p.inDigest {
			return nil
		}

		return setField(p.current, trimmed, index)
	case indent == digestIndent && p.inDigest:
		return setDigestField(p.current, trimmed, index)
	default:
		return fmt.Errorf("%w: line %d: unexpected indentation", ErrSyntax, index+1)
	}
}

// top consumes a line at column zero: only `pins:`, once.
func (p *parser) top(index int, trimmed string) error {
	if trimmed != "pins:" {
		return fmt.Errorf("%w: line %d: only a top-level `pins:` is allowed, got %q",
			ErrSyntax, index+1, trimmed)
	}

	if p.inPins {
		return fmt.Errorf("%w: line %d: `pins:` declared twice", ErrSyntax, index+1)
	}

	p.inPins = true

	return nil
}

// newEntry is an entry with every line index unset.
func newEntry() Entry {
	return Entry{
		digestVersionLine: -1, sha256Line: -1,
		renovateLine: -1, extractVersionLine: -1, versioningLine: -1, versionLine: -1,
	}
}

// setField records one `key: value` line of the entry.
func setField(entry *Entry, field string, line int) error {
	key, value, err := splitField(field, line)
	if err != nil {
		return err
	}

	switch key {
	case fieldName:
		entry.Name = value
	case fieldRenovate:
		entry.Renovate = strings.Fields(value)
		entry.renovateLine = line
	case fieldExtractVersion:
		entry.ExtractVersion = value
		entry.extractVersionLine = line
	case fieldVersioning:
		entry.Versioning = value
		entry.versioningLine = line
	case fieldVersion:
		entry.Version = value
		entry.versionLine = line
	case fieldURL:
		entry.URL = value
	case fieldVerify:
		entry.Verify = strings.Fields(value)
	default:
		return fmt.Errorf("%w: line %d: unknown field %q", ErrEntry, line+1, key)
	}

	return nil
}

// setDigestField records one line of the digest mapping.
func setDigestField(entry *Entry, field string, line int) error {
	key, value, err := splitField(field, line)
	if err != nil {
		return err
	}

	switch key {
	case fieldVersion:
		entry.DigestVersion = value
		entry.digestVersionLine = line
	case fieldSHA256:
		entry.SHA256 = value
		entry.sha256Line = line
	default:
		return fmt.Errorf("%w: line %d: unknown digest field %q", ErrEntry, line+1, key)
	}

	return nil
}

// splitField splits `key: value`, dropping a trailing comment.
func splitField(field string, line int) (key, value string, err error) {
	key, value, found := strings.Cut(field, ":")
	if !found || strings.TrimSpace(key) == "" {
		return "", "", fmt.Errorf("%w: line %d: expected `key: value`, got %q", ErrSyntax, line+1, field)
	}

	value = strings.TrimSpace(value)
	if hash := strings.Index(value, " #"); hash >= 0 {
		value = strings.TrimSpace(value[:hash])
	}

	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') &&
		value[len(value)-1] == value[0] { //nolint:mnd // a quoted scalar.
		value = value[1 : len(value)-1]
	}

	return strings.TrimSpace(key), value, nil
}

// validate is the entry's shape: every field present, well-formed, and the
// verification method known with its arity.
func (e Entry) validate() error {
	label := e.Name
	if label == "" {
		label = "(unnamed)"
	}

	switch {
	case !nameRE.MatchString(e.Name):
		return fmt.Errorf("%w: %s: name must match %s", ErrEntry, label, nameRE)
	case e.Version == "":
		return fmt.Errorf("%w: %s: no version", ErrEntry, label)
	case len(e.Renovate) != 2: //nolint:mnd // datasource, depName.
		return fmt.Errorf("%w: %s: renovate must be `<datasource> <depName>`"+
			" (extract-version and versioning are their own lines)", ErrEntry, label)
	case e.URL == "":
		return fmt.Errorf("%w: %s: no url", ErrEntry, label)
	case len(e.Verify) == 0:
		return fmt.Errorf("%w: %s: no verify method (one of %s)", ErrEntry, label, strings.Join(Methods(), ", "))
	case e.digestVersionLine < 0 || e.sha256Line < 0:
		return fmt.Errorf("%w: %s: digest needs both version and sha256 (run `limen pins refresh` to fill them)",
			ErrEntry, label)
	case !sha256RE.MatchString(e.SHA256):
		return fmt.Errorf("%w: %s: sha256 must be 64 hex characters", ErrEntry, label)
	}

	want, known := verifyArity[e.Verify[0]]
	if !known {
		return fmt.Errorf("%w: %s: unknown verify method %q (one of %s)",
			ErrEntry, label, e.Verify[0], strings.Join(Methods(), ", "))
	}

	if got := len(e.Verify) - 1; got < want.min || got > want.max {
		return fmt.Errorf("%w: %s: verify %s takes %d to %d argument(s), got %d",
			ErrEntry, label, e.Verify[0], want.min, want.max, got)
	}

	return e.validateOrder()
}

// validateOrder holds the Renovate block to the order the preset's regex
// reads: renovate, then extract-version and versioning when present, then
// version, with nothing but blank and comment lines between. An entry
// written in another order parses, and Renovate never moves it.
func (e Entry) validateOrder() error {
	order := []int{e.renovateLine, e.extractVersionLine, e.versioningLine, e.versionLine}

	previous := -1

	for _, line := range order {
		if line < 0 {
			continue
		}

		if line < previous {
			return fmt.Errorf("%w: %s: renovate, extract-version, versioning and version must come in that order,"+
				" one after the other", ErrEntry, e.Name)
		}

		previous = line
	}

	return nil
}

// Methods lists the verification methods, sorted.
func Methods() []string {
	methods := make([]string, 0, len(verifyArity))
	for method := range verifyArity {
		methods = append(methods, method)
	}

	slices.Sort(methods)

	return methods
}

// Get serves one field of one pin to a build: version, url (resolved), sha256.
func (m Manifest) Get(name, field string) (string, error) {
	for _, entry := range m.Entries {
		if entry.Name != name {
			continue
		}

		switch field {
		case fieldVersion:
			return entry.Version, nil
		case fieldURL:
			return entry.ResolvedURL(), nil
		case fieldSHA256:
			return entry.SHA256, nil
		default:
			return "", fmt.Errorf("%w: %q (one of version, url, sha256)", ErrNoSuchField, field)
		}
	}

	return "", fmt.Errorf("%w: %q", ErrNoSuchPin, name)
}

// Stale lists the entries whose digest was computed for another version.
func (m Manifest) Stale() []string {
	var names []string

	for _, entry := range m.Entries {
		if entry.Stale() {
			names = append(names, entry.Name)
		}
	}

	return names
}

// Text is the manifest as a file.
func (m Manifest) Text() string { return strings.Join(m.lines, "\n") + "\n" }

// String names the manifest's entries, for messages.
func (m Manifest) String() string { return strconv.Itoa(len(m.Entries)) + " pin(s)" }

// withDigest returns the manifest text with one entry's digest rewritten in
// place: the two lines change, everything else is byte for byte the file
// the project wrote.
func (m Manifest) withDigest(index int, sha256 string) Manifest {
	entry := m.Entries[index]
	lines := slices.Clone(m.lines)

	lines[entry.digestVersionLine] = rewriteScalar(lines[entry.digestVersionLine], entry.Version)
	lines[entry.sha256Line] = rewriteScalar(lines[entry.sha256Line], sha256)

	out := Manifest{lines: lines, Entries: slices.Clone(m.Entries)}
	out.Entries[index].DigestVersion = entry.Version
	out.Entries[index].SHA256 = sha256

	return out
}

// rewriteScalar replaces the value of a `key: value` line, keeping its
// indentation and any trailing comment.
func rewriteScalar(line, value string) string {
	key, rest, _ := strings.Cut(line, ":")

	comment := ""
	if hash := strings.Index(rest, " #"); hash >= 0 {
		comment = rest[hash:]
	}

	return key + ": " + value + comment
}

// blockIsContiguous rejects a line between the Renovate block's first and
// last lines that is not one of the block's own, blank, or a comment — the
// preset's regex would stop matching there.
func (m Manifest) blockIsContiguous(entry Entry) error {
	own := map[int]bool{entry.renovateLine: true, entry.extractVersionLine: true, entry.versioningLine: true}

	for line := entry.renovateLine + 1; line < entry.versionLine; line++ {
		trimmed := strings.TrimSpace(m.lines[line])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || own[line] {
			continue
		}

		return fmt.Errorf("%w: %s: %q sits between the renovate line and the version line;"+
			" only extract-version and versioning may", ErrEntry, entry.Name, trimmed)
	}

	return nil
}

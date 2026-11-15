// Package signers reads and rewrites an OpenSSH allowed-signers file, the
// form .lint-signers has (ssh-keygen(1), ALLOWED SIGNERS): who may sign
// commits, each key with the window it is valid in.
//
// A rotation is two edits that commute and delete nothing: the successor key
// is added with a valid-after, the retired key gets a valid-before. Commits
// the old key signed inside its window keep verifying; one it signs after is
// refused. Add and Retire are those two edits, and String writes the file
// the same way whatever order they came in.
package signers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// File is the allowed-signers file of a repository, at its root.
const File = ".lint-signers"

var (
	// ErrInvalid is a file, or a line of it, that is not allowed-signers
	// text this package can rewrite without losing something.
	ErrInvalid = errors.New("invalid allowed-signers file")
	// ErrArgument is a principal, a key, a fingerprint or a time the caller
	// gave that is not one.
	ErrArgument = errors.New("invalid argument")
	// ErrConflict is an edit the file's content refuses: the key is already
	// listed otherwise, or already retired at another time.
	ErrConflict = errors.New("conflicts with the file")
)

// The options this package reads and writes, in the order it writes them.
const (
	optionNamespaces    = "namespaces"
	optionCertAuthority = "cert-authority"
	optionValidAfter    = "valid-after"
	optionValidBefore   = "valid-before"

	// gitNamespace is the signature namespace git signs commits and tags in.
	gitNamespace = "git"
)

// An Entry is one line: the principals a key signs as, the key, and what
// bounds its use.
type Entry struct {
	// Principals is the comma-separated pattern list, as written.
	Principals string
	// Namespaces is the namespaces option's value, empty when absent.
	Namespaces string
	// ValidAfter and ValidBefore bound the signatures the key verifies, in
	// the file's own form (YYYYMMDD[HHMM[SS]][Z]); empty when absent.
	ValidAfter, ValidBefore string
	// CertAuthority marks the key as a certificate authority's.
	CertAuthority bool
	// KeyType and Key are the public key: its algorithm and its base64 blob.
	KeyType, Key string
	// Comment is what follows the key on the line, if anything.
	Comment string
}

// Fingerprint is the key's SHA256 fingerprint, as ssh-keygen -l prints it.
func (e Entry) Fingerprint() string {
	blob, err := base64.StdEncoding.DecodeString(e.Key)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(blob)

	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// line is the entry as the file carries it.
func (e Entry) line() string {
	var options []string

	if e.Namespaces != "" {
		options = append(options, optionNamespaces+`="`+e.Namespaces+`"`)
	}

	if e.CertAuthority {
		options = append(options, optionCertAuthority)
	}

	if e.ValidAfter != "" {
		options = append(options, optionValidAfter+`="`+e.ValidAfter+`"`)
	}

	if e.ValidBefore != "" {
		options = append(options, optionValidBefore+`="`+e.ValidBefore+`"`)
	}

	fields := []string{e.Principals}
	if len(options) > 0 {
		fields = append(fields, strings.Join(options, ","))
	}

	fields = append(fields, e.KeyType, e.Key)
	if e.Comment != "" {
		fields = append(fields, e.Comment)
	}

	return strings.Join(fields, " ")
}

// A List is an allowed-signers file: the comment lines it opens with, and
// its entries.
type List struct {
	// Header is the comment and empty lines before the first entry, kept as
	// they are.
	Header  []string
	Entries []Entry
}

// Parse reads allowed-signers text. Comment and empty lines are kept when
// they come before the first entry and refused after it: String writes the
// entries sorted, and a comment between two of them would lose its place.
func Parse(text string) (List, error) {
	var list List

	if text == "" {
		return list, nil
	}

	for number, raw := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		line := strings.TrimSpace(raw)

		switch {
		case line == "" && len(list.Entries) > 0:
		case line == "" || strings.HasPrefix(line, "#"):
			if len(list.Entries) > 0 {
				return List{}, fmt.Errorf("%w: line %d: a comment after the first entry", ErrInvalid, number+1)
			}

			list.Header = append(list.Header, raw)
		default:
			entry, err := parseEntry(line)
			if err != nil {
				return List{}, fmt.Errorf("%w: line %d: %w", ErrInvalid, number+1, err)
			}

			list.Entries = append(list.Entries, entry)
		}
	}

	return list, nil
}

// errLine is what is wrong with one line, wrapped by Parse with its number.
var errLine = errors.New("not an allowed-signers line")

// parseEntry reads one line: principals, an optional options field, the key
// type, the key, and an optional comment.
func parseEntry(line string) (Entry, error) {
	principals, rest, found := strings.Cut(line, " ")
	if !found {
		return Entry{}, fmt.Errorf("%w: no key", errLine)
	}

	entry := Entry{Principals: principals}
	rest = strings.TrimSpace(rest)

	if !isKeyType(firstField(rest)) {
		options, after, err := cutOptions(rest)
		if err != nil {
			return Entry{}, err
		}

		if err := setOptions(&entry, options); err != nil {
			return Entry{}, err
		}

		rest = after
	}

	const withKey, withComment = 2, 3

	fields := strings.SplitN(rest, " ", withComment)

	if len(fields) < withKey || !isKeyType(fields[0]) {
		return Entry{}, fmt.Errorf("%w: no key of a known type", errLine)
	}

	entry.KeyType, entry.Key = fields[0], fields[1]

	if len(fields) > withKey {
		entry.Comment = strings.TrimSpace(fields[2])
	}

	if err := checkKey(entry.KeyType, entry.Key); err != nil {
		return Entry{}, err
	}

	return entry, nil
}

func firstField(text string) string {
	field, _, _ := strings.Cut(text, " ")

	return field
}

// cutOptions splits the options field off the front of text. An option's
// value is quoted and may hold a space, so the field ends at the first space
// outside quotes.
func cutOptions(text string) (options []string, rest string, err error) {
	quoted := false
	start := 0

	for index, char := range text {
		switch {
		case char == '"':
			quoted = !quoted
		case char == ',' && !quoted:
			options = append(options, text[start:index])
			start = index + 1
		case char == ' ' && !quoted:
			return append(options, text[start:index]), strings.TrimSpace(text[index:]), nil
		}
	}

	return nil, "", fmt.Errorf("%w: options with no key after them", errLine)
}

// setOptions reads the options this package knows into the entry and refuses
// any other: a line rewritten without one would lose it.
func setOptions(entry *Entry, options []string) error {
	for _, option := range options {
		name, value, hasValue := strings.Cut(option, "=")
		value = strings.Trim(value, `"`)

		switch {
		case name == optionCertAuthority && !hasValue:
			entry.CertAuthority = true
		case name == optionNamespaces && value != "":
			entry.Namespaces = value
		case name == optionValidAfter && isTime(value):
			entry.ValidAfter = value
		case name == optionValidBefore && isTime(value):
			entry.ValidBefore = value
		default:
			return fmt.Errorf("%w: option %q", errLine, option)
		}
	}

	return nil
}

// keyTypes are the public key algorithms OpenSSH writes in a key line.
func keyTypes() []string {
	return []string{
		"ssh-ed25519", "sk-ssh-ed25519@openssh.com",
		"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"sk-ecdsa-sha2-nistp256@openssh.com",
		"ssh-rsa", "ssh-dss",
	}
}

func isKeyType(text string) bool { return slices.Contains(keyTypes(), text) }

// checkKey holds a key to what its line says it is: base64, and a blob that
// opens with the algorithm's own name, as every OpenSSH public key does. A
// key cut short or pasted under another type fails here.
func checkKey(keyType, key string) error {
	blob, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("%w: the key is not base64", errLine)
	}

	const lengthPrefix = 4

	if len(blob) < lengthPrefix {
		return fmt.Errorf("%w: the key is too short", errLine)
	}

	// The blob opens with its own type; a blob that ends there holds no key.
	rest := blob[lengthPrefix:]

	length := uint64(binary.BigEndian.Uint32(blob[:lengthPrefix]))
	if length >= uint64(len(rest)) || string(rest[:length]) != keyType {
		return fmt.Errorf("%w: the key is not a %s key", errLine, keyType)
	}

	return nil
}

// isTime reports whether text is a time as the file writes one:
// YYYYMMDD, YYYYMMDDHHMM or YYYYMMDDHHMMSS, then an optional Z.
func isTime(text string) bool {
	digits := strings.TrimSuffix(text, "Z")

	const date, minutes, seconds = 8, 12, 14

	if len(digits) != date && len(digits) != minutes && len(digits) != seconds {
		return false
	}

	for _, char := range digits {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

// isUTC reports whether text is a time with the Z that makes it one instant
// everywhere. Without it ssh-keygen reads the time in the local zone of the
// machine that verifies, and one file would mean two windows on two machines.
func isUTC(text string) bool { return isTime(text) && strings.HasSuffix(text, "Z") }

// instant is a time's digits padded to seconds, so two times compare as
// text. Only meaningful between two UTC times.
func instant(text string) string {
	digits := strings.TrimSuffix(text, "Z")

	const seconds = 14

	return digits + strings.Repeat("0", seconds-len(digits))
}

// Add lists a key for a principal, for git's signatures, valid from
// validAfter when one is given. Adding what is already listed that way
// changes nothing. A key the file lists any other way is a conflict, since
// the file never says two things of one key: for another principal, with
// another window, retired, as a certificate authority, or for namespaces
// that leave git out.
func (l *List) Add(principal, keyType, key, validAfter string) (changed bool, err error) {
	switch {
	case principal == "" || strings.ContainsAny(principal, " \t,\"#"):
		return false, fmt.Errorf("%w: %q is not a principal", ErrArgument, principal)
	case !isKeyType(keyType):
		return false, fmt.Errorf("%w: %q is not a key type", ErrArgument, keyType)
	case validAfter != "" && !isUTC(validAfter):
		return false, fmt.Errorf("%w: %q is not a UTC time (YYYYMMDD[HHMM[SS]]Z)", ErrArgument, validAfter)
	}

	if err := checkKey(keyType, key); err != nil {
		return false, fmt.Errorf("%w: %w", ErrArgument, err)
	}

	entry := Entry{
		Principals: principal, Namespaces: gitNamespace, ValidAfter: validAfter, KeyType: keyType, Key: key,
	}

	for _, listed := range l.Entries {
		if listed.Key != key {
			continue
		}

		if listed.Principals == principal && listed.ValidAfter == validAfter && listed.signsForGitNow() {
			return false, nil
		}

		return false, fmt.Errorf("%w: %s is already listed, as %q", ErrConflict, entry.Fingerprint(), listed.line())
	}

	l.Entries = append(l.Entries, entry)

	return true, nil
}

// signsForGitNow reports whether the entry is a key that signs for git with
// no end to its window: no namespaces, which is all of them, or a list that
// names git; not a certificate authority; not retired.
func (e Entry) signsForGitNow() bool {
	forGit := e.Namespaces == "" || slices.Contains(strings.Split(e.Namespaces, ","), gitNamespace)

	return forGit && !e.CertAuthority && e.ValidBefore == ""
}

// Retire ends the window of every entry whose key has the fingerprint, at
// validBefore. Nothing is deleted: signatures made before that instant keep
// verifying. Retiring what is already retired at that time changes nothing;
// another time, or one that is not after the key's valid-after, is a
// conflict, and a conflict on one entry leaves every entry as it was.
func (l *List) Retire(fingerprint, validBefore string) (changed bool, err error) {
	if !isUTC(validBefore) {
		return false, fmt.Errorf("%w: %q is not a UTC time (YYYYMMDD[HHMM[SS]]Z)", ErrArgument, validBefore)
	}

	var found, toRetire []int

	for index, listed := range l.Entries {
		if listed.Fingerprint() != fingerprint {
			continue
		}

		found = append(found, index)

		switch {
		case listed.ValidBefore == validBefore:
		case listed.ValidBefore != "":
			return false, fmt.Errorf("%w: %s is already retired, at %s", ErrConflict, fingerprint, listed.ValidBefore)
		case isUTC(listed.ValidAfter) && instant(validBefore) <= instant(listed.ValidAfter):
			return false, fmt.Errorf("%w: %s is valid from %s, which is not before %s",
				ErrConflict, fingerprint, listed.ValidAfter, validBefore)
		default:
			toRetire = append(toRetire, index)
		}
	}

	if len(found) == 0 {
		return false, fmt.Errorf("%w: no key with the fingerprint %s", ErrArgument, fingerprint)
	}

	// Only now: every entry of the key was judged before one is changed.
	for _, index := range toRetire {
		l.Entries[index].ValidBefore = validBefore
	}

	return len(toRetire) > 0, nil
}

// String is the file: the header as it was, then the entries, sorted by
// principal, then by the start of their window with an unbounded one first,
// then by key, so that the same edits give the same bytes in any order.
func (l *List) String() string {
	entries := slices.Clone(l.Entries)

	slices.SortStableFunc(entries, func(left, right Entry) int {
		return strings.Compare(sortKey(left), sortKey(right))
	})

	lines := slices.Clone(l.Header)
	for _, entry := range entries {
		lines = append(lines, entry.line())
	}

	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n") + "\n"
}

func sortKey(entry Entry) string {
	return strings.Join([]string{entry.Principals, instant(entry.ValidAfter), entry.KeyType, entry.Key}, "\x00")
}

// ParsePublicKey reads a public key as ssh-keygen and an agent print one:
// the algorithm, the base64 blob, and a comment that is not kept. Lines that
// are empty or a comment are skipped; the first key is the key.
func ParsePublicKey(text string) (keyType, key string, err error) {
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)

		const withKey = 2

		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		if len(fields) < withKey || !isKeyType(fields[0]) {
			// Never the line itself: a private key file handed over by
			// mistake would be printed.
			return "", "", fmt.Errorf("%w: not a public key: no key of a known type on its first line", ErrArgument)
		}

		if err := checkKey(fields[0], fields[1]); err != nil {
			return "", "", fmt.Errorf("%w: %w", ErrArgument, err)
		}

		return fields[0], fields[1], nil
	}

	return "", "", fmt.Errorf("%w: no public key", ErrArgument)
}

package signers_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/signers"
)

// Two throwaway keys, with the fingerprints `ssh-keygen -l` printed for
// them: an Ed25519 one and an ECDSA P-256 one.
const (
	oldType        = "ssh-ed25519"
	oldKey         = "AAAAC3NzaC1lZDI1NTE5AAAAIB793rYmwsRpJZmlvburK1y0gqztdGKJ3QMXt+qqhfo5"
	oldFingerprint = "SHA256:Lk8CUMm1S/AIlQwdhZgS8PwyB2SY2oGy/3dxaaN6o9g"

	newType = "ecdsa-sha2-nistp256"
	newKey  = "AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBE/+VTz/dLOUemALuRJcWmqCqGRIHLbCy9KUF1uuDzbbW" +
		"Ii31zvjJXvSJhruim23WK+sdbDRklbVBCdgRcf3m6Q="
	newFingerprint = "SHA256:ytXZEgTXAggAV4/WZHZQM99BC/bTzSU/VgASzPhelr0"

	bot   = "bot@example.test"
	human = "human@example.test"

	created = "20261013055404Z"
	retired = "202610130600Z"
)

func mustParse(t *testing.T, text string) signers.List {
	t.Helper()

	list, err := signers.Parse(text)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}

	return list
}

// TestFingerprintIsSSHKeygens: the fingerprint is the one ssh-keygen prints,
// which is what a person has in hand when a key is to be retired.
func TestFingerprintIsSSHKeygens(t *testing.T) {
	t.Parallel()

	for want, entry := range map[string]signers.Entry{
		oldFingerprint: {KeyType: oldType, Key: oldKey},
		newFingerprint: {KeyType: newType, Key: newKey},
	} {
		if got := entry.Fingerprint(); got != want {
			t.Errorf("%s: fingerprint %s, want %s", entry.KeyType, got, want)
		}
	}
}

// TestRotationIsTwoEditsInAnyOrder: adding the successor and retiring the
// old key give the same file whichever comes first, nothing is deleted, and
// each edit done twice changes nothing the second time.
func TestRotationIsTwoEditsInAnyOrder(t *testing.T) {
	t.Parallel()

	start := bot + ` namespaces="git" ` + oldType + " " + oldKey + " the first key\n"

	want := bot + ` namespaces="git",valid-before="` + retired + `" ` + oldType + " " + oldKey + " the first key\n" +
		bot + ` namespaces="git",valid-after="` + created + `" ` + newType + " " + newKey + "\n"

	add := func(list *signers.List) (bool, error) { return list.Add(bot, newType, newKey, created) }
	retire := func(list *signers.List) (bool, error) { return list.Retire(oldFingerprint, retired) }

	for name, edits := range map[string][]func(*signers.List) (bool, error){
		"add then retire": {add, retire},
		"retire then add": {retire, add},
	} {
		list := mustParse(t, start)

		for _, edit := range edits {
			if changed, err := edit(&list); err != nil || !changed {
				t.Fatalf("%s: changed=%v err=%v, want a change", name, changed, err)
			}
		}

		if got := list.String(); got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}

		for _, edit := range edits {
			if changed, err := edit(&list); err != nil || changed {
				t.Errorf("%s, the edit again: changed=%v err=%v, want nothing", name, changed, err)
			}
		}

		if got := list.String(); got != want {
			t.Errorf("%s, after the edits again:\n%s", name, got)
		}
	}
}

// TestFileIsWrittenSorted: by principal, then by the start of the window
// with a key valid from the beginning first, whatever order the entries
// arrived in, by an edit or in the file read.
func TestFileIsWrittenSorted(t *testing.T) {
	t.Parallel()

	first := bot + ` namespaces="git",valid-before="` + retired + `" ` + oldType + " " + oldKey + "\n"
	second := bot + ` namespaces="git",valid-after="` + created + `" ` + newType + " " + newKey + "\n"
	third := human + ` namespaces="git" ` + oldType + " " + oldKey + "\n"

	for _, text := range []string{third + second + first, second + third + first, first + second + third} {
		list := mustParse(t, text)
		if got := list.String(); got != first+second+third {
			t.Errorf("read as:\n%s\nwritten as:\n%s", text, got)
		}
	}

	var forward, backward signers.List

	if _, err := forward.Add(bot, oldType, oldKey, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := forward.Add(human, newType, newKey, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := backward.Add(human, newType, newKey, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := backward.Add(bot, oldType, oldKey, ""); err != nil {
		t.Fatal(err)
	}

	if forward.String() != backward.String() || !strings.HasPrefix(forward.String(), bot) {
		t.Errorf("two orders of the same adds:\n%s\nand:\n%s", forward.String(), backward.String())
	}
}

// TestAddRefuses: what is not a principal, a key or a UTC time is the
// caller's error; a key the file already lists otherwise is a conflict.
func TestAddRefuses(t *testing.T) {
	t.Parallel()

	listed := bot + ` namespaces="git",valid-after="` + created + `" ` + oldType + " " + oldKey + "\n"

	tests := []struct {
		name                           string
		principal, keyType, key, after string
		want                           error
	}{
		{"no principal", "", oldType, oldKey, "", signers.ErrArgument},
		{"a principal with a space", "a b", oldType, oldKey, "", signers.ErrArgument},
		{"two principals", "a@x,b@x", oldType, oldKey, "", signers.ErrArgument},
		{"an unknown key type", bot, "ssh-ed448", oldKey, "", signers.ErrArgument},
		{"a key that is not base64", bot, oldType, "not base64!", "", signers.ErrArgument},
		{"a key of another type", bot, oldType, newKey, "", signers.ErrArgument},
		{"a key cut short", bot, oldType, oldKey[:20], "", signers.ErrArgument},
		{"a local time", bot, newType, newKey, "20261013055404", signers.ErrArgument},
		{"a time that is not one", bot, newType, newKey, "2026-10-13Z", signers.ErrArgument},
		{"the same key from another time", bot, oldType, oldKey, "20270101Z", signers.ErrConflict},
		{"the same key with no time", bot, oldType, oldKey, "", signers.ErrConflict},
		{"the same key for another principal", human, oldType, oldKey, created, signers.ErrConflict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			list := mustParse(t, listed)

			changed, err := list.Add(tc.principal, tc.keyType, tc.key, tc.after)
			if !errors.Is(err, tc.want) || changed || list.String() != listed {
				t.Errorf("changed=%v err=%v, want %v and the file as it was", changed, err, tc.want)
			}
		})
	}
}

// TestAddIsAlreadyListedOnlyWhenTheKeySignsForGit: the same principal, key
// and start are "already listed" when the line signs a commit today, and a
// conflict when it does not: the answer "already listed" is a promise that
// nothing is left to do.
func TestAddIsAlreadyListedOnlyWhenTheKeySignsForGit(t *testing.T) {
	t.Parallel()

	key := " " + oldType + " " + oldKey + "\n"

	for options, want := range map[string]error{
		"":                          nil,
		` namespaces="git"`:         nil,
		` namespaces="file,git"`:    nil,
		` namespaces="file"`:        signers.ErrConflict,
		` namespaces="gitea"`:       signers.ErrConflict,
		" cert-authority":           signers.ErrConflict,
		` valid-before="20260101Z"`: signers.ErrConflict,
		` namespaces="git",valid-before="20260101Z"`: signers.ErrConflict,
	} {
		listed := bot + options + key
		list := mustParse(t, listed)

		changed, err := list.Add(bot, oldType, oldKey, "")
		if !errors.Is(err, want) || (want == nil && err != nil) || changed || list.String() != listed {
			t.Errorf("listed as %q: changed=%v err=%v, want %v and the file as it was", bot+options, changed, err, want)
		}
	}
}

// TestRetireEndsEveryLineOfTheKey: a key the file lists on two lines is
// retired on both, and when one of them refuses the time neither is changed.
func TestRetireEndsEveryLineOfTheKey(t *testing.T) {
	t.Parallel()

	key := oldType + " " + oldKey + "\n"
	list := mustParse(t, bot+` namespaces="git" `+key+human+` namespaces="git" `+key)

	changed, err := list.Retire(oldFingerprint, retired)
	want := bot + ` namespaces="git",valid-before="` + retired + `" ` + key +
		human + ` namespaces="git",valid-before="` + retired + `" ` + key

	if err != nil || !changed || list.String() != want {
		t.Errorf("changed=%v err=%v:\n%s\nwant:\n%s", changed, err, list.String(), want)
	}

	half := bot + ` namespaces="git" ` + key + human + ` namespaces="git",valid-before="20250101Z" ` + key
	list = mustParse(t, half)

	changed, err = list.Retire(oldFingerprint, retired)
	if !errors.Is(err, signers.ErrConflict) || changed || list.String() != half {
		t.Errorf("one line already retired at another time: changed=%v err=%v:\n%s", changed, err, list.String())
	}
}

// TestRetireRefuses: a fingerprint the file does not have and a time that is
// not UTC are the caller's error; a key already retired at another time, or
// retired at or before the time it is valid from, is a conflict.
func TestRetireRefuses(t *testing.T) {
	t.Parallel()

	listed := bot + ` namespaces="git",valid-before="` + retired + `" ` + oldType + " " + oldKey + "\n" +
		bot + ` namespaces="git",valid-after="` + created + `" ` + newType + " " + newKey + "\n"

	tests := []struct {
		name                string
		fingerprint, before string
		want                error
	}{
		{
			"an unknown fingerprint",
			"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"20270101Z",
			signers.ErrArgument,
		},
		{"a local time", newFingerprint, "20270101", signers.ErrArgument},
		{"already retired at another time", oldFingerprint, "20270101Z", signers.ErrConflict},
		{"at the time it is valid from", newFingerprint, created, signers.ErrConflict},
		{"before the time it is valid from", newFingerprint, "20260101Z", signers.ErrConflict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			list := mustParse(t, listed)

			changed, err := list.Retire(tc.fingerprint, tc.before)
			if !errors.Is(err, tc.want) || changed || list.String() != listed {
				t.Errorf("changed=%v err=%v, want %v and the file as it was", changed, err, tc.want)
			}
		})
	}
}

// TestRetireNotListed: a fingerprint no entry has is ErrNotListed, and an
// argument still. A time that is not one is refused first, so ErrNotListed
// is never said of it, of an empty file either: what a caller with many
// files reads the two answers for.
func TestRetireNotListed(t *testing.T) {
	t.Parallel()

	const unknown = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	for name, text := range map[string]string{
		"a file that lists other keys": bot + ` namespaces="git" ` + newType + " " + newKey + "\n",
		"an empty file":                "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			list := mustParse(t, text)

			_, err := list.Retire(unknown, "20270101Z")
			if !errors.Is(err, signers.ErrNotListed) || !errors.Is(err, signers.ErrArgument) {
				t.Errorf("err=%v, want ErrNotListed and ErrArgument", err)
			}

			_, err = list.Retire(unknown, "20270101")
			if errors.Is(err, signers.ErrNotListed) || !errors.Is(err, signers.ErrArgument) {
				t.Errorf("a local time: err=%v, want ErrArgument and not ErrNotListed", err)
			}
		})
	}
}

// TestParseKeepsWhatItWillWrite: a line's fields survive a parse and a
// write; a file is refused when rewriting it would lose something.
func TestParseKeepsWhatItWillWrite(t *testing.T) {
	t.Parallel()

	kept := "# who signs here\n\n" +
		"a@x,*@y cert-authority " + oldType + " " + oldKey + "\n" +
		bot + ` namespaces="git,file",valid-after="20260101Z",valid-before="20270101Z" ` + newType + " " + newKey + " a comment, kept\n" +
		"plain@x " + oldType + " " + oldKey + "\n"

	list := mustParse(t, kept)
	if got := list.String(); got != kept {
		t.Errorf("a file written back is not itself:\n%s\nwant:\n%s", got, kept)
	}

	empty := mustParse(t, "")
	if got := empty.String(); got != "" {
		t.Errorf("an empty file written back: %q", got)
	}

	for name, text := range map[string]string{
		"a comment after the first entry":      bot + " " + oldType + " " + oldKey + "\n# later\n",
		"an option this package does not know": bot + ` valid-until="20270101Z" ` + oldType + " " + oldKey + "\n",
		"a time that is not one":               bot + ` valid-after="yesterday" ` + oldType + " " + oldKey + "\n",
		"options and no key":                   bot + ` namespaces="git"` + "\n",
		"no key":                               bot + "\n",
		"an unknown key type":                  bot + " ssh-ed448 " + oldKey + "\n",
		"a key that is not base64":             bot + " " + oldType + " !!!\n",
		"a key of another type":                bot + " " + oldType + " " + newKey + "\n",
	} {
		if _, err := signers.Parse(text); !errors.Is(err, signers.ErrInvalid) {
			t.Errorf("%s: %v, want an invalid file", name, err)
		}
	}
}

// TestThisRepositorysFileParses: the file the commands exist for.
func TestThisRepositorysFileParses(t *testing.T) {
	t.Parallel()

	list, err := signers.Load(filepath.Join("..", "..", signers.File))
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Entries) == 0 {
		t.Fatal("this repository's .lint-signers lists nobody")
	}

	for _, entry := range list.Entries {
		if entry.Namespaces != "git" || entry.Fingerprint() == "" {
			t.Errorf("an entry that is not a git signer with a key: %+v", entry)
		}
	}
}

// TestSaveReplacesTheFileWhole: a saved list is what Load reads back, the
// file is replaced and nothing staged is left beside it; a file that is not
// there loads as an empty list.
func TestSaveReplacesTheFileWhole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, signers.File)

	list, err := signers.Load(path)
	if err != nil || len(list.Entries) != 0 {
		t.Fatalf("a file that is not there: %+v, %v", list, err)
	}

	if _, err = list.Add(bot, oldType, oldKey, ""); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A second name for the file as it is: a rename leaves it the old bytes,
	// a write over the file gives it the new ones.
	witness := filepath.Join(t.TempDir(), "witness")
	if err = os.Link(path, witness); err != nil {
		t.Fatal(err)
	}

	if err = signers.Save(path, list); err != nil {
		t.Fatal(err)
	}

	if old, readErr := os.ReadFile(witness); readErr != nil || string(old) != "stale\n" {
		t.Errorf("the file was written over, not replaced: its other name reads %q (%v)", old, readErr)
	}

	back, err := signers.Load(path)
	if err != nil || back.String() != list.String() {
		t.Errorf("read back %q (%v), want %q", back.String(), err, list.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("the directory holds %d entries after a save (%v), want the file alone", len(entries), err)
	}
}

// TestParsePublicKey: a public key file as ssh-keygen and an agent write
// one, comments and blank lines aside; anything else is the caller's error.
func TestParsePublicKey(t *testing.T) {
	t.Parallel()

	keyType, key, err := signers.ParsePublicKey("# the bot's\n\n" + oldType + " " + oldKey + " bot@host\n")
	if err != nil || keyType != oldType || key != oldKey {
		t.Errorf("got %s %s (%v)", keyType, key, err)
	}

	for name, text := range map[string]string{
		"nothing":               "",
		"a private key's line":  "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		"a type and no key":     oldType + "\n",
		"a key of another type": oldType + " " + newKey + "\n",
	} {
		if _, _, err := signers.ParsePublicKey(text); !errors.Is(err, signers.ErrArgument) {
			t.Errorf("%s: %v, want an argument error", name, err)
		}
	}
}

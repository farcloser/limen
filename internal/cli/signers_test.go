package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sshKeygen is the program that reads the file the signers commands write.
// Nothing else can say a window means what the command was told, so the
// test does not run without it.
func sshKeygen(t *testing.T, stdin []byte, args ...string) (string, error) {
	t.Helper()

	path, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Fatalf("ssh-keygen is not on PATH, and it is what reads an allowed-signers file: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), path, args...)
	cmd.Stdin = bytes.NewReader(stdin)

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// signedWith makes a key pair named name in dir and signs message with it,
// for git: the public key's path, its fingerprint as ssh-keygen prints it,
// and the signature's path.
func signedWith(t *testing.T, dir, name string, message []byte) (public, fingerprint, signature string) {
	t.Helper()

	key := filepath.Join(dir, name)
	if out, err := sshKeygen(t, nil, "-q", "-t", "ed25519", "-N", "", "-C", "", "-f", key); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	listed, err := sshKeygen(t, nil, "-l", "-f", key+".pub")
	if err != nil || len(strings.Fields(listed)) < 2 {
		t.Fatalf("ssh-keygen -l: %v\n%s", err, listed)
	}

	signed := filepath.Join(dir, name+".message")
	if err := os.WriteFile(signed, message, 0o600); err != nil {
		t.Fatal(err)
	}

	if out, err := sshKeygen(t, nil, "-Y", "sign", "-f", key, "-n", "git", signed); err != nil {
		t.Fatalf("ssh-keygen -Y sign: %v\n%s", err, out)
	}

	return key + ".pub", strings.Fields(listed)[1], signed + ".sig"
}

// signers runs `limen signers` on one file and returns the exit code, both
// streams, and the file as it then is.
func signers(t *testing.T, file, command string, args ...string) (code int, said, written string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	full := append([]string{"signers", command, "-file", file}, args...)
	code = run(full, &stdout, &stderr)

	content, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	return code, stdout.String() + stderr.String(), string(content)
}

// TestSignersRotationIsWhatSSHKeygenReads: a rotation done with the two
// commands, each run twice, gives a file under which ssh-keygen accepts
// each key's signature inside that key's window and refuses it outside.
func TestSignersRotationIsWhatSSHKeygenReads(t *testing.T) {
	t.Parallel()

	const (
		principal = "bot@example.test"
		rotation  = "20260601120000Z"
		earlier   = "20260101Z"
		later     = "20270101Z"
	)

	dir := t.TempDir()
	file := filepath.Join(dir, ".lint-signers")
	message := []byte("what was signed\n")

	oldPub, oldPrint, oldSig := signedWith(t, dir, "old", message)
	newPub, newPrint, newSig := signedWith(t, dir, "new", message)

	steps := []struct {
		command     string
		args        []string
		did, repeat string
	}{
		{"add", []string{principal, oldPub}, "added " + oldPrint + " for " + principal, "already listed"},
		{"add", []string{"-valid-after", rotation, principal, newPub}, "added " + newPrint, "already listed"},
		{"retire", []string{"-valid-before", rotation, oldPrint}, "retired " + oldPrint, "already retired"},
	}

	for _, step := range steps {
		code, out, written := signers(t, file, step.command, step.args...)
		if code != 0 || !strings.Contains(out, step.did) {
			t.Fatalf("%s %v: exit %d, %q, want %q", step.command, step.args, code, out, step.did)
		}

		code, out, again := signers(t, file, step.command, step.args...)
		if code != 0 || !strings.Contains(out, step.repeat) || again != written {
			t.Fatalf("%s %v, again: exit %d, %q, file changed=%v", step.command, step.args, code, out, again != written)
		}
	}

	for _, tc := range []struct {
		name, sig, at string
		accepted      bool
	}{
		{"the old key, before the rotation", oldSig, earlier, true},
		{"the old key, after the rotation", oldSig, later, false},
		{"the new key, after the rotation", newSig, later, true},
		{"the new key, before the rotation", newSig, earlier, false},
	} {
		out, err := sshKeygen(t, message,
			"-Y", "verify", "-f", file, "-I", principal, "-n", "git", "-s", tc.sig, "-Overify-time="+tc.at)
		if (err == nil) != tc.accepted {
			t.Errorf("%s: accepted=%v, want %v\n%s", tc.name, err == nil, tc.accepted, out)
		}
	}
}

// TestSignersRefusalsLeaveTheFile: a wrong invocation is exit 2, an edit
// the file contradicts is exit 1, and neither writes.
func TestSignersRefusalsLeaveTheFile(t *testing.T) {
	t.Parallel()

	const principal = "bot@example.test"

	dir := t.TempDir()
	file := filepath.Join(dir, ".lint-signers")
	pub, fingerprint, _ := signedWith(t, dir, "key", []byte("x"))

	if code, out, _ := signers(t, file, "add", "-valid-after", "20260101Z", principal, pub); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	if code, out, _ := signers(t, file, "retire", "-valid-before", "20270101Z", fingerprint); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	_, _, before := signers(t, file, "retire", "-valid-before", "20270101Z", fingerprint)

	for _, tc := range []struct {
		name    string
		command string
		args    []string
		code    int
		says    string
	}{
		{"no public key file", "add", []string{principal}, 2, "Usage: limen signers add"},
		{"no time to retire at", "retire", []string{fingerprint}, 2, "Usage: limen signers retire"},
		{"an unknown flag", "retire", []string{"-at", "20270101Z", fingerprint}, 2, "Usage: limen signers retire"},
		{"a public key file that is not there", "add", []string{principal, pub + ".missing"}, 1, ".missing"},
		{"a private key in its place", "add", []string{principal, strings.TrimSuffix(pub, ".pub")}, 1, "not a public key"},
		{"a local time", "add", []string{"-valid-after", "20260101", "other@example.test", pub}, 1, `"20260101" is not a UTC time`},
		{"the key for another principal", "add", []string{"other@example.test", pub}, 1, principal},
		{"retired at another time", "retire", []string{"-valid-before", "20280101Z", fingerprint}, 1, "20270101Z"},
		{"a fingerprint the file does not list", "retire", []string{"-valid-before", "20270101Z", "SHA256:none"}, 1, "SHA256:none"},
	} {
		code, out, after := signers(t, file, tc.command, tc.args...)
		if strings.Contains(out, "PRIVATE KEY") {
			t.Errorf("%s: a line of the private key is printed: %q", tc.name, out)
		}

		if code != tc.code || !strings.Contains(out, tc.says) || after != before {
			t.Errorf("%s: exit %d, %q; want exit %d naming %q, the file unchanged (changed=%v)",
				tc.name, code, out, tc.code, tc.says, after != before)
		}
	}

	for _, args := range [][]string{{"signers"}, {"signers", "rotate"}} {
		var stdout, stderr bytes.Buffer
		if code := run(
			args,
			&stdout,
			&stderr,
		); code != 2 ||
			!strings.Contains(stderr.String(), "limen signers retire") {
			t.Errorf("%v: exit %d, %q, want the usage and exit 2", args, code, stderr.String())
		}
	}

	// An edit that changes nothing does not write: a file kept by hand in an
	// order of its own keeps it.
	unsorted := filepath.Join(dir, "unsorted")

	public, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}

	keyFields := strings.Fields(string(public))
	byHand := "zed@example.test namespaces=\"git\" " + keyFields[0] + " " + keyFields[1] + "\n" +
		"abe@example.test cert-authority " + keyFields[0] + " " + keyFields[1] + "\n"

	if err = os.WriteFile(unsorted, []byte(byHand), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, out, is := signers(t, unsorted, "add", "zed@example.test", pub); code != 0 ||
		!strings.Contains(out, "already listed") || is != byHand {
		t.Errorf("a key already listed, in a file not sorted: exit %d, %q, changed=%v", code, out, is != byHand)
	}

	// A file the command cannot read whole is refused, never rewritten.
	broken := filepath.Join(dir, "broken")
	if err := os.WriteFile(
		broken,
		[]byte(principal+" ssh-ed25519 AAAA\n# a note the rewrite would lose\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, _, was := signers(t, broken, "retire", "-valid-before", "20270101Z", fingerprint)
	if code, out, is := signers(t, broken, "add", principal, pub); code != 1 || is != was {
		t.Errorf("a file that does not parse: exit %d, %q, changed=%v", code, out, is != was)
	}
}

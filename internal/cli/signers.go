package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/farcloser/limen/internal/signers"
)

// cmdSigners is the allowed-signers subcommand family (internal/signers).
const cmdSigners = "signers"

const (
	signersAddUsage    = "Usage: limen signers add [-file path] [-valid-after time] <principal> <public-key-file>"
	signersRetireUsage = "Usage: limen signers retire [-file path] -valid-before time <fingerprint>"
)

func runSigners(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		signersUsage(stderr)

		return 2
	}

	switch args[0] {
	case "add":
		return runSignersAdd(args[1:], stdout, stderr)
	case "retire":
		return runSignersRetire(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "limen: unknown signers command %q\n\n", args[0])
		signersUsage(stderr)

		return 2
	}
}

func signersUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `Usage:
  limen signers add [-file path] [-valid-after time] <principal> <public-key-file>
      List a key for a principal (an email), for git's signatures, valid
      from the time given when one is.
  limen signers retire [-file path] -valid-before time <fingerprint>
      End the window of the key with that SHA256 fingerprint: signatures it
      made before the time keep verifying, later ones are refused.

.lint-signers, at the repository root, is an OpenSSH allowed-signers file:
who may sign commits here, each key with the window it is valid in. A key
rotation is these two edits, in either order, and deletes nothing. The file
is rewritten whole, its entries sorted, so the same edits give the same file.

A time is UTC, written YYYYMMDD, YYYYMMDDHHMM or YYYYMMDDHHMMSS and then Z.
The Z is required: without it ssh-keygen reads the time in the local zone of
whoever verifies, and one file would mean two windows on two machines.
`)
}

func runSignersAdd(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdSigners+" add", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	file := flagSet.String("file", signers.File, "the allowed-signers file")
	validAfter := flagSet.String("valid-after", "", "the key signs from this time on (UTC, ending in Z)")

	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(stderr, signersAddUsage)

		flagSet.PrintDefaults()
	}

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	const want = 2

	if flagSet.NArg() != want {
		_, _ = fmt.Fprintln(stderr, signersAddUsage)

		return 2
	}

	principal, keyFile := flagSet.Arg(0), flagSet.Arg(1)

	keyText, err := os.ReadFile(keyFile) // #nosec G304 -- the public key file the caller designates.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	keyType, key, err := signers.ParsePublicKey(string(keyText))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "limen: %s: %v\n", keyFile, err)

		return 1
	}

	list, err := signers.Load(*file)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	changed, err := list.Add(principal, keyType, key, *validAfter)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	fingerprint := signers.Entry{KeyType: keyType, Key: key}.Fingerprint()

	return saveSigners(*file, list, changed,
		"added "+fingerprint+" for "+principal, fingerprint+" is already listed for "+principal, stdout, stderr)
}

func runSignersRetire(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdSigners+" retire", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	file := flagSet.String("file", signers.File, "the allowed-signers file")
	validBefore := flagSet.String("valid-before", "", "the key signs nothing from this time on (UTC, ending in Z)")

	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(stderr, signersRetireUsage)

		flagSet.PrintDefaults()
	}

	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	if flagSet.NArg() != 1 || *validBefore == "" {
		_, _ = fmt.Fprintln(stderr, signersRetireUsage)

		return 2
	}

	fingerprint := flagSet.Arg(0)

	list, err := signers.Load(*file)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	changed, err := list.Retire(fingerprint, *validBefore)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	return saveSigners(*file, list, changed,
		"retired "+fingerprint+" at "+*validBefore, fingerprint+" is already retired at "+*validBefore, stdout, stderr)
}

// saveSigners writes the list when the edit changed it, and says which of
// the two happened. An edit that changes nothing leaves the file untouched,
// its order included.
func saveSigners(file string, list signers.List, changed bool, did, already string, stdout, stderr io.Writer) int {
	if !changed {
		_, _ = fmt.Fprintln(stdout, already)

		return 0
	}

	if err := signers.Save(file, list); err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	_, _ = fmt.Fprintln(stdout, did+" in "+file)

	return 0
}

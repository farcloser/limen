package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/farcloser/limen/internal/pins"
)

// cmdPins is the pinned-artifacts subcommand family (internal/pins).
const cmdPins = "pins"

func runPins(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		pinsUsage(stderr)

		return 2
	}

	switch args[0] {
	case "get":
		return runPinsGet(args[1:], stdout, stderr)
	case "refresh":
		return runPinsRefresh(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "limen: unknown pins command %q\n\n", args[0])
		pinsUsage(stderr)

		return 2
	}
}

func pinsUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `Usage:
  limen pins get <name> <field>      Print one field of a pin: version, url (resolved), or sha256
  limen pins refresh [-all] [path]   Recompute the digests whose version moved, through each
                                     entry's verify method, and rewrite pins.yaml in place

pins.yaml, at the repository root, declares the artifacts a build fetches by
hand, pinned by digest: the build reads values back with get, Renovate moves
the version through the shared preset, and refresh brings the digest along —
the checksum workflow runs it on every Renovate branch. "limen check" fails a
digest computed for another version than the one pinned.
`)
}

func runPinsGet(args []string, stdout, stderr io.Writer) int {
	const want = 2

	if len(args) != want {
		_, _ = fmt.Fprintln(stderr, "Usage: limen pins get <name> <field>")

		return 2
	}

	manifest, found, err := pins.Load(".")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	if !found {
		_, _ = fmt.Fprintf(stderr, "limen: no %s in the working directory\n", pins.File)

		return 2
	}

	value, err := manifest.Get(args[0], args[1])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 2
	}

	_, _ = fmt.Fprintln(stdout, value)

	return 0
}

func runPinsRefresh(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet(cmdPins+" refresh", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	all := flagSet.Bool("all", false, "recompute every digest, not only the stale ones")

	flagSet.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: limen pins refresh [-all] [path]")

		flagSet.PrintDefaults()
	}

	flags, positional := splitPathFromFlags(args)

	if err := flagSet.Parse(flags); err != nil {
		return 2
	}

	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "limen: too many paths (got %d)\n", len(positional))

		return 2
	}

	root := "."
	if len(positional) == 1 {
		root = positional[0]
	}

	// Downloads can be large; Ctrl-C ends the run with the file untouched by
	// the entry in flight.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	refresh := pins.Refresh
	if *all {
		refresh = pins.RefreshAll
	}

	changed, err := refresh(ctx, root, stdout)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	if len(changed) == 0 {
		_, _ = fmt.Fprintln(stdout, "every digest current")
	}

	return 0
}

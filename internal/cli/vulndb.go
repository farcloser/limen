package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/farcloser/limen/internal/vulndb"
)

// cmdVulndb fetches the Go vulnerability database for govulncheck's -db.
const cmdVulndb = "vulndb"

func runVulndb(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(cmdVulndb, flag.ContinueOnError)
	flags.SetOutput(stderr)

	source := flags.String("source", vulndb.Source, "the database archive to fetch")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if flags.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "Usage: limen vulndb [-source url] <dir>")

		return 2
	}

	dbURL, err := vulndb.Fetch(ctx, *source, flags.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)

		return 1
	}

	_, _ = fmt.Fprintln(stdout, dbURL)

	return 0
}

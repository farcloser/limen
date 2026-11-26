package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/farcloser/limen/internal/guard"
)

// cmdGuard runs a command under a memory limit a process.
const cmdGuard = "guard"

// mebibyte is the unit of the guard's -memory flag.
const mebibyte = 1 << 20

func runGuard(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(cmdGuard, flag.ContinueOnError)
	flags.SetOutput(stderr)

	memory := flags.Uint64("memory", 0, "the memory one process of the command may hold, in MiB")
	dir := flags.String("dir", "", "the directory the command runs in (default: the current one)")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	command := flags.Args()

	if *memory == 0 || *memory > math.MaxUint64/mebibyte || len(command) == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: limen guard -memory <MiB> [-dir <directory>] -- <command> [args…]")

		return 2
	}

	result, err := guard.Run(ctx, guard.Options{
		Limit:  *memory * mebibyte,
		Dir:    *dir,
		Stdin:  os.Stdin,
		Stdout: stdout,
		Stderr: stderr,
		Report: stderr,
	}, command[0], command[1:]...)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errFormat, err)
	}

	return result.ExitCode
}

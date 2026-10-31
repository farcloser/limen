// ghstub is the fake gh the tests of package github put on PATH. argv mirrors
// the production invocation, `api --method METHOD PATH [--input -]` and
// `secret set NAME --org ORG …`; bodies go to stdout with exit 0, errors to
// stderr with exit 1, as gh's do. The test names the directory of its canned
// responses in LIMEN_TEST_GH_STUB_DIR; every invocation is appended to
// calls.log there, write payloads included. TestMain builds it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const stubEnv = "LIMEN_TEST_GH_STUB_DIR"

// response is one canned answer, the shape audit_test.go's stubResponse
// writes: the body, or the pages of a paginated listing; a verbatim gh
// failure line; a 404; a 500.
type response struct {
	Body     string   `json:"body"`
	Pages    []string `json:"pages,omitempty"`
	Stderr   string   `json:"stderr"`
	NotFound bool     `json:"notFound"`
	Fail     bool     `json:"fail"`
}

// render is the body for the flags gh was invoked with: with --paginate the
// pages, all JSON arrays, are merged into one array; with --paginate --slurp
// they come back as one array of page bodies.
func (r response) render(paginate, slurp bool) string {
	pages := r.Pages
	if len(pages) == 0 {
		pages = []string{r.Body}
	}

	switch {
	case !paginate:
		return pages[0]
	case slurp:
		return "[" + strings.Join(pages, ",") + "]"
	default:
		elements := make([]string, 0, len(pages))

		for _, page := range pages {
			inner := strings.TrimSpace(page)
			inner = strings.TrimSuffix(strings.TrimPrefix(inner, "["), "]")

			if strings.TrimSpace(inner) != "" {
				elements = append(elements, inner)
			}
		}

		return "[" + strings.Join(elements, ",") + "]"
	}
}

func main() {
	os.Exit(run(os.Getenv(stubEnv), os.Args[1:]))
}

func run(dir string, args []string) int {
	// The preflight asks the API nothing, so it needs no responses.
	if len(args) == 1 && args[0] == "--version" {
		_, _ = os.Stdout.WriteString("gh version 0.0.0 (stub)\n")

		return 0
	}

	if dir == "" {
		fmt.Fprintln(os.Stderr, "gh stub: no responses for this test (stubGH not called)")

		return 1
	}

	if len(args) < 4 {
		fmt.Fprintf(os.Stderr, "gh stub: unexpected argv %q\n", args)

		return 1
	}

	if args[0] == "secret" {
		return runSecret(dir, args)
	}

	method, path := args[2], args[3]
	key := method + " " + path

	// Log first, respond second, so an unlisted write is logged too. The
	// response lookup keys on method and path alone; --paginate and --slurp
	// ride along in the log so a test can assert a listing asked for every
	// page.
	paginate := slices.Contains(args, "--paginate")
	slurp := slices.Contains(args, "--slurp")

	logged := key
	if paginate {
		logged += " --paginate"
	}

	if slurp {
		logged += " --slurp"
	}

	withPayload := len(args) >= 6 && args[4] == "--input"
	if err := logCall(dir, logged, withPayload); err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	responses, err := load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	answer, listed := responses[key]

	switch {
	case !listed:
		// A read the test did not list is the unverifiable path; a write it
		// did not list is one the plan never showed, and it fails the same way.
		fmt.Fprintln(os.Stderr, "gh: boom (HTTP 500)")

		return 1
	case answer.NotFound:
		fmt.Fprintln(os.Stderr, "gh: Not Found (HTTP 404)")

		return 1
	case answer.Stderr != "":
		fmt.Fprintln(os.Stderr, answer.Stderr)

		return 1
	case answer.Fail:
		fmt.Fprintln(os.Stderr, "gh: boom (HTTP 500)")

		return 1
	default:
		fmt.Fprint(os.Stdout, answer.render(paginate, slurp))

		return 0
	}
}

// runSecret plays `gh secret set`: keyed on its first five words, the
// secret's value (stdin) logged like a write payload, failing when the test
// listed the key as failing.
func runSecret(dir string, args []string) int {
	const keyWords = 5 // secret set NAME --org ORG

	if len(args) < keyWords {
		fmt.Fprintf(os.Stderr, "gh stub: unexpected secret argv %q\n", args)

		return 1
	}

	key := strings.Join(args[:keyWords], " ")

	if err := logCall(dir, key, true); err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	responses, err := load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh stub: %v\n", err)

		return 1
	}

	if responses[key].Fail {
		fmt.Fprintln(os.Stderr, "gh: secret set refused (HTTP 403)")

		return 1
	}

	return 0
}

// logCall appends the call's key and, when the call carries one, its stdin
// payload on the lines after it, to calls.log.
func logCall(dir, key string, withPayload bool) error {
	const ownerOnly = 0o600

	// #nosec G304 G703 -- the directory is the test's own, named in the environment it set.
	logFile, err := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, ownerOnly)
	if err != nil {
		return fmt.Errorf("opening the call log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	if _, err = fmt.Fprintln(logFile, key); err != nil {
		return fmt.Errorf("logging the call: %w", err)
	}

	if !withPayload {
		return nil
	}

	if _, err = io.Copy(logFile, os.Stdin); err != nil {
		return fmt.Errorf("logging the payload: %w", err)
	}

	if _, err = fmt.Fprintln(logFile); err != nil {
		return fmt.Errorf("logging the payload: %w", err)
	}

	return nil
}

func load(dir string) (map[string]response, error) {
	// #nosec G304 G703 -- the directory is the test's own, named in the environment it set.
	raw, err := os.ReadFile(filepath.Join(dir, "responses.json"))
	if err != nil {
		return nil, fmt.Errorf("reading the responses: %w", err)
	}

	var responses map[string]response
	if err := json.Unmarshal(raw, &responses); err != nil {
		return nil, fmt.Errorf("decoding the responses: %w", err)
	}

	return responses, nil
}

package lintgo

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// The rendered file's header, and how it is written.
const (
	renderedHeader = "# Rendered by limen-lint-go from the baseline it embeds and " + OverlayFile +
		": edit the overlay, never this file.\n"
	renderedIndent  = 2
	dirPermissions  = 0o700
	filePermissions = 0o600
)

// load reads dir's module, the baseline for it and dir's overlay, and applies
// the one to the other.
func load(dir string, raw []byte) (baseline, report, error) {
	module, err := modulePath(dir)
	if err != nil {
		return baseline{}, report{}, err
	}

	base, err := parseBaseline(raw, module)
	if err != nil {
		return baseline{}, report{}, err
	}

	overlay, found, err := readOverlay(dir)
	if err != nil {
		return baseline{}, report{}, err
	}

	applied, err := merge(&base, overlay)
	if err != nil {
		return baseline{}, report{}, err
	}

	applied.found = found

	return base, applied, nil
}

// render writes the golangci-lint configuration for dir's module: to stdout,
// or to the -o file, created with its directory.
func render(args []string, dir string, raw []byte, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet(cmdRender, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	out := set.String(flagOut, "", "write the configuration here instead of stdout")

	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return fmt.Errorf("%w: %s takes -o FILE and nothing else", ErrUsage, cmdRender)
	}

	base, applied, err := load(dir, raw)
	if err != nil {
		return err
	}

	for _, note := range applied.notes {
		fmt.Fprintln(stderr, "limen-lint-go: note: "+note)
	}

	fmt.Fprintln(stderr, "limen-lint-go: "+applied.summary())

	text, err := encode(base.golangci)
	if err != nil {
		return err
	}

	if *out == "" {
		if _, err := stdout.Write(text); err != nil {
			return fmt.Errorf("%w: writing: %w", ErrBaseline, err)
		}

		return nil
	}

	if err := os.MkdirAll(filepath.Dir(*out), dirPermissions); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}

	// The -o argument is the caller's.
	if err := os.WriteFile(*out, text, filePermissions); err != nil { // #nosec G304 -- see above.
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}

	return nil
}

// encode is doc as golangci-lint reads it, behind the header.
func encode(doc document) ([]byte, error) {
	var buf bytes.Buffer

	_, _ = buf.WriteString(renderedHeader)

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(renderedIndent)

	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("%w: encoding: %w", ErrBaseline, err)
	}

	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("%w: encoding: %w", ErrBaseline, err)
	}

	return buf.Bytes(), nil
}

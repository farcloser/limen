package lintgo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The baseline's sections, which an overlay shares: go-lint's own metadata,
// the go-licenses lane, and the golangci-lint configuration.
const (
	keyGoLint       = "lint-go"
	keyGolangciLint = "golangci-lint"
	keyLicenses     = "licenses"
	keyGolangci     = "golangci"
)

// The two placeholders the baseline carries, filled from the module's go.mod:
// the module path, and its first two elements (host and owner), which is what
// gci groups the sibling modules by.
const (
	placeholderModulePath   = "${MODULE_PATH}"
	placeholderModulePrefix = "${MODULE_PREFIX}"
	modulePrefixElements    = 2
	pathSeparator           = "/"
	goModFile               = "go.mod"
	moduleDirective         = "module "
)

// document is a YAML mapping as the parser hands it over.
type document = map[string]any

// baseline is the parsed baseline, its placeholders filled.
type baseline struct {
	// floor is the oldest golangci-lint release every linter name in the
	// golangci section exists in.
	floor    string
	licenses document
	golangci document
}

// parseBaseline reads the baseline with its placeholders filled for module;
// an empty module leaves them in place, which check, needing only the floor,
// relies on.
func parseBaseline(raw []byte, module string) (baseline, error) {
	text := string(raw)
	if module != "" {
		text = strings.ReplaceAll(text, placeholderModulePath, module)
		text = strings.ReplaceAll(text, placeholderModulePrefix, modulePrefix(module))
	}

	var doc document
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return baseline{}, fmt.Errorf("%w: %w", ErrBaseline, err)
	}

	meta, err := section(doc, keyGoLint, ErrBaseline)
	if err != nil {
		return baseline{}, err
	}

	floor, isString := meta[keyGolangciLint].(string)
	if !isString || floor == "" {
		return baseline{}, fmt.Errorf(
			"%w: %s.%s must name the golangci-lint floor", ErrBaseline, keyGoLint, keyGolangciLint,
		)
	}

	licenses, err := section(doc, keyLicenses, ErrBaseline)
	if err != nil {
		return baseline{}, err
	}

	golangci, err := section(doc, keyGolangci, ErrBaseline)
	if err != nil {
		return baseline{}, err
	}

	return baseline{floor: floor, licenses: licenses, golangci: golangci}, nil
}

// section is doc's mapping at key: an empty one when absent, sentinel-wrapped
// when it is there and not a mapping.
func section(doc document, key string, sentinel error) (document, error) {
	value, present := doc[key]
	if !present || value == nil {
		return document{}, nil
	}

	mapping, isMap := value.(document)
	if !isMap {
		return nil, fmt.Errorf("%w: %s must be a mapping", sentinel, key)
	}

	return mapping, nil
}

// modulePrefix is the module path's first two elements (github.com/farcloser
// for github.com/farcloser/limen), or the whole path when it has fewer.
func modulePrefix(module string) string {
	elements := strings.Split(module, pathSeparator)
	if len(elements) <= modulePrefixElements {
		return module
	}

	return strings.Join(elements[:modulePrefixElements], pathSeparator)
}

// modulePath is the module directive of dir's go.mod.
func modulePath(dir string) (string, error) {
	// dir is the caller's working directory or its -C argument.
	data, err := os.ReadFile(filepath.Join(dir, goModFile)) // #nosec G304 -- see above.
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModule, err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if module, found := strings.CutPrefix(line, moduleDirective); found {
			return strings.TrimSpace(module), nil
		}
	}

	return "", fmt.Errorf("%w: no module directive in %s", ErrModule, filepath.Join(dir, goModFile))
}

// readOverlay is dir's .lint-go.yaml: the mapping, whether the file exists,
// and the error when it exists and is not a mapping.
func readOverlay(dir string) (document, bool, error) {
	// dir is the caller's working directory or its -C argument.
	data, err := os.ReadFile(filepath.Join(dir, OverlayFile)) // #nosec G304 -- see above.
	if err != nil {
		if os.IsNotExist(err) {
			return document{}, false, nil
		}

		return nil, false, fmt.Errorf("%w: %w", ErrOverlay, err)
	}

	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, true, fmt.Errorf("%w: %w", ErrOverlay, err)
	}

	if doc == nil {
		doc = document{}
	}

	return doc, true, nil
}

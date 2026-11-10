package limen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGoModuleStatementCopiesAgree: the statement that lists the Go modules a
// lane enters is lib.just's go_modules_init for the commands handed to
// _per-platform and written out in every recipe body, since the shell lane
// lints a body with its interpolations opaque. The copies must be the one
// statement.
func TestGoModuleStatementCopiesAgree(t *testing.T) {
	t.Parallel()

	lib, err := os.ReadFile(filepath.Join(".limen", "just", "lib.just"))
	if err != nil {
		t.Fatal(err)
	}

	match := regexp.MustCompile(`(?m)^go_modules_init := '(.*)'$`).FindStringSubmatch(string(lib))
	if match == nil {
		t.Fatal("lib.just has no go_modules_init")
	}

	want := match[1]
	copies := 0

	files, err := filepath.Glob(filepath.Join(".limen", "just", "*.just"))
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "modules=(.)") || strings.Contains(line, "go_modules_init :=") {
				continue
			}

			copies++

			if got := strings.TrimSpace(line); got != want {
				t.Errorf(
					"%s:%d writes the module statement as\n  %s\nlib.just's go_modules_init is\n  %s",
					file,
					i+1,
					got,
					want,
				)
			}
		}
	}

	if copies == 0 {
		t.Error("no recipe body writes the module statement out")
	}
}

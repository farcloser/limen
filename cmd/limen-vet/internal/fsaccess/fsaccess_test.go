package fsaccess_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/farcloser/limen/cmd/limen-vet/internal/fsaccess"
)

func TestFsaccess(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), fsaccess.Analyzer,
		"example.com/helper", "example.com/lib/internal/files", "example.com/lib", "example.com/cmd/tool")
}

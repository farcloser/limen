// Package buildversion reports build constraints that test the Go release
// (go1.N). A module's go line is the oldest supported release and every build
// uses the one pinned toolchain, so such a term is either always true or
// selects a file no supported build compiles.
package buildversion

import (
	"bytes"
	"fmt"
	"go/build/constraint"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/farcloser/limen/cmd/limen-vet/internal/allow"
)

const name = "buildversion"

// Analyzer is the buildversion analyzer.
var Analyzer = &analysis.Analyzer{ //nolint:gochecknoglobals // the analysis framework's shape: an analyzer is a package-level value.
	Name: name,
	Doc:  "reports build constraints that test the Go release (go1.N)",
	Run:  run,
}

var releaseTag = regexp.MustCompile(`^go1\.\d+$`)

func run(pass *analysis.Pass) (any, error) {
	report := allow.New(pass, name)

	for _, f := range pass.Files {
		file := pass.Fset.File(f.FileStart)

		src, err := pass.ReadFile(file.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file.Name(), err)
		}

		check(report, file, src)
	}

	// The files no platform of this run builds are the ones a go1.N term
	// most often hides; they reach the analyzer only by name.
	for _, filename := range slices.Concat(pass.OtherFiles, pass.IgnoredFiles) {
		src, err := pass.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filename, err)
		}

		file := pass.Fset.AddFile(filename, -1, len(src))
		file.SetLinesForContent(src)
		check(report, file, src)
	}

	return nil, nil //nolint:nilnil // an analyzer without a result: analysis.Analyzer.Run's shape.
}

func check(report *allow.Reporter, file *token.File, src []byte) {
	report.Read(file, src)

	for i, line := range header(src) {
		expr, err := constraint.Parse(line)
		if err != nil {
			continue // not a constraint, or a malformed one vet's buildtag reports
		}

		for _, tag := range releaseTags(expr) {
			report.Reportf(
				file.LineStart(i+1),
				"build constraint tests the Go release (%s): version-conditional files are not allowed",
				tag,
			)
		}
	}
}

// header returns the lines a build constraint may occupy, by index: those
// before the first line that is neither blank nor a line comment.
func header(src []byte) []string {
	var lines []string

	for line := range bytes.SplitSeq(src, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if text != "" && !strings.HasPrefix(text, "//") {
			break
		}

		lines = append(lines, text)
	}

	return lines
}

func releaseTags(expr constraint.Expr) []string {
	switch node := expr.(type) {
	case *constraint.TagExpr:
		if releaseTag.MatchString(node.Tag) {
			return []string{node.Tag}
		}

		return nil
	case *constraint.NotExpr:
		return releaseTags(node.X)
	case *constraint.AndExpr:
		return append(releaseTags(node.X), releaseTags(node.Y)...)
	case *constraint.OrExpr:
		return append(releaseTags(node.X), releaseTags(node.Y)...)
	default:
		return nil
	}
}

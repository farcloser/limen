// Package allow reads the directive that silences one finding of one analyzer,
//
//	//limen:allow <analyzer> -- <reason>
//
// on the line of the finding or on the line above it. The reason is required:
// a directive without one silences nothing and is itself reported.
//
// On the line directly above a package clause, the same directive sanctions
// the whole package for an analyzer that honours it (Sanctions).
package allow

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const prefix = "//limen:allow "

// Reporter reports an analyzer's findings, minus those a directive silences.
type Reporter struct {
	pass     *analysis.Pass
	analyzer string
	allowed  map[*token.File]map[int]bool
}

// New returns a Reporter for the named analyzer.
func New(pass *analysis.Pass, analyzer string) *Reporter {
	return &Reporter{pass: pass, analyzer: analyzer, allowed: map[*token.File]map[int]bool{}}
}

// Read records the directives in file, whose content is src, and reports
// those without a reason. Every file a finding can land in is read first.
func (r *Reporter) Read(file *token.File, src []byte) {
	lines := map[int]bool{}

	for i, line := range bytes.Split(src, []byte("\n")) {
		directive, reasoned := parse(line, r.analyzer)
		if !directive {
			continue
		}

		if !reasoned {
			r.pass.Reportf(
				file.LineStart(i+1),
				"limen:allow %s needs a reason: //limen:allow %s -- <why the code is right>",
				r.analyzer,
				r.analyzer,
			)

			continue
		}

		lines[i+1] = true
	}

	r.allowed[file] = lines
}

// Reportf reports a finding at pos unless a directive silences its line.
func (r *Reporter) Reportf(pos token.Pos, format string, args ...any) {
	if file := r.pass.Fset.File(pos); file != nil {
		line := file.Line(pos)
		if r.allowed[file][line] || r.allowed[file][line-1] {
			return
		}
	}

	r.pass.Reportf(pos, format, args...)
}

// Sanctions reports whether file, whose content is src, carries a reasoned
// directive for analyzer on the line directly above its package clause. The
// directive lives in the package's source, so every analysis that reads the
// package sees it: the package's own and each of its importers'.
func Sanctions(fset *token.FileSet, file *ast.File, src []byte, analyzer string) bool {
	above := fset.PositionFor(file.Package, false).Line - 1 // 1-based, the file's own lines
	if above < 1 {
		return false
	}

	lines := bytes.SplitN(src, []byte("\n"), above+1)
	if len(lines) < above {
		return false
	}

	directive, reasoned := parse(lines[above-1], analyzer)

	return directive && reasoned
}

// parse reports whether line holds a directive for analyzer, and whether it
// gives a reason. A directive is a line comment, alone on its line or
// trailing code.
func parse(line []byte, analyzer string) (directive, reasoned bool) {
	_, text, found := bytes.Cut(line, []byte(prefix))
	if !found {
		return false, false
	}

	before, reason, _ := strings.Cut(string(text), " -- ")
	if fields := strings.Fields(before); len(fields) == 0 || fields[0] != analyzer {
		return false, false
	}

	return true, strings.TrimSpace(reason) != ""
}

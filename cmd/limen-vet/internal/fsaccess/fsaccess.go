// Package fsaccess reports library code that reaches the file system: a call
// to a function that resolves a path (os.Open, os.ReadFile, filepath.Walk…),
// or to any function outside the standard library that reaches one through
// its static calls. A library takes an io.Reader, an io.Writer or an fs.FS
// from its caller; the program decides where bytes come from.
//
// Reach is carried across packages as a fact on each function, through
// static calls only. A call through an interface carries nothing, so a
// library that reads an io.Reader stays clean whatever its caller passes.
// The standard library is not traversed: its functions count only when
// listed, so time.LoadLocation reading zoneinfo or net reading /etc/hosts is
// not a library's choice to touch files.
//
// A package under internal/ is reported only through its importers: nothing
// outside its module can call it, so it is part of a command, or its reach
// surfaces where a public package of the library calls it.
//
// A package whose job is the file system sanctions itself in its source,
//
//	//limen:allow fsaccess -- <reason>
//
// on the line directly above its package clause, in any non-test file. It
// then reports nothing and passes nothing on: calling it is not reported, in
// its own module or in any that imports it.
package fsaccess

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/farcloser/limen/cmd/limen-vet/internal/allow"
)

const name = "fsaccess"

// Analyzer is the fsaccess analyzer.
var Analyzer = &analysis.Analyzer{ //nolint:gochecknoglobals // the analysis framework's shape: an analyzer is a package-level value.
	Name:      name,
	Doc:       "reports library code that reaches the file system instead of taking readers and writers",
	Run:       run,
	FactTypes: []analysis.Fact{new(Reaches)},
}

// Reaches marks a function that reaches the file system through static
// calls; Sink names the listed function it reaches, as pkg.Func.
type Reaches struct {
	Sink string
}

// AFact marks Reaches as an analysis fact.
func (*Reaches) AFact() {}

func (f *Reaches) String() string { return "reaches(" + f.Sink + ")" }

func run(pass *analysis.Pass) (any, error) {
	if standard(pass.Pkg.Path()) || pass.Pkg.Name() == "main" {
		return nil, nil //nolint:nilnil // an analyzer without a result: analysis.Analyzer.Run's shape.
	}

	var files []*ast.File

	srcs := map[*ast.File][]byte{}

	for _, file := range pass.Files {
		filename := pass.Fset.File(file.FileStart).Name()
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}

		src, err := pass.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filename, err)
		}

		// A sanctioned package reports nothing and exports no fact, so a call
		// into it is not reported either, in any module that imports it.
		if allow.Sanctions(pass.Fset, file, src, name) {
			return nil, nil //nolint:nilnil // an analyzer without a result: analysis.Analyzer.Run's shape.
		}

		files = append(files, file)
		srcs[file] = src
	}

	// Under internal/, facts are exported and nothing is reported.
	var report *allow.Reporter
	if !internal(pass.Pkg.Path()) {
		report = allow.New(pass, name)
	}

	// uses maps each function declared here to the functions its body names;
	// a reference counts like a call (a method value, a func passed along).
	uses := map[*types.Func][]*types.Func{}

	for _, file := range files {
		if report != nil {
			report.Read(pass.Fset.File(file.FileStart), srcs[file])
		}

		for _, decl := range file.Decls {
			visit(pass, report, decl, uses)
		}
	}

	exportReach(pass, uses)

	return nil, nil //nolint:nilnil // an analyzer without a result: analysis.Analyzer.Run's shape.
}

// visit records the functions a function declaration names and, unless
// report is nil, reports every one from another package that reaches the
// file system.
func visit(pass *analysis.Pass, report *allow.Reporter, decl ast.Decl, uses map[*types.Func][]*types.Func) {
	var owner *types.Func

	if fd, isFunc := decl.(*ast.FuncDecl); isFunc {
		if def, isDef := pass.TypesInfo.Defs[fd.Name].(*types.Func); isDef {
			owner = def
		}
	}

	ast.Inspect(decl, func(node ast.Node) bool {
		ident, isIdent := node.(*ast.Ident)
		if !isIdent {
			return true
		}

		callee, isFunc := pass.TypesInfo.Uses[ident].(*types.Func)
		if !isFunc {
			return true
		}

		callee = callee.Origin()
		if owner != nil {
			uses[owner] = append(uses[owner], callee)
		}

		// A function of this package is reported at its own sinks, not at
		// every call.
		if report == nil || callee.Pkg() == pass.Pkg {
			return true
		}

		if sink := sinkName(callee); sink != "" {
			report.Reportf(
				ident.Pos(),
				"library code touches the file system: %s (take an io.Reader, io.Writer or fs.FS from the caller)",
				sink,
			)
		} else if fact := new(Reaches); pass.ImportObjectFact(callee, fact) {
			report.Reportf(
				ident.Pos(),
				"library code calls %s, which touches the file system through %s (take an io.Reader, io.Writer or fs.FS from the caller)",
				qualified(callee),
				fact.Sink,
			)
		}

		return true
	})
}

// exportReach exports a Reaches fact on every function declared here that
// reaches a sink: directly, through an imported function's fact, or through
// another function of this package, iterated to a fixed point.
func exportReach(pass *analysis.Pass, uses map[*types.Func][]*types.Func) {
	reach := map[*types.Func]string{}

	// In source order, so the sink a fact names is the same on every run.
	owners := slices.SortedFunc(maps.Keys(uses), func(a, b *types.Func) int { return cmp.Compare(a.Pos(), b.Pos()) })

	for changed := true; changed; {
		changed = false

		for _, owner := range owners {
			if reach[owner] != "" {
				continue
			}

			if sink := firstSink(pass, reach, uses[owner]); sink != "" {
				reach[owner] = sink
				changed = true
			}
		}
	}

	for owner, sink := range reach {
		pass.ExportObjectFact(owner, &Reaches{Sink: sink})
	}
}

// firstSink returns the sink the first of callees reaches, as far as reach
// knows for this package's functions, or "".
func firstSink(pass *analysis.Pass, reach map[*types.Func]string, callees []*types.Func) string {
	for _, callee := range callees {
		if sink := sinkName(callee); sink != "" {
			return sink
		}

		if callee.Pkg() == pass.Pkg {
			if sink := reach[callee]; sink != "" {
				return sink
			}

			continue
		}

		if fact := new(Reaches); pass.ImportObjectFact(callee, fact) {
			return fact.Sink
		}
	}

	return ""
}

func sinkName(callee *types.Func) string {
	if callee.Pkg() == nil || callee.Signature().Recv() != nil {
		return ""
	}

	if sinks[callee.Pkg().Path()][callee.Name()] {
		return callee.Pkg().Name() + "." + callee.Name()
	}

	return ""
}

func qualified(callee *types.Func) string {
	if recv := callee.Signature().Recv(); recv != nil {
		typ := types.TypeString(recv.Type(), (*types.Package).Name)
		if strings.HasPrefix(typ, "*") {
			typ = "(" + typ + ")"
		}

		return typ + "." + callee.Name()
	}

	return callee.Pkg().Name() + "." + callee.Name()
}

// internal reports whether path has an internal element, which only its
// module may import.
func internal(path string) bool {
	return slices.Contains(strings.Split(path, "/"), "internal")
}

// standard reports whether path names a standard-library package: its first
// element has no dot, which every module path outside it does.
func standard(path string) bool {
	first, _, _ := strings.Cut(path, "/")

	return !strings.Contains(first, ".")
}

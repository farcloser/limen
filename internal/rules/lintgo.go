package rules

// The lintgo rule: a Go repository carries the Go lint baseline,
// .limen/lint-go.yaml, content-pinned like the other .limen/ files, and its
// carve-outs from it in a root .lint-go.yaml, seeded once and the project's
// own. The lint recipes have limen-lint-go, the driver shipped in limen's
// release, render the one with the other into the golangci-lint
// configuration they run, under build/, and pass it with -c; a golangci-lint
// configuration at the root is therefore a stray: golangci-lint never reads
// it here, and it misleads whoever does (book/per-language.md, "one
// baseline, per-project carve-outs").
const (
	ruleLintGo     = "lintgo"
	lintGoBaseline = ".limen/lint-go.yaml"
	lintGoOverlay  = ".lint-go.yaml"

	strayGolangciMessage = " is a golangci-lint configuration nothing here reads: the recipes render theirs" +
		" under build/ from " + lintGoBaseline + " and " + lintGoOverlay +
		" — move its carve-outs into " + lintGoOverlay + " and delete it"
)

// strayGolangciConfigs are the file names golangci-lint discovers on its own,
// in its order.
//
//nolint:gochecknoglobals // immutable baseline data.
var strayGolangciConfigs = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// lintGoOverlaySeed is the .lint-go.yaml a repository starts from: the
// vocabulary as commented examples, nothing carved out. Comments only, so
// the parser sees an empty document and limen-lint-go an empty overlay.
const lintGoOverlaySeed = `# This project's carve-outs from the Go lint baseline (` + lintGoBaseline + `):
# the baseline's sections, in its shape, holding only what this project adds,
# changes or takes out. ` + "`just do lint go`" + ` renders the two on every run
# (book/per-language.md, "one baseline, per-project carve-outs"). Seeded once;
# the file is the project's own. Every key the overlay accepts is below, with
# what it does to the baseline; anything else is refused as baseline policy.
#
# golangci:
#   linters:
#     disable:                       # takes a linter out of the baseline's set
#       - zerologlint
#     enable:                        # adds one the baseline turns off
#       - godox
#     exclusions:
#       paths:                       # appends: files golangci-lint skips entirely
#         - third_party/
#       presets:                     # appends golangci-lint's exclusion presets
#         - comments
#       rules:                       # appends: a linter, or a finding text, off under a path
#         - path: testutil/
#           linters: [gosec]
#     settings:                      # per linter, merged key by key into the baseline's:
#       wrapcheck:                   #   a scalar overrides, a mapping recurses, a list appends,
#         ignore-package-globs:      #   a named entry (a revive rule) overrides its namesake
#           - github.com/example/project/*
#   formatters:
#     enable:                        # adds a formatter
#       - goimports
#     exclusions:
#       paths:                       # appends: files the formatters skip
#         - internal/generated/
#     settings:                      # per formatter, merged like the linters'
#       golines:
#         max-len: 100
# licenses:
#   allowed:                         # replaces the baseline's list of accepted dependency licenses
#     - Apache-2.0
#     - MIT
#   ignore:                          # appends modules go-licenses skips (its known false positives)
#     - gotest.tools/v3
# nilaway:
#   blocking: false                  # findings print without failing the lane, while a backlog is worked off
#   exclude-pkgs:                    # appends package prefixes NilAway leaves out
#     - github.com/example/project/internal/generated
#   exclude-errors-in-files:         # appends file prefixes: NilAway has no per-line suppression
#     - internal/legacy/
`

// checkLintGo evaluates the rule; ok=false when the repository is not a Go
// module, so the caller omits the finding.
func checkLintGo(root string) (Finding, bool) {
	if rootGoMod(root) == nil {
		return Finding{}, false
	}

	if f := checkPinned(root, ruleLintGo, lintGoBaseline, CanonicalLintGo); f != nil {
		return *f, true
	}

	if name, found := findFirst(root, strayGolangciConfigs...); found {
		return fail(ruleLintGo, name, name+strayGolangciMessage), true
	}

	message := lintGoOverlay + " carries the carve-outs from the Go lint baseline"
	if _, err := readRepoFile(root, lintGoOverlay); err != nil {
		message = "no " + lintGoOverlay + ": the Go lint baseline as is (limen fix seeds the file)"
	}

	return Finding{
		Rule:    ruleLintGo,
		Status:  StatusOK,
		Path:    lintGoOverlay,
		Message: message,
	}, true
}

// remediateLintGo pins the baseline exactly and seeds the overlay once in a
// Go repository. A stray root configuration is an advisory: its carve-outs
// are a human's to move. Nothing for a repository that is not a Go module.
func remediateLintGo(root string) []Outcome {
	if rootGoMod(root) == nil {
		return nil
	}

	out := []Outcome{
		pinExact(root, ruleLintGo, lintGoBaseline, CanonicalLintGo),
		seedIfMissing(root, ruleLintGo, lintGoOverlay, lintGoOverlaySeed,
			"seeded "+lintGoOverlay+" (the carve-outs are the project's own from here)"),
	}

	if name, found := findFirst(root, strayGolangciConfigs...); found {
		out = append(out, Outcome{
			Rule:    ruleLintGo,
			Action:  ActionAdvisory,
			Path:    name,
			Message: name + strayGolangciMessage,
		})
	}

	return out
}

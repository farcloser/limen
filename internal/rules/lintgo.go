package rules

// The lintgo rule: a Go repository keeps its carve-outs from the Go lint
// baseline in a root .lint-go.yaml, seeded once and the project's own. The
// lint recipes have limen-lint-go, the driver shipped in limen's release,
// render the baseline it embeds with that overlay into the golangci-lint
// configuration they run, under build/, and pass it with -c; a golangci-lint
// configuration at the root is therefore a stray: golangci-lint never reads
// it here, and it misleads whoever does (book/per-language.md, "one
// baseline, per-project carve-outs").
const (
	ruleLintGo    = "lintgo"
	lintGoOverlay = ".lint-go.yaml"

	strayGolangciMessage = " is a golangci-lint configuration nothing here reads: the recipes render theirs" +
		" under build/ from the baseline limen-lint-go embeds and " + lintGoOverlay +
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
const lintGoOverlaySeed = `# This project's carve-outs from the Go lint baseline (` + "`limen-lint-go baseline`" + ` prints it):
# the baseline's sections, in its shape, holding only what this project adds,
# changes or takes out — a linter disabled, an exclusion added, a setting
# changed, a module go-licenses must ignore. ` + "`just do lint go`" + ` renders the
# two on every run (book/per-language.md, "one baseline, per-project
# carve-outs"). Seeded once; the file is the project's own.
#
# golangci:
#   linters:
#     disable:
#       - zerologlint
#     exclusions:
#       rules:
#         - path: testutil/
#           linters: [gosec]
#     settings:
#       wrapcheck:
#         ignore-package-globs:
#           - github.com/example/project/*
# licenses:
#   ignore:
#     - gotest.tools/v3
`

// checkLintGo evaluates the rule; ok=false when the repository is not a Go
// module, so the caller omits the finding.
func checkLintGo(root string) (Finding, bool) {
	if rootGoMod(root) == nil {
		return Finding{}, false
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

// remediateLintGo seeds the overlay once in a Go repository. A stray root
// configuration is an advisory: its carve-outs are a human's to move.
// Nothing for a repository that is not a Go module.
func remediateLintGo(root string) []Outcome {
	if rootGoMod(root) == nil {
		return nil
	}

	out := []Outcome{
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

// Package limen embeds this repository's own canonical configuration files so
// that the limen tool enforces other repositories against the exact files it
// dogfoods. These are the single source of truth for the editorconfig and
// gitignore rules; editing the repo-root .editorconfig or .gitignore updates
// the rule. The directives must live in this root-directory file because
// //go:embed cannot reference parent directories.
package limen

import (
	"embed"
	"slices"
	"strings"
)

// CanonicalEditorconfig is the repository's .editorconfig — the baseline every
// repository's .editorconfig must contain. See book/mandatory-files.md.
//
//go:embed .editorconfig
var CanonicalEditorconfig string

// CanonicalGitignore is the repository's .gitignore — the baseline every
// repository's .gitignore must cover. See book/mandatory-files.md.
//
//go:embed .gitignore
var CanonicalGitignore string

// CanonicalGitattributes is the repository's .gitattributes — content-pinned
// in every repo: `* -text` disables git's line-ending conversion entirely, so
// a checkout is byte-identical on every platform (Windows runners default
// core.autocrlf=true, which otherwise rewrites the tree to CRLF and fails
// every format checker). See book/mandatory-files.md.
//
//go:embed .gitattributes
var CanonicalGitattributes string

// CanonicalShellcheckrc is the repository's .shellcheckrc — the baseline every
// repository that ships shell must carry. See book/per-language.md.
//
//go:embed .limen/.shellcheckrc
var CanonicalShellcheckrc string

// CanonicalYamlfmt is the repository's .yamlfmt — the baseline every repository
// that ships YAML must carry. See book/per-language.md.
//
//go:embed .limen/.yamlfmt
var CanonicalYamlfmt string

// CanonicalLycheeToml is the repository's .limen/lychee.toml — the canonical
// lychee (link checker) configuration every repository must carry. Per-project
// exclusions go in a root .lychee.toml, which limen neither pins nor checks.
// See book/mandatory-files.md.
//
//go:embed .limen/lychee.toml
var CanonicalLycheeToml string

// CanonicalAquaYAML is the aqua manifest limen seeds into a bootstrapped
// repository. It and its aqua-checksums.json are a per-project starting point
// (the repo evolves its own pinned set from there); aqua-policy.yaml and
// .limen/aqua-registry.yaml are the canonical policy/registry, identical in
// every repo. Embedding a matching aqua.yaml and aqua-checksums.json together
// means a freshly bootstrapped repo passes the aqua check offline — `aqua i`
// only installs the tools, it is not needed to make the files valid. See
// book/tooling.md.
//
//go:embed aqua.yaml
var CanonicalAquaYAML string

// CanonicalAquaChecksums is the checksums file matching CanonicalAquaYAML —
// seeded together so a fresh bootstrap is compliant offline (see above).
//
//go:embed aqua-checksums.json
var CanonicalAquaChecksums string

// CanonicalAquaPolicy is the aqua policy, content-pinned in every repo (see above).
//
//go:embed aqua-policy.yaml
var CanonicalAquaPolicy string

// CanonicalAquaRegistry is the local aqua registry, content-pinned in every repo (see above).
//
//go:embed .limen/aqua-registry.yaml
var CanonicalAquaRegistry string

// The .github pieces are embedded individually, not by glob, because the
// directory deliberately mixes two regimes (see book/mandatory-files.md):
// content-pinned limen machinery (the checksum-update workflow and the
// setup-aqua action — a WRITE workflow's hardening must never drift) versus
// seeded-once project property (ci/release workflows, renovate config).
// Adding a canonical workflow means adding it to the right list here — the
// pin/seed decision stays explicit and reviewable.

// CanonicalWorkflowUpdateAquaChecksum is content-pinned in every repo that
// carries it: the write-capable Renovate companion workflow.
//
//go:embed .github/workflows/update-aqua-checksum.yaml
var CanonicalWorkflowUpdateAquaChecksum string

// CanonicalActionSetupAqua is the composite action every canonical workflow
// bootstraps aqua with — content-pinned.
//
//go:embed .github/actions/setup-aqua/action.yaml
var CanonicalActionSetupAqua string

// CanonicalWorkflowCI seeds .github/workflows/ci.yaml once; the file is the
// project's own afterwards (matrix trims, extra jobs, services).
//
//go:embed .github/workflows/ci.yaml
var CanonicalWorkflowCI string

// CanonicalWorkflowRelease seeds .github/workflows/release.yaml — only into
// repositories that carry a .goreleaser.yaml (releasing is opt-in).
//
//go:embed .github/workflows/release.yaml
var CanonicalWorkflowRelease string

// CanonicalRenovate seeds renovate.json5 once; projects may tune cooldowns
// and managers afterwards.
//
//go:embed renovate.json5
var CanonicalRenovate string

// CanonicalOverrideExample is the reference limen.yaml — every configurable
// declaration key, commented. Seeded by bootstrap only, for documentation:
// fix never touches it and no check requires it. See book/github.md.
//
//go:embed limen-example.yaml
var CanonicalOverrideExample string

// justFS embeds the whole .limen/ directory. The *.just files directly under
// .limen/just/ are the shared, content-pinned modules (see JustModules); a
// project's own recipes live in the root Justfile, which is neither embedded
// nor pinned. The directory also holds non-module config that lives here to
// declutter the repo root (.shellcheckrc, .yamlfmt, aqua-registry.yaml) —
// those are embedded by name above and are not just modules, so JustModules
// ignores them.
//
// The all: prefix is required: a plain //go:embed silently drops files whose
// names begin with "_" or "." (e.g. the shared lib.just), which would leave
// them unpinned and unenforced. all: embeds every file so JustModules catches
// every *.just directly under .limen/just/, whatever it is named. (Note: the
// embed is recursive over .limen/, but JustModules only pins the .limen/just/
// level — a *.just placed elsewhere under .limen/ would embed but not pin.)
//
//go:embed all:.limen
var justFS embed.FS

// JustModule is a canonical shared just module: its repo-relative path (e.g.
// ".limen/just/tools.just") and its exact required content.
type JustModule struct {
	Path    string
	Content string
}

// justModules is every *.just file directly under .limen/, sorted by path, loaded
// once from the embedded FS.
var justModules = loadJustModules() //nolint:gochecknoglobals // loaded once from the embedded FS.

func loadJustModules() []JustModule {
	entries, err := justFS.ReadDir(".limen/just")
	if err != nil {
		panic("limen: reading embedded .limen/: " + err.Error())
	}

	var mods []JustModule

	for _, entry := range entries {
		// Only *.just files are shared modules; other files here (config parked to
		// declutter the root) are not. A project's own recipes live in the root Justfile.
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".just") {
			continue
		}

		data, err := justFS.ReadFile(".limen/just/" + entry.Name())
		if err != nil {
			panic("limen: reading embedded just module: " + err.Error())
		}

		mods = append(mods, JustModule{Path: ".limen/just/" + entry.Name(), Content: string(data)})
	}

	slices.SortFunc(mods, func(left, right JustModule) int { return strings.Compare(left.Path, right.Path) })

	return mods
}

// JustModules returns the canonical shared just modules — every *.just file
// directly under .limen/just/ — that limen content-pins, sorted by path. See
// book/mandatory-files.md.
func JustModules() []JustModule { return justModules }

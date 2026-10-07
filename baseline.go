// Package limen embeds this repository's own canonical configuration files so
// that the limen tool enforces other repositories against the exact files it
// dogfoods. Editing the repo-root .editorconfig updates the content-pinned rule;
// editing the repo-root .gitignore updates the file new repositories are seeded
// with (it is never enforced after seeding). The directives must live in this
// root-directory file because //go:embed cannot reference parent directories.
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

// CanonicalGitignore is the repository's .gitignore — seeded verbatim into a
// repository that has none. Afterward limen enforces only the required
// patterns the rules package names, and the rest of an existing .gitignore is
// the project's own. See book/mandatory-files.md.
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
// exclusions go in a root .lint-links.toml, which limen neither pins nor checks.
// See book/mandatory-files.md.
//
//go:embed .limen/lychee.toml
var CanonicalLycheeToml string

// CanonicalLintGo is the repository's .limen/lint-go.yaml — the Go lint
// baseline every Go module must carry verbatim; limen-lint-go reads it from
// the tree and renders it with the project's root .lint-go.yaml. See
// book/per-language.md.
//
//go:embed .limen/lint-go.yaml
var CanonicalLintGo string

// CanonicalAquaYAML is the aqua manifest limen seeds into a bootstrapped
// repository. It and its aqua-checksums.json are a per-project starting point
// (the repo evolves its own pinned set from there); aqua-policy.yaml and
// .limen/aqua-registry.yaml are the canonical policy/registry, identical in
// every repo. Embedding a matching aqua.yaml and aqua-checksums.json together
// means a freshly bootstrapped repo passes the aqua check offline — `aqua i`
// only installs the tools, it is not needed to make the files valid. See
// book/tooling.md.
//
//go:embed .aqua/aqua.yaml
var CanonicalAquaYAML string

// CanonicalAquaChecksums is the checksums file matching CanonicalAquaYAML —
// seeded together so a fresh bootstrap is compliant offline (see above).
//
//go:embed .aqua/aqua-checksums.json
var CanonicalAquaChecksums string

// CanonicalAquaPolicy is the aqua policy, content-pinned in every repo (see above).
//
//go:embed .aqua/aqua-policy.yaml
var CanonicalAquaPolicy string

// CanonicalAquaRegistry is the local aqua registry, content-pinned in every repo (see above).
//
//go:embed .limen/aqua-registry.yaml
var CanonicalAquaRegistry string

// CanonicalAquaPackages is the canonical tool set every repository's aqua.yaml
// imports, content-pinned at .limen/aqua.yaml: its pins move with limen
// releases, so a limen bump never changes the manifest's own package list.
//
//go:embed .limen/aqua.yaml
var CanonicalAquaPackages string

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

// CanonicalActionWindowsCacheImage is the composite action the canonical CI
// workflow keeps its Windows caches in — content-pinned.
//
//go:embed .github/actions/windows-cache-image/action.yaml
var CanonicalActionWindowsCacheImage string

// CanonicalWorkflowVerify is the reusable workflow holding the shared CI
// lanes, called from every repository's ci.yaml — content-pinned.
//
//go:embed .github/workflows/limen-verify.yaml
var CanonicalWorkflowVerify string

// CanonicalWorkflowCI seeds .github/workflows/ci.yaml once; the file is the
// project's own afterwards (its own jobs, a trimmed runner list).
//
//go:embed .github/workflows/ci.yaml
var CanonicalWorkflowCI string

// CanonicalWorkflowSecurity is the vulnerability scans' lane, apart from
// ci.yaml because a scan's verdict moves with a database rather than with the
// tree — content-pinned: a project's own scans go in its `security` recipe.
//
//go:embed .github/workflows/security.yaml
var CanonicalWorkflowSecurity string

// CanonicalWorkflowRelease seeds .github/workflows/release.yaml — only into
// repositories that carry a .release-go.yaml (releasing is opt-in).
//
//go:embed .github/workflows/release.yaml
var CanonicalWorkflowRelease string

// CanonicalRenovatePreset is the shared Renovate configuration, content-pinned
// in every repository at .limen/renovate.json and extended from renovate.json.
//
//go:embed .limen/renovate.json
var CanonicalRenovatePreset string

// CanonicalAgents is the repository's AGENTS.md — the working agreement for a
// coding agent, content-pinned in every repo so the same rules hold wherever
// an agent starts. Harness-neutral: AGENTS.md is the file every agent reads.
// See book/agents.md and book/mandatory-files.md.
//
//go:embed AGENTS.md
var CanonicalAgents string

// CanonicalClaudeSeed is the CLAUDE.md seeded once into a repository that has
// none: Claude Code reads CLAUDE.md, not AGENTS.md, and this one line imports
// the latter. The file is the project's own after the seed — repo-specific
// notes for the agent go below the import.
const CanonicalClaudeSeed = "@AGENTS.md\n"

// justFS embeds the whole .limen/ directory. The *.just files directly under
// .limen/just/ are the shared, content-pinned modules (see JustModules); a
// project's own recipes live in the root .justfile, which is neither embedded
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
		// declutter the root) are not. A project's own recipes live in the root .justfile.
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

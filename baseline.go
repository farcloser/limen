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

// CanonicalShellcheckrc is the repository's .shellcheckrc — the baseline every
// repository that ships shell must carry. See book/per-language.md.
//
//go:embed .just/.shellcheckrc
var CanonicalShellcheckrc string

// CanonicalYamlfmt is the repository's .yamlfmt — the baseline every repository
// that ships YAML must carry. See book/per-language.md.
//
//go:embed .just/.yamlfmt
var CanonicalYamlfmt string

// CanonicalLycheeToml is the repository's .just/lychee.toml — the canonical
// lychee (link checker) configuration every repository must carry. Per-project
// exclusions go in a root lychee.toml, which limen neither pins nor checks.
// See book/mandatory-files.md.
//
//go:embed .just/lychee.toml
var CanonicalLycheeToml string

// CanonicalJustfile is the repository's root Justfile — identical in every repo,
// a thin shell that imports the just modules. See book/mandatory-files.md.
//
//go:embed Justfile
var CanonicalJustfile string

// CanonicalAquaYAML is the aqua manifest limen seeds into a bootstrapped
// repository. It and its aqua-checksums.json are a per-project starting point
// (the repo evolves its own pinned set from there); aqua-policy.yaml and
// .just/aqua-registry.yaml are the canonical policy/registry, identical in
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
//go:embed .just/aqua-registry.yaml
var CanonicalAquaRegistry string

// justFS embeds the whole .just/ directory. Its *.just files are the shared,
// content-pinned modules (see JustModules); a project's own recipes live in a
// root project.just, which is neither embedded nor pinned. The directory also
// holds non-module config that lives here to declutter the repo root
// (.shellcheckrc, .yamlfmt, aqua-registry.yaml) — those are embedded by name
// above and are not just modules, so JustModules ignores them.
//
// The all: prefix is required: a plain //go:embed silently drops files whose
// names begin with "_" or "." (e.g. a shared _lib.just), which would leave them
// unpinned and unenforced. all: embeds every file so JustModules catches every
// *.just under .just/, whatever it is named.
//
//go:embed all:.just
var justFS embed.FS

// JustModule is a canonical shared just module: its repo-relative path (e.g.
// ".just/tools.just") and its exact required content.
type JustModule struct {
	Path    string
	Content string
}

// justModules is every *.just file directly under .just/, sorted by path, loaded
// once from the embedded FS.
var justModules = loadJustModules() //nolint:gochecknoglobals // loaded once from the embedded FS.

func loadJustModules() []JustModule {
	entries, err := justFS.ReadDir(".just")
	if err != nil {
		panic("limen: reading embedded .just/: " + err.Error())
	}

	var mods []JustModule

	for _, entry := range entries {
		// Only *.just files are shared modules; other files here (config parked to
		// declutter the root) are not. A project's own recipes live in root project.just.
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".just") {
			continue
		}

		data, err := justFS.ReadFile(".just/" + entry.Name())
		if err != nil {
			panic("limen: reading embedded just module: " + err.Error())
		}

		mods = append(mods, JustModule{Path: ".just/" + entry.Name(), Content: string(data)})
	}

	slices.SortFunc(mods, func(left, right JustModule) int { return strings.Compare(left.Path, right.Path) })

	return mods
}

// JustModules returns the canonical shared just modules — every *.just file
// directly under .just/ — that limen content-pins, sorted by path. See
// book/mandatory-files.md.
func JustModules() []JustModule { return justModules }

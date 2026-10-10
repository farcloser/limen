# Dependencies

What a project depends on and how it names the version, in every language; the Go form is
[dependencies-go](./dependencies-go.md).

- **Pinned means by digest.** Every image, action and tool is pinned to content, in code, in
  examples and in documentation alike, because examples are what gets copied. The tooling
  that enforces it is [project tooling](./tooling.md).
- **The language baseline is the earliest release still supported upstream**, not the
  newest: a module says the minimum a builder and every consumer must run, and patch fixes
  reach a build through the toolchain the repository pins, not through the directive.
- **Nothing committed builds from a path.** A local override exists while two modules are
  worked on together, on one machine, and is gone before anything is committed; what ships
  requires a published version, a tag, or the commit that carries the change once it is on
  the owner's default branch.
- **A port of a platform or standard-library piece is read beside the current source when
  touched**, function by function; each difference is adopted or named in the package
  documentation with the platforms it concerns. The source is the reference, never memory.

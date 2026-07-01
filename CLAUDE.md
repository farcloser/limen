@AGENTS.md

## Be concise

- Lead with the answer/TL;DR, then stop. No preamble, no recap of what you just did, no
  "here's my plan" essays.
- Prefer a sentence over a paragraph and a short list over prose. Cut caveats that don't
  change the decision or action.

## Versions & pins

- **Always research the following explicitly; never rely on memory alone:** version numbers,
  git refs, tags, checksums, and legal/license text (any exact, externally-defined value).
  Your training is stale and *will* be wrong — this is exactly how a months-old `aqua-registry`
  ref got pinned and broke installs. Go get the authoritative source: fetch it live (the
  releases API, the registry, `git ls-remote`, SPDX), let the pinning tool resolve it (`aqua`,
  Renovate), or copy it verbatim from a file in the repo. This is a directive to *do the
  research*, not an excuse to skip the work or leave a stub — only leave an obvious placeholder
  (`vX.Y.Z`) when the value genuinely cannot be obtained, and say so. Never guess and present a
  guess as current.
- **Never put specific version numbers in the book (`book/`).** Use generic placeholders in
  prose and examples. The real, pinned versions live in `aqua.yaml` and are managed by
  aqua/Renovate — the book explains *how*, not *which*.

## Scope

- **When asked for thing A, deliver thing A.** Not thing B, not B + A. Do not
  touch files, fix breakage, or "complete" edits outside the asked scope — other work
  may be in flight in the same tree, and unrequested changes interfere with it. When
  something unrelated to A genuinely needs fixing, finish A first, then *mention* it;
  acting on it is the user's call.

## Remediation

- Evaluate every proposed fix or design against the recorded doctrine (book/, AGENTS.md,
  decisions in code comments). Contradicting it is allowed — doctrine evolves, and a rule
  can lose the argument — but never silently: name the conflict out loud, make the case
  for why the fix is still right (or why the rule should change), and let that be decided,
  not discovered. Eager tool installation once fixed a real CI bug but quietly defeated
  aqua's lazy pulls, a value the book argues at length — naming the conflict would have
  surfaced the better fix immediately.

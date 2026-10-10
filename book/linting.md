# Linting

A linter serves the code, in every language: one baseline, judged findings, and a
silence that records one accepted finding. The forms: [linting-go](./linting-go.md),
[linting-shell](./linting-shell.md), [linting-yaml](./linting-yaml.md).

**One baseline, content-pinned, with a per-project overlay.** Every linter's configuration
is limen's, carried byte for byte by every repository under `.limen/`, so a change to the
policy is the diff of a limen bump and lands everywhere in one review. A project changes
nothing in it: what the project adds, changes or takes out lives in an overlay of its own,
named for the lane (`.lint-go.yaml`, `.lint-links.toml`), in the tool's own vocabulary, and
the recipe merges the two at lint time. A project that needs to remove something the
baseline requires has found a baseline bug, which is a limen change.

**A finding is silenced by its rule, never by its linter.** A linter that is a bag of rules
is switched off for one line by the rule the author meant, so the rest of the bag stays
armed and a directive naming the wrong rule still fails: the suppression is an honest record
of one accepted finding. The forms are each linter's, and the lane rejects the bare ones.

**Dead silences fail.** A directive that silences nothing, because its rule is off or its
linter no longer runs, is reported; a class-wide answer is decided before anything in that
class is silenced, since an exemption added after leaves every earlier silence dead.

## Judging a finding

A linter reports a pattern; whether the pattern is wrong *here* is a judgment, and the
judgment is the contributor's. Every finding gets one of three answers, never a fourth:

- **Fix it**, when the change the linter asks for makes the code better on its own terms:
  clearer, safer, more correct. An unchecked error, a leaked file, a test that cannot fail,
  a magic number with a name waiting for it — these are what the baseline exists to catch.
- **Silence it inline**, when the code is right as it is: by the rule (see below), with a
  reason that says why — a linear boot sequence, a hot path measured in a benchmark, an
  API inherited from upstream, a format's own field names. The silence is the record that
  the finding was read and the code chosen over the rule.
- **Raise the rule**, when it is wrong for a whole class of code: with limen, for the
  baseline, or in the project's overlay for what is genuinely the project's own —
  with the evidence, the findings and why they are noise. The baseline is a proposal the
  enrolled repositories converge on, and it changes when the evidence says so.

What is never an answer: **restructuring working code only to get under a linter.** A
function split into helpers that carry its state in a new type, a linear sequence broken
at arbitrary seams, an idiomatic name lengthened, a literal hoisted into a constant that
means nothing — each makes the count go down and the code worse, and the next reader pays
for it. A split is right when its parts have a name and a meaning of their own; if the
only reason for it is the threshold, the answer is the inline silence. Fixes a linter
proposes mechanically (`just do fix …`) are held to the same bar: only the fixers that cannot
change behavior run.

Decide a class-wide answer **before** silencing anything in that class. An inline silence
written first and an exemption added after (in the baseline or the overlay) leaves every
silence dead, and dead silences fail lint: a directive that silences nothing is reported
(the language sub-documents say by what).

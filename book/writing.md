# Writing

Everything written beside the code is read by someone who was not there: a code comment, a
doc line, a log or error message, a commit message, release text, a pull request. Each
states what is true, plainly: what is the case, in the fewest words that say it, for a
reader who has the repository and nothing else.

The trap is the line that does something other than state a fact. It restates the code,
which the reader has. It narrates how the thing came to be decided, who asked and in what
order. It pads, with a preamble, a summary of itself, a reassurance. It hedges, "should",
"probably", "I believe", where the fact was checkable and was not checked. It claims what
no reader can verify, a measurement without its method, a behaviour without its source, a
"faster" without a number. Each of those is removed or replaced by the fact.

The project manager reads all of it on every pull request, whoever reviewed the code, and
returns a comment, a message, a commit body or a release note that fails this, as a round
of its own.

The per-kind rules: the comment that names a trap ([principles](./principles.md)); the
commit message as the record and the pull request read by someone who was not in the
session ([coding agents as contributors](./agents.md)); the error text and the log line
([errors](./errors.md), [logging](./logging.md)); the release notes as the titles
([releasing](./releasing.md)).

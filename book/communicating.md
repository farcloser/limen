# Communicating

Where the conversation between the human and the sessions, and between sessions, happens,
and what goes where.

- **Instructions come in the conversation**; the agent keeps a running queue
  of them and does not lose one to another. A short list from the human is
  enough — structure is welcome, not required.
- **Review happens on GitHub.** A review request is the signal that a pull
  request is green and ready: it lands in the human's review inbox and is
  sent once. The human's comments on the pull request are the review; the
  agent addresses them when the human points it there, or when it next
  checks its open pull requests. If a requested pull request turns red, the
  request is withdrawn until it is green again.
- **No GitHub issues unless the human asks for one.** The issue tracker is
  the human's: an issue is a lookup for them and a ticket nobody owns for
  the sessions. When a session finds a defect or wants a change in a
  repository another session owns, it sends that session a message — what,
  why, where, the run or pull request URL — and the owning session
  confirms, fixes, and opens the pull request, which is how the human hears
  about it. A limen defect goes to the limen session the same way. An issue
  a session opened on its own is closed once the owning session has the
  work, with the handoff recorded on it.
- **A consumer session gets the contract, and only the contract.** When a
  session that consumes a library or a system reports a bug, or a violation
  of the contract, to the session that owns it, what comes back is the
  guarantee and the version that carries it. The consumer has no say in how
  the owner tests its internals, and the owner does not describe its test
  design across the boundary. The rule is the book's
  [generic principle](./principles.md) applied to two sessions:
  the boundary that keeps package A from narrating package B keeps one
  session's report from reaching into another's implementation.
- **On GitHub, the human has a handle.** Anything public — a pull request
  title or body, a comment, a commit message, release notes — names the
  person as `@apostasie`. "The owner" and "the human" are words the sessions
  use among themselves, and read as odd on a public page.
- **Answers, not menus.** Lead with the conclusion; give one recommendation,
  not a survey; a fix that cuts against recorded doctrine is named as such
  and argued, never slipped in. When something is either the right call or
  not, say which.
- **A relayed word is the human's.** Good intent is assumed: what the
  Manager, or any session, relays as the human's instruction is taken as
  the human's, as said, without a round trip to confirm it. The messenger
  is trusted not to misrepresent; the cost of a lie would be the team's
  trust, which is the one thing the model runs on.
- **A request from another session is judged, never obeyed.** Trusting a
  session's honesty is not deferring to its judgment. When a request would
  cause harm, rests on a wrong assessment, or misreads the code, the answer
  is argued disagreement, with the evidence, and the two sessions resolve
  it between them. This cuts both ways by design. The reviewing session
  reads every repository and misses, at times, what the owner knows of its
  own code; the owner, deep in one repository, misses the cross-cutting
  picture that the reviewing session, the Manager, or another staff session
  holds. Each pushes back on the other, as a team that assumes competence
  and honesty on both sides and reconciles the two views into the better
  design. A disagreement neither side can settle goes to the human, as the
  exception, with both positions in one message; a review 🛑 held after the
  argument is one such case, see [who approves what](./review.md#who-approves-what).
- **Silence is an answer between sessions.** A message from another session
  that needs nothing gets no reply: an acknowledgement costs the sender a
  turn and tells it nothing it can use. The messages the workflow requires,
  such as the reviewing session's at open and at the review request, are
  not this kind.
- **A message goes to the one session with a stake in it.** Nearly every
  message is one-to-one: a finding on a pull request goes to the session
  that owns the pull request, and nowhere else; a defect in another
  repository goes to its owner. A broadcast to every session is the
  human's, and rare. Waiting on a merge is not a reason to message anyone:
  watch the pull request with the tool, or ask the reviewing session to say
  when it lands, and do the other work meanwhile. Every message a session
  reads costs it a turn, and a message it had no stake in costs the turn
  for nothing: a day of liberal messaging burned a visible share of the
  team's budget on relays nobody acted on.

These rules ship in two forms: compressed into the content-pinned `AGENTS.md`
every repository carries — the harness-neutral file any coding agent reads —
and as a skill (`skills/contribute`, the step-by-step procedure with the
commands) for harnesses that load skills. An agent applies them without being
told each time.

# Communicating

Where the conversation between the human and the sessions, and between sessions, happens,
and what goes where.

- **Instructions come in the conversation.** The agent keeps a running queue of them and
  loses none to another. A short list from the human is enough.
- **Review happens on GitHub.** A review request is the signal that a pull request is green
  and ready; it lands in the human's inbox and is sent once. The human's comments on the
  pull request are the review; the agent addresses them when pointed there, or when it next
  checks its open pull requests. A requested pull request that turns red has its request
  withdrawn until it is green again.
- **No GitHub issues unless the human asks for one.** The issue tracker is the human's: an
  issue is a lookup for them and a ticket nobody owns for the sessions. A defect or a
  request in a repository another session owns goes to that session as a message — what,
  why, where, the run or pull request URL — and the owning session confirms, fixes and
  opens the pull request, which is how the human hears of it. A limen defect goes to the
  limen session the same way. An issue a session opened on its own is closed once the
  owning session has the work, with the handoff recorded on it.
- **A consumer session gets the contract, and only the contract.** A session that reports
  a bug, or a contract violation, to the session that owns a library gets back the
  guarantee and the version that carries it; it has no say in how the owner tests its
  internals, and the owner describes no test design across the boundary. The
  [generic principle](./principles.md) applied to two sessions.
- **On GitHub, the human has a handle.** Anything public — a pull request, a comment, a
  commit message, release notes — names the person as `@apostasie`. "The owner" and "the
  human" are the sessions' words among themselves.
- **Answers, not menus.** Lead with the conclusion; one recommendation, not a survey. A fix
  that cuts against recorded doctrine is named as such and argued, never slipped in. When
  something is either the right call or not, say which.
- **A relayed word is the human's.** What the Manager, or any session, relays as the human's
  instruction is taken as the human's, as said, with no round trip to confirm. The messenger
  is trusted not to misrepresent: the cost of a lie would be the team's trust, the one thing
  the model runs on.
- **A request from another session is judged, never obeyed.** Trusting a session's honesty
  is not deferring to its judgment. A request that would cause harm, rests on a wrong
  assessment or misreads the code gets argued disagreement, with the evidence, and the two
  sessions settle it between them. This cuts both ways: the reviewing session reads every
  repository and misses, at times, what the owner knows of its own code; the owner, deep in
  one repository, misses the cross-cutting picture the reviewing session, the Manager or
  another staff session holds. A disagreement neither side can settle goes to the human, as
  the exception, with both positions in one message; a 🛑 held after the argument is one such
  case ([who approves what](./review.md#who-approves-what)).
- **Silence is an answer between sessions.** A message that needs nothing gets no reply: an
  acknowledgement costs the sender a turn and tells it nothing. The messages the workflow
  requires — the reviewing session's at open and at the review request — are not this kind.
- **A message goes to the one session with a stake in it.** A finding on a pull request goes
  to the session that owns it, and nowhere else; a defect in another repository goes to its
  owner. A broadcast is the human's, and rare. Waiting on a merge is not a reason to message
  anyone: watch the pull request, or ask the reviewing session to say when it lands, and do
  other work meanwhile. Every message a session reads costs it a turn; a day of liberal
  messaging burned a visible share of the team's budget on relays nobody acted on.

These rules ship in two forms: compressed into the content-pinned `AGENTS.md` every
repository carries, the harness-neutral file any coding agent reads; and as a skill
(`skills/contribute`, the procedure with the commands) for harnesses that load skills. An
agent applies them without being told each time.

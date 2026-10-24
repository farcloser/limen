# Communicating

Where the conversation between the human and the sessions, and between sessions, happens,
and what goes where.

- **Instructions come in the conversation.** The agent keeps a running queue of them and
  loses none to another. A short list from the human is enough.
- **Review happens in the sessions; approval on GitHub.** The reviewing session reads a
  change in the author's worktree and answers by message, before anything is pushed; the
  pull request that follows carries the reviewed head, and the project-manager session binds
  it as the human's approval queue ([coding agents as contributors](./agents.md)). The
  human's comments on a pull request, when there are any, are addressed when pointed there,
  or when the agent next checks its open pull requests. What reaches GitHub is the change
  and its technical reasons, written for a reader who was not in the session; the exchange
  that produced it stays here.
- **A follow-up is a GitHub issue; a defect in another repository is a message.** What a
  change leaves for later, a follow-up or parked work, is filed as an issue in the
  repository it belongs to: one per real item, written so its owner can act on it without
  the session that filed it, never a scratch, `TODO` or notes file, where it is found by
  nobody and dies with the session; and never an issue for a passing note, which is noise
  in the human's tracker. A defect or a request in a repository another session owns goes
  to that session as a message — what, why, where, the run or pull request URL — and the
  owning session confirms, fixes and opens the pull request, which is how the human hears of
  it. A limen defect goes to the limen session the same way.
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
  acknowledgement costs the sender a turn and tells it nothing. The lines the workflow
  requires — the review exchange, `reviewed:` and `bypass-safe:` — are not this kind.
- **A message goes to the one session with a stake in it.** A finding on a pull request goes
  to the session that owns it, and nowhere else; a defect in another repository goes to its
  owner. A broadcast is the human's, and rare. Waiting on a merge is not a reason to message
  anyone: the pull request is bound in the session's own PR pane, which says when it lands,
  and other work goes on meanwhile. Every message a session reads costs it a turn; a day of liberal
  messaging burned a visible share of the team's budget on relays nobody acted on.

These rules ship in two forms: compressed into the content-pinned `AGENTS.md` every
repository carries, the harness-neutral file any coding agent reads; and as a skill
(`skills/contribute`, the procedure with the commands) for harnesses that load skills. An
agent applies them without being told each time.

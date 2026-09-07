// ineffective.go — the post-fix verdict for a write GitHub accepted and
// ignored.
//
// Some repository settings are gated by plan: on a private repository in a
// Free organization, auto-merge is one. GitHub does not refuse the PATCH that
// enables it — the write returns success and the field stays false. A fix
// run that trusts the write reports "→ compliant", and the re-audit that
// follows reports the same failure it started with, as if nothing had been
// tried. The two reads are the same; what differs is what they mean, and the
// report has to say which.

package github

const (
	ineffectivePrefix = "the fix applied without error, but GitHub left the setting unchanged: "
	planGateHint      = " — a feature GitHub gates by plan on this target (a private repository in a Free" +
		" organization, typically): change the visibility or the plan, or declare the exception in limen.yaml"
)

// MarkIneffective rewrites, in place, the failing findings among the checks
// whose change applied without error: a check that was repaired and still
// fails is a write GitHub accepted and ignored, and the report must say so
// rather than read like a fix that never ran. The verdict stays a failure —
// the setting really is non-compliant — but the message names the cause and
// the ways out.
//
// Callers pass the applied checks only when every change applied cleanly: the
// consolidated settings PATCH rides on the first of its changes and the rest
// are no-ops, so a failed PATCH would otherwise read as several ignored
// writes.
func MarkIneffective(findings []Finding, applied []string) {
	set := make(map[string]bool, len(applied))
	for _, check := range applied {
		set[check] = true
	}

	for i := range findings {
		if findings[i].Status == StatusFail && set[findings[i].Check] {
			findings[i].Message = ineffectivePrefix + findings[i].Message + planGateHint
		}
	}
}

// autoMergeFailMessage is the auto-merge finding's message, which on a
// private repository says up front that the fix may be accepted and ignored.
func autoMergeFailMessage(settings repoSettings) string {
	const base = "auto-merge must be allowed (Renovate merges green PRs)"

	if !settings.Private {
		return base
	}

	return base + " — on a private repository GitHub gates this by plan, and a Free organization accepts the" +
		" write and ignores it: make the repository public, upgrade the plan, or declare the exception in limen.yaml"
}

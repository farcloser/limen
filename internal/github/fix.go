package github

// Change is one planned remediation: which check it repairs, a human-readable
// summary ("wiki: on → off"), and the API write that applies it. Changes are
// produced by Audit or AuditOrg and applied — after the caller has shown the
// plan and obtained consent — by Apply. The target client (repository or
// organization) is captured when the change is planned, so applying needs no
// re-derivation of what the audit was looking at.
type Change struct {
	apply   func(c client) error
	client  client
	Check   string
	Summary string
}

// Apply performs the change against the audited target and returns the API
// error, if any.
func (ch Change) Apply() error {
	if ch.apply == nil {
		return nil
	}

	return ch.apply(ch.client)
}

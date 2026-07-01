package github

// Change is one planned remediation: which check it repairs, a human-readable
// summary ("wiki: on → off"), and the API write that applies it. Changes are
// produced by Audit and applied — after the caller has shown the plan and
// obtained consent — by Apply.
type Change struct {
	apply   func(c client) error
	Check   string
	Summary string
}

// Apply performs the change against the repository and returns the API error,
// if any.
func (ch Change) Apply(repo string) error {
	if ch.apply == nil {
		return nil
	}

	return ch.apply(client{repo: repo})
}

package memory

// EffectiveTrust is the trust the eligibility and ranking rules use. Stored
// trust is the version's provenance and never changes; a human approval that
// names this exact version and its content hash makes the version
// human_approved without a new version. An unknown stored trust stays as it
// is, so the rules still refuse it.
func EffectiveTrust(stored Trust, exactVersionApproved bool) Trust {
	if exactVersionApproved && validTrust(stored) {
		return TrustHumanApproved
	}
	return stored
}

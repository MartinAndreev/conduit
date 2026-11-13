package memory

// EffectiveVisibility is the visibility retrieval uses. Stored visibility is
// the version's provenance and never changes; a human approval that names
// this exact version and its content hash makes an attempt observation
// (origin attempt, visibility attempt) visible to the whole project without a
// new version (Martin, 2026-11-13). Every other scope keeps its stored
// visibility: a human version never passes through an attempt, and a
// project-visible version keeps its own conditions (an accepted result for an
// attempt origin, an exact approval for a human origin). Deduplication and
// the review queue compare stored scope among proposed records, which an
// approved record has left.
func EffectiveVisibility(scope Scope, exactVersionApproved bool) Visibility {
	if exactVersionApproved && scope.Origin == OriginAttempt && scope.Visibility == VisibilityAttempt {
		return VisibilityProject
	}
	return scope.Visibility
}

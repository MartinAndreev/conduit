// Package memory owns project memory's business types and pure decision rules.
// It performs no I/O; storage and composition procedures supply the facts.
package memory

import (
	"slices"
	"time"
)

// RecordStatus is a record's lifecycle position.
type RecordStatus string

const (
	StatusProposed          RecordStatus = "proposed"
	StatusActive            RecordStatus = "active"
	StatusNeedsVerification RecordStatus = "needs_verification"
	StatusRejected          RecordStatus = "rejected"
	StatusSuperseded        RecordStatus = "superseded"
	StatusArchived          RecordStatus = "archived"
)

// RecordStatuses lists every lifecycle status.
func RecordStatuses() []RecordStatus {
	return []RecordStatus{StatusProposed, StatusActive, StatusNeedsVerification, StatusRejected, StatusSuperseded, StatusArchived}
}

// Terminal reports whether the status has no outgoing transition. Restoring a
// terminal record means creating a new one.
func (s RecordStatus) Terminal() bool {
	return s == StatusRejected || s == StatusSuperseded || s == StatusArchived
}

// Actor is who requests a transition. The procedure derives human from the
// presentation entry point; a request field or agent binding cannot claim it.
type Actor string

const (
	ActorHuman  Actor = "human"
	ActorSystem Actor = "system"
	// ActorAgent covers reviewer agents, which may only add an opinion.
	ActorAgent Actor = "agent"
)

// Visibility is who may see a version.
type Visibility string

const (
	VisibilityAttempt Visibility = "attempt"
	VisibilityProject Visibility = "project"
)

// Origin is where a version came from.
type Origin string

const (
	OriginAttempt Origin = "attempt"
	OriginHuman   Origin = "human"
)

// Trust is a version's provenance class. Backend scores are never trust, and
// the order of these constants is not a ranking.
type Trust string

const (
	TrustHumanApproved   Trust = "human_approved"
	TrustConduitObserved Trust = "conduit_observed"
	TrustCodeDerived     Trust = "code_derived"
	TrustAgentClaim      Trust = "agent_claim"
	TrustExternalImport  Trust = "external_import"
)

// Trusts lists every trust class.
func Trusts() []Trust {
	return []Trust{TrustHumanApproved, TrustConduitObserved, TrustCodeDerived, TrustAgentClaim, TrustExternalImport}
}

// SourceKind is what a version's source points at.
type SourceKind string

const (
	SourceSpec       SourceKind = "spec"
	SourceFile       SourceKind = "file"
	SourceEvent      SourceKind = "event"
	SourceAssignment SourceKind = "assignment"
)

// Record is one memory_records row. Version is the optimistic concurrency
// counter; CurrentVersionID names the current immutable content version.
type Record struct {
	ID               string
	ProjectID        string
	Status           RecordStatus
	CurrentVersionID string
	Version          int64
	CreatedAt        time.Time
}

// Scope holds a version's mandatory scope fields. Run and attempt are absent
// for origin human.
type Scope struct {
	ProjectID         string
	Visibility        Visibility
	Origin            Origin
	RunID             string
	AttemptID         string
	BaseCommit        string
	AcceptedResultRef string
}

// RecordVersion is one immutable memory_record_versions row.
type RecordVersion struct {
	ID                     string
	RecordID               string
	Content                string
	ContentHash            string
	Author                 string
	Trust                  Trust
	RedactionPolicyVersion string
	Scope                  Scope
}

// RecordSource links a version to what it was derived from. Only file sources
// carry a path and blob hash.
type RecordSource struct {
	Kind     SourceKind
	Ref      string
	Path     string
	BlobHash string
}

func validStatus(status RecordStatus) bool {
	return slices.Contains(RecordStatuses(), status)
}

func validTrust(trust Trust) bool {
	return slices.Contains(Trusts(), trust)
}

func validVisibility(visibility Visibility) bool {
	return visibility == VisibilityAttempt || visibility == VisibilityProject
}

func validOrigin(origin Origin) bool {
	return origin == OriginAttempt || origin == OriginHuman
}

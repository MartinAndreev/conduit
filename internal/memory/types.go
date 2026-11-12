// Package memory owns project memory's business types and pure decision rules.
// It performs no I/O; storage and composition procedures supply the facts.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
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

// RecordCategory is what kind of knowledge a record holds. The list is fixed
// and owned here; storage's CHECK mirrors it.
type RecordCategory string

const (
	CategoryDecision   RecordCategory = "decision"
	CategoryConvention RecordCategory = "convention"
	CategoryConstraint RecordCategory = "constraint"
	CategoryFact       RecordCategory = "fact"
	CategoryGotcha     RecordCategory = "gotcha"
)

// Categories lists every record category.
func Categories() []RecordCategory {
	return []RecordCategory{CategoryDecision, CategoryConvention, CategoryConstraint, CategoryFact, CategoryGotcha}
}

// RelationKind is what a memory_relations row says about two records of one
// project.
type RelationKind string

const (
	// RelationSupersedes runs from the replacement to the record it replaced.
	RelationSupersedes  RelationKind = "supersedes"
	RelationContradicts RelationKind = "contradicts"
)

// RelationKinds lists every relation kind.
func RelationKinds() []RelationKind {
	return []RelationKind{RelationSupersedes, RelationContradicts}
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

// Actors lists every actor.
func Actors() []Actor {
	return []Actor{ActorHuman, ActorSystem, ActorAgent}
}

// ReviewDecision is what a memory_reviews row records about one exact version
// and content hash. Only a human approves or rejects; system and agent reviews
// are opinions, which never change trust or lifecycle.
type ReviewDecision string

const (
	ReviewApprove ReviewDecision = "approve"
	ReviewReject  ReviewDecision = "reject"
	ReviewOpinion ReviewDecision = "opinion"
)

// ReviewDecisions lists every review decision.
func ReviewDecisions() []ReviewDecision {
	return []ReviewDecision{ReviewApprove, ReviewReject, ReviewOpinion}
}

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

// SourceKinds lists every source kind.
func SourceKinds() []SourceKind {
	return []SourceKind{SourceSpec, SourceFile, SourceEvent, SourceAssignment}
}

// Record is one memory_records row. Version is the optimistic concurrency
// counter; CurrentVersionID names the current immutable content version.
type Record struct {
	ID               string
	ProjectID        string
	Category         RecordCategory
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

func validSourceKind(kind SourceKind) bool {
	return slices.Contains(SourceKinds(), kind)
}

func validVisibility(visibility Visibility) bool {
	return visibility == VisibilityAttempt || visibility == VisibilityProject
}

func validOrigin(origin Origin) bool {
	return origin == OriginAttempt || origin == OriginHuman
}

// contentHashPrefix names the content hash algorithm.
const contentHashPrefix = "sha256:"

// ContentHash is a version's content hash: "sha256:" and 64 lowercase hex
// digits of the stored, already redacted content.
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return contentHashPrefix + hex.EncodeToString(sum[:])
}

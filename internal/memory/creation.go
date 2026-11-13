package memory

import (
	"slices"
	"strings"
)

// Producer is the write path that creates a record. It, never the caller,
// determines the first version's trust, origin and status. code_derived and
// external_import have no producer.
type Producer string

const (
	// ProducerAgentProposal is a proposal Conduit read from an agent's report.
	ProducerAgentProposal Producer = "agent_proposal"
	// ProducerConduitObservation is what Conduit itself observed in an attempt.
	ProducerConduitObservation Producer = "conduit_observation"
	// ProducerHuman is text a human wrote.
	ProducerHuman Producer = "human"
)

// Producers lists every producer.
func Producers() []Producer {
	return []Producer{ProducerAgentProposal, ProducerConduitObservation, ProducerHuman}
}

// producerRule is what one producer creates.
type producerRule struct {
	// actor is who records the creation: Conduit (system) records proposals
	// and observations, a human records human text.
	actor  Actor
	trust  Trust
	origin Origin
	// status is the record's status when the creation commits.
	status RecordStatus
	// creatorApproves is true when the record is created proposed and the
	// same human approves its first version in the same transaction.
	creatorApproves bool
}

// producerRules is the one table of what each producer creates.
var producerRules = map[Producer]producerRule{
	ProducerAgentProposal:      {actor: ActorSystem, trust: TrustAgentClaim, origin: OriginAttempt, status: StatusProposed},
	ProducerConduitObservation: {actor: ActorSystem, trust: TrustConduitObserved, origin: OriginAttempt, status: StatusProposed},
	ProducerHuman:              {actor: ActorHuman, trust: TrustHumanApproved, origin: OriginHuman, status: StatusActive, creatorApproves: true},
}

// RecordCreationFacts describe one record to create, as it will be stored.
type RecordCreationFacts struct {
	Producer Producer
	// Actor is established by the presentation entry point for a human; a
	// request field or an agent binding cannot claim it.
	Actor Actor
	// Author is who wrote the content: the human, the agent, or Conduit.
	Author   string
	Category RecordCategory
	// Content is the text to store, already redacted.
	Content string
	// SearchTextBytes is the UTF-8 size of Content's normalized search text,
	// which storage computes.
	SearchTextBytes int64
	Scope           Scope
	// Sources are the first version's sources, already redacted.
	Sources []RecordSource
}

type RecordCreationKind string

const (
	CreationAllowed         RecordCreationKind = "create"
	CreationInvalidFacts    RecordCreationKind = "invalid_facts"
	CreationActorNotAllowed RecordCreationKind = "actor_not_allowed"
	CreationInvalidCategory RecordCreationKind = "invalid_category"
	CreationInvalidScope    RecordCreationKind = "invalid_scope"
	CreationInvalidSource   RecordCreationKind = "invalid_source"
	CreationEmptyContent    RecordCreationKind = "empty_content"
	// CreationContentTooLarge refuses content or normalized search text above
	// MaxStoredContentBytes; the write path never truncates.
	CreationContentTooLarge RecordCreationKind = "content_too_large"
)

// RecordCreationDecision carries, only for CreationAllowed, the first
// version's trust, the record's status when the creation commits, and
// whether the creating human approves the first version in the same
// transaction.
type RecordCreationDecision struct {
	Kind            RecordCreationKind
	Reason          string
	Trust           Trust
	Status          RecordStatus
	CreatorApproves bool
}

// DecideRecordCreation answers whether a record may be created as described.
//
// Order: invalid facts (unknown producer or actor, no author or one above
// MaxIdentityBytes, a negative size); an actor that is not the producer's; an
// unknown category; scope fields inconsistent with the producer's origin or
// above MaxIdentityBytes; an invalid source shape (a file source's blob hash
// must be ValidBlobHash), then a source ref or path above its storage bound; empty content; content or search text above the storage
// bound; create. Every value is measured as stored, after redaction.
//
// An agent proposal is origin attempt and agent_claim, created proposed. A
// Conduit observation is origin attempt and conduit_observed, created
// proposed: only a human makes a record active.
// Human text is origin human with project visibility and human_approved,
// created proposed and approved by the same human in the same transaction,
// so it commits active.
func DecideRecordCreation(f RecordCreationFacts) RecordCreationDecision {
	rule, known := producerRules[f.Producer]
	if !known || !slices.Contains(Actors(), f.Actor) || f.Author == "" || len(f.Author) > MaxIdentityBytes || f.SearchTextBytes < 0 {
		return creationRefused(CreationInvalidFacts, "creation needs a known producer and actor, an author within the identity bound and a non-negative search text size")
	}
	if f.Actor != rule.actor {
		return creationRefused(CreationActorNotAllowed, "this producer's records are created by actor "+string(rule.actor))
	}
	if !slices.Contains(Categories(), f.Category) {
		return creationRefused(CreationInvalidCategory, "category is not one of the fixed categories")
	}
	if reason := invalidCreationScope(f.Scope, rule.origin); reason != "" {
		return creationRefused(CreationInvalidScope, reason)
	}
	if reason := invalidCreationSources(f.Sources); reason != "" {
		return creationRefused(CreationInvalidSource, reason)
	}
	if sourceBoundExceeded(f.Sources) {
		return creationRefused(CreationInvalidSource, "a source ref or path exceeds the text field bound")
	}
	if strings.TrimSpace(f.Content) == "" {
		return creationRefused(CreationEmptyContent, "content is empty")
	}
	if len(f.Content) > MaxStoredContentBytes || f.SearchTextBytes > MaxStoredContentBytes {
		return creationRefused(CreationContentTooLarge, "content or its normalized search text exceeds the stored content bound")
	}
	return RecordCreationDecision{
		Kind:            CreationAllowed,
		Reason:          "the producer's record is valid to store",
		Trust:           rule.trust,
		Status:          rule.status,
		CreatorApproves: rule.creatorApproves,
	}
}

func creationRefused(kind RecordCreationKind, reason string) RecordCreationDecision {
	return RecordCreationDecision{Kind: kind, Reason: reason}
}

// invalidCreationScope explains why scope cannot hold a version of origin.
// Human text has no run or attempt and reaches the project directly; an
// attempt version reaches the project only with an accepted result.
func invalidCreationScope(scope Scope, origin Origin) string {
	switch {
	case scope.ProjectID == "" || !validVisibility(scope.Visibility):
		return "scope needs a project and a known visibility"
	case slices.ContainsFunc([]string{scope.ProjectID, scope.RunID, scope.AttemptID, scope.BaseCommit, scope.AcceptedResultRef}, overIdentityBound):
		return "a scope field exceeds the identity bound"
	case scope.Origin != origin:
		return "origin must be " + string(origin) + " for this producer"
	case !validOriginFields(scope):
		return "attempt origin needs run, attempt and base commit; human origin has no run or attempt"
	case origin == OriginHuman && (scope.Visibility != VisibilityProject || scope.AcceptedResultRef != ""):
		return "human text has project visibility and no accepted result"
	case scope.Visibility == VisibilityProject && scope.Origin == OriginAttempt && scope.AcceptedResultRef == "":
		return "attempt origin reaches the project only with an accepted result"
	}
	return ""
}

func overIdentityBound(value string) bool { return len(value) > MaxIdentityBytes }

// sourceBoundExceeded reports a source ref or path above MaxTextFieldBytes,
// which storage cannot hold. A blob hash's grammar bounds it already.
func sourceBoundExceeded(sources []RecordSource) bool {
	return slices.ContainsFunc(sources, func(source RecordSource) bool {
		return len(source.Ref) > MaxTextFieldBytes || len(source.Path) > MaxTextFieldBytes
	})
}

// invalidCreationSources follows the memory_sources shapes: a file source has
// a path and a valid blob hash (ValidBlobHash), and its path is unique in the
// version; any other source has a ref and neither a path nor a blob hash.
func invalidCreationSources(sources []RecordSource) string {
	paths := make(map[string]bool)
	for _, source := range sources {
		switch {
		case !validSourceKind(source.Kind):
			return "source kind is unknown"
		case source.Kind == SourceFile && (source.Path == "" || !ValidBlobHash(source.BlobHash)):
			return "file source needs a path and a blob hash of 40 or 64 lowercase hex digits"
		case source.Kind == SourceFile && paths[source.Path]:
			return "file source path appears twice"
		case source.Kind != SourceFile && (source.Ref == "" || source.Path != "" || source.BlobHash != ""):
			return "non-file source needs a ref and no path or blob hash"
		}
		if source.Kind == SourceFile {
			paths[source.Path] = true
		}
	}
	return ""
}

package memory

import (
	"strings"
	"testing"
)

func attemptScope(visibility Visibility) Scope {
	scope := Scope{ProjectID: "p1", Visibility: visibility, Origin: OriginAttempt, RunID: "run", AttemptID: "a1", BaseCommit: "commit"}
	if visibility == VisibilityProject {
		scope.AcceptedResultRef = "result"
	}
	return scope
}

func creationFacts(producer Producer, change ...func(*RecordCreationFacts)) RecordCreationFacts {
	f := RecordCreationFacts{
		Producer:        producer,
		Actor:           ActorSystem,
		Author:          "agent",
		Category:        CategoryConvention,
		Content:         "Миграциите са адитивни",
		SearchTextBytes: int64(len("миграциите са адитивни")),
		Scope:           attemptScope(VisibilityAttempt),
		Sources: []RecordSource{
			{Kind: SourceFile, Path: "a.go", BlobHash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"},
			{Kind: SourceFile, Path: "b.go", BlobHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286", Ref: "line 3"},
			{Kind: SourceSpec, Ref: "specs/012"},
			{Kind: SourceEvent, Ref: "e1"},
		},
	}
	if producer == ProducerHuman {
		f.Actor, f.Author, f.Scope = ActorHuman, "martin", Scope{ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginHuman}
	}
	for _, c := range change {
		c(&f)
	}
	return f
}

func TestDecideRecordCreationAllows(t *testing.T) {
	tests := []struct {
		name  string
		facts RecordCreationFacts
		want  RecordCreationDecision
	}{
		{"agent proposal in its attempt", creationFacts(ProducerAgentProposal),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustAgentClaim, Status: StatusProposed}},
		{"agent proposal from an accepted result", creationFacts(ProducerAgentProposal, func(f *RecordCreationFacts) { f.Scope = attemptScope(VisibilityProject) }),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustAgentClaim, Status: StatusProposed}},
		{"agent proposal without sources", creationFacts(ProducerAgentProposal, func(f *RecordCreationFacts) { f.Sources = nil }),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustAgentClaim, Status: StatusProposed}},
		{"Conduit observation", creationFacts(ProducerConduitObservation),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustConduitObserved, Status: StatusProposed}},
		{"human text", creationFacts(ProducerHuman),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustHumanApproved, Status: StatusActive, CreatorApproves: true}},
		{"human text with base commit", creationFacts(ProducerHuman, func(f *RecordCreationFacts) { f.Scope.BaseCommit = "commit" }),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustHumanApproved, Status: StatusActive, CreatorApproves: true}},
		{"values at their storage bounds", creationFacts(ProducerAgentProposal, func(f *RecordCreationFacts) {
			f.Author = strings.Repeat("a", MaxIdentityBytes)
			f.Scope.ProjectID, f.Scope.RunID = strings.Repeat("p", MaxIdentityBytes), strings.Repeat("r", MaxIdentityBytes)
			f.Scope.AttemptID, f.Scope.BaseCommit = strings.Repeat("t", MaxIdentityBytes), strings.Repeat("c", MaxIdentityBytes)
			f.Sources = []RecordSource{
				{Kind: SourceFile, Path: strings.Repeat("f", MaxTextFieldBytes), BlobHash: strings.Repeat("b", 64), Ref: strings.Repeat("l", MaxTextFieldBytes)},
				{Kind: SourceSpec, Ref: strings.Repeat("s", MaxTextFieldBytes)},
			}
		}), RecordCreationDecision{Kind: CreationAllowed, Trust: TrustAgentClaim, Status: StatusProposed}},
		{"content at the storage bound", creationFacts(ProducerHuman, func(f *RecordCreationFacts) {
			f.Content, f.SearchTextBytes = strings.Repeat("a", MaxStoredContentBytes), MaxStoredContentBytes
		}), RecordCreationDecision{Kind: CreationAllowed, Trust: TrustHumanApproved, Status: StatusActive, CreatorApproves: true}},
	}
	for _, category := range Categories() {
		tests = append(tests, struct {
			name  string
			facts RecordCreationFacts
			want  RecordCreationDecision
		}{"category " + string(category), creationFacts(ProducerAgentProposal, func(f *RecordCreationFacts) { f.Category = category }),
			RecordCreationDecision{Kind: CreationAllowed, Trust: TrustAgentClaim, Status: StatusProposed}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRecordCreation(tt.facts)
			if got.Reason == "" {
				t.Fatal("decision has no reason")
			}
			got.Reason = ""
			if got != tt.want {
				t.Fatalf("decision = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecideRecordCreationRefuses(t *testing.T) {
	type change = func(*RecordCreationFacts)
	tests := []struct {
		name     string
		producer Producer
		changes  []change
		want     RecordCreationKind
	}{
		{"unknown producer", "code_derived", nil, CreationInvalidFacts},
		{"empty producer", "", nil, CreationInvalidFacts},
		{"unknown actor", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Actor = "operator" }}, CreationInvalidFacts},
		{"no author", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Author = "" }}, CreationInvalidFacts},
		{"negative search size", ProducerHuman, []change{func(f *RecordCreationFacts) { f.SearchTextBytes = -1 }}, CreationInvalidFacts},
		{"invalid facts precede actor", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Author, f.Actor = "", ActorAgent }}, CreationInvalidFacts},
		{"author over the identity bound", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Author = strings.Repeat("ж", MaxIdentityBytes/2) + "x" }}, CreationInvalidFacts},

		{"agent creates its own proposal", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Actor = ActorAgent }}, CreationActorNotAllowed},
		{"human claims an agent proposal", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Actor = ActorHuman }}, CreationActorNotAllowed},
		{"system writes human text", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Actor = ActorSystem }}, CreationActorNotAllowed},
		{"agent writes human text", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Actor = ActorAgent }}, CreationActorNotAllowed},
		{"actor precedes category", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Actor, f.Category = ActorAgent, "note" }}, CreationActorNotAllowed},

		{"unknown category", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Category = "architecture_decision" }}, CreationInvalidCategory},
		{"empty category", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Category = "" }}, CreationInvalidCategory},
		{"category precedes scope", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Category, f.Scope = "", Scope{} }}, CreationInvalidCategory},

		{"no project", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.ProjectID = "" }}, CreationInvalidScope},
		{"unknown visibility", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Scope.Visibility = "team" }}, CreationInvalidScope},
		{"agent proposal with human origin", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Scope = Scope{ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginHuman}
		}}, CreationInvalidScope},
		{"observation with human origin", ProducerConduitObservation, []change{func(f *RecordCreationFacts) {
			f.Scope = Scope{ProjectID: "p1", Visibility: VisibilityProject, Origin: OriginHuman}
		}}, CreationInvalidScope},
		{"human text with attempt origin", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope = attemptScope(VisibilityProject) }}, CreationInvalidScope},
		{"attempt origin without run", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Scope.RunID = "" }}, CreationInvalidScope},
		{"attempt origin without attempt", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Scope.AttemptID = "" }}, CreationInvalidScope},
		{"attempt origin without base commit", ProducerConduitObservation, []change{func(f *RecordCreationFacts) { f.Scope.BaseCommit = "" }}, CreationInvalidScope},
		{"human text with run", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.RunID = "run" }}, CreationInvalidScope},
		{"human text with attempt", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.AttemptID = "a1" }}, CreationInvalidScope},
		{"human text in an attempt", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.Visibility = VisibilityAttempt }}, CreationInvalidScope},
		{"human text with accepted result", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.AcceptedResultRef = "result" }}, CreationInvalidScope},
		{"project proposal without accepted result", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Scope = attemptScope(VisibilityProject)
			f.Scope.AcceptedResultRef = ""
		}}, CreationInvalidScope},
		{"ProjectID over the identity bound", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Scope.ProjectID = strings.Repeat("x", MaxIdentityBytes+1) }}, CreationInvalidScope},
		{"RunID over the identity bound", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Scope.RunID = strings.Repeat("x", MaxIdentityBytes+1) }}, CreationInvalidScope},
		{"AttemptID over the identity bound", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Scope.AttemptID = strings.Repeat("x", MaxIdentityBytes+1) }}, CreationInvalidScope},
		{"BaseCommit over the identity bound", ProducerConduitObservation, []change{func(f *RecordCreationFacts) { f.Scope.BaseCommit = strings.Repeat("x", MaxIdentityBytes+1) }}, CreationInvalidScope},
		{"accepted result ref over the identity bound", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Scope = attemptScope(VisibilityProject)
			f.Scope.AcceptedResultRef = strings.Repeat("x", MaxIdentityBytes+1)
		}}, CreationInvalidScope},
		{"scope bound precedes sources", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Scope.ProjectID = strings.Repeat("x", MaxIdentityBytes+1)
			f.Sources = []RecordSource{{Kind: "url", Ref: "x"}}
		}}, CreationInvalidScope},
		{"scope precedes sources", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Scope.RunID = "run"
			f.Sources = []RecordSource{{Kind: "url", Ref: "x"}}
		}}, CreationInvalidScope},

		{"unknown source kind", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: "url", Ref: "x"}} }}, CreationInvalidSource},
		{"file source without path", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, BlobHash: "e9d71f5ee7c92d6dc9e92ffdad17b8bd49418f98"}}
		}}, CreationInvalidSource},
		{"file source without blob hash", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go"}} }}, CreationInvalidSource},
		{"repeated file path", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "7e83ca2a65d6f90a809c8570c6c905a941b87732"}, {Kind: SourceFile, Path: "a.go", BlobHash: "32f28ea03b1b20126629d2ca63fc6665b0bbb604"}}
		}}, CreationInvalidSource},
		{"spec source without ref", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceSpec}} }}, CreationInvalidSource},
		{"event source with path", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceEvent, Ref: "e1", Path: "a.go"}} }}, CreationInvalidSource},
		{"assignment source with blob hash", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceAssignment, Ref: "as1", BlobHash: "e9d71f5ee7c92d6dc9e92ffdad17b8bd49418f98"}}
		}}, CreationInvalidSource},
		{"source ref over the text bound", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceSpec, Ref: strings.Repeat("x", MaxTextFieldBytes+1)}}
		}}, CreationInvalidSource},
		{"file path over the text bound in bytes", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: strings.Repeat("ж", MaxTextFieldBytes/2) + "x", BlobHash: "e9d71f5ee7c92d6dc9e92ffdad17b8bd49418f98"}}
		}}, CreationInvalidSource},
		{"blob hash over the identity bound", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: strings.Repeat("a", MaxIdentityBytes+1)}}
		}}, CreationInvalidSource},
		{"secret as blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "password=hunter2"}}
		}}, CreationInvalidSource},
		{"uppercase blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: strings.Repeat("A", 40)}}
		}}, CreationInvalidSource},
		{"39-digit blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: strings.Repeat("a", 39)}}
		}}, CreationInvalidSource},
		{"41-digit blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: strings.Repeat("a", 41)}}
		}}, CreationInvalidSource},
		{"63-digit blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: strings.Repeat("a", 63)}}
		}}, CreationInvalidSource},
		{"prefixed blob hash", ProducerAgentProposal, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "sha256:" + strings.Repeat("a", 64)}}
		}}, CreationInvalidSource},
		{"source bound precedes content", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources, f.Content = []RecordSource{{Kind: SourceSpec, Ref: strings.Repeat("x", MaxTextFieldBytes+1)}}, ""
		}}, CreationInvalidSource},
		{"sources precede content", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources, f.Content = []RecordSource{{Kind: SourceSpec}}, ""
		}}, CreationInvalidSource},

		{"empty content", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Content = "" }}, CreationEmptyContent},
		{"blank content", ProducerAgentProposal, []change{func(f *RecordCreationFacts) { f.Content = " \n\t" }}, CreationEmptyContent},
		{"empty precedes size", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Content, f.SearchTextBytes = "", MaxStoredContentBytes+1 }}, CreationEmptyContent},

		{"content over the bound", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Content, f.SearchTextBytes = strings.Repeat("a", MaxStoredContentBytes+1), MaxStoredContentBytes
		}}, CreationContentTooLarge},
		// Lowercasing can grow the bytes: content within the bound whose
		// normalized search text is not.
		{"search text over the bound", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Content, f.SearchTextBytes = strings.Repeat("a", MaxStoredContentBytes), MaxStoredContentBytes+1
		}}, CreationContentTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRecordCreation(creationFacts(tt.producer, tt.changes...))
			if got.Kind != tt.want || got.Reason == "" || got.Trust != "" || got.Status != "" || got.CreatorApproves {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

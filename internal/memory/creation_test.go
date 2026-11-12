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
			{Kind: SourceFile, Path: "a.go", BlobHash: "blob-a"},
			{Kind: SourceFile, Path: "b.go", BlobHash: "blob-b", Ref: "line 3"},
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
		{"scope precedes sources", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Scope.RunID = "run"
			f.Sources = []RecordSource{{Kind: "url", Ref: "x"}}
		}}, CreationInvalidScope},

		{"unknown source kind", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: "url", Ref: "x"}} }}, CreationInvalidSource},
		{"file source without path", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceFile, BlobHash: "b"}} }}, CreationInvalidSource},
		{"file source without blob hash", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go"}} }}, CreationInvalidSource},
		{"repeated file path", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceFile, Path: "a.go", BlobHash: "b1"}, {Kind: SourceFile, Path: "a.go", BlobHash: "b2"}}
		}}, CreationInvalidSource},
		{"spec source without ref", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceSpec}} }}, CreationInvalidSource},
		{"event source with path", ProducerHuman, []change{func(f *RecordCreationFacts) { f.Sources = []RecordSource{{Kind: SourceEvent, Ref: "e1", Path: "a.go"}} }}, CreationInvalidSource},
		{"assignment source with blob hash", ProducerHuman, []change{func(f *RecordCreationFacts) {
			f.Sources = []RecordSource{{Kind: SourceAssignment, Ref: "as1", BlobHash: "b"}}
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

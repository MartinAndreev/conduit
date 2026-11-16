package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

const testMarker render.Marker = "conduit-memory-000102030405060708090a0b0c0d0e0f"

func assignment(revision uint64) provider.AssignmentRef {
	return provider.AssignmentRef{ProjectID: "project", RunID: "run", AttemptID: "attempt", AssignmentID: "assignment", Revision: revision}
}

func startCommand() Command {
	return Command{
		IntentID:  "intent-start",
		Kind:      IntentStartTurn,
		SessionID: "session",
		Session:   provider.SessionRef{AdapterSessionID: "adapter-session"},
		Current:   assignment(3),
		ResultContract: provider.ResultContractRef{
			Name: "result", Version: "1", Schema: []byte(`{"type":"object"}`),
			Requested: provider.SchemaNative, AllowedFallbacks: []provider.SchemaEnforcement{provider.SchemaPromptOnly},
		},
		Parts:  []provider.InputPart{{Kind: provider.InputInstruction, Text: "do the work"}},
		Memory: MemoryInput{Kind: MemoryInputNone},
	}
}

func steerCommand() Command {
	return Command{
		IntentID:     "intent-steer",
		Kind:         IntentSteer,
		SessionID:    "session",
		Session:      provider.SessionRef{AdapterSessionID: "adapter-session"},
		Current:      assignment(3),
		New:          assignment(4),
		ExpectedTurn: provider.TurnRef{AdapterTurnID: "turn-a"},
		Mode:         provider.DeliverActiveTurn,
		Parts:        []provider.InputPart{{Kind: provider.InputInstruction, Text: "correct course"}},
		Memory:       MemoryInput{Kind: MemoryInputNone},
	}
}

func composedRequest() compose.Request {
	return compose.Request{ProjectID: "project", AttemptID: "attempt", Snapshot: "snapshot", Marker: testMarker}
}

// composedComposition is a Composed outcome over two items with text whose
// hash it reports.
func composedComposition(text string) compose.Composition {
	return compose.Composition{
		Kind:     compose.Composed,
		Part:     provider.InputPart{Kind: provider.InputContext, Text: text},
		TextHash: PayloadHash(text),
		Items: []compose.ItemResult{
			{RecordID: "r1", VersionID: "r1-v1", Reason: "linked by the assignment", Mode: render.ModeIncludedText, Trust: memory.TrustHumanApproved, PolicyVersion: "1"},
			{RecordID: "r2", VersionID: "r2-v3", Reason: "found by search", Mode: render.ModeReferenced, Trust: memory.TrustConduitObserved, PolicyVersion: "1"},
		},
		Exclusions: []compose.Exclusion{{RecordID: "r9", VersionID: "r9-v1", Stage: compose.StageEligibility, Kind: "not_retrievable", Reason: "archived"}},
		Search:     compose.SearchOutcome{Status: compose.SearchOK},
		Expected: []memory.ExpectedRecordVersion{
			{RecordID: "r1", VersionID: "r1-v1", RecordVersion: 2},
			{RecordID: "r2", VersionID: "r2-v3", RecordVersion: 5},
		},
	}
}

type emptyMemory struct{}

func (emptyMemory) ReadLinkedRecords(context.Context, string, []string) ([]compose.Candidate, error) {
	return nil, nil
}

func (emptyMemory) SearchRecords(context.Context, compose.SearchQuery) (compose.SearchResult, error) {
	return compose.SearchResult{}, nil
}

type noSnapshot struct{}

func (noSnapshot) ObserveFiles(_ context.Context, _ compose.SnapshotRef, paths []string) ([]compose.FileObservation, error) {
	return make([]compose.FileObservation, len(paths)), nil
}

// realEmptyComposition is the ComposedEmpty DTO the real Composer returns
// when nothing is linked and search is disabled.
func realEmptyComposition(t *testing.T) compose.Composition {
	t.Helper()
	composer, err := compose.New(emptyMemory{}, noSnapshot{}, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composer.ComposeContext(context.Background(), composedRequest())
	if err != nil {
		t.Fatal(err)
	}
	if composition.Kind != compose.ComposedEmpty {
		t.Fatalf("composition = %+v", composition)
	}
	return composition
}

func fill(size int) string { return strings.Repeat("x", size) }

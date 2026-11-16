package execution

import (
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/sessions"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

var startedAt = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

func binding() SessionBinding {
	return SessionBinding{Assignment: assignment(3), AdapterSessionID: "adapter-session"}
}

func acceptedStart(sequence int64, turn provider.AdapterTurnID) AcceptedStart {
	return AcceptedStart{SessionSequence: sequence, AdapterSessionID: "adapter-session", Assignment: assignment(3), AdapterTurnID: turn, CreatedAt: startedAt}
}

// newIntentFacts are the facts of a new steer with no memory that records.
func newIntentFacts(c Command) IntentFacts {
	return IntentFacts{
		Command:     c,
		RequestHash: RequestHash(c),
		Stored: StoredSessionFacts{
			SessionFound:   true,
			Session:        binding(),
			AcceptedStarts: []AcceptedStart{acceptedStart(1, "turn-a")},
		},
	}
}

func composedSteer() (Command, string) {
	c := steerCommand()
	c.Memory = MemoryInput{Kind: MemoryInputComposed, Request: composedRequest(), Composition: composedComposition("memory")}
	m := DecideManifestEnvelope(ManifestFacts{Assignment: c.New, Request: c.Memory.Request, Composition: c.Memory.Composition, MaxPayloadBytes: MaxManifestPayloadBytes}).Manifest
	return c, m.ManifestHash
}

func currentStates() []memory.CurrentRecordState {
	return []memory.CurrentRecordState{
		{RecordID: "r1", Status: memory.StatusActive, CurrentVersionID: "r1-v1", RecordVersion: 2},
		{RecordID: "r2", Status: memory.StatusActive, CurrentVersionID: "r2-v3", RecordVersion: 5},
	}
}

func TestDecideDispatchIntentExistingID(t *testing.T) {
	composed, manifestHash := composedSteer()
	stored := steerCommand()
	stored.Memory = MemoryInput{Kind: MemoryInputStored}
	none := steerCommand()
	for _, tc := range []struct {
		name     string
		command  Command
		manifest string
		existing ExistingIntent
		want     IntentDecisionKind
	}{
		{"none, same", none, "", ExistingIntent{RequestHash: RequestHash(none)}, IntentExisting},
		{"none, other request", none, "", ExistingIntent{RequestHash: RequestHash(startCommand())}, IntentIDConflict},
		{"none, stored has manifest", none, "", ExistingIntent{RequestHash: RequestHash(none), HasManifest: true, ManifestHash: manifestHash}, IntentIDConflict},
		{"composed, same", composed, manifestHash, ExistingIntent{RequestHash: RequestHash(composed), HasManifest: true, ManifestHash: manifestHash}, IntentExisting},
		{"composed, other manifest", composed, manifestHash, ExistingIntent{RequestHash: RequestHash(composed), HasManifest: true, ManifestHash: RequestHash(none)}, IntentIDConflict},
		{"composed, other request", composed, manifestHash, ExistingIntent{RequestHash: RequestHash(none), HasManifest: true, ManifestHash: manifestHash}, IntentIDConflict},
		{"composed, stored without manifest", composed, manifestHash, ExistingIntent{RequestHash: RequestHash(composed)}, IntentIDConflict},
		{"stored, same", stored, "", ExistingIntent{RequestHash: RequestHash(stored), HasManifest: true, ManifestHash: manifestHash}, IntentExisting},
		{"stored, other request", stored, "", ExistingIntent{RequestHash: RequestHash(none), HasManifest: true, ManifestHash: manifestHash}, IntentIDConflict},
		{"stored, no manifest", stored, "", ExistingIntent{RequestHash: RequestHash(stored)}, IntentStoredWithoutManifest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The existing phase ignores every other fact: no session, an
			// open intent and a recheck that would recompose.
			facts := IntentFacts{
				Command: tc.command, RequestHash: RequestHash(tc.command), ManifestHash: tc.manifest,
				ExistingFound: true, Existing: tc.existing,
				Stored:  StoredSessionFacts{OpenIntent: true},
				Recheck: memory.ManifestRecheckFacts{Expected: tc.command.Memory.Composition.Expected},
			}
			if got := DecideDispatchIntent(facts); got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestDecideDispatchIntentNewIDOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*IntentFacts)
		want   IntentDecisionKind
	}{
		{"records", func(*IntentFacts) {}, IntentRecord},
		{"no request hash", func(f *IntentFacts) { f.RequestHash = "" }, IntentInvalidFacts},
		{"unknown memory", func(f *IntentFacts) { f.Command.Memory.Kind = "later" }, IntentInvalidFacts},
		{"composed without manifest hash", func(f *IntentFacts) { f.Command.Memory.Kind = MemoryInputComposed }, IntentInvalidFacts},
		{"duplicate start sequence", func(f *IntentFacts) {
			f.Stored.AcceptedStarts = append(f.Stored.AcceptedStarts, acceptedStart(1, "turn-b"))
		}, IntentInvalidFacts},
		{"stored before session", func(f *IntentFacts) {
			f.Command.Memory.Kind = MemoryInputStored
			f.Stored.SessionFound = false
		}, IntentStoredNotFound},
		{"no session before binding", func(f *IntentFacts) {
			f.Stored.SessionFound = false
			f.Stored.Session.AdapterSessionID = "other"
		}, IntentSessionNotFound},
		{"other adapter session", func(f *IntentFacts) { f.Stored.Session.AdapterSessionID = "other" }, IntentBindingMismatch},
		{"other assignment before revision", func(f *IntentFacts) {
			f.Stored.Session.Assignment.AssignmentID = "other"
			f.Stored.Session.Assignment.Revision = 9
		}, IntentBindingMismatch},
		{"stale revision before correction", func(f *IntentFacts) { f.Stored.Session.Assignment.Revision = 2 }, IntentStaleRevision},
		{"correction skips a revision", func(f *IntentFacts) { f.Command.New.Revision = 5 }, IntentCorrectionRefused},
		{"correction changes identity", func(f *IntentFacts) { f.Command.New.AttemptID = "other" }, IntentCorrectionRefused},
		{"no start", func(f *IntentFacts) { f.Stored.AcceptedStarts = nil }, IntentTurnUnknown},
		{"start of another adapter session", func(f *IntentFacts) { f.Stored.AcceptedStarts[0].AdapterSessionID = "old" }, IntentTurnUnknown},
		{"start of another assignment", func(f *IntentFacts) { f.Stored.AcceptedStarts[0].Assignment.AssignmentID = "old" }, IntentTurnUnknown},
		{"start above the current revision", func(f *IntentFacts) { f.Stored.AcceptedStarts[0].Assignment.Revision = 4 }, IntentTurnUnknown},
		{"start at an earlier revision", func(f *IntentFacts) { f.Stored.AcceptedStarts[0].Assignment.Revision = 1 }, IntentRecord},
		{"other turn", func(f *IntentFacts) { f.Command.ExpectedTurn.AdapterTurnID = "turn-z" }, IntentTurnMismatch},
		{"turn before open intent", func(f *IntentFacts) {
			f.Stored.AcceptedStarts = nil
			f.Stored.OpenIntent = true
		}, IntentTurnUnknown},
		{"open intent", func(f *IntentFacts) { f.Stored.OpenIntent = true }, IntentBlockedByOpenIntent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := newIntentFacts(steerCommand())
			tc.change(&facts)
			got := DecideDispatchIntent(facts)
			if got.Kind != tc.want || got.Reason == "" {
				t.Fatalf("decision = %+v", got)
			}
			if tc.want == IntentCorrectionRefused && got.Correction == "" {
				t.Fatalf("correction kind missing: %+v", got)
			}
		})
	}
}

func TestDecideDispatchIntentStartTurn(t *testing.T) {
	facts := newIntentFacts(startCommand())
	facts.Stored.AcceptedStarts = nil
	if got := DecideDispatchIntent(facts); got.Kind != IntentRecord {
		t.Fatalf("start = %+v", got)
	}
	facts.Stored.OpenIntent = true
	if got := DecideDispatchIntent(facts); got.Kind != IntentBlockedByOpenIntent {
		t.Fatalf("start with open intent = %+v", got)
	}
}

// The recheck runs last and only for a new composed command.
func TestDecideDispatchIntentRecheck(t *testing.T) {
	c, manifestHash := composedSteer()
	facts := newIntentFacts(c)
	facts.ManifestHash = manifestHash
	facts.Recheck = memory.ManifestRecheckFacts{Expected: c.Memory.Composition.Expected, Current: currentStates()}
	if got := DecideDispatchIntent(facts); got.Kind != IntentRecord {
		t.Fatalf("unchanged = %+v", got)
	}
	changed := facts
	changed.Recheck.Current = currentStates()
	changed.Recheck.Current[1].RecordVersion = 6
	if got := DecideDispatchIntent(changed); got.Kind != IntentRecompose || got.Recheck.Change != memory.ChangeRecordChanged || got.Recheck.RecordID != "r2" {
		t.Fatalf("changed = %+v", got)
	}
	deleted := facts
	deleted.Recheck.Current = currentStates()
	deleted.Recheck.Current[0].ContentDeleted = true
	if got := DecideDispatchIntent(deleted); got.Kind != IntentRecompose || got.Recheck.Change != memory.ChangeContentDeleted {
		t.Fatalf("content deleted = %+v", got)
	}
	blocked := changed
	blocked.Stored.OpenIntent = true
	if got := DecideDispatchIntent(blocked); got.Kind != IntentBlockedByOpenIntent {
		t.Fatalf("open intent before recheck = %+v", got)
	}
	invalid := facts
	invalid.Recheck.Current = append(currentStates(), currentStates()[0])
	if got := DecideDispatchIntent(invalid); got.Kind != IntentInvalidFacts {
		t.Fatalf("invalid recheck = %+v", got)
	}
}

// P5: the target turn is the latest accepted start by session sequence on
// the same adapter session and assignment identity. Rejected and invalidated
// starts are never accepted starts, so they do not replace it.
func TestDecideDispatchIntentTargetTurn(t *testing.T) {
	steer := func(starts []AcceptedStart, open bool, turn provider.AdapterTurnID) IntentDecisionKind {
		c := steerCommand()
		c.ExpectedTurn.AdapterTurnID = turn
		facts := newIntentFacts(c)
		facts.Stored.AcceptedStarts = starts
		facts.Stored.OpenIntent = open
		return DecideDispatchIntent(facts).Kind
	}
	a := acceptedStart(1, "turn-a")
	// accepted A, then B rejected or invalidated: only A is an accepted start.
	if got := steer([]AcceptedStart{a}, false, "turn-a"); got != IntentRecord {
		t.Fatalf("A after rejected or invalidated B = %s", got)
	}
	// accepted A, then accepted B: B defines the turn.
	b := acceptedStart(2, "turn-b")
	if got := steer([]AcceptedStart{a, b}, false, "turn-a"); got != IntentTurnMismatch {
		t.Fatalf("A after accepted B = %s", got)
	}
	if got := steer([]AcceptedStart{b, a}, false, "turn-b"); got != IntentRecord {
		t.Fatalf("B listed first = %s", got)
	}
	// accepted A, then B dispatching: the open intent blocks.
	if got := steer([]AcceptedStart{a}, true, "turn-a"); got != IntentBlockedByOpenIntent {
		t.Fatalf("A with open B = %s", got)
	}
	// Equal creation times: the session sequence decides.
	late := acceptedStart(7, "turn-late")
	early := acceptedStart(3, "turn-early")
	late.CreatedAt, early.CreatedAt = startedAt, startedAt
	if got := steer([]AcceptedStart{late, early}, false, "turn-late"); got != IntentRecord {
		t.Fatalf("equal times = %s", got)
	}
	// A later start on another adapter session or assignment is ignored.
	other := acceptedStart(9, "turn-other")
	other.AdapterSessionID = "replacement"
	identity := acceptedStart(10, "turn-identity")
	identity.Assignment.AssignmentID = "other"
	if got := steer([]AcceptedStart{a, other, identity}, false, "turn-a"); got != IntentRecord {
		t.Fatalf("foreign later starts = %s", got)
	}
}

func TestDecideDispatchIntentCorrectionKinds(t *testing.T) {
	facts := newIntentFacts(steerCommand())
	facts.Command.New.Revision = 3
	if got := DecideDispatchIntent(facts); got.Kind != IntentCorrectionRefused || got.Correction != sessions.CorrectionInvalidNextRevision {
		t.Fatalf("decision = %+v", got)
	}
}

// Two empty compositions that differ only in their exclusions are different
// manifests under one intent ID.
func TestDecideDispatchIntentEmptyCompositionsConflict(t *testing.T) {
	empty := realEmptyComposition(t)
	other := empty
	other.Exclusions = []compose.Exclusion{{RecordID: "r1", Stage: compose.StageGather, Kind: compose.ExcludedNotFound, Reason: "missing"}}
	hash := func(c compose.Composition) string {
		return DecideManifestEnvelope(ManifestFacts{Assignment: assignment(3), Request: composedRequest(), Composition: c, MaxPayloadBytes: MaxManifestPayloadBytes}).Manifest.ManifestHash
	}
	command := startCommand()
	command.Memory = MemoryInput{Kind: MemoryInputComposed, Request: composedRequest(), Composition: other}
	facts := IntentFacts{
		Command: command, RequestHash: RequestHash(command), ManifestHash: hash(other), ExistingFound: true,
		Existing: ExistingIntent{RequestHash: RequestHash(command), HasManifest: true, ManifestHash: hash(empty)},
	}
	if got := DecideDispatchIntent(facts); got.Kind != IntentIDConflict {
		t.Fatalf("decision = %+v", got)
	}
	facts.Existing.ManifestHash = hash(other)
	if got := DecideDispatchIntent(facts); got.Kind != IntentExisting {
		t.Fatalf("same empty composition = %+v", got)
	}
}

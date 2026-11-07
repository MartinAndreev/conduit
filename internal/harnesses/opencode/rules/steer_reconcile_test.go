package rules

import "testing"

func TestDecideSteerReconcileTrigger(t *testing.T) {
	tests := []struct {
		name  string
		facts SteerReconcileTriggerFacts
		want  SteerReconcileTriggerKind
	}{
		{"ended turn with unresolved steer", SteerReconcileTriggerFacts{SteerUnresolved: true}, SteerReconcileStart},
		{"nothing unresolved", SteerReconcileTriggerFacts{}, SteerReconcileSkip},
		{"turn still observed", SteerReconcileTriggerFacts{SteerUnresolved: true, ActiveTurn: true}, SteerReconcileSkip},
		{"already querying", SteerReconcileTriggerFacts{SteerUnresolved: true, Reconciling: true}, SteerReconcileSkip},
		{"closed or lost", SteerReconcileTriggerFacts{SteerUnresolved: true, Unavailable: true}, SteerReconcileSkip},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideSteerReconcileTrigger(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideSteerReconcile(t *testing.T) {
	current := SteerReconcileFacts{SameSteerUnresolved: true, RevisionMatches: true}
	tests := []struct {
		name   string
		change func(*SteerReconcileFacts)
		want   SteerReconcileKind
	}{
		{"in history is delivered", func(f *SteerReconcileFacts) { f.InHistory = true }, SteerReconcileDelivered},
		{"absent everywhere is not delivered", func(f *SteerReconcileFacts) {}, SteerReconcileNotDelivered},
		{"pending in inbox keeps doubt", func(f *SteerReconcileFacts) { f.InInbox = true }, SteerReconcileDoubtRemains},
		{"query failure keeps doubt", func(f *SteerReconcileFacts) { f.QueryFailed = true; f.InHistory = true }, SteerReconcileDoubtRemains},
		{"released meanwhile precedes everything", func(f *SteerReconcileFacts) { f.SameSteerUnresolved = false; f.InHistory = true }, SteerReconcileSuperseded},
		{"delivered with changed revision keeps doubt", func(f *SteerReconcileFacts) { f.InHistory = true; f.RevisionMatches = false }, SteerReconcileDoubtRemains},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := current
			tt.change(&facts)
			if got := DecideSteerReconcile(facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestDecideReleasedSteerSettled(t *testing.T) {
	tests := []struct {
		name  string
		facts ReleasedSteerFacts
		want  ReleasedSteerKind
	}{
		{"no longer retained", ReleasedSteerFacts{InHistory: true, IdleAfterMessage: true}, ReleasedSteerSuperseded},
		{"query failure", ReleasedSteerFacts{SameReleased: true, QueryFailed: true, InHistory: true, IdleAfterMessage: true}, ReleasedSteerMayRun},
		{"delivered alone never settles", ReleasedSteerFacts{SameReleased: true, InHistory: true}, ReleasedSteerMayRun},
		{"delivered and an execution ended after it", ReleasedSteerFacts{SameReleased: true, InHistory: true, IdleAfterMessage: true}, ReleasedSteerFinished},
		{"still in inbox", ReleasedSteerFacts{SameReleased: true, InInbox: true}, ReleasedSteerMayRun},
		{"never admitted", ReleasedSteerFacts{SameReleased: true}, ReleasedSteerFinished},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideReleasedSteerSettled(tt.facts); got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want %s", got, tt.want)
			}
		})
	}
}

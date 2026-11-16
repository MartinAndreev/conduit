package rules

import "testing"

func TestDecidePermissionProfile(t *testing.T) {
	tests := []struct {
		name  string
		facts PermissionProfileFacts
		want  PermissionProfileKind
	}{
		{"workspace-write, writable binding", PermissionProfileFacts{ModeWorkspaceWrite: true, WorkspaceWritable: true}, PermissionProfileWorkspaceWrite},
		{"workspace-write, read-only binding", PermissionProfileFacts{ModeWorkspaceWrite: true}, PermissionProfileConflict},
		{"read-only, writable binding", PermissionProfileFacts{ModeReadOnly: true, WorkspaceWritable: true}, PermissionProfileWorkspaceRead},
		{"read-only, read-only binding", PermissionProfileFacts{ModeReadOnly: true}, PermissionProfileWorkspaceRead},
		{"no mode", PermissionProfileFacts{WorkspaceWritable: true}, PermissionProfileUnsupportedMode},
		{"both modes", PermissionProfileFacts{ModeReadOnly: true, ModeWorkspaceWrite: true, WorkspaceWritable: true}, PermissionProfileUnsupportedMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecidePermissionProfile(tt.facts)
			if decision.Kind != tt.want || decision.Reason == "" {
				t.Fatalf("decision = %#v, want %s", decision, tt.want)
			}
		})
	}
}

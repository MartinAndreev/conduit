package rules

import "testing"

func TestDecideSandbox(t *testing.T) {
	ready := SandboxFacts{Darwin: true, SandboxExecFound: true, HomeResolved: true, ProtectedPath: "/Users/u/.conduit"}
	with := func(change func(*SandboxFacts)) SandboxFacts {
		facts := ready
		change(&facts)
		return facts
	}
	tests := []struct {
		name  string
		facts SandboxFacts
		want  SandboxKind
	}{
		{"darwin, everything resolved", ready, SandboxApply},
		{"path with spaces and unicode", with(func(f *SandboxFacts) { f.ProtectedPath = "/Users/Мартин A/.conduit" }), SandboxApply},
		{"not darwin", with(func(f *SandboxFacts) { f.Darwin = false }), SandboxUnsupportedPlatform},
		{"not darwin takes precedence", SandboxFacts{}, SandboxUnsupportedPlatform},
		{"sandbox-exec missing", with(func(f *SandboxFacts) { f.SandboxExecFound = false }), SandboxExecMissing},
		{"sandbox-exec missing before home", with(func(f *SandboxFacts) { f.SandboxExecFound, f.HomeResolved = false, false }), SandboxExecMissing},
		{"home unresolved", with(func(f *SandboxFacts) { f.HomeResolved, f.ProtectedPath = false, "" }), SandboxHomeUnresolved},
		{"double quote", with(func(f *SandboxFacts) { f.ProtectedPath = `/Users/a"b/.conduit` }), SandboxPathInadmissible},
		{"backslash", with(func(f *SandboxFacts) { f.ProtectedPath = `/Users/a\b/.conduit` }), SandboxPathInadmissible},
		{"newline", with(func(f *SandboxFacts) { f.ProtectedPath = "/Users/a\nb/.conduit" }), SandboxPathInadmissible},
		{"NUL", with(func(f *SandboxFacts) { f.ProtectedPath = "/Users/a\x00b/.conduit" }), SandboxPathInadmissible},
		{"DEL", with(func(f *SandboxFacts) { f.ProtectedPath = "/Users/a\x7fb/.conduit" }), SandboxPathInadmissible},
		{"relative", with(func(f *SandboxFacts) { f.ProtectedPath = "Users/u/.conduit" }), SandboxPathInadmissible},
		{"empty path", with(func(f *SandboxFacts) { f.ProtectedPath = "" }), SandboxPathInadmissible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecideSandbox(tt.facts)
			if decision.Kind != tt.want || decision.Reason == "" || decision.Applied() != (tt.want == SandboxApply) {
				t.Fatalf("decision = %#v, want %s", decision, tt.want)
			}
		})
	}
}

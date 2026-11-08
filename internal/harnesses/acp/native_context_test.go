package acp

import (
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestSnapshotReportsNativeContextDefaults(t *testing.T) {
	// An observed session/new with mcpServers:[] must not change the state.
	adapter := New(testDescriptor(), Config{})
	report := adapter.capabilities(provider.ModelSelection{}, AgentFacts{Observed: true, AgentVersion: "1.0.0"}, time.Now()).NativeContext
	for _, source := range provider.NativeContextSources() {
		if got := report.SourceState(source); got != provider.NativeContextPossible {
			t.Fatalf("%s = %s, want possible", source, got)
		}
	}
	if len(report.Sources) != len(provider.NativeContextSources()) {
		t.Fatalf("sources = %+v", report.Sources)
	}
	if report.ProtectedPaths != provider.ProtectedPathsUnknown {
		t.Fatalf("protected paths = %s, want unknown", report.ProtectedPaths)
	}
}

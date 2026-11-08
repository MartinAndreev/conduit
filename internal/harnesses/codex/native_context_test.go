package codex

import (
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

func TestSnapshotReportsNativeContextDefaults(t *testing.T) {
	report := codexCapabilities(provider.ModelSelection{}, time.Now()).NativeContext
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

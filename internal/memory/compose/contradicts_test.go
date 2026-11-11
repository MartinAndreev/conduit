package compose

import (
	"slices"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// A record whose contradicts relations were not all read is labelled, even
// when every relation that was read points at retired memory.
func TestTruncatedContradictsLabelConflict(t *testing.T) {
	truncated := func(c *Candidate) { c.ContradictsTruncated = true }
	mem := &fakeMemory{versions: []Candidate{
		note("truncated", "deploy truncated", contradicts("archived", memory.StatusArchived), truncated),
		note("bare", "deploy bare", truncated),
		note("complete", "deploy complete", contradicts("archived", memory.StatusArchived)),
	}}
	got := compose(t, mem, request(query("deploy"), linked("bare")))
	wantKind(t, got, Composed)
	labelled := map[string]bool{}
	for _, item := range got.Items {
		labelled[item.RecordID] = slices.Contains(item.Labels, memory.LabelConflict)
	}
	if !labelled["truncated"] || !labelled["bare"] || labelled["complete"] || len(labelled) != 3 {
		t.Fatalf("conflict labels = %v", labelled)
	}
}

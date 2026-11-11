package compose

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// withheldContent describes stored content the reader did not load because
// it is above the per-item limit.
func withheldContent(bytes int64) func(*Candidate) {
	return func(c *Candidate) { c.Version.Content, c.ContentBytes = "", bytes }
}

// withheldSources describes stored sources the reader did not load.
func withheldSources(count, bytes int64) func(*Candidate) {
	return func(c *Candidate) { c.Sources, c.SourceCount, c.SourceBytes = nil, count, bytes }
}

func manySources(count int) func(*Candidate) {
	return func(c *Candidate) {
		for i := range count {
			c.Sources = append(c.Sources, memory.RecordSource{Kind: memory.SourceSpec, Ref: "specs/" + strings.Repeat("s", i+1)})
		}
	}
}

// humanApproved makes c a human note approved in its exact version, which
// passes eligibility without a file source.
func humanApproved(c *Candidate) {
	c.Version.Trust, c.ExactVersionApproved = memory.TrustHumanApproved, true
	c.Version.Scope = memory.Scope{ProjectID: "p1", Visibility: memory.VisibilityProject, Origin: memory.OriginHuman}
}

// A linked record that is too large, or was not read in full, blocks; it is
// never sent with partial text or sources. The size check runs before
// eligibility, so withheld sources are reported as oversized whatever the
// record's trust, and the snapshot is never asked about them.
func TestMandatoryMaterialOverSizeLimitsBlocks(t *testing.T) {
	tests := []struct {
		name   string
		change []func(*Candidate)
		want   Kind
		stage  Stage
		kind   ExclusionKind
	}{
		{"withheld content", []func(*Candidate){withheldContent(memory.MaxMaterialBytes + 1)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"withheld sources by count", []func(*Candidate){withheldSources(memory.MaxSourcesPerItem+1, 100)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"withheld sources by bytes", []func(*Candidate){withheldSources(1, memory.MaxSourceBytes+1)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"oversized and not eligible", []func(*Candidate){withheldContent(memory.MaxMaterialBytes + 1), func(c *Candidate) { c.Version.Trust = memory.TrustAgentClaim }}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"human approved, withheld sources by count", []func(*Candidate){humanApproved, withheldSources(memory.MaxSourcesPerItem+1, 100)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"human approved, withheld sources by bytes", []func(*Candidate){humanApproved, withheldSources(1, memory.MaxSourceBytes+1)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"loaded huge source ref", []func(*Candidate){func(c *Candidate) {
			c.Sources = append(c.Sources, memory.RecordSource{Kind: memory.SourceEvent, Ref: strings.Repeat("р", memory.MaxSourceBytes)})
		}}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"loaded too many sources", []func(*Candidate){manySources(memory.MaxSourcesPerItem)}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeBlockedOversizedMandatory)},
		{"content partly read", []func(*Candidate){func(c *Candidate) { c.ContentBytes = int64(len(c.Version.Content)) + 1 }}, BlockedMandatoryOversized, StageSize, ExclusionKind(memory.SizeInvalidFacts)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := &fakeMemory{versions: []Candidate{note("r1", "deploy", tt.change...)}}
			snap := &fakeSnapshot{}
			got := composeWith(t, mem, snap, nil, request(linked("r1")))
			wantKind(t, got, tt.want)
			wantExcluded(t, got, "r1-v1", tt.stage, tt.kind)
			if !exclusion(t, got, "r1-v1").Mandatory || got.Part != (provider.InputPart{}) || got.Items != nil {
				t.Fatalf("blocked composition carries material: %+v", got)
			}
			if len(snap.calls) != 0 {
				t.Fatalf("snapshot was read for material over the limits: %v", snap.calls)
			}
		})
	}
}

func TestOptionalMaterialOverSizeLimitsIsExcluded(t *testing.T) {
	mem := &fakeMemory{
		versions: []Candidate{note("ok", "deploy")},
		extraHits: []SearchHit{
			{Candidate: note("text", "", withheldContent(memory.MaxMaterialBytes+1)), MatchedTerms: 1},
			{Candidate: note("count", "deploy", withheldSources(memory.MaxSourcesPerItem+1, 100)), MatchedTerms: 1},
			{Candidate: note("bytes", "deploy", withheldSources(1, memory.MaxSourceBytes+1)), MatchedTerms: 1},
			{Candidate: note("partial", "deploy", func(c *Candidate) { c.ContentBytes = 100 }), MatchedTerms: 1},
		},
	}
	got := compose(t, mem, request(query("deploy")))
	wantKind(t, got, Composed)
	if !slices.Equal(itemIDs(got), []string{"ok"}) {
		t.Fatalf("items = %v", itemIDs(got))
	}
	wantExcluded(t, got, "text-v1", StageSize, ExclusionKind(memory.SizeOversized))
	wantExcluded(t, got, "partial-v1", StageSize, ExclusionKind(memory.SizeInvalidFacts))
	for _, id := range []string{"count-v1", "bytes-v1"} {
		wantExcluded(t, got, id, StageSize, ExclusionKind(memory.SizeOversized))
		if x := exclusion(t, got, id); x.Mandatory {
			t.Fatalf("%s exclusion = %+v", id, x)
		}
	}
}

// A record excluded for size is never evaluated, so as a counterpart it
// labels while its stored status is retrievable, and not once it is retired.
func TestOversizedCounterpartIsUnevaluated(t *testing.T) {
	mem := &fakeMemory{versions: []Candidate{
		note("live", "deploy live", contradicts("hugelive", memory.StatusActive)),
		note("hugelive", "", withheldContent(memory.MaxMaterialBytes+1), contradicts("live", memory.StatusActive)),
		note("calm", "deploy calm", contradicts("hugeold", memory.StatusArchived)),
	}}
	// The withheld record has no loaded text to match, so it comes as a hit.
	mem.extraHits = []SearchHit{{Candidate: mem.versions[1], MatchedTerms: 1}}
	snap := &fakeSnapshot{}
	got := composeWith(t, mem, snap, nil, request(query("deploy")))
	wantKind(t, got, Composed)
	wantExcluded(t, got, "hugelive-v1", StageSize, ExclusionKind(memory.SizeOversized))
	labels := map[string]bool{}
	for _, item := range got.Items {
		labels[item.RecordID] = slices.Contains(item.Labels, memory.LabelConflict)
	}
	if !maps.Equal(labels, map[string]bool{"live": true, "calm": false}) {
		t.Fatalf("conflict labels = %v", labels)
	}
	if slices.Contains(snap.calls, "snap1:docs/hugelive.md") {
		t.Fatalf("snapshot was read for oversized material: %v", snap.calls)
	}
}

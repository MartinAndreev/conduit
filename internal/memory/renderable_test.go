package memory

import "testing"

func TestDecideRenderableItem(t *testing.T) {
	file := RecordSource{Kind: SourceFile, Path: "a.go", BlobHash: "h"}
	event := RecordSource{Kind: SourceEvent, Ref: "e1"}
	valid := func(change func(*RenderableItemFacts)) RenderableItemFacts {
		f := RenderableItemFacts{Trust: TrustHumanApproved, Labels: RetrievalLabels(), Sources: []RecordSource{file, event}}
		change(&f)
		return f
	}
	tests := []struct {
		name  string
		facts RenderableItemFacts
		want  RenderableItemKind
	}{
		{"every label and source kind", valid(func(*RenderableItemFacts) {}), RenderableItemAccepted},
		{"no labels", valid(func(f *RenderableItemFacts) { f.Labels = nil }), RenderableItemAccepted},
		{"file source with ref", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Kind: SourceFile, Ref: "r", Path: "a.go"}} }), RenderableItemAccepted},
		{"spec and assignment refs", valid(func(f *RenderableItemFacts) {
			f.Sources = []RecordSource{{Kind: SourceSpec, Ref: "s"}, {Kind: SourceAssignment, Ref: "a"}}
		}), RenderableItemAccepted},
		{"unknown trust", valid(func(f *RenderableItemFacts) { f.Trust = "admin" }), RenderableItemInvalidFacts},
		{"unknown label", valid(func(f *RenderableItemFacts) { f.Labels = []RetrievalLabel{"trusted"} }), RenderableItemInvalidFacts},
		{"duplicate label", valid(func(f *RenderableItemFacts) { f.Labels = []RetrievalLabel{LabelConflict, LabelConflict} }), RenderableItemInvalidFacts},
		{"no sources", valid(func(f *RenderableItemFacts) { f.Sources = nil }), RenderableItemInvalidFacts},
		{"unknown source kind", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Kind: "url", Ref: "x"}} }), RenderableItemInvalidFacts},
		{"empty source kind", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Ref: "x"}} }), RenderableItemInvalidFacts},
		{"file source without path", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Kind: SourceFile, Ref: "x"}} }), RenderableItemInvalidFacts},
		{"event source without ref", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Kind: SourceEvent}} }), RenderableItemInvalidFacts},
		{"spec source with path", valid(func(f *RenderableItemFacts) { f.Sources = []RecordSource{{Kind: SourceSpec, Ref: "s", Path: "p"}} }), RenderableItemInvalidFacts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideRenderableItem(tt.facts)
			if got.Kind != tt.want || got.Reason == "" {
				t.Fatalf("decision = %+v, want kind %q with a reason", got, tt.want)
			}
		})
	}
}

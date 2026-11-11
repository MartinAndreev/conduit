package memory

import "slices"

// RenderableItemFacts are the domain facts of one admitted item about to be
// shown to the agent. A source's blob hash is not shown, so it is not checked.
type RenderableItemFacts struct {
	Trust   Trust
	Labels  []RetrievalLabel
	Sources []RecordSource
}

type RenderableItemKind string

const (
	RenderableItemAccepted     RenderableItemKind = "renderable"
	RenderableItemInvalidFacts RenderableItemKind = "invalid_facts"
)

type RenderableItemDecision struct {
	Kind   RenderableItemKind
	Reason string
}

// DecideRenderableItem answers whether an item's trust, labels and sources
// are valid to show. A referenced item has nothing but its sources to show, so
// every item needs one. Source shapes follow the memory_sources schema: a file
// source has a path, any other source has a ref and no path.
func DecideRenderableItem(f RenderableItemFacts) RenderableItemDecision {
	if !validTrust(f.Trust) {
		return renderableInvalid("trust is unknown")
	}
	for i, label := range f.Labels {
		if !slices.Contains(RetrievalLabels(), label) || slices.Contains(f.Labels[:i], label) {
			return renderableInvalid("labels must be known and appear once")
		}
	}
	if len(f.Sources) == 0 {
		return renderableInvalid("item has no source")
	}
	for _, source := range f.Sources {
		if !validSourceKind(source.Kind) {
			return renderableInvalid("source kind is unknown")
		}
		if source.Kind == SourceFile && source.Path == "" {
			return renderableInvalid("file source has no path")
		}
		if source.Kind != SourceFile && (source.Ref == "" || source.Path != "") {
			return renderableInvalid("non-file source needs a ref and no path")
		}
	}
	return RenderableItemDecision{Kind: RenderableItemAccepted, Reason: "trust, labels and sources are valid"}
}

func renderableInvalid(reason string) RenderableItemDecision {
	return RenderableItemDecision{Kind: RenderableItemInvalidFacts, Reason: reason}
}

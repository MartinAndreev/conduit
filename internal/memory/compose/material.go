package compose

import (
	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
)

// decideSizes bounds each item's text and source metadata, before redaction,
// from the stored sizes the reader reported and what it actually loaded.
// Material the reader withheld, or loaded only in part, never passes.
func decideSizes(entries []*entry, out *Composition) []*entry {
	var kept []*entry
	for _, e := range entries {
		c := e.candidate
		decision := memory.DecideMaterialSize(memory.MaterialSizeFacts{
			Mandatory:         e.mandatory,
			TextBytes:         c.ContentBytes,
			LoadedTextBytes:   int64(len(c.Version.Content)),
			SourceCount:       c.SourceCount,
			LoadedSourceCount: int64(len(c.Sources)),
			SourceBytes:       c.SourceBytes,
			LoadedSourceBytes: memory.SourceBytes(c.Sources),
		})
		if decision.Kind != memory.SizeWithinLimit {
			exclude(out, e, StageSize, ExclusionKind(decision.Kind), decision.Reason)
			continue
		}
		kept = append(kept, e)
	}
	blockOnMandatory(out, BlockedMandatoryOversized, "a record linked by the assignment exceeds a per-item size limit or was not read in full")
	return kept
}

// redact passes each item's text and every source ref and path through the
// current policy, whatever policy the stored version was written under.
// Findings come from this material as read; Render's later pass sees only
// masked text and finds nothing, so these findings are the ones reported.
// Mandatory material with findings blocks; optional material goes on masked.
//
// The masked values, not the originals, are what Render receives. Redaction is
// idempotent, so Render's own pass leaves them unchanged and the measured and
// the sent bytes are the same; measure and render confirm it.
func (c *Composer) redact(entries []*entry, out *Composition) []*entry {
	pass := &redactionPass{redactor: c.redactor}
	var kept []*entry
	for _, e := range entries {
		item, findings := pass.item(e)
		if pass.reason != "" {
			out.Kind, out.Reason = InvalidRedaction, pass.reason
			return nil
		}
		decision := redaction.DecideRedactedMaterial(redaction.MaterialFacts{Mandatory: e.mandatory, Findings: findings})
		switch decision.Kind {
		case redaction.MaterialClean, redaction.MaterialRedacted:
			e.item, e.findings, e.policyVersion = item, findings, pass.policy
			kept = append(kept, e)
		case redaction.MaterialBlockedUnsafeMandatory:
			exclude(out, e, StageRedaction, ExclusionKind(decision.Kind), decision.Reason)
		default:
			out.Kind, out.Reason = InvalidRedaction, decision.Reason
			return nil
		}
	}
	blockOnMandatory(out, BlockedUnsafeMandatory, "a record linked by the assignment contains detected secrets")
	return kept
}

// redactionPass redacts one composition. Every result must name the same
// policy version.
type redactionPass struct {
	redactor redaction.Redactor
	policy   string
	reason   string
}

// item returns e as a render item with masked text and sources, and the
// findings of all of it.
func (r *redactionPass) item(e *entry) (render.Item, []redaction.Finding) {
	var findings []redaction.Finding
	redact := func(value string) string {
		if value == "" {
			return ""
		}
		result := r.redactor.Redact(value)
		switch {
		case result.PolicyVersion == "":
			r.reason = "redactor returned no policy version"
		case r.policy != "" && result.PolicyVersion != r.policy:
			r.reason = "redactor returned different policy versions"
		default:
			r.policy = result.PolicyVersion
		}
		findings = redaction.MergeFindings(findings, result.Findings)
		return result.Text
	}
	candidate := e.candidate
	sources := make([]memory.RecordSource, len(candidate.Sources))
	for i, source := range candidate.Sources {
		sources[i] = memory.RecordSource{Kind: source.Kind, Ref: redact(source.Ref), Path: redact(source.Path)}
	}
	return render.Item{
		RecordID:    candidate.Record.ID,
		VersionID:   candidate.Version.ID,
		ContentHash: candidate.Version.ContentHash,
		Trust:       memory.EffectiveTrust(candidate.Version.Trust, candidate.ExactVersionApproved),
		Labels:      e.labels,
		Mode:        render.ModeIncludedText,
		Sources:     sources,
		Text:        redact(candidate.Version.Content),
	}, findings
}

// measure renders each item alone and budgets the whole result:
// ceil(bytes / 3) of the header, the item block and the closing line. The
// final part has one header and closing line for all items, so the sum of
// the admitted items' tokens is never below the final part's estimate; the
// header (several hundred bytes) also covers item numbers growing past one
// digit. An item Render rejects is excluded, or blocks when mandatory. Every
// item mode is included_text: deleted content never gets here, and no rule
// chooses a reference for content that exists.
func (c *Composer) measure(marker render.Marker, entries []*entry, out *Composition) []*entry {
	var kept []*entry
	for _, e := range entries {
		alone := render.Render(render.Input{Marker: marker, Items: []render.Item{e.item}}, c.redactor)
		switch alone.Kind {
		case render.RenderedPart:
		case render.RenderInvalidInput:
			exclude(out, e, StageRender, ExcludedNotRenderable, alone.Reason)
			continue
		default:
			out.Kind, out.Reason = InvalidRedaction, alone.Reason
			return nil
		}
		if reason := reRedacted(alone.Items[0], e); reason != "" {
			out.Kind, out.Reason = InvalidRedaction, reason
			return nil
		}
		e.tokens = memory.EstimateTokens(alone.Part.Text)
		kept = append(kept, e)
	}
	blockOnMandatory(out, BlockedExplicitUnavailable, "a record linked by the assignment cannot be rendered")
	return kept
}

// reRedacted confirms Render's second pass over masked material changed
// nothing: it found nothing and used the same policy.
func reRedacted(result render.ItemResult, e *entry) string {
	if len(result.Findings) > 0 {
		return "redactor found secrets in already redacted material"
	}
	if result.PolicyVersion != e.policyVersion {
		return "redactor changed its policy version during composition"
	}
	return ""
}

// Package compose owns the ComposeContext procedure: it gathers memory for
// one dispatch request, applies memory's rules and renders the admitted items
// into one context part. It reads through consumer-side ports and performs no
// writes; the manifest transaction, which rechecks the expected versions with
// memory.DecideManifestRecheck, belongs to its caller.
package compose

import (
	"context"
	"errors"
	"slices"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// Request is one dispatch request's composition input.
type Request struct {
	ProjectID string
	// AttemptID is the attempt the material is for; attempt-scoped versions
	// are visible only to it.
	AttemptID string
	// Snapshot is the target workspace snapshot that file sources are
	// compared with.
	Snapshot SnapshotRef
	// ExplicitRecordIDs are the records the assignment links. They are
	// mandatory: each must exist, be eligible and fit, or composition blocks.
	ExplicitRecordIDs []string
	// Query is the search text; blank disables search.
	Query string
	// ScanLimit bounds the search scan; zero means DefaultScanLimit and a
	// negative value is invalid.
	ScanLimit int
	// OtherMandatory are token counts of assignment material that is not
	// memory (instructions, spec). It is composed elsewhere; here it only
	// takes its share of the budget first.
	OtherMandatory        []memory.MandatoryItem
	ModelLimitKnown       bool
	ModelLimit            int64
	HistoryTokens         int64
	NativeContextPossible bool
	// Marker is the manifest's boundary marker, drawn once by the caller.
	Marker render.Marker
}

// DefaultScanLimit is the search scan bound when a request names none, the
// initial value measured in spec 012 "Търсене" (5 000 current versions: about
// 20 ms without hits, about 78 ms with about 2 450 hits).
const DefaultScanLimit = 5000

// Kind is a composition's outcome.
type Kind string

const (
	Composed Kind = "composed"
	// ComposedEmpty means nothing was admitted, so no part is sent.
	ComposedEmpty  Kind = "empty"
	InvalidRequest Kind = "invalid_request"
	// BlockedExplicitNotFound: a linked record does not exist in the project.
	BlockedExplicitNotFound Kind = "blocked_explicit_not_found"
	// BlockedExplicitUnavailable: a linked record exists but is not eligible,
	// its content was deleted, its facts are invalid, or it cannot be rendered.
	BlockedExplicitUnavailable Kind = "blocked_explicit_unavailable"
	BlockedMandatoryOversized  Kind = "blocked_mandatory_oversized"
	BlockedUnsafeMandatory     Kind = "blocked_unsafe_mandatory"
	BlockedMandatoryOverBudget Kind = "blocked_mandatory_over_budget"
	// InvalidRedaction: the redactor broke its contract (no or changing policy
	// version, invalid findings, findings in already redacted material).
	InvalidRedaction Kind = "invalid_redaction"
	// RenderRejected: the admitted items, each renderable alone, could not be
	// rendered together.
	RenderRejected Kind = "render_rejected"
)

// SearchStatus is recorded in the manifest.
type SearchStatus string

const (
	SearchOK       SearchStatus = "ok"
	SearchDegraded SearchStatus = "degraded"
	SearchDisabled SearchStatus = "disabled"
)

// SearchOutcome keeps a failed search's cause for logs; composition goes on.
type SearchOutcome struct {
	Status SearchStatus
	Reason string
	Cause  error
}

// Stage is the phase that excluded an item.
type Stage string

const (
	StageGather      Stage = "gather"
	StageEligibility Stage = "eligibility"
	StageConflict    Stage = "conflict"
	StageContent     Stage = "content"
	StageSize        Stage = "size"
	StageRedaction   Stage = "redaction"
	StageRender      Stage = "render"
	StageAdmission   Stage = "admission"
)

// ExclusionKind is why an item stayed out. At the eligibility, conflict,
// size, redaction and admission stages it is the deciding rule's kind
// (memory.RetrievalKind, memory.ConflictKind, memory.MaterialSizeKind,
// redaction.MaterialKind, memory.AdmissionExclusionKind); the constants
// below are composition's own.
type ExclusionKind string

const (
	ExcludedNotFound       ExclusionKind = "not_found"
	ExcludedContentDeleted ExclusionKind = "content_deleted"
	ExcludedNotRenderable  ExclusionKind = "not_renderable"
	// ExcludedNoRecency marks a search candidate without a version time,
	// which admission cannot order.
	ExcludedNoRecency ExclusionKind = "no_recency"
)

// Exclusion records one item that stayed out. VersionID is empty when the
// item has no local version. IdentityWithheld means a stored ID could not be
// shown, so RecordID and VersionID are both empty.
type Exclusion struct {
	RecordID         string
	VersionID        string
	IdentityWithheld bool
	Mandatory        bool
	Stage            Stage
	Kind             ExclusionKind
	Reason           string
}

// ItemResult is one rendered item's manifest facts. Findings and
// PolicyVersion come from redacting the material as read with the current
// policy, not from Render's second pass over already masked text. Tokens is
// the item's budgeted estimate.
type ItemResult struct {
	RecordID           string
	VersionID          string
	ContentHash        string
	Mandatory          bool
	Reason             string
	Mode               render.Mode
	Trust              memory.Trust
	Labels             []memory.RetrievalLabel
	Findings           []redaction.Finding
	PolicyVersion      string
	NeutralizedMarkers int
	Tokens             int64
}

// Composition is the procedure's outcome. Part, TextHash, Tokens, Items and
// Expected are set only for Composed. Exclusions, Search and
// StaleIndexResults hold what was decided before the outcome; MemoryBudget
// is set once admission ran with a known model limit.
type Composition struct {
	Kind              Kind
	Reason            string
	Part              provider.InputPart
	TextHash          string
	Tokens            int64
	MemoryBudget      int64
	Items             []ItemResult
	Exclusions        []Exclusion
	Search            SearchOutcome
	StaleIndexResults int
	// Expected are the included records' versions for the commit recheck,
	// in item order.
	Expected []memory.ExpectedRecordVersion
}

// Composer runs ComposeContext over its ports.
type Composer struct {
	memory   MemoryReader
	snapshot SnapshotReader
	redactor redaction.Redactor
}

// New returns a composer; every port is required.
func New(memoryReader MemoryReader, snapshot SnapshotReader, redactor redaction.Redactor) (*Composer, error) {
	if memoryReader == nil || snapshot == nil || redactor == nil {
		return nil, errors.New("compose: memory reader, snapshot reader and redactor are required")
	}
	return &Composer{memory: memoryReader, snapshot: snapshot, redactor: redactor}, nil
}

// entry is one candidate moving through the phases.
type entry struct {
	candidate Candidate
	mandatory bool
	fromIndex bool
	relevance float64
	labels    []memory.RetrievalLabel
	// item is the redacted item as it is measured and rendered.
	item          render.Item
	findings      []redaction.Finding
	policyVersion string
	tokens        int64
}

// ComposeContext composes one request's memory context. Its phases are
// gather, size, eligibility, redaction, measurement, admission and render;
// a phase that blocks returns the composition at once. Size runs before
// eligibility: material the reader withheld or read in part is reported as
// oversized (a linked record blocks), its file sources are never looked up
// in the snapshot, and it is never evaluated, so as a conflict counterpart
// it counts as unevaluated and labels while its stored status is
// retrievable. A stale index hit that is also oversized is reported as
// oversized and not counted as stale. It returns an error
// only when a linked read, a snapshot read or the context fails; a failed
// search degrades instead.
//
// Every identity in the result can be shown: linked IDs must pass
// render.SafeIdentity, rendered items passed it inside Render, and an
// exclusion whose stored record or version ID fails it is reported with both
// IDs empty and IdentityWithheld set. Reasons never repeat a raw value.
func (c *Composer) ComposeContext(ctx context.Context, req Request) (Composition, error) {
	if reason := c.invalidRequest(req); reason != "" {
		return Composition{Kind: InvalidRequest, Reason: reason}, nil
	}
	out, err := c.compose(ctx, req)
	if err != nil {
		return Composition{}, err
	}
	c.withholdUnsafeIdentities(&out)
	return out, nil
}

// compose runs the phases over a valid request.
func (c *Composer) compose(ctx context.Context, req Request) (Composition, error) {
	var out Composition
	entries, err := c.gather(ctx, req, &out)
	if err != nil {
		return Composition{}, err
	}
	if out.Kind != "" {
		return out, nil
	}
	if entries = decideSizes(entries, &out); out.Kind != "" {
		return out, nil
	}
	entries, err = c.decideEligibility(ctx, req, entries, &out)
	if err != nil {
		return Composition{}, err
	}
	if out.Kind != "" {
		return out, nil
	}
	if entries = c.redact(entries, &out); out.Kind != "" {
		return out, nil
	}
	if entries = c.measure(req.Marker, entries, &out); out.Kind != "" {
		return out, nil
	}
	if entries = admit(req, entries, &out); out.Kind != "" {
		return out, nil
	}
	c.render(req.Marker, entries, &out)
	return out, nil
}

func (c *Composer) invalidRequest(req Request) string {
	switch {
	case req.ProjectID == "":
		return "project is missing"
	case req.Snapshot == "":
		return "target snapshot is missing"
	case slices.ContainsFunc(req.ExplicitRecordIDs, func(id string) bool { return !render.SafeIdentity(id, c.redactor) }):
		return "a linked record id is not a valid record id"
	case req.ScanLimit < 0:
		return "scan limit is negative"
	}
	// Render owns the marker grammar; an input without items checks only it.
	if probe := render.Render(render.Input{Marker: req.Marker}, c.redactor); probe.Kind != render.RenderedNothing {
		return probe.Reason
	}
	return ""
}

// exclude records why e stayed out.
func exclude(out *Composition, e *entry, stage Stage, kind ExclusionKind, reason string) {
	out.Exclusions = append(out.Exclusions, Exclusion{
		RecordID:  e.candidate.Record.ID,
		VersionID: e.candidate.Version.ID,
		Mandatory: e.mandatory,
		Stage:     stage,
		Kind:      kind,
		Reason:    reason,
	})
}

// withholdUnsafeIdentities empties the IDs of every exclusion whose stored
// record or version ID could not be shown: it fails the ID grammar or holds a
// detected secret. Both are emptied so the pair cannot be partly matched.
func (c *Composer) withholdUnsafeIdentities(out *Composition) {
	for i := range out.Exclusions {
		x := &out.Exclusions[i]
		if c.showable(x.RecordID) && c.showable(x.VersionID) {
			continue
		}
		x.RecordID, x.VersionID, x.IdentityWithheld = "", "", true
	}
}

// showable accepts an absent ID, which a candidate without a local version
// has, and otherwise a safe identity.
func (c *Composer) showable(id string) bool {
	return id == "" || render.SafeIdentity(id, c.redactor)
}

// blockOnMandatory blocks the composition when the current phase excluded
// mandatory material; each phase returns before the next, so any mandatory
// exclusion is this phase's.
func blockOnMandatory(out *Composition, kind Kind, reason string) {
	if slices.ContainsFunc(out.Exclusions, func(x Exclusion) bool { return x.Mandatory }) {
		out.Kind, out.Reason = kind, reason
	}
}

// admit reserves the other assignment material and the linked records first,
// then admits search candidates by trust, recency and relevance. The linked
// records lead the admitted list in request order, followed by search
// candidates in admission order.
func admit(req Request, entries []*entry, out *Composition) []*entry {
	facts := memory.ManifestAdmissionFacts{
		Mandatory:             slices.Clone(req.OtherMandatory),
		ModelLimitKnown:       req.ModelLimitKnown,
		ModelLimit:            req.ModelLimit,
		HistoryTokens:         req.HistoryTokens,
		NativeContextPossible: req.NativeContextPossible,
	}
	var admitted []*entry
	optional := make(map[string]*entry)
	for _, e := range entries {
		id := e.candidate.Record.ID
		if e.mandatory {
			facts.Mandatory = append(facts.Mandatory, memory.MandatoryItem{ID: id, Tokens: e.tokens})
			admitted = append(admitted, e)
			continue
		}
		optional[id] = e
		facts.Candidates = append(facts.Candidates, memory.MemoryCandidate{
			ID:        id,
			Tokens:    e.tokens,
			Trust:     e.candidate.Version.Trust,
			RecencyAt: e.candidate.VersionCreatedAt,
			Relevance: e.relevance,
		})
	}
	decision := memory.DecideManifestAdmission(facts)
	for _, x := range decision.Excluded {
		exclude(out, optional[x.CandidateID], StageAdmission, ExclusionKind(x.Kind), x.Reason)
	}
	switch decision.Kind {
	case memory.AdmissionAdmitted:
		out.MemoryBudget = decision.MemoryBudget
		for _, id := range decision.Admitted {
			admitted = append(admitted, optional[id])
		}
		return admitted
	case memory.AdmissionBlockedMandatoryOverBudget:
		out.Kind, out.Reason = BlockedMandatoryOverBudget, decision.Reason
	default:
		// Candidates were validated by the earlier phases, so only the
		// request's budget facts can be invalid.
		out.Kind, out.Reason = InvalidRequest, "budget: "+decision.Reason
	}
	return nil
}

// render builds the part from the admitted items. The item facts reported
// are composition's: findings and policy from the material as read.
func (c *Composer) render(marker render.Marker, admitted []*entry, out *Composition) {
	items := make([]render.Item, len(admitted))
	for i, e := range admitted {
		items[i] = e.item
	}
	rendered := render.Render(render.Input{Marker: marker, Items: items}, c.redactor)
	switch rendered.Kind {
	case render.RenderedPart:
	case render.RenderedNothing:
		out.Kind, out.Reason = ComposedEmpty, "no memory item was admitted"
		return
	default:
		out.Kind, out.Reason = RenderRejected, rendered.Reason
		return
	}
	results := make([]ItemResult, len(admitted))
	expected := make([]memory.ExpectedRecordVersion, len(admitted))
	for i, e := range admitted {
		if reason := reRedacted(rendered.Items[i], e); reason != "" {
			out.Kind, out.Reason = InvalidRedaction, reason
			return
		}
		results[i] = itemResult(e, rendered.Items[i].NeutralizedMarkers)
		expected[i] = memory.ExpectedRecordVersion{
			RecordID:      e.candidate.Record.ID,
			VersionID:     e.candidate.Version.ID,
			RecordVersion: e.candidate.Record.Version,
		}
	}
	out.Kind, out.Reason = Composed, "memory composed"
	out.Part, out.TextHash = rendered.Part, rendered.TextHash
	out.Tokens = memory.EstimateTokens(rendered.Part.Text)
	out.Items, out.Expected = results, expected
}

func itemResult(e *entry, neutralized int) ItemResult {
	reason := "found by search"
	if e.mandatory {
		reason = "linked by the assignment"
	}
	return ItemResult{
		RecordID:           e.item.RecordID,
		VersionID:          e.item.VersionID,
		ContentHash:        e.item.ContentHash,
		Mandatory:          e.mandatory,
		Reason:             reason,
		Mode:               e.item.Mode,
		Trust:              e.item.Trust,
		Labels:             e.item.Labels,
		Findings:           e.findings,
		PolicyVersion:      e.policyVersion,
		NeutralizedMarkers: neutralized,
		Tokens:             e.tokens,
	}
}

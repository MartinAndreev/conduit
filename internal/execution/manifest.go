package execution

import (
	"slices"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

// NewManifest is a normalized manifest ready to be stored with its intent.
// Ordinals are the slice positions. RedactionPolicyVersion is empty (NULL)
// only for an empty composition. Payload is the exact text stored and sent;
// it is empty for an empty composition, which has no payload row. Expected
// are the versions the composition relied on, for the commit recheck; they
// are not stored.
type NewManifest struct {
	Composition            ManifestComposition
	PayloadHash            string
	Payload                string
	RedactionPolicyVersion string
	SearchStatus           compose.SearchStatus
	Items                  []ManifestItem
	Exclusions             []ManifestExclusion
	Expected               []memory.ExpectedRecordVersion
	ManifestHash           string
}

// ManifestItem is one included record version. Trust is empty (NULL) when the
// composition reported none.
type ManifestItem struct {
	RecordID  string
	VersionID string
	Mode      render.Mode
	Reason    string
	Trust     memory.Trust
}

// ManifestExclusion is one item that stayed out, as composition reported it.
type ManifestExclusion struct {
	RecordID         string
	VersionID        string
	IdentityWithheld bool
	Mandatory        bool
	Stage            compose.Stage
	Kind             compose.ExclusionKind
	Reason           string
}

// ManifestFacts are a composition and the intent it is for.
type ManifestFacts struct {
	// Assignment is the intent's target assignment.
	Assignment      provider.AssignmentRef
	Request         compose.Request
	Composition     compose.Composition
	MaxPayloadBytes int
}

type ManifestEnvelopeKind string

const (
	ManifestEnvelope            ManifestEnvelopeKind = "envelope"
	ManifestInvalidFacts        ManifestEnvelopeKind = "invalid_facts"
	ManifestKindNotDispatchable ManifestEnvelopeKind = "kind_not_dispatchable"
	ManifestScopeMismatch       ManifestEnvelopeKind = "scope_mismatch"
	ManifestPartInvalid         ManifestEnvelopeKind = "part_invalid"
	ManifestHashMismatch        ManifestEnvelopeKind = "hash_mismatch"
	ManifestCoverageMismatch    ManifestEnvelopeKind = "coverage_mismatch"
	ManifestPolicyMismatch      ManifestEnvelopeKind = "policy_mismatch"
	ManifestPayloadTooLarge     ManifestEnvelopeKind = "payload_too_large"
	ManifestEmptyNotEmpty       ManifestEnvelopeKind = "empty_not_empty"
)

// ManifestEnvelopeDecision carries Manifest only for ManifestEnvelope.
type ManifestEnvelopeDecision struct {
	Kind     ManifestEnvelopeKind
	Reason   string
	Manifest NewManifest
}

func manifestRefusal(kind ManifestEnvelopeKind, reason string) ManifestEnvelopeDecision {
	return ManifestEnvelopeDecision{Kind: kind, Reason: reason}
}

// DecideManifestEnvelope checks a composition before it is stored, without
// I/O, and normalizes it into the manifest that is stored and hashed. Only
// Composed and ComposedEmpty are dispatchable; the request must be for the
// intent's project and attempt. A composed part must be a context part with
// no URI or media type and non-empty text whose SHA-256 is TextHash; every
// item must be covered by exactly one expected version and back; all items
// share one non-empty redaction policy; and the text's bytes must fit
// MaxPayloadBytes. An empty composition carries no part, hash, items or
// expected versions.
func DecideManifestEnvelope(f ManifestFacts) ManifestEnvelopeDecision {
	if f.MaxPayloadBytes < 1 || f.Assignment.ProjectID == "" || f.Assignment.AttemptID == "" {
		return manifestRefusal(ManifestInvalidFacts, "manifest facts need a payload limit and the intent's project and attempt")
	}
	c := f.Composition
	if c.Kind != compose.Composed && c.Kind != compose.ComposedEmpty {
		return manifestRefusal(ManifestKindNotDispatchable, "only a composed or empty composition can be dispatched")
	}
	if !slices.Contains(SearchStatuses(), c.Search.Status) {
		return manifestRefusal(ManifestInvalidFacts, "a dispatchable composition needs a known search status")
	}
	if f.Request.ProjectID != string(f.Assignment.ProjectID) || f.Request.AttemptID != string(f.Assignment.AttemptID) {
		return manifestRefusal(ManifestScopeMismatch, "composition request is for another project or attempt than the intent")
	}
	if c.Kind == compose.ComposedEmpty {
		if c.Part != (provider.InputPart{}) || c.TextHash != "" || len(c.Items) > 0 || len(c.Expected) > 0 {
			return manifestRefusal(ManifestEmptyNotEmpty, "an empty composition carries no part, hash, items or expected versions")
		}
		return envelope(NewManifest{
			Composition:  CompositionEmpty,
			PayloadHash:  EmptyPayloadHash,
			SearchStatus: c.Search.Status,
			Exclusions:   exclusions(c.Exclusions),
		})
	}
	switch {
	case c.Part.Kind != provider.InputContext || c.Part.URI != "" || c.Part.MediaType != "" || c.Part.Text == "":
		return manifestRefusal(ManifestPartInvalid, "composed part must be a context part with text and no URI or media type")
	case PayloadHash(c.Part.Text) != c.TextHash:
		return manifestRefusal(ManifestHashMismatch, "composed text does not hash to the reported text hash")
	case !coverageMatches(c.Items, c.Expected):
		return manifestRefusal(ManifestCoverageMismatch, "every item needs exactly one expected version with the same record and version, and no expected version may be extra")
	}
	policy, ok := sharedPolicy(c.Items)
	if !ok {
		return manifestRefusal(ManifestPolicyMismatch, "items need one shared, non-empty redaction policy version")
	}
	if len(c.Part.Text) > f.MaxPayloadBytes {
		return manifestRefusal(ManifestPayloadTooLarge, "composed text exceeds the manifest payload limit; it is refused, not truncated")
	}
	items := make([]ManifestItem, len(c.Items))
	for i, item := range c.Items {
		items[i] = ManifestItem{RecordID: item.RecordID, VersionID: item.VersionID, Mode: item.Mode, Reason: item.Reason, Trust: item.Trust}
	}
	return envelope(NewManifest{
		Composition:            CompositionComposed,
		PayloadHash:            c.TextHash,
		Payload:                c.Part.Text,
		RedactionPolicyVersion: policy,
		SearchStatus:           c.Search.Status,
		Items:                  items,
		Exclusions:             exclusions(c.Exclusions),
		Expected:               slices.Clone(c.Expected),
	})
}

// SearchStatuses lists the search statuses a manifest stores.
func SearchStatuses() []compose.SearchStatus {
	return []compose.SearchStatus{compose.SearchOK, compose.SearchDegraded, compose.SearchDisabled}
}

func envelope(m NewManifest) ManifestEnvelopeDecision {
	m.ManifestHash = ManifestHash(m)
	return ManifestEnvelopeDecision{Kind: ManifestEnvelope, Reason: "composition is dispatchable", Manifest: m}
}

func exclusions(in []compose.Exclusion) []ManifestExclusion {
	if len(in) == 0 {
		return nil
	}
	out := make([]ManifestExclusion, len(in))
	for i, x := range in {
		out[i] = ManifestExclusion{
			RecordID:         x.RecordID,
			VersionID:        x.VersionID,
			IdentityWithheld: x.IdentityWithheld,
			Mandatory:        x.Mandatory,
			Stage:            x.Stage,
			Kind:             x.Kind,
			Reason:           x.Reason,
		}
	}
	return out
}

type recordVersion struct{ record, version string }

func coverageMatches(items []compose.ItemResult, expected []memory.ExpectedRecordVersion) bool {
	if len(items) == 0 || len(items) != len(expected) {
		return false
	}
	remaining := make(map[recordVersion]int, len(expected))
	for _, e := range expected {
		if e.RecordID == "" || e.VersionID == "" {
			return false
		}
		remaining[recordVersion{e.RecordID, e.VersionID}]++
	}
	for _, item := range items {
		key := recordVersion{item.RecordID, item.VersionID}
		if item.RecordID == "" || item.VersionID == "" || remaining[key] != 1 {
			return false
		}
		remaining[key] = 0
	}
	return true
}

func sharedPolicy(items []compose.ItemResult) (string, bool) {
	policy := items[0].PolicyVersion
	if policy == "" {
		return "", false
	}
	for _, item := range items[1:] {
		if item.PolicyVersion != policy {
			return "", false
		}
	}
	return policy, true
}

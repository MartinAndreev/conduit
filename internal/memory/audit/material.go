package audit

import (
	"context"
	"errors"
	"fmt"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/render"
)

// MaxAuditPageSize is the most records one audit page holds.
const MaxAuditPageSize = 100

// MaxAuditPageContentBytes bounds the total stored content bytes of one
// audit page (initial value). A page stops before the record that would
// exceed it, but always holds at least one record when any remains; one
// record is already bounded by memory.MaxStoredContentBytes. Sources do not
// count toward it: each record's are bounded by memory.MaxSourcesPerItem and
// memory.MaxSourceBytes, or omitted.
const MaxAuditPageContentBytes = 256 << 10

// ErrInvalidMaterialRequest reports a material request without a project or
// with a page size outside 1..MaxAuditPageSize.
var ErrInvalidMaterialRequest = errors.New("audit material request needs a project and a page size from 1 to the maximum")

// MaterialQuery is one page read of the records to audit.
type MaterialQuery struct {
	ProjectID string
	// Statuses are the record statuses to read (memory.AuditedStatuses).
	Statuses []memory.RecordStatus
	After    records.ProposalPosition
	Limit    int
	// ContentBudget bounds the page's total stored content bytes.
	ContentBudget int
}

// MaterialReader reads the records an audit agent reviews. It only reads;
// the rules decide.
type MaterialReader interface {
	// ReadAuditMaterial reads, in one consistent snapshot, the project's
	// records in one of query.Statuses whose current version's content is
	// not deleted, strictly after query.After in (created_at, id) order, in
	// that order: at most query.Limit records, stopping before the record
	// that would take the page's total content bytes over
	// query.ContentBudget, but always at least one when any remains. More
	// reports whether records remain after the page. Each record carries its
	// stored source count and memory.SourceBytes size; its sources are
	// loaded only when both are within memory.MaxSourcesPerItem and
	// memory.MaxSourceBytes, otherwise none are.
	ReadAuditMaterial(ctx context.Context, query MaterialQuery) (MaterialPage, error)
}

// MaterialRecord is one record with its current version as stored.
type MaterialRecord struct {
	// Record.CurrentVersionID is the version a suggestion cites.
	Record      memory.Record
	ContentHash string
	Content     string
	// Trust is the stored trust; ExactVersionApproved is whether a human
	// approved this exact version and content hash.
	Trust                memory.Trust
	ExactVersionApproved bool
	Sources              []memory.RecordSource
	// SourceCount and SourceBytes describe all stored sources of the
	// current version, loaded or not.
	SourceCount int64
	SourceBytes int64
}

// MaterialPage is one read of the audit material.
type MaterialPage struct {
	Records []MaterialRecord
	More    bool
}

// MaterialRequest asks for one page after a position; a zero After starts at
// the oldest record.
type MaterialRequest struct {
	ProjectID string
	After     records.ProposalPosition
	Limit     int
}

// MaterialItem is one record to audit, with its effective trust.
// SourcesOmitted is the stored source count when the sources were not loaded
// because they exceed the per-item bounds; Sources is then empty. The
// rendering shows it as a visible label, and the agent may still suggest
// archiving or deleting the record.
type MaterialItem struct {
	Record         memory.Record
	ContentHash    string
	Content        string
	EffectiveTrust memory.Trust
	Sources        []memory.RecordSource
	SourcesOmitted int64
}

// Material is one page. Next continues after it and is zero at the end.
type Material struct {
	Items []MaterialItem
	Next  records.ProposalPosition
}

// AuditMaterial reads one page of the project's records an audit agent
// reviews (Martin, 2026-11-13): only memory.AuditedStatuses, the current
// version, with deleted content left out, paged by (created_at, id) within
// MaxAuditPageContentBytes. Effective trust is memory.EffectiveTrust of the
// stored trust and the exact version's approval.
func (s Service) AuditMaterial(ctx context.Context, request MaterialRequest) (Material, error) {
	if request.ProjectID == "" || request.Limit < 1 || request.Limit > MaxAuditPageSize {
		return Material{}, ErrInvalidMaterialRequest
	}
	page, err := s.Material.ReadAuditMaterial(ctx, MaterialQuery{
		ProjectID:     request.ProjectID,
		Statuses:      memory.AuditedStatuses(),
		After:         request.After,
		Limit:         request.Limit,
		ContentBudget: MaxAuditPageContentBytes,
	})
	if err != nil {
		return Material{}, fmt.Errorf("read audit material: %w", err)
	}
	material := Material{Items: make([]MaterialItem, len(page.Records))}
	for index, stored := range page.Records {
		item := MaterialItem{
			Record:         stored.Record,
			ContentHash:    stored.ContentHash,
			Content:        stored.Content,
			EffectiveTrust: memory.EffectiveTrust(stored.Trust, stored.ExactVersionApproved),
			Sources:        stored.Sources,
		}
		// Sources not loaded in full are shown as omitted, never in part.
		if int64(len(stored.Sources)) != stored.SourceCount {
			item.Sources, item.SourcesOmitted = nil, stored.SourceCount
		}
		material.Items[index] = item
	}
	if page.More && len(page.Records) > 0 {
		last := page.Records[len(page.Records)-1].Record
		material.Next = records.ProposalPosition{CreatedAt: last.CreatedAt, RecordID: last.ID}
	}
	return material, nil
}

// RenderInput is the page as render.RenderAudit's input under marker: the
// rendering redacts, neutralizes the marker, shows each record's ID, current
// version ID and content hash for the agent to cite, and labels omitted
// sources.
func (m Material) RenderInput(marker render.Marker) render.AuditInput {
	input := render.AuditInput{Marker: marker, Items: make([]render.AuditItem, len(m.Items))}
	for index, item := range m.Items {
		input.Items[index] = render.AuditItem{
			RecordID:    item.Record.ID,
			VersionID:   item.Record.CurrentVersionID,
			ContentHash: item.ContentHash,
			Category:    item.Record.Category,
			Status:      item.Record.Status,
			Trust:       item.EffectiveTrust,
			CreatedAt:   item.Record.CreatedAt,
			Sources:     item.Sources,
			Omitted:     item.SourcesOmitted,
			Text:        item.Content,
		}
	}
	return input
}

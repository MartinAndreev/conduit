package audit

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
)

type fakeMaterial struct {
	page    MaterialPage
	err     error
	queries []MaterialQuery
}

func (f *fakeMaterial) ReadAuditMaterial(_ context.Context, query MaterialQuery) (MaterialPage, error) {
	f.queries = append(f.queries, query)
	return f.page, f.err
}

func materialRecord(id string, trust memory.Trust, approved bool) MaterialRecord {
	stored := storedRecord(id, memory.StatusActive)
	return MaterialRecord{Record: stored.Record, ContentHash: stored.CurrentContentHash, Content: "text of " + id, Trust: trust, ExactVersionApproved: approved}
}

func TestAuditMaterialReadsOnePage(t *testing.T) {
	first := materialRecord("r1", memory.TrustAgentClaim, true)
	first.Sources, first.SourceCount = []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, 1
	second := materialRecord("r2", memory.TrustConduitObserved, false)
	second.Record.Status = memory.StatusNeedsVerification
	reader := &fakeMaterial{page: MaterialPage{Records: []MaterialRecord{first, second}, More: true}}
	after := records.ProposalPosition{CreatedAt: auditTime.Add(-2 * time.Hour), RecordID: "r0"}
	got, err := Service{Material: reader}.AuditMaterial(context.Background(), MaterialRequest{ProjectID: "p1", After: after, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	query := reader.queries[0]
	if query.ProjectID != "p1" || query.After != after || query.Limit != 2 || query.ContentBudget != MaxAuditPageContentBytes ||
		!slices.Equal(query.Statuses, memory.AuditedStatuses()) {
		t.Fatalf("query = %+v", query)
	}
	if len(got.Items) != 2 || got.Items[0].EffectiveTrust != memory.TrustHumanApproved || got.Items[1].EffectiveTrust != memory.TrustConduitObserved ||
		got.Items[0].Content != "text of r1" || !slices.Equal(got.Items[0].Sources, first.Sources) || got.Items[1].Record != second.Record {
		t.Fatalf("material = %+v", got)
	}
	if got.Next != (records.ProposalPosition{CreatedAt: second.Record.CreatedAt, RecordID: "r2"}) {
		t.Fatalf("next = %+v", got.Next)
	}

	rendered := render.RenderAudit(got.RenderInput(testMarker), redaction.Policy{})
	if rendered.Kind != render.RenderedPart {
		t.Fatalf("render = %s %s", rendered.Kind, rendered.Reason)
	}
	for _, cited := range []string{"record: r1\n", "version: r1-v\n", "content_hash: " + hashOf("r1") + "\n", "trust: human_approved\n", "status: needs_verification\n", "| text of r2\n"} {
		if !strings.Contains(rendered.Part.Text, cited) {
			t.Fatalf("rendering lacks %q:\n%s", cited, rendered.Part.Text)
		}
	}
}

const testMarker render.Marker = "conduit-memory-000102030405060708090a0b0c0d0e0f"

func TestAuditMaterialEndsWithoutANextPosition(t *testing.T) {
	for name, page := range map[string]MaterialPage{
		"last page":       {Records: []MaterialRecord{materialRecord("r1", memory.TrustHumanApproved, false)}},
		"empty with more": {More: true},
		"empty":           {},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Service{Material: &fakeMaterial{page: page}}.AuditMaterial(context.Background(), MaterialRequest{ProjectID: "p1", Limit: MaxAuditPageSize})
			if err != nil || got.Next != (records.ProposalPosition{}) || len(got.Items) != len(page.Records) {
				t.Fatalf("material %+v, error %v", got, err)
			}
		})
	}
}

func TestAuditMaterialRefusesInvalidRequests(t *testing.T) {
	reader := &fakeMaterial{}
	for _, request := range []MaterialRequest{{Limit: 1}, {ProjectID: "p1"}, {ProjectID: "p1", Limit: -1}, {ProjectID: "p1", Limit: MaxAuditPageSize + 1}} {
		if _, err := (Service{Material: reader}).AuditMaterial(context.Background(), request); !errors.Is(err, ErrInvalidMaterialRequest) {
			t.Fatalf("%+v: %v", request, err)
		}
	}
	if len(reader.queries) != 0 {
		t.Fatalf("queries = %d", len(reader.queries))
	}
	failing := &fakeMaterial{err: errInjected}
	if _, err := (Service{Material: failing}).AuditMaterial(context.Background(), MaterialRequest{ProjectID: "p1", Limit: 1}); !errors.Is(err, errInjected) {
		t.Fatalf("error = %v", err)
	}
}

// Sources not loaded in full are omitted as a whole and counted; the record
// stays on the page.
func TestAuditMaterialOmitsSourcesOverTheLimits(t *testing.T) {
	loaded := materialRecord("r1", memory.TrustHumanApproved, false)
	loaded.Sources, loaded.SourceCount = []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, 1
	withheld := materialRecord("r2", memory.TrustHumanApproved, false)
	withheld.SourceCount, withheld.SourceBytes = memory.MaxSourcesPerItem+1, 100
	partial := materialRecord("r3", memory.TrustHumanApproved, false)
	partial.Sources, partial.SourceCount = []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "a"}}, 2
	reader := &fakeMaterial{page: MaterialPage{Records: []MaterialRecord{loaded, withheld, partial}}}
	got, err := Service{Material: reader}.AuditMaterial(context.Background(), MaterialRequest{ProjectID: "p1", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].SourcesOmitted != 0 || len(got.Items[0].Sources) != 1 ||
		got.Items[1].SourcesOmitted != memory.MaxSourcesPerItem+1 || got.Items[1].Sources != nil ||
		got.Items[2].SourcesOmitted != 2 || got.Items[2].Sources != nil {
		t.Fatalf("items = %+v", got.Items)
	}
	input := got.RenderInput(testMarker)
	if input.Items[1].Omitted != memory.MaxSourcesPerItem+1 || input.Items[0].Omitted != 0 {
		t.Fatalf("render input = %+v", input.Items)
	}
}

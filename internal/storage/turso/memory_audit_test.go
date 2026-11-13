package turso

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/audit"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	tursogo "turso.tech/database/tursogo"
)

func auditService(store *Store) audit.Service {
	return audit.Service{
		Records:       recordService(store, nil),
		Deletion:      deletion.Service{Store: store.MemoryDeletion()},
		Material:      store.AuditMaterial(),
		Suggestions:   store.AuditSuggestions(),
		Confirmations: store.SuggestionConfirmations(),
	}
}

// humanAt is human text of the project created at minute after recordsNow,
// which orders the audit pages.
func humanAt(t *testing.T, store *Store, content string, minute int) records.CreateResult {
	t.Helper()
	request := humanText(content)
	request.Now = recordsNow.Add(time.Duration(minute) * time.Minute)
	return mustCreate(t, store, request)
}

func auditReport(entries ...string) string {
	return "Прегледах паметта.\n\n```conduit-memory-audit\n{\"suggestions\":[" + strings.Join(entries, ",\n") + "]}\n```\n"
}

func suggestionEntry(created records.CreateResult, action memory.Suggestion, reason string) string {
	return fmt.Sprintf(`{"record_id":%q,"version_id":%q,"content_hash":%q,"action":%q,"reason":%q}`,
		created.RecordID, created.VersionID, created.ContentHash, action, reason)
}

func mustRecordSuggestions(t *testing.T, store *Store, report string, at time.Time) audit.RecordResult {
	t.Helper()
	got, err := auditService(store).RecordSuggestions(context.Background(), audit.RecordRequest{Report: report, ProjectID: "project", Reviewer: "auditor", Now: at})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func pendingReviews(t *testing.T, store *Store) []audit.PendingSuggestion {
	t.Helper()
	got, err := auditService(store).PendingSuggestions(context.Background(), audit.PendingRequest{ProjectID: "project", Limit: audit.MaxPendingPageSize})
	if err != nil {
		t.Fatal(err)
	}
	return got.Suggestions
}

func confirm(t *testing.T, store *Store, reviewID string, expectedVersion int64, change func(*audit.ConfirmRequest)) audit.ConfirmResult {
	t.Helper()
	request := audit.ConfirmRequest{
		ProjectID: "project", ReviewID: reviewID, ExpectedVersion: expectedVersion,
		Actor: memory.ActorHuman, Reviewer: "martin", Reason: "съгласен", Now: recordsNow.Add(time.Hour),
	}
	if change != nil {
		change(&request)
	}
	got, err := auditService(store).ConfirmSuggestion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The suggestion CHECKs are literal DDL; this keeps their list equal to the
// domain owner's, and only an agent opinion carries a suggestion.
func TestReviewSuggestionMatchesDomain(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	fixture := newMemoryFixture("suggested", "project")
	mustInsertMemory(t, store.db, fixture)
	for _, actor := range memory.Actors() {
		for _, decision := range memory.ReviewDecisions() {
			for _, suggestion := range memory.Suggestions() {
				review := newReview(string(actor)+"-"+string(decision)+"-"+string(suggestion), fixture.version, actor, decision)
				review.Suggestion = optional(string(suggestion))
				err := insertReview(ctx, store.db, review)
				allowed := actor == memory.ActorAgent && decision == memory.ReviewOpinion
				if allowed != (err == nil) {
					t.Fatalf("%s %s %s: allowed=%v, err=%v", actor, decision, suggestion, allowed, err)
				}
			}
		}
	}
	for name, suggestion := range map[string]string{"unknown": "forget", "upper case": "ARCHIVE", "empty": ""} {
		review := newReview("invalid-"+name, fixture.version, memory.ActorAgent, memory.ReviewOpinion)
		review.Suggestion = optional(suggestion)
		if insertReview(ctx, store.db, review) == nil {
			t.Fatalf("%s suggestion accepted", name)
		}
	}
}

// The audit reads only active and stale records of the project, their
// current version, without deleted content, paged by (created_at, id).
func TestAuditMaterialPagesActiveAndStaleRecords(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	service := recordService(store, nil)
	first := humanAt(t, store, "Първо решение", 1)
	mustCreate(t, store, proposalFromAcceptedResult("Предложение"))
	archived := humanAt(t, store, "Архивирано", 3)
	if got, err := service.Archive(ctx, records.ReasonRequest{ProjectID: "project", RecordID: archived.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "x", Now: recordsNow}); err != nil || got.Kind != memory.TransitionAllowed {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	stale := humanAt(t, store, "Остаряло", 4)
	mustExec(t, store, "UPDATE memory_records SET status = 'needs_verification' WHERE id = ?", stale.RecordID)
	deleted := humanAt(t, store, "Изтрито", 5)
	if got, err := (deletion.Service{Store: store.MemoryDeletion()}).DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: deleted.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Now: recordsNow}); err != nil || got.Kind != memory.DeletionDelete {
		t.Fatalf("delete = %+v, %v", got, err)
	}
	foreign := humanText("Друг проект")
	foreign.Scope.ProjectID, foreign.Now = "other", recordsNow.Add(6*time.Minute)
	mustCreate(t, store, foreign)
	approvedRequest := proposalFromAcceptedResult("Одобрено предложение")
	approvedRequest.Now = recordsNow.Add(7 * time.Minute)
	approved := mustCreate(t, store, approvedRequest)
	if got, err := service.Approve(ctx, records.ApproveRequest{ProjectID: "project", RecordID: approved.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", VersionID: approved.VersionID, ContentHash: approved.ContentHash, Now: recordsNow}); err != nil || got.Kind != memory.TransitionAllowed {
		t.Fatalf("approve = %+v, %v", got, err)
	}

	auditor := auditService(store)
	page, err := auditor.AuditMaterial(ctx, audit.MaterialRequest{ProjectID: "project", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if ids := materialIDs(page); !slices.Equal(ids, []string{first.RecordID, stale.RecordID}) || page.Next.RecordID != stale.RecordID {
		t.Fatalf("first page = %v, next %+v", ids, page.Next)
	}
	item := page.Items[0]
	if item.Record.CurrentVersionID != first.VersionID || item.ContentHash != first.ContentHash || item.Content != "Първо решение" ||
		item.EffectiveTrust != memory.TrustHumanApproved || item.Record.Status != memory.StatusActive || item.Record.Version != 1 ||
		!slices.Equal(item.Sources, []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}) || !item.Record.CreatedAt.Equal(recordsNow.Add(time.Minute)) {
		t.Fatalf("item = %+v", item)
	}
	if page.Items[1].Record.Status != memory.StatusNeedsVerification {
		t.Fatalf("stale item = %+v", page.Items[1])
	}
	next, err := auditor.AuditMaterial(ctx, audit.MaterialRequest{ProjectID: "project", After: page.Next, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if ids := materialIDs(next); !slices.Equal(ids, []string{approved.RecordID}) || next.Next != (records.ProposalPosition{}) {
		t.Fatalf("second page = %v, next %+v", ids, next.Next)
	}
	if next.Items[0].EffectiveTrust != memory.TrustHumanApproved || len(next.Items[0].Sources) != 2 {
		t.Fatalf("approved proposal = %+v", next.Items[0])
	}

	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rendered := render.RenderAudit(page.RenderInput(marker), redaction.Policy{})
	for _, cited := range []string{"record: " + first.RecordID, "version: " + first.VersionID, "content_hash: " + first.ContentHash, "| Остаряло"} {
		if rendered.Kind != render.RenderedPart || !strings.Contains(rendered.Part.Text, cited+"\n") {
			t.Fatalf("rendering %s lacks %q:\n%s", rendered.Kind, cited, rendered.Part.Text)
		}
	}
}

func materialIDs(material audit.Material) []string {
	ids := make([]string, len(material.Items))
	for index, item := range material.Items {
		ids[index] = item.Record.ID
	}
	return ids
}

// The page stops before the record that would exceed the content budget,
// but always holds one record.
func TestAuditMaterialStopsAtTheContentBudget(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	a := humanAt(t, store, strings.Repeat("а", 50), 1) // 100 bytes
	b := humanAt(t, store, strings.Repeat("b", 60), 2)
	c := humanAt(t, store, strings.Repeat("c", 10), 3)
	reader := store.AuditMaterial()
	read := func(budget int) ([]string, bool) {
		t.Helper()
		page, err := reader.ReadAuditMaterial(ctx, audit.MaterialQuery{ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 10, ContentBudget: budget})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(page.Records))
		for index, stored := range page.Records {
			ids[index] = stored.Record.ID
		}
		return ids, page.More
	}
	for _, tc := range []struct {
		budget int
		want   []string
		more   bool
	}{
		{1, []string{a.RecordID}, true},
		{100, []string{a.RecordID}, true},
		{159, []string{a.RecordID}, true},
		{160, []string{a.RecordID, b.RecordID}, true},
		{170, []string{a.RecordID, b.RecordID, c.RecordID}, false},
	} {
		if ids, more := read(tc.budget); !slices.Equal(ids, tc.want) || more != tc.more {
			t.Fatalf("budget %d = %v more=%v, want %v more=%v", tc.budget, ids, more, tc.want, tc.more)
		}
	}
	page, err := reader.ReadAuditMaterial(ctx, audit.MaterialQuery{ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 2, ContentBudget: audit.MaxAuditPageContentBytes})
	if err != nil || len(page.Records) != 2 || !page.More {
		t.Fatalf("limit page = %+v, %v", page, err)
	}
	empty, err := reader.ReadAuditMaterial(ctx, audit.MaterialQuery{ProjectID: "nobody", Statuses: memory.AuditedStatuses(), Limit: 2, ContentBudget: 1})
	if err != nil || empty.Records != nil || empty.More {
		t.Fatalf("empty page = %+v, %v", empty, err)
	}
}

// Suggestions are recorded end to end as agent opinions with the suggestion
// column, and a recording refusal writes nothing.
func TestRecordSuggestionsEndToEnd(t *testing.T) {
	store := openMemoryStore(t)
	first := humanAt(t, store, "Първо решение", 1)
	second := humanAt(t, store, "Второ решение", 2)
	old := first
	old.VersionID, old.ContentHash = "version-unknown", memory.ContentHash("x")
	report := auditReport(
		suggestionEntry(first, memory.SuggestArchive, "дублира второто; token="+fakeSecretValue),
		suggestionEntry(second, memory.SuggestDelete, "отменено"),
	)
	got := mustRecordSuggestions(t, store, report, recordsNow.Add(10*time.Minute))
	if len(got.Recorded) != 2 || got.Recorded[0].Result.Kind != memory.OpinionAdd || got.Recorded[1].Result.Kind != memory.OpinionAdd || len(got.Extraction.Findings) != 1 {
		t.Fatalf("recorded = %+v", got)
	}
	rows := queryStrings(t, store, "SELECT version_id || '|' || actor || '|' || decision || '|' || reviewer || '|' || coalesce(suggestion, '-') || '|' || coalesce(reason, '-') FROM memory_reviews WHERE actor = 'agent' ORDER BY reviewed_at, id")
	want := sortedStrings(
		first.VersionID+"|agent|opinion|auditor|archive|дублира второто; token=[REDACTED:secret_assignment]",
		second.VersionID+"|agent|opinion|auditor|delete|отменено",
	)
	slices.Sort(rows)
	if !slices.Equal(rows, want) {
		t.Fatalf("reviews = %v", rows)
	}

	before := dumpMemoryTables(t, store)
	refused := mustRecordSuggestions(t, store, auditReport(suggestionEntry(old, memory.SuggestDelete, "x")), recordsNow.Add(11*time.Minute))
	if len(refused.Recorded) != 1 || refused.Recorded[0].Result.Kind != memory.OpinionNotFound {
		t.Fatalf("refused = %+v", refused)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))

	// Both reviews share reviewed_at, so their order is by review id.
	pending := pendingReviews(t, store)
	if len(pending) != 2 {
		t.Fatalf("pending = %+v", pending)
	}
	byRecord := map[string]audit.PendingSuggestion{pending[0].Record.ID: pending[0], pending[1].Record.ID: pending[1]}
	archive, remove := byRecord[first.RecordID], byRecord[second.RecordID]
	if archive.Action != memory.SuggestArchive || archive.Record.Version != 1 || archive.Reviewer != "auditor" ||
		archive.VersionID != first.VersionID || archive.ContentHash != first.ContentHash || !archive.ReviewedAt.Equal(recordsNow.Add(10*time.Minute)) ||
		remove.Action != memory.SuggestDelete || remove.VersionID != second.VersionID {
		t.Fatalf("pending = %+v", pending)
	}
}

// replaceCurrentVersion gives the record a new current version and bumps its
// optimistic version, as a reactivation would.
func replaceCurrentVersion(t *testing.T, store *Store, created records.CreateResult, versionID string) {
	t.Helper()
	mustExec(t, store, `INSERT INTO memory_record_versions (id, record_id, project_id, content, content_hash, search_text, search_normalization_version,
		author, trust, redaction_policy_version, visibility, origin, created_at)
		SELECT ?, record_id, project_id, 'нов текст', ?, 'нов текст', search_normalization_version, author, trust, redaction_policy_version, visibility, origin, created_at
		FROM memory_record_versions WHERE id = ?`, versionID, memory.ContentHash("нов текст"), created.VersionID)
	mustExec(t, store, "UPDATE memory_records SET current_version_id = ?, version = version + 1 WHERE id = ?", versionID, created.RecordID)
}

// Stale suggestions are not pending: a newer version or a status that no
// longer permits the action.
func TestPendingSuggestionsExcludeStaleOnes(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	renewed := humanAt(t, store, "Ще бъде променено", 1)
	archived := humanAt(t, store, "Ще бъде архивирано", 2)
	kept := humanAt(t, store, "Остава", 3)
	mustRecordSuggestions(t, store, auditReport(
		suggestionEntry(renewed, memory.SuggestDelete, "a"),
		suggestionEntry(archived, memory.SuggestArchive, "b"),
		suggestionEntry(kept, memory.SuggestArchive, "c"),
	), recordsNow.Add(10*time.Minute))
	mustRecordSuggestions(t, store, auditReport(suggestionEntry(archived, memory.SuggestDelete, "d")), recordsNow.Add(11*time.Minute))
	replaceCurrentVersion(t, store, renewed, "renewed-v2")
	if got, err := recordService(store, nil).Archive(ctx, records.ReasonRequest{ProjectID: "project", RecordID: archived.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "x", Now: recordsNow}); err != nil || got.Kind != memory.TransitionAllowed {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	pending := pendingReviews(t, store)
	var listed []string
	for _, suggestion := range pending {
		listed = append(listed, suggestion.Record.ID+":"+string(suggestion.Action))
	}
	// The delete suggestion of the archived record still applies.
	if !slices.Equal(listed, []string{kept.RecordID + ":archive", archived.RecordID + ":delete"}) {
		t.Fatalf("pending = %v", listed)
	}

	// Pages move past stale suggestions.
	reader := store.AuditSuggestions()
	page, err := reader.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 2)
	if err != nil || len(page.Suggestions) != 2 || !page.More {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	last := page.Suggestions[1]
	rest, err := reader.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{ReviewedAt: last.ReviewedAt, ReviewID: last.ReviewID}, 2)
	if err != nil || len(rest.Suggestions) != 2 || rest.More {
		t.Fatalf("second page = %+v, %v", rest, err)
	}
	other, err := reader.ReadAgentSuggestions(ctx, "other", audit.SuggestionPosition{}, 2)
	if err != nil || other.Suggestions != nil {
		t.Fatalf("other project = %+v, %v", other, err)
	}
}

func TestConfirmArchiveSuggestion(t *testing.T) {
	store := openMemoryStore(t)
	created := humanAt(t, store, "За архивиране", 1)
	recorded := mustRecordSuggestions(t, store, auditReport(suggestionEntry(created, memory.SuggestArchive, "остаряло")), recordsNow.Add(10*time.Minute))
	reviewID := recorded.Recorded[0].Result.ReviewID
	got := confirm(t, store, reviewID, 1, nil)
	if got.Kind != memory.SuggestionApplies || got.Action != memory.SuggestArchive || !got.Applied || got.Transition.RecordVersion != 2 {
		t.Fatalf("confirm = %+v", got)
	}
	if status := queryStrings(t, store, "SELECT status || ':' || CAST(version AS TEXT) FROM memory_records WHERE id = ?", created.RecordID); !slices.Equal(status, []string{"archived:2"}) {
		t.Fatalf("record = %v", status)
	}
	reviews := queryStrings(t, store, "SELECT actor || '|' || decision || '|' || coalesce(suggestion, '-') || '|' || coalesce(reason, '-') FROM memory_reviews WHERE version_id = ? ORDER BY actor, decision", created.VersionID)
	if !slices.Equal(reviews, []string{"agent|opinion|archive|остаряло", "human|approve|-|-", "human|opinion|-|съгласен"}) {
		t.Fatalf("reviews = %v", reviews)
	}
	if pending := pendingReviews(t, store); len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
	before := dumpMemoryTables(t, store)
	if again := confirm(t, store, reviewID, 2, nil); again.Kind != memory.SuggestionStatusMoved || again.Applied {
		t.Fatalf("second confirm = %+v", again)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
}

func TestConfirmDeleteSuggestion(t *testing.T) {
	store := openMemoryStore(t)
	created := humanAt(t, store, "За изтриване", 1)
	recorded := mustRecordSuggestions(t, store, auditReport(suggestionEntry(created, memory.SuggestDelete, "грешно")), recordsNow.Add(10*time.Minute))
	reviewID := recorded.Recorded[0].Result.ReviewID
	got := confirm(t, store, reviewID, 1, func(r *audit.ConfirmRequest) { r.Reason = "" })
	if got.Kind != memory.SuggestionApplies || got.Action != memory.SuggestDelete || !got.Applied || got.Deletion.RecordVersion != 2 || len(got.Deletion.Receipts) != 1 {
		t.Fatalf("confirm = %+v", got)
	}
	if content := queryStrings(t, store, "SELECT coalesce(content, '<deleted>') FROM memory_record_versions WHERE record_id = ?", created.RecordID); !slices.Equal(content, []string{"<deleted>"}) {
		t.Fatalf("content = %v", content)
	}
	// The suggestion stays as a tombstoned opinion: its reason is cleared
	// with the content.
	if reviews := queryStrings(t, store, "SELECT coalesce(suggestion, '-') || '|' || coalesce(reason, '-') FROM memory_reviews WHERE id = ?", reviewID); !slices.Equal(reviews, []string{"delete|-"}) {
		t.Fatalf("suggestion = %v", reviews)
	}
	if receipts := queryStrings(t, store, "SELECT subject_id FROM context_deletion_receipts"); !slices.Equal(receipts, []string{created.VersionID}) {
		t.Fatalf("receipts = %v", receipts)
	}
	if pending := pendingReviews(t, store); len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

// A confirmation refused because the record moved, by version or status, or
// by the action's own rule, leaves every memory table byte-identical.
func TestConfirmRefusalsWriteNothing(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	renewed := humanAt(t, store, "Ще бъде променено", 1)
	moved := humanAt(t, store, "Ще бъде архивирано", 2)
	waiting := humanAt(t, store, "Чака", 3)
	recorded := mustRecordSuggestions(t, store, auditReport(
		suggestionEntry(renewed, memory.SuggestDelete, "a"),
		suggestionEntry(moved, memory.SuggestArchive, "b"),
		suggestionEntry(waiting, memory.SuggestArchive, "c"),
	), recordsNow.Add(10*time.Minute))
	reviewOf := func(index int) string { return recorded.Recorded[index].Result.ReviewID }
	replaceCurrentVersion(t, store, renewed, "renewed-v2")
	if got, err := recordService(store, nil).Archive(ctx, records.ReasonRequest{ProjectID: "project", RecordID: moved.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "x", Now: recordsNow}); err != nil || got.Kind != memory.TransitionAllowed {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	approval := queryStrings(t, store, "SELECT id FROM memory_reviews WHERE actor = 'human' AND decision = 'approve' AND version_id = ?", waiting.VersionID)
	for _, tc := range []struct {
		name     string
		reviewID string
		version  int64
		change   func(*audit.ConfirmRequest)
		kind     memory.SuggestionKind
		action   memory.RecordTransitionKind
	}{
		{"new version", reviewOf(0), 2, nil, memory.SuggestionStale, ""},
		{"status moved", reviewOf(1), 2, nil, memory.SuggestionStatusMoved, ""},
		{"stale expected version", reviewOf(2), 3, nil, memory.SuggestionApplies, memory.TransitionVersionConflict},
		{"no human reason", reviewOf(2), 1, func(r *audit.ConfirmRequest) { r.Reason = "" }, memory.SuggestionApplies, memory.TransitionReasonRequired},
		{"agent confirms", reviewOf(2), 1, func(r *audit.ConfirmRequest) { r.Actor = memory.ActorAgent }, memory.SuggestionActorNotAllowed, ""},
		{"other project", reviewOf(2), 1, func(r *audit.ConfirmRequest) { r.ProjectID = "other" }, memory.SuggestionNotFound, ""},
		{"missing review", "review-missing", 1, nil, memory.SuggestionNotFound, ""},
		{"human approval is not a suggestion", approval[0], 1, nil, memory.SuggestionNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := dumpMemoryTables(t, store)
			got := confirm(t, store, tc.reviewID, tc.version, tc.change)
			if got.Kind != tc.kind || got.Applied || got.Transition.Kind != tc.action {
				t.Fatalf("confirm = %+v", got)
			}
			assertUnchanged(t, before, dumpMemoryTables(t, store))
		})
	}
}

func TestAuditReadFailures(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	created := humanAt(t, store, "Решение", 1)
	mustRecordSuggestions(t, store, auditReport(suggestionEntry(created, memory.SuggestArchive, "a")), recordsNow.Add(10*time.Minute))
	material, suggestions := store.AuditMaterial(), store.AuditSuggestions()
	query := audit.MaterialQuery{ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 1, ContentBudget: 1}
	for name, invalid := range map[string]audit.MaterialQuery{
		"zero page":     {ProjectID: "project", Statuses: memory.AuditedStatuses(), ContentBudget: 1},
		"zero budget":   {ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 1},
		"no statuses":   {ProjectID: "project", Limit: 1, ContentBudget: 1},
		"negative page": {ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: -1, ContentBudget: 1},
	} {
		if _, err := material.ReadAuditMaterial(ctx, invalid); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := suggestions.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 0); err == nil {
		t.Fatal("zero suggestion page accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := material.ReadAuditMaterial(cancelled, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled material = %v", err)
	}
	if _, err := suggestions.ReadAgentSuggestions(cancelled, "project", audit.SuggestionPosition{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled suggestions = %v", err)
	}

	mustExec(t, store, "UPDATE memory_reviews SET reviewed_at = 'later' WHERE actor = 'agent'")
	if _, err := suggestions.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 1); !errors.Is(err, ErrInvalidStoredTimestamp) || strings.Contains(err.Error(), "later") {
		t.Fatalf("invalid review time = %v", err)
	}
	mustExec(t, store, "UPDATE memory_reviews SET reviewed_at = ? WHERE actor = 'agent'", formatStoredTime(recordsNow))
	mustExec(t, store, "UPDATE memory_records SET created_at = 'yesterday' WHERE id = ?", created.RecordID)
	if _, err := material.ReadAuditMaterial(ctx, query); !errors.Is(err, ErrInvalidStoredTimestamp) || strings.Contains(err.Error(), "yesterday") {
		t.Fatalf("invalid creation time = %v", err)
	}
	if _, err := suggestions.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 1); !errors.Is(err, ErrInvalidStoredTimestamp) {
		t.Fatalf("invalid record time in suggestions = %v", err)
	}
	mustExec(t, store, "UPDATE memory_records SET created_at = ?, version = 'many' WHERE id = ?", formatStoredTime(recordsNow), created.RecordID)
	if _, err := material.ReadAuditMaterial(ctx, query); !errors.Is(err, ErrUndecodableMemoryRow) {
		t.Fatalf("undecodable record = %v", err)
	}
	if _, err := suggestions.ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 1); !errors.Is(err, ErrUndecodableMemoryRow) {
		t.Fatalf("undecodable suggestion = %v", err)
	}
}

// Every statement of the audit reads and of a confirmation fails cleanly
// with its cause, and the pooled connection is not left inside the
// transaction.
func TestAuditReportsStatementFailures(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	created := humanAt(t, seeded, "Решение", 1)
	recorded := mustRecordSuggestions(t, seeded, auditReport(suggestionEntry(created, memory.SuggestArchive, "a")), recordsNow.Add(10*time.Minute))
	reviewID := recorded.Recorded[0].Result.ReviewID
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	store := &Store{db: db}
	reads := map[string]func() error{
		"material": func() error {
			page, err := store.AuditMaterial().ReadAuditMaterial(ctx, audit.MaterialQuery{ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 5, ContentBudget: 1 << 20})
			if err == nil && (len(page.Records) != 1 || len(page.Records[0].Sources) != 1) {
				return fmt.Errorf("page = %+v", page)
			}
			return err
		},
		"suggestions": func() error {
			page, err := store.AuditSuggestions().ReadAgentSuggestions(ctx, "project", audit.SuggestionPosition{}, 5)
			if err == nil && len(page.Suggestions) != 1 {
				return fmt.Errorf("page = %+v", page)
			}
			return err
		},
		// A confirmation without the human's reason reads everything and
		// writes nothing, so it can repeat.
		"confirmation": func() error {
			got, err := auditService(store).ConfirmSuggestion(ctx, audit.ConfirmRequest{
				ProjectID: "project", ReviewID: reviewID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Now: recordsNow,
			})
			if err == nil && got.Transition.Kind != memory.TransitionReasonRequired {
				return fmt.Errorf("confirm = %+v", got)
			}
			return err
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			connector.mu.Lock()
			connector.statements = nil
			connector.mu.Unlock()
			if err := read(); err != nil {
				t.Fatal(err)
			}
			statements := slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(connector.statements))), func(statement string) bool {
				return strings.HasPrefix(statement, "PRAGMA") || strings.HasPrefix(statement, "ROLLBACK") || strings.HasPrefix(statement, "COMMIT")
			})
			if len(statements) == 0 {
				t.Fatalf("recorded statements = %q", statements)
			}
			for _, statement := range statements {
				injected := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
				connector.inject(strings.TrimSpace(statement), injected, 1)
				if err := read(); !errors.Is(err, injected) {
					t.Fatalf("failure of %.60q = %v", statement, err)
				}
				if err := read(); err != nil {
					t.Fatalf("read after failure of %.60q: %v", statement, err)
				}
			}
		})
	}
}

// A small record whose sources exceed the per-record count or byte bound
// stays on the page with its stored count and size, without its sources, and
// the rendering labels them as omitted.
func TestAuditMaterialOmitsSourcesOverTheLimits(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	within := humanAt(t, store, "В границите", 1)
	many := humanText("Много източници")
	many.Now = recordsNow.Add(2 * time.Minute)
	many.Sources = nil
	for index := range memory.MaxSourcesPerItem + 1 {
		many.Sources = append(many.Sources, memory.RecordSource{Kind: memory.SourceSpec, Ref: fmt.Sprintf("specs/%03d", index)})
	}
	overCount := mustCreate(t, store, many)
	large := humanText("Дълги източници")
	large.Now = recordsNow.Add(3 * time.Minute)
	large.Sources = nil
	for index := range 3 {
		large.Sources = append(large.Sources, memory.RecordSource{Kind: memory.SourceSpec, Ref: fmt.Sprintf("%d", index) + strings.Repeat("r", 3999)})
	}
	overBytes := mustCreate(t, store, large)

	page, err := store.AuditMaterial().ReadAuditMaterial(ctx, audit.MaterialQuery{ProjectID: "project", Statuses: memory.AuditedStatuses(), Limit: 10, ContentBudget: audit.MaxAuditPageContentBytes})
	if err != nil || len(page.Records) != 3 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	stored := map[string]audit.MaterialRecord{}
	for _, record := range page.Records {
		stored[record.Record.ID] = record
	}
	if got := stored[within.RecordID]; got.SourceCount != 1 || got.SourceBytes != int64(len("specs/012")) || len(got.Sources) != 1 {
		t.Fatalf("within = %+v", got)
	}
	if got := stored[overCount.RecordID]; got.SourceCount != memory.MaxSourcesPerItem+1 || got.Sources != nil {
		t.Fatalf("over count = %+v", got)
	}
	if got := stored[overBytes.RecordID]; got.SourceCount != 3 || got.SourceBytes != 12000 || got.Sources != nil {
		t.Fatalf("over bytes = %+v", got)
	}

	material, err := auditService(store).AuditMaterial(ctx, audit.MaterialRequest{ProjectID: "project", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rendered := render.RenderAudit(material.RenderInput(marker), redaction.Policy{})
	for _, label := range []string{
		fmt.Sprintf("sources: omitted (%d, over the per-record limit)", memory.MaxSourcesPerItem+1),
		"sources: omitted (3, over the per-record limit)",
		"source: spec ref=\"specs/012\"",
	} {
		if rendered.Kind != render.RenderedPart || !strings.Contains(rendered.Part.Text, label+"\n") {
			t.Fatalf("rendering %s lacks %q", rendered.Kind, label)
		}
	}
	if strings.Contains(rendered.Part.Text, "specs/000") || strings.Contains(rendered.Part.Text, "rrrr") {
		t.Fatal("omitted sources were rendered")
	}
}

// openFaultyWithSuggestion seeds one record and an agent suggestion of
// action about it, then reopens the database through the fault injector.
func openFaultyWithSuggestion(t *testing.T, action memory.Suggestion) (*Store, *faultConnector, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	created := humanAt(t, seeded, "Решение", 1)
	recorded := mustRecordSuggestions(t, seeded, auditReport(suggestionEntry(created, action, "a")), recordsNow.Add(10*time.Minute))
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	return &Store{db: db}, connector, recorded.Recorded[0].Result.ReviewID
}

// A confirmation is one transaction: a failure after its first write, or of
// its COMMIT, returns the error, rolls everything back and leaves every
// memory table byte-identical; a retry then applies the action once.
func TestConfirmRollsBackAfterItsFirstWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action memory.Suggestion
		// fails is the prefix of a statement after the first write, or COMMIT.
		fails string
	}{
		{"archive review insert", memory.SuggestArchive, "INSERT INTO memory_reviews"},
		{"archive commit", memory.SuggestArchive, "COMMIT"},
		{"delete content clear", memory.SuggestDelete, "UPDATE memory_record_versions"},
		{"delete receipts", memory.SuggestDelete, "INSERT INTO context_deletion_receipts"},
		{"delete commit", memory.SuggestDelete, "COMMIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, connector, reviewID := openFaultyWithSuggestion(t, tc.action)
			before := dumpMemoryTables(t, store)
			injected := fmt.Errorf("%w: injected", tursogo.ErrTursoGeneric)
			connector.inject(tc.fails, injected, 1)
			request := audit.ConfirmRequest{
				ProjectID: "project", ReviewID: reviewID, ExpectedVersion: 1,
				Actor: memory.ActorHuman, Reviewer: "martin", Reason: "съгласен", Now: recordsNow.Add(time.Hour),
			}
			got, err := auditService(store).ConfirmSuggestion(context.Background(), request)
			if !errors.Is(err, injected) || got.Kind != "" || got.Applied {
				t.Fatalf("result %+v, error %v", got, err)
			}
			if connector.count("UPDATE memory_records") != 1 {
				t.Fatalf("first write ran %d times", connector.count("UPDATE memory_records"))
			}
			assertUnchanged(t, before, dumpMemoryTables(t, store))
			assertPoolOutsideTransaction(t, store)
			retried, err := auditService(store).ConfirmSuggestion(context.Background(), request)
			if err != nil || !retried.Applied {
				t.Fatalf("retry = %+v, %v", retried, err)
			}
		})
	}
}

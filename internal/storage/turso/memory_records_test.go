package turso

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/compose"
	"github.com/MartinAndreev/conduit/internal/memory/deletion"
	"github.com/MartinAndreev/conduit/internal/memory/records"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
	"github.com/MartinAndreev/conduit/internal/storage/turso/internal/childprocess"
)

var recordsNow = time.Date(2026, 11, 12, 10, 0, 0, 7, time.FixedZone("EET", 2*60*60))

// fakeSecretValue has a vendor token shape the redaction policy masks.
const fakeSecretValue = "sk-test-0123456789abcdefghijklmnop"

// snapshotFiles is a target snapshot with fixed file hashes; other paths are
// missing.
type snapshotFiles map[string]string

func (f snapshotFiles) ObserveFile(_ context.Context, _ compose.SnapshotRef, path string) (compose.FileObservation, error) {
	if hash, ok := f[path]; ok {
		return compose.FileObservation{Observation: memory.SourcePresent, Hash: hash}, nil
	}
	return compose.FileObservation{Observation: memory.SourceMissing}, nil
}

func recordService(store *Store, files snapshotFiles) records.Service {
	return records.Service{Store: store.MemoryRecords(), Snapshot: files}
}

func humanText(content string) records.CreateRequest {
	return records.CreateRequest{
		Producer: memory.ProducerHuman,
		Actor:    memory.ActorHuman,
		Author:   "martin",
		Category: memory.CategoryDecision,
		Content:  content,
		Scope:    memory.Scope{ProjectID: "project", Visibility: memory.VisibilityProject, Origin: memory.OriginHuman},
		Sources:  []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}},
		Now:      recordsNow,
	}
}

// proposalFromAcceptedResult is an agent proposal visible to the project
// through an accepted result, with one file source.
func proposalFromAcceptedResult(content string) records.CreateRequest {
	return records.CreateRequest{
		Producer: memory.ProducerAgentProposal,
		Actor:    memory.ActorSystem,
		Author:   "codex",
		Category: memory.CategoryGotcha,
		Content:  content,
		Scope: memory.Scope{
			ProjectID: "project", Visibility: memory.VisibilityProject, Origin: memory.OriginAttempt,
			RunID: "run", AttemptID: "attempt", BaseCommit: "abc", AcceptedResultRef: "result",
		},
		Sources: []memory.RecordSource{
			{Kind: memory.SourceFile, Path: "internal/storage/turso/migration.go", BlobHash: "f6783ab4cb25f4cfec064862e7d2f5a8501117a0"},
			{Kind: memory.SourceEvent, Ref: "event-1"},
		},
		Now: recordsNow,
	}
}

func mustCreate(t *testing.T, store *Store, request records.CreateRequest) records.CreateResult {
	t.Helper()
	got, err := recordService(store, nil).CreateRecord(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != memory.CreationAllowed {
		t.Fatalf("create = %+v", got)
	}
	return got
}

// composeFor composes the project's memory for a search, so eligibility is
// decided by the real rules over what the real reader returns.
func composeFor(t *testing.T, store *Store, files snapshotFiles, query string, linked ...string) compose.Composition {
	t.Helper()
	composer, err := compose.New(store.MemoryReader(), files, redaction.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := render.NewMarker(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := composer.ComposeContext(context.Background(), compose.Request{
		ProjectID: "project", AttemptID: "other-attempt", Snapshot: "snapshot", Query: query, ExplicitRecordIDs: linked, Marker: marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func itemTrust(c compose.Composition, recordID string) (memory.Trust, bool) {
	for _, item := range c.Items {
		if item.RecordID == recordID {
			return item.Trust, true
		}
	}
	return "", false
}

func exclusionKind(c compose.Composition, recordID string) compose.ExclusionKind {
	for _, x := range c.Exclusions {
		if x.RecordID == recordID {
			return x.Kind
		}
	}
	return ""
}

func TestCreateHumanRecordIsStoredApprovedAndEligible(t *testing.T) {
	store := openMemoryStore(t)
	created := mustCreate(t, store, humanText("Паметта е локална; api_key = "+fakeSecretValue))
	if created.Status != memory.StatusActive || len(created.Findings) != 1 {
		t.Fatalf("create = %+v", created)
	}
	if got := queryStrings(t, store, "SELECT category || '|' || status || '|' || CAST(version AS TEXT) || '|' || created_at || '|' || updated_at FROM memory_records"); !slices.Equal(got, []string{
		"decision|active|1|" + formatStoredTime(recordsNow) + "|" + formatStoredTime(recordsNow),
	}) {
		t.Fatalf("record = %v", got)
	}
	versions := queryStrings(t, store, "SELECT content || '|' || search_text || '|' || content_hash || '|' || trust || '|' || author || '|' || redaction_policy_version || '|' || origin || '|' || visibility || '|' || CAST(search_normalization_version AS TEXT) || '|' || coalesce(run_id, '-') FROM memory_record_versions")
	content := "Паметта е локална; api_key = [REDACTED:secret_assignment]"
	if !slices.Equal(versions, []string{content + "|" + normalizeSearchText(content) + "|" + memory.ContentHash(content) + "|human_approved|martin|" + redaction.PolicyVersion + "|human|project|1|-"}) {
		t.Fatalf("versions = %v", versions)
	}
	if got := queryStrings(t, store, "SELECT version_id || '|' || content_hash || '|' || decision || '|' || actor || '|' || reviewer || '|' || coalesce(reason, '-') || '|' || reviewed_at FROM memory_reviews"); !slices.Equal(got, []string{
		created.VersionID + "|" + created.ContentHash + "|approve|human|martin|-|" + formatStoredTime(recordsNow),
	}) {
		t.Fatalf("reviews = %v", got)
	}
	if got := queryStrings(t, store, "SELECT kind || '|' || ref FROM memory_sources"); !slices.Equal(got, []string{"spec|specs/012"}) {
		t.Fatalf("sources = %v", got)
	}
	got := composeFor(t, store, nil, "паметта")
	if trust, ok := itemTrust(got, created.RecordID); !ok || trust != memory.TrustHumanApproved {
		t.Fatalf("composition = %+v", got)
	}
	linked := composeFor(t, store, nil, "", created.RecordID)
	if linked.Kind != compose.Composed {
		t.Fatalf("linked composition = %+v", linked)
	}
}

// An agent proposal stays out until a human approves its exact version; the
// approval makes it eligible as human_approved without a new version.
func TestAgentProposalBecomesEligibleOnlyByExactApproval(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	files := snapshotFiles{"internal/storage/turso/migration.go": "f6783ab4cb25f4cfec064862e7d2f5a8501117a0"}
	created := mustCreate(t, store, proposalFromAcceptedResult("Неуспешен COMMIT оставя транзакцията отворена"))
	if created.Status != memory.StatusProposed {
		t.Fatalf("create = %+v", created)
	}
	before := composeFor(t, store, files, "commit")
	if kind := exclusionKind(before, created.RecordID); kind != compose.ExclusionKind(memory.RetrievalLifecycleExcluded) {
		t.Fatalf("proposal exclusion = %q in %+v", kind, before)
	}
	service := recordService(store, files)
	approve := records.ApproveRequest{ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", VersionID: created.VersionID, ContentHash: created.ContentHash, Now: recordsNow}
	wrongHash := approve
	wrongHash.ContentHash = memory.ContentHash("other")
	if got, err := service.Approve(ctx, wrongHash); err != nil || got.Kind != memory.TransitionApprovalMismatch {
		t.Fatalf("approve other hash = %+v, %v", got, err)
	}
	got, err := service.Approve(ctx, approve)
	if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 2 {
		t.Fatalf("approve = %+v, %v", got, err)
	}
	if got := queryStrings(t, store, "SELECT status || '|' || CAST(version AS TEXT) FROM memory_records"); !slices.Equal(got, []string{"active|2"}) {
		t.Fatalf("record = %v", got)
	}
	if got := queryStrings(t, store, "SELECT trust FROM memory_record_versions"); !slices.Equal(got, []string{"agent_claim"}) {
		t.Fatalf("stored trust = %v", got)
	}
	after := composeFor(t, store, files, "commit")
	if trust, ok := itemTrust(after, created.RecordID); !ok || trust != memory.TrustHumanApproved {
		t.Fatalf("composition = %+v", after)
	}
	if got, err := service.Approve(ctx, approve); err != nil || got.Kind != memory.TransitionVersionConflict {
		t.Fatalf("second approval = %+v, %v", got, err)
	}
}

func TestRejectArchiveAndOpinionWriteRedactedReasons(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	service := recordService(store, nil)
	proposal := mustCreate(t, store, proposalFromAcceptedResult("Предложение за отхвърляне"))
	active := mustCreate(t, store, humanText("Решение за архивиране"))
	reason := "дублира друг запис; token=" + fakeSecretValue
	rejected, err := service.Reject(ctx, records.ReasonRequest{ProjectID: "project", RecordID: proposal.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: reason, Now: recordsNow})
	if err != nil || rejected.Kind != memory.TransitionAllowed || len(rejected.Findings) != 1 {
		t.Fatalf("reject = %+v, %v", rejected, err)
	}
	archived, err := service.Archive(ctx, records.ReasonRequest{ProjectID: "project", RecordID: active.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "остаряло", Now: recordsNow})
	if err != nil || archived.Kind != memory.TransitionAllowed {
		t.Fatalf("archive = %+v, %v", archived, err)
	}
	opinion, err := service.AddOpinion(ctx, records.OpinionRequest{ProjectID: "project", RecordID: active.RecordID, VersionID: active.VersionID, ContentHash: active.ContentHash, Actor: memory.ActorAgent, Reviewer: "auditor", Reason: "може да се изтрие", Now: recordsNow})
	if err != nil || opinion.Kind != memory.OpinionAdd {
		t.Fatalf("opinion = %+v, %v", opinion, err)
	}
	if got := queryStrings(t, store, "SELECT id || ':' || status || ':' || CAST(version AS TEXT) FROM memory_records ORDER BY id"); !slices.Equal(got, sortedStrings(
		proposal.RecordID+":rejected:2", active.RecordID+":archived:2",
	)) {
		t.Fatalf("records = %v", got)
	}
	reviews := queryStrings(t, store, "SELECT version_id || '|' || decision || '|' || actor || '|' || coalesce(reason, '-') FROM memory_reviews ORDER BY reviewed_at, decision, actor")
	want := sortedStrings(
		proposal.VersionID+"|reject|human|дублира друг запис; token=[REDACTED:secret_assignment]",
		active.VersionID+"|approve|human|-",
		active.VersionID+"|opinion|human|остаряло",
		active.VersionID+"|opinion|agent|може да се изтрие",
	)
	slices.Sort(reviews)
	if !slices.Equal(reviews, want) {
		t.Fatalf("reviews = %v", reviews)
	}
	if strings.Contains(strings.Join(reviews, "\n"), fakeSecretValue) {
		t.Fatal("secret persisted in a reason")
	}
}

func sortedStrings(values ...string) []string {
	slices.Sort(values)
	return values
}

func TestMarkNeedsVerificationAndReactivate(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	request := humanText("Миграцията държи write lock")
	request.Sources = []memory.RecordSource{
		{Kind: memory.SourceFile, Path: "internal/storage/turso/migration.go", BlobHash: "f6783ab4cb25f4cfec064862e7d2f5a8501117a0"},
		{Kind: memory.SourceSpec, Ref: "specs/012"},
	}
	created := mustCreate(t, store, request)
	changed := snapshotFiles{"internal/storage/turso/migration.go": "0009ce64669c375934511edc6125facbcae1063f"}
	service := recordService(store, changed)
	unchanged, err := recordService(store, snapshotFiles{"internal/storage/turso/migration.go": "f6783ab4cb25f4cfec064862e7d2f5a8501117a0"}).MarkNeedsVerification(ctx, records.VerificationRequest{ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Snapshot: "snapshot", Now: recordsNow})
	if err != nil || unchanged.Kind != memory.TransitionSourceNotMismatched {
		t.Fatalf("mark unchanged = %+v, %v", unchanged, err)
	}
	marked, err := service.MarkNeedsVerification(ctx, records.VerificationRequest{ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Snapshot: "snapshot", Now: recordsNow})
	if err != nil || marked.Kind != memory.TransitionAllowed || marked.RecordVersion != 2 {
		t.Fatalf("mark = %+v, %v", marked, err)
	}
	reactivate := records.ReactivateRequest{ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 2, Actor: memory.ActorHuman, Reviewer: "martin", Snapshot: "snapshot", Now: recordsNow.Add(time.Minute)}
	if got, err := service.Reactivate(ctx, reactivate); err != nil || got.Kind != memory.TransitionVerificationMissing {
		t.Fatalf("reactivate without verification = %+v, %v", got, err)
	}
	reactivate.Verifications = []records.Verification{{Path: "internal/storage/turso/migration.go", VerifiedHash: "0009ce64669c375934511edc6125facbcae1063f"}}
	got, err := service.Reactivate(ctx, reactivate)
	if err != nil || got.Kind != memory.TransitionAllowed || got.RecordVersion != 3 || got.VersionID == "" {
		t.Fatalf("reactivate = %+v, %v", got, err)
	}
	if rows := queryStrings(t, store, "SELECT status || '|' || current_version_id || '|' || CAST(version AS TEXT) FROM memory_records"); !slices.Equal(rows, []string{"active|" + got.VersionID + "|3"}) {
		t.Fatalf("record = %v", rows)
	}
	versions := queryStrings(t, store, "SELECT id || '|' || content_hash || '|' || trust || '|' || created_at || '|' || (content = (SELECT content FROM memory_record_versions WHERE id = ?)) FROM memory_record_versions ORDER BY created_at", created.VersionID)
	if !slices.Equal(versions, []string{
		created.VersionID + "|" + created.ContentHash + "|human_approved|" + formatStoredTime(recordsNow) + "|1",
		got.VersionID + "|" + created.ContentHash + "|human_approved|" + formatStoredTime(recordsNow.Add(time.Minute)) + "|1",
	}) {
		t.Fatalf("versions = %v", versions)
	}
	if rows := queryStrings(t, store, "SELECT kind || '|' || coalesce(path, ref) || '|' || coalesce(blob_hash, '-') FROM memory_sources WHERE version_id = ? ORDER BY kind", got.VersionID); !slices.Equal(rows, []string{
		"file|internal/storage/turso/migration.go|0009ce64669c375934511edc6125facbcae1063f", "spec|specs/012|-",
	}) {
		t.Fatalf("new sources = %v", rows)
	}
	if rows := queryStrings(t, store, "SELECT decision || '|' || actor FROM memory_reviews WHERE version_id = ?", got.VersionID); !slices.Equal(rows, []string{"approve|human"}) {
		t.Fatalf("new version reviews = %v", rows)
	}
	after := composeFor(t, store, changed, "write")
	if trust, ok := itemTrust(after, created.RecordID); !ok || trust != memory.TrustHumanApproved || after.Items[0].VersionID != got.VersionID {
		t.Fatalf("composition = %+v", after)
	}
}

func TestSupersedeWritesTheReplacementAndRelation(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	old := mustCreate(t, store, humanText("Стара политика за паметта"))
	got, err := recordService(store, nil).Supersede(ctx, records.SupersedeRequest{
		ProjectID: "project", RecordID: old.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Author: "martin",
		Category: memory.CategoryConvention, Content: "Нова политика за паметта",
		Sources: []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, Now: recordsNow,
	})
	if err != nil || got.Creation.Kind != memory.CreationAllowed || got.Transition.Kind != memory.TransitionAllowed || got.RecordVersion != 2 {
		t.Fatalf("supersede = %+v, %v", got, err)
	}
	if rows := queryStrings(t, store, "SELECT id || '|' || status || '|' || category FROM memory_records ORDER BY id"); !slices.Equal(rows, sortedStrings(
		old.RecordID+"|superseded|decision", got.Replacement+"|active|convention",
	)) {
		t.Fatalf("records = %v", rows)
	}
	if rows := queryStrings(t, store, "SELECT from_record_id || '|' || kind || '|' || to_record_id FROM memory_relations"); !slices.Equal(rows, []string{got.Replacement + "|supersedes|" + old.RecordID}) {
		t.Fatalf("relations = %v", rows)
	}
	composed := composeFor(t, store, nil, "политика")
	if _, ok := itemTrust(composed, got.Replacement); !ok || exclusionKind(composed, old.RecordID) != compose.ExclusionKind(memory.RetrievalLifecycleExcluded) {
		t.Fatalf("composition = %+v", composed)
	}
}

func TestArchiveExpiredProposalsSweep(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	createdAt := func(days int) records.CreateRequest {
		request := proposalFromAcceptedResult("Предложение")
		request.Now = recordsNow.Add(-time.Duration(days) * 24 * time.Hour)
		return request
	}
	oldest, old, young := mustCreate(t, store, createdAt(120)), mustCreate(t, store, createdAt(90)), mustCreate(t, store, createdAt(30))
	decided := mustCreate(t, store, createdAt(100))
	// A proposal with a human decision that is still proposed is not
	// archived by the system.
	mustExec(t, store, "INSERT INTO memory_reviews (id, version_id, content_hash, decision, actor, reviewer, reviewed_at) VALUES ('decided-reject', ?, ?, 'reject', 'human', 'martin', ?)", decided.VersionID, decided.ContentHash, formatStoredTime(recordsNow))
	service := recordService(store, nil)
	first, err := service.ArchiveExpiredProposals(ctx, records.SweepRequest{Now: recordsNow, Limit: 2})
	if err != nil || first.Examined != 2 || !slices.Equal(first.Archived, []string{oldest.RecordID}) || first.Refused[memory.TransitionDecisionRecorded] != 1 {
		t.Fatalf("first sweep = %+v, %v", first, err)
	}
	if first.Next.After.RecordID != decided.RecordID || !first.Next.Cutoff.Equal(memory.ProposalRetentionCutoff(recordsNow)) {
		t.Fatalf("first cursor = %+v", first.Next)
	}
	second, err := service.ArchiveExpiredProposals(ctx, records.SweepRequest{Now: recordsNow, Limit: 10, Cursor: first.Next})
	if err != nil || second.Examined != 1 || !slices.Equal(second.Archived, []string{old.RecordID}) || second.Next != (records.SweepCursor{}) {
		t.Fatalf("second sweep = %+v, %v", second, err)
	}
	if rows := queryStrings(t, store, "SELECT status FROM memory_records WHERE id = ?", young.RecordID); !slices.Equal(rows, []string{"proposed"}) {
		t.Fatalf("young = %v", rows)
	}
	if rows := queryStrings(t, store, "SELECT CAST(version AS TEXT) || '|' || updated_at FROM memory_records WHERE id = ?", old.RecordID); !slices.Equal(rows, []string{"2|" + formatStoredTime(recordsNow)}) {
		t.Fatalf("archived = %v", rows)
	}
}

// N refused proposals older than an allowed one cannot block it: consecutive
// batches of limit N continue after them and reach it, and every refusal is
// still counted.
func TestSweepReachesProposalsBehindRefusedOnes(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	const limit = 3
	for index := range limit {
		request := proposalFromAcceptedResult("Решено предложение")
		request.Now = recordsNow.Add(-time.Duration(200-index) * 24 * time.Hour)
		decided := mustCreate(t, store, request)
		mustExec(t, store, "INSERT INTO memory_reviews (id, version_id, content_hash, decision, actor, reviewer, reviewed_at) VALUES (?, ?, ?, 'approve', 'human', 'martin', ?)", "approve-"+decided.RecordID, decided.VersionID, decided.ContentHash, formatStoredTime(recordsNow))
	}
	request := proposalFromAcceptedResult("Изтекло предложение")
	request.Now = recordsNow.Add(-100 * 24 * time.Hour)
	open := mustCreate(t, store, request)
	service := recordService(store, nil)

	// Without the cursor every batch would be the same refused rows.
	restart, err := service.ArchiveExpiredProposals(ctx, records.SweepRequest{Now: recordsNow, Limit: limit})
	if err != nil || restart.Examined != limit || len(restart.Archived) != 0 || restart.Refused[memory.TransitionDecisionRecorded] != limit {
		t.Fatalf("first batch = %+v, %v", restart, err)
	}
	next, err := service.ArchiveExpiredProposals(ctx, records.SweepRequest{Now: recordsNow.Add(time.Minute), Limit: limit, Cursor: restart.Next})
	if err != nil || next.Examined != 1 || !slices.Equal(next.Archived, []string{open.RecordID}) || next.Next != (records.SweepCursor{}) {
		t.Fatalf("second batch = %+v, %v", next, err)
	}
	if rows := queryStrings(t, store, "SELECT status FROM memory_records WHERE id = ?", open.RecordID); !slices.Equal(rows, []string{"archived"}) {
		t.Fatalf("open proposal = %v", rows)
	}
}

// A stored creation time the cursor cannot use fails the batch without a
// stored value in the error.
func TestSweepRejectsInvalidStoredTime(t *testing.T) {
	store := openMemoryStore(t)
	request := proposalFromAcceptedResult("Предложение")
	request.Now = recordsNow.Add(-200 * 24 * time.Hour)
	created := mustCreate(t, store, request)
	mustExec(t, store, "UPDATE memory_records SET created_at = '2020-01-01 noon' WHERE id = ?", created.RecordID)
	_, err := recordService(store, nil).ArchiveExpiredProposals(context.Background(), records.SweepRequest{Now: recordsNow, Limit: 5})
	if !errors.Is(err, ErrInvalidStoredTimestamp) || strings.Contains(err.Error(), "noon") {
		t.Fatalf("error = %v", err)
	}
}

// After a deletion, nothing writes a reason, a source or an approval onto the
// tombstoned versions; transitions that write nothing onto them still run.
func TestTombstonedVersionsRefuseReasonsAndApprovals(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	service := recordService(store, nil)
	active := mustCreate(t, store, humanText("Текст за изтриване"))
	proposal := mustCreate(t, store, proposalFromAcceptedResult("Предложение за изтриване"))
	for _, created := range []records.CreateResult{active, proposal} {
		deleted, err := deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Now: recordsNow})
		if err != nil || deleted.Kind != memory.DeletionDelete {
			t.Fatalf("delete = %+v, %v", deleted, err)
		}
	}
	before := dumpMemoryTables(t, store)
	reason := records.ReasonRequest{ProjectID: "project", RecordID: active.RecordID, ExpectedVersion: 2, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "остаряло", Now: recordsNow}
	if got, err := service.Archive(ctx, reason); err != nil || got.Kind != memory.TransitionContentDeleted {
		t.Fatalf("archive = %+v, %v", got, err)
	}
	reason.RecordID = proposal.RecordID
	if got, err := service.Reject(ctx, reason); err != nil || got.Kind != memory.TransitionContentDeleted {
		t.Fatalf("reject = %+v, %v", got, err)
	}
	if got, err := service.Approve(ctx, records.ApproveRequest{ProjectID: "project", RecordID: proposal.RecordID, ExpectedVersion: 2, Actor: memory.ActorHuman, Reviewer: "martin", VersionID: proposal.VersionID, ContentHash: proposal.ContentHash, Now: recordsNow}); err != nil || got.Kind != memory.TransitionContentDeleted {
		t.Fatalf("approve = %+v, %v", got, err)
	}
	opinion := records.OpinionRequest{ProjectID: "project", RecordID: active.RecordID, VersionID: active.VersionID, ContentHash: active.ContentHash, Actor: memory.ActorAgent, Reviewer: "auditor", Reason: "цитира изтрития текст", Now: recordsNow}
	if got, err := service.AddOpinion(ctx, opinion); err != nil || got.Kind != memory.OpinionContentDeleted {
		t.Fatalf("opinion = %+v, %v", got, err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))

	opinion.Reason = ""
	if got, err := service.AddOpinion(ctx, opinion); err != nil || got.Kind != memory.OpinionAdd {
		t.Fatalf("opinion without reason = %+v, %v", got, err)
	}
	superseded, err := service.Supersede(ctx, records.SupersedeRequest{ProjectID: "project", RecordID: active.RecordID, ExpectedVersion: 2, Actor: memory.ActorHuman, Author: "martin", Category: memory.CategoryFact, Content: "Заместител", Sources: []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, Now: recordsNow})
	if err != nil || superseded.Transition.Kind != memory.TransitionAllowed {
		t.Fatalf("supersede deleted = %+v, %v", superseded, err)
	}
	if rows := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM memory_reviews r JOIN memory_record_versions v ON v.id = r.version_id WHERE v.content_deleted_at IS NOT NULL AND r.reason IS NOT NULL"); !slices.Equal(rows, []string{"0"}) {
		t.Fatalf("reasons on tombstones = %v", rows)
	}
}

// The adapter's guards hold even if a procedure decided wrongly: each write
// that does not match the stored rows fails with ErrMemoryWriteDiverged and
// rolls back every write of its transaction.
func TestRecordWriteGuardsRollBack(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	live := mustCreate(t, store, humanText("Жив запис"))
	gone := mustCreate(t, store, humanText("Изтрит запис"))
	if _, err := (deletion.Service{Store: store.MemoryDeletion()}).DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: gone.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Now: recordsNow}); err != nil {
		t.Fatal(err)
	}
	before := dumpMemoryTables(t, store)
	change := records.StatusChange{ProjectID: "project", RecordID: live.RecordID, ExpectedVersion: 1, From: memory.StatusActive, To: memory.StatusArchived, UpdatedAt: recordsNow}
	review := records.Review{ID: "guarded-review", VersionID: live.VersionID, ContentHash: live.ContentHash, Decision: memory.ReviewOpinion, Actor: memory.ActorAgent, Reviewer: "auditor", Reason: "x", ReviewedAt: recordsNow}
	copied := records.VersionCopy{ProjectID: "project", RecordID: live.RecordID, FromVersionID: live.VersionID, VersionID: "copied", CreatedAt: recordsNow}
	cases := map[string]func(records.Transaction) error{
		"stale version": func(tx records.Transaction) error { c := change; c.ExpectedVersion = 2; return tx.ChangeStatus(ctx, c) },
		"other status": func(tx records.Transaction) error {
			c := change
			c.From = memory.StatusProposed
			return tx.ChangeStatus(ctx, c)
		},
		"foreign project": func(tx records.Transaction) error { c := change; c.ProjectID = "other"; return tx.ChangeStatus(ctx, c) },
		"reason on deleted": func(tx records.Transaction) error {
			r := review
			r.VersionID, r.ContentHash = gone.VersionID, gone.ContentHash
			return tx.InsertReview(ctx, r)
		},
		"review of nothing": func(tx records.Transaction) error {
			r := review
			r.ContentHash = memory.ContentHash("other")
			return tx.InsertReview(ctx, r)
		},
		"copy deleted": func(tx records.Transaction) error {
			c := copied
			c.RecordID, c.FromVersionID = gone.RecordID, gone.VersionID
			return tx.CopyVersion(ctx, c)
		},
		"copy foreign": func(tx records.Transaction) error {
			c := copied
			c.RecordID = gone.RecordID
			return tx.CopyVersion(ctx, c)
		},
		"after a good write": func(tx records.Transaction) error {
			_ = tx.InsertReview(ctx, review)
			c := change
			c.ExpectedVersion = 5
			return tx.ChangeStatus(ctx, c)
		},
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			err := store.MemoryRecords().WithinRecordWrite(ctx, write)
			if !errors.Is(err, ErrMemoryWriteDiverged) {
				t.Fatalf("error = %v", err)
			}
			assertUnchanged(t, before, dumpMemoryTables(t, store))
		})
	}
	// Without a reason, a review of a deleted version passes the guard.
	err := store.MemoryRecords().WithinRecordWrite(ctx, func(tx records.Transaction) error {
		r := review
		r.VersionID, r.ContentHash, r.Reason = gone.VersionID, gone.ContentHash, ""
		return tx.InsertReview(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadRecordFacts(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	created := mustCreate(t, store, proposalFromAcceptedResult("Факти"))
	var facts records.RecordFacts
	var missing records.RecordFacts
	var version, otherVersion records.VersionFacts
	err := store.MemoryRecords().WithinRecordWrite(ctx, func(tx records.Transaction) error {
		var err error
		if facts, err = tx.ReadRecord(ctx, created.RecordID); err != nil {
			return err
		}
		if missing, err = tx.ReadRecord(ctx, "missing"); err != nil {
			return err
		}
		if version, err = tx.ReadVersion(ctx, created.RecordID, created.VersionID); err != nil {
			return err
		}
		otherVersion, err = tx.ReadVersion(ctx, "missing", created.VersionID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := memory.Record{ID: created.RecordID, ProjectID: "project", Category: memory.CategoryGotcha, Status: memory.StatusProposed, CurrentVersionID: created.VersionID, Version: 1, CreatedAt: recordsNow.UTC()}
	if !facts.Found || facts.Record != want || facts.CurrentContentHash != created.ContentHash || facts.CurrentContentDeleted || facts.HumanDecisionRecorded {
		t.Fatalf("facts = %+v", facts)
	}
	// Sources come back ordered by their adapter-local, random IDs.
	byKind := func(a, b memory.RecordSource) int { return strings.Compare(string(a.Kind), string(b.Kind)) }
	wantSources := slices.SortedFunc(slices.Values(proposalFromAcceptedResult("").Sources), byKind)
	if !slices.Equal(slices.SortedFunc(slices.Values(facts.Sources), byKind), wantSources) {
		t.Fatalf("sources = %+v", facts.Sources)
	}
	if missing.Found || !version.Found || version.ContentHash != created.ContentHash || version.ContentDeleted || otherVersion.Found {
		t.Fatalf("missing %+v, version %+v, other %+v", missing, version, otherVersion)
	}
}

// Stored values that do not fit the reader's types fail with sentinel errors
// that carry no stored value.
func TestReadRecordRejectsUndecodableRows(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	created := mustCreate(t, store, humanText("Неразчетим запис"))
	read := func() error {
		return store.MemoryRecords().WithinRecordWrite(ctx, func(tx records.Transaction) error {
			_, err := tx.ReadRecord(ctx, created.RecordID)
			return err
		})
	}
	mustExec(t, store, "UPDATE memory_records SET created_at = 'yesterday' WHERE id = ?", created.RecordID)
	if err := read(); !errors.Is(err, ErrInvalidStoredTimestamp) || strings.Contains(err.Error(), "yesterday") {
		t.Fatalf("error = %v", err)
	}
	mustExec(t, store, "UPDATE memory_records SET created_at = ?, version = 'many' WHERE id = ?", formatStoredTime(recordsNow), created.RecordID)
	if err := read(); !errors.Is(err, ErrUndecodableMemoryRow) {
		t.Fatalf("error = %v", err)
	}
}

// Two writers transition the same record with the same expected version: the
// write lock serializes them, so exactly one wins and the other sees the
// change as a version conflict.
func TestConcurrentTransitionsOneWins(t *testing.T) {
	store := openMemoryStore(t)
	created := mustCreate(t, store, proposalFromAcceptedResult("Конкурентно одобрение"))
	service := recordService(store, nil)
	var wait sync.WaitGroup
	results := make([]records.TransitionResult, 2)
	failures := make([]error, 2)
	for index := range results {
		wait.Go(func() {
			if index == 0 {
				results[index], failures[index] = service.Approve(context.Background(), records.ApproveRequest{
					ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin",
					VersionID: created.VersionID, ContentHash: created.ContentHash, Now: recordsNow,
				})
				return
			}
			results[index], failures[index] = service.Reject(context.Background(), records.ReasonRequest{
				ProjectID: "project", RecordID: created.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "reviewer", Reason: "не", Now: recordsNow,
			})
		})
	}
	wait.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatal(err)
	}
	kinds := []memory.RecordTransitionKind{results[0].Kind, results[1].Kind}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []memory.RecordTransitionKind{memory.TransitionAllowed, memory.TransitionVersionConflict}) {
		t.Fatalf("kinds = %v", kinds)
	}
	if rows := queryStrings(t, store, "SELECT CAST(version AS TEXT) FROM memory_records"); !slices.Equal(rows, []string{"2"}) {
		t.Fatalf("record version = %v", rows)
	}
	if rows := queryStrings(t, store, "SELECT CAST(count(*) AS TEXT) FROM memory_reviews"); !slices.Equal(rows, []string{"1"}) {
		t.Fatalf("reviews = %v", rows)
	}
}

// A failed COMMIT is rolled back on the same connection: no result, nothing
// written, the pool is usable, and a retry creates the record.
func TestRecordWriteCommitFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	db, connector := openFaulty(t, path, 5000)
	store := &Store{db: db}
	before := dumpMemoryTables(t, store)
	commitFailure := errors.New("injected commit failure")
	connector.inject("COMMIT", commitFailure, 1)
	got, err := recordService(store, nil).CreateRecord(context.Background(), humanText("Неуспешен commit"))
	if !errors.Is(err, commitFailure) || got.Kind != "" || got.RecordID != "" {
		t.Fatalf("result %+v, error %v", got, err)
	}
	if connector.count("ROLLBACK") != 1 || connector.closes.Load() != 0 {
		t.Fatalf("rollbacks %d, closed connections %d", connector.count("ROLLBACK"), connector.closes.Load())
	}
	assertPoolOutsideTransaction(t, store)
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	retried := mustCreate(t, store, humanText("Неуспешен commit"))
	if rows := queryStrings(t, store, "SELECT id FROM memory_records"); !slices.Equal(rows, []string{retried.RecordID}) {
		t.Fatalf("records = %v", rows)
	}
}

// A creation the rule refuses writes nothing, and the schema refuses a
// category outside the fixed list even when a write bypasses the rule.
func TestCreateRefusalsAndCategoryCheck(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	before := dumpMemoryTables(t, store)
	tooLarge := humanText(strings.Repeat("Ж", memory.MaxStoredContentBytes/2) + "x")
	unknown := humanText("Категория")
	unknown.Category = "architecture_decision"
	for name, request := range map[string]records.CreateRequest{"too large": tooLarge, "unknown category": unknown} {
		got, err := recordService(store, nil).CreateRecord(ctx, request)
		if err != nil || got.Kind == memory.CreationAllowed {
			t.Fatalf("%s = %+v, %v", name, got, err)
		}
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	err := store.MemoryRecords().WithinRecordWrite(ctx, func(tx records.Transaction) error {
		return tx.InsertRecord(ctx, records.NewRecord{
			Record:    memory.Record{ID: "bypass", ProjectID: "project", Category: "architecture_decision", Status: memory.StatusActive, CurrentVersionID: "bypass-v1", Version: 1},
			Version:   memory.RecordVersion{ID: "bypass-v1", Content: "x", ContentHash: memory.ContentHash("x"), Author: "a", Trust: memory.TrustHumanApproved, RedactionPolicyVersion: "1", Scope: memory.Scope{Visibility: memory.VisibilityProject, Origin: memory.OriginHuman}},
			CreatedAt: recordsNow,
		})
	})
	if err == nil {
		t.Fatal("unknown category stored")
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
}

const (
	childApprovePathVariable = "CONDUIT_TURSO_CHILD_APPROVE_PATH"
	contendedContent         = "Оспорвано предложение"
)

// seedContendedProposal stores one proposal with known IDs: record-1 and
// version-2.
func seedContendedProposal(t *testing.T, store *Store) {
	t.Helper()
	next := 0
	service := records.Service{Store: store.MemoryRecords(), NewID: func(prefix string) (string, error) {
		next++
		return prefix + "-" + string(rune('0'+next)), nil
	}}
	got, err := service.CreateRecord(context.Background(), proposalFromAcceptedResult(contendedContent))
	if err != nil || got.RecordID != "record-1" || got.VersionID != "version-2" {
		t.Fatalf("seed = %+v, %v", got, err)
	}
}

// TestChildProcessApproveRecord is the approving child: it opens with the
// multiprocess option, waits at the start barrier, approves the contended
// proposal and reports one CHILD-APPROVE line.
func TestChildProcessApproveRecord(t *testing.T) {
	path := os.Getenv(childApprovePathVariable)
	if path == "" {
		t.Skip("child process helper")
	}
	ctx := context.Background()
	store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		fmt.Printf("CHILD-APPROVE failed open: %v\n", err)
	}
	if store != nil {
		defer store.Close()
	}
	if err := childprocess.AwaitBegin(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := childprocess.Signal(childReported); err != nil {
			t.Error(err)
		}
		if err := childprocess.AwaitRelease(); err != nil {
			t.Error(err)
		}
	}()
	if store == nil {
		return
	}
	got, err := recordService(store, nil).Approve(ctx, records.ApproveRequest{
		ProjectID: "project", RecordID: "record-1", ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin",
		VersionID: "version-2", ContentHash: memory.ContentHash(contendedContent), Now: recordsNow,
	})
	if err != nil {
		fmt.Printf("CHILD-APPROVE failed approve: %v\n", err)
		return
	}
	fmt.Printf("CHILD-APPROVE %s\n", got.Kind)
}

func childApproval(t *testing.T, child *childprocess.Child) string {
	t.Helper()
	output := child.Finish()
	for line := range strings.SplitSeq(output, "\n") {
		if result, ok := strings.CutPrefix(line, "CHILD-APPROVE "); ok {
			return result
		}
	}
	t.Fatalf("child reported nothing:\n%s", output)
	return ""
}

// Two processes approve the same proposal with the same expected version:
// exactly one approves, the other gets version_conflict, and one review is
// written.
func TestCrossProcessApproversApproveOnce(t *testing.T) {
	requireMultiprocessPlatform(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	seeded, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	seedContendedProposal(t, seeded)
	if err := seeded.Close(); err != nil {
		t.Fatal(err)
	}
	first := childprocess.Start(t, "TestChildProcessApproveRecord", childApprovePathVariable+"="+path)
	second := childprocess.Start(t, "TestChildProcessApproveRecord", childApprovePathVariable+"="+path)
	childprocess.BeginTogether(first, second)
	first.Await(childReported)
	second.Await(childReported)
	kinds := []string{childApproval(t, first), childApproval(t, second)}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{string(memory.TransitionAllowed), string(memory.TransitionVersionConflict)}) {
		t.Fatalf("children = %v", kinds)
	}
	store, err := OpenWithOptions(ctx, path, Options{MultiprocessWAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if rows := queryStrings(t, store, "SELECT status || '|' || CAST(version AS TEXT) || '|' || (SELECT CAST(count(*) AS TEXT) FROM memory_reviews) FROM memory_records"); !slices.Equal(rows, []string{"active|2|1"}) {
		t.Fatalf("record = %v", rows)
	}
}

// raceArchiveAndDeletion runs a human archive and a deletion of the same
// active record, both expecting version 1, either concurrently or in order.
func raceArchiveAndDeletion(t *testing.T, store *Store, recordID string, order string) (records.TransitionResult, deletion.Result) {
	t.Helper()
	ctx := context.Background()
	var archived records.TransitionResult
	var deleted deletion.Result
	var archiveErr, deleteErr error
	archive := func() {
		archived, archiveErr = recordService(store, nil).Archive(ctx, records.ReasonRequest{ProjectID: "project", RecordID: recordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Reviewer: "martin", Reason: "остаряло", Now: recordsNow})
	}
	remove := func() {
		deleted, deleteErr = deletion.Service{Store: store.MemoryDeletion()}.DeleteRecord(ctx, deletion.Request{ProjectID: "project", RecordID: recordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Now: recordsNow})
	}
	switch order {
	case "archive first":
		archive()
		remove()
	case "deletion first":
		remove()
		archive()
	default:
		var wait sync.WaitGroup
		wait.Go(archive)
		wait.Go(remove)
		wait.Wait()
	}
	if err := errors.Join(archiveErr, deleteErr); err != nil {
		t.Fatal(err)
	}
	return archived, deleted
}

// A record writer and a deletion of the same record serialize on the write
// lock. Exactly one of them changes the record; the other sees the bumped
// version as version_conflict, and the rows are those of the winner only.
func TestRecordWriterRacesDeletion(t *testing.T) {
	for _, order := range []string{"archive first", "deletion first", "concurrent 1", "concurrent 2", "concurrent 3", "concurrent 4"} {
		t.Run(order, func(t *testing.T) {
			store := openMemoryStore(t)
			created := mustCreate(t, store, humanText("Запис в надпревара"))
			archived, deleted := raceArchiveAndDeletion(t, store, created.RecordID, order)
			state := queryStrings(t, store, `SELECT r.status || '|' || CAST(r.version AS TEXT) || '|' || (v.content IS NULL) || '|' ||
				(SELECT CAST(count(*) AS TEXT) FROM context_deletion_receipts) || '|' ||
				(SELECT CAST(count(*) AS TEXT) FROM memory_reviews WHERE decision = 'opinion') || '|' ||
				(SELECT CAST(count(*) AS TEXT) FROM memory_reviews WHERE reason IS NOT NULL) || '|' ||
				(SELECT CAST(count(*) AS TEXT) FROM memory_sources)
				FROM memory_records r JOIN memory_record_versions v ON v.id = r.current_version_id`)
			switch {
			case archived.Kind == memory.TransitionAllowed && deleted.Kind == memory.DeletionVersionConflict:
				if order == "deletion first" || !slices.Equal(state, []string{"archived|2|0|0|1|1|1"}) {
					t.Fatalf("archive won (%s): %v", order, state)
				}
			case deleted.Kind == memory.DeletionDelete && archived.Kind == memory.TransitionVersionConflict:
				if order == "archive first" || !slices.Equal(state, []string{"active|2|1|1|0|0|0"}) {
					t.Fatalf("deletion won (%s): %v", order, state)
				}
			default:
				t.Fatalf("archive %+v, deletion %+v", archived, deleted)
			}
		})
	}
}

// failingLastWrite wraps the real transaction and makes the procedure's last
// statement fail on the engine's own guard or constraint.
type failingLastWrite struct{ records.Store }

func (f failingLastWrite) WithinRecordWrite(ctx context.Context, operation func(records.Transaction) error) error {
	return f.Store.WithinRecordWrite(ctx, func(tx records.Transaction) error {
		return operation(failingLastTx{tx})
	})
}

type failingLastTx struct{ records.Transaction }

// InsertReview is Reactivate's last write: a wrong content hash fails the
// guard after the version copy and the status change.
func (f failingLastTx) InsertReview(ctx context.Context, review records.Review) error {
	if review.Reviewer == "fail-last" {
		review.ContentHash = memory.ContentHash("not this version")
	}
	return f.Transaction.InsertReview(ctx, review)
}

// InsertRelation is Supersede's last write: an unknown kind fails the CHECK
// after the replacement, its approval and the status change.
func (f failingLastTx) InsertRelation(ctx context.Context, relation records.Relation) error {
	relation.Kind = "duplicates"
	return f.Transaction.InsertRelation(ctx, relation)
}

// A failure on the last statement of Reactivate and Supersede rolls back the
// writes before it: the tables are byte-identical to before.
func TestFailureAfterIntermediateWritesRollsBack(t *testing.T) {
	ctx := context.Background()
	store := openMemoryStore(t)
	request := humanText("Запис за проверка")
	request.Sources = []memory.RecordSource{{Kind: memory.SourceFile, Path: "a.go", BlobHash: "388b69a7a7db1b7be1ba7d3867c07e1cdc19d9f7"}}
	stale := mustCreate(t, store, request)
	files := snapshotFiles{"a.go": "d63f2a2ff97b62fba0150067982990bf2a5f8286"}
	if got, err := recordService(store, files).MarkNeedsVerification(ctx, records.VerificationRequest{ProjectID: "project", RecordID: stale.RecordID, ExpectedVersion: 1, Snapshot: "snapshot", Now: recordsNow}); err != nil || got.Kind != memory.TransitionAllowed {
		t.Fatalf("mark = %+v, %v", got, err)
	}
	active := mustCreate(t, store, humanText("Запис за замяна"))
	before := dumpMemoryTables(t, store)
	failing := records.Service{Store: failingLastWrite{store.MemoryRecords()}, Snapshot: files}

	_, err := failing.Reactivate(ctx, records.ReactivateRequest{ProjectID: "project", RecordID: stale.RecordID, ExpectedVersion: 2, Actor: memory.ActorHuman, Reviewer: "fail-last", Snapshot: "snapshot",
		Verifications: []records.Verification{{Path: "a.go", VerifiedHash: "d63f2a2ff97b62fba0150067982990bf2a5f8286"}}, Now: recordsNow})
	if !errors.Is(err, ErrMemoryWriteDiverged) {
		t.Fatalf("reactivate error = %v", err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))

	_, err = failing.Supersede(ctx, records.SupersedeRequest{ProjectID: "project", RecordID: active.RecordID, ExpectedVersion: 1, Actor: memory.ActorHuman, Author: "martin",
		Category: memory.CategoryFact, Content: "Заместител", Sources: []memory.RecordSource{{Kind: memory.SourceSpec, Ref: "specs/012"}}, Now: recordsNow})
	if err == nil || !strings.Contains(err.Error(), "insert supersedes relation") {
		t.Fatalf("supersede error = %v", err)
	}
	assertUnchanged(t, before, dumpMemoryTables(t, store))
	assertPoolOutsideTransaction(t, store)
}

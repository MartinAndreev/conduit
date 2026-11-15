package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/render"
)

const testMarker render.Marker = "conduit-memory-000102030405060708090a0b0c0d0e0f"

// fakeToken has a GitHub token's shape and no real value.
var fakeToken = "ghp_" + strings.Repeat("A", 36)

var createdAt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// note is an active, accepted attempt observation in project p1 whose file
// source matches the fake snapshot.
func note(id, content string, change ...func(*Candidate)) Candidate {
	c := Candidate{
		RecordFound: true,
		Record:      memory.Record{ID: id, ProjectID: "p1", Status: memory.StatusActive, CurrentVersionID: id + "-v1", Version: 1, CreatedAt: createdAt},
		Version: memory.RecordVersion{
			ID:                     id + "-v1",
			RecordID:               id,
			Content:                content,
			ContentHash:            hashOf(id + content),
			Author:                 "agent",
			Trust:                  memory.TrustConduitObserved,
			RedactionPolicyVersion: "0",
			Scope: memory.Scope{
				ProjectID: "p1", Visibility: memory.VisibilityProject, Origin: memory.OriginAttempt,
				RunID: "run1", AttemptID: "a0", BaseCommit: "c0", AcceptedResultRef: "result1",
			},
		},
		VersionCreatedAt: createdAt,
		Sources:          []memory.RecordSource{{Kind: memory.SourceFile, Path: "docs/" + id + ".md", BlobHash: "blob-" + id}},
	}
	for _, f := range change {
		f(&c)
	}
	return c
}

func inProject(project string) func(*Candidate) {
	return func(c *Candidate) {
		c.Record.ProjectID = project
		c.Version.Scope.ProjectID = project
	}
}

func contradicts(id string, status memory.RecordStatus) func(*Candidate) {
	return func(c *Candidate) {
		c.Contradicts = append(c.Contradicts, RelatedRecord{RecordID: id, Status: status})
	}
}

// fakeMemory keeps every version; linked reads return current versions and
// search scans versions in insertion order, as the Turso adapter will.
type fakeMemory struct {
	versions  []Candidate
	extraHits []SearchHit
	result    *SearchResult
	readErr   error
	searchErr error
	extraRead []Candidate
	searches  int
	limits    []int
}

func (f *fakeMemory) ReadLinkedRecords(_ context.Context, projectID string, ids []string) ([]Candidate, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	var out []Candidate
	for _, c := range f.versions {
		if c.Record.ProjectID == projectID && c.Version.ID == c.Record.CurrentVersionID && slices.Contains(ids, c.Record.ID) {
			out = append(out, c)
		}
	}
	return storedSizes(append(out, f.extraRead...)), nil
}

// storedSizes reports each candidate's size facts from what it carries, as
// the Turso reader does for material it loaded in full. Facts a test set
// itself, to describe withheld material, are kept.
func storedSizes(candidates []Candidate) []Candidate {
	for i := range candidates {
		c := &candidates[i]
		if c.ContentBytes == 0 {
			c.ContentBytes = int64(len(c.Version.Content))
		}
		if c.SourceCount == 0 && c.SourceBytes == 0 {
			c.SourceCount, c.SourceBytes = int64(len(c.Sources)), memory.SourceBytes(c.Sources)
		}
	}
	return candidates
}

func (f *fakeMemory) SearchRecords(_ context.Context, q SearchQuery) (SearchResult, error) {
	f.searches++
	f.limits = append(f.limits, q.ScanLimit)
	if f.searchErr != nil {
		return SearchResult{}, f.searchErr
	}
	if f.result != nil {
		return *f.result, nil
	}
	terms := strings.Fields(strings.ToLower(q.Text))
	result := SearchResult{QueryTerms: len(terms)}
	scanned := 0
	for _, c := range f.versions {
		if c.Record.ProjectID != q.ProjectID || c.ContentDeleted {
			continue
		}
		if scanned == q.ScanLimit {
			result.LimitReached = true
			break
		}
		scanned++
		matched := 0
		for _, term := range terms {
			if strings.Contains(strings.ToLower(c.Version.Content), term) {
				matched++
			}
		}
		if matched > 0 {
			result.Hits = append(result.Hits, SearchHit{Candidate: c, MatchedTerms: matched})
		}
	}
	result.Hits = append(result.Hits, f.extraHits...)
	for i := range result.Hits {
		result.Hits[i].Candidate = storedSizes([]Candidate{result.Hits[i].Candidate})[0]
	}
	return result, nil
}

// fakeSnapshot reports every path present with the hash note gives its
// source, unless overridden. calls lists every observed path in order,
// batches the number of ObserveFiles calls.
type fakeSnapshot struct {
	files   map[string]FileObservation
	err     error
	calls   []string
	batches int
	// short drops the last observation, breaking the port's contract.
	short bool
}

func (f *fakeSnapshot) ObserveFiles(_ context.Context, snapshot SnapshotRef, paths []string) ([]FileObservation, error) {
	f.batches++
	for _, p := range paths {
		f.calls = append(f.calls, string(snapshot)+":"+p)
	}
	if f.err != nil {
		return nil, f.err
	}
	observed := make([]FileObservation, 0, len(paths))
	for _, p := range paths {
		if observation, ok := f.files[p]; ok {
			observed = append(observed, observation)
			continue
		}
		observed = append(observed, FileObservation{Observation: memory.SourcePresent, Hash: "blob-" + strings.TrimSuffix(path.Base(p), ".md")})
	}
	if f.short {
		observed = observed[:len(observed)-1]
	}
	return observed, nil
}

// scriptedRedactor wraps the real policy and lets a test change the policy
// version per call or report findings without masking.
type scriptedRedactor struct {
	calls    int
	policy   func(call int) string
	noMask   bool
	findings []redaction.Finding
}

func (s *scriptedRedactor) Redact(text string) redaction.Result {
	s.calls++
	result := redaction.Policy{}.Redact(text)
	if s.policy != nil {
		result.PolicyVersion = s.policy(s.calls)
	}
	if s.noMask && strings.Contains(text, "LEAK") {
		result.Text = text
		result.Findings = []redaction.Finding{{Kind: redaction.KindJWT, Count: 1}}
	}
	if s.findings != nil {
		result.Findings = s.findings
	}
	return result
}

func request(change ...func(*Request)) Request {
	r := Request{
		ProjectID:       "p1",
		AttemptID:       "a1",
		Snapshot:        "snap1",
		ScanLimit:       100,
		ModelLimitKnown: true,
		ModelLimit:      200_000,
		Marker:          testMarker,
	}
	for _, f := range change {
		f(&r)
	}
	return r
}

func query(text string) func(*Request) { return func(r *Request) { r.Query = text } }

func linked(ids ...string) func(*Request) {
	return func(r *Request) { r.ExplicitRecordIDs = ids }
}

// Package proposals owns stage 2 of project memory: reading an agent's
// proposals from its report and recording them, and the human review of
// proposals (the review queue and batch decisions). Where the report comes
// from is spec 003; this package takes its text.
package proposals

import (
	"encoding/json"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/reportblock"
)

// BlockInfo is the info string of the fenced code block that carries an
// agent's proposals.
const BlockInfo = "conduit-memory"

// SearchNormalizer is storage's search text normalization, which defines a
// duplicate (memory.SameSearchText). It performs no I/O.
type SearchNormalizer interface {
	// SearchTextIdentity is the identity of the search text storage would
	// write for content.
	SearchTextIdentity(content string) memory.SearchTextIdentity
}

// Proposal is one kept entry, with its content, refs and paths redacted.
type Proposal struct {
	// Entry is the entry's 1-based position in the block.
	Entry    int
	Category memory.RecordCategory
	Content  string
	Sources  []memory.RecordSource
}

// DropCount counts the entries dropped for one memory.ProposalKeepKind.
type DropCount struct {
	Kind  memory.ProposalKeepKind
	Count int
}

// Extraction is what a report proposes. Findings count what redaction
// replaced in the fields Extract redacted, by kind, never the values.
// Entries counts the block's entries; Proposals are the kept ones in report
// order, and Dropped the dropped ones by kind, in memory.ProposalKeepKinds
// order, without zero counts. It holds nothing else from the report: no raw
// text, field or field name, and no decoder error.
type Extraction struct {
	Block     reportblock.Kind
	Findings  []redaction.Finding
	Entries   int
	Proposals []Proposal
	Dropped   []DropCount
}

// Extract reads an agent's report (Martin, 2026-11-13): a report over
// memory.MaxReportBytes is refused whole (reportblock.TooLarge). Otherwise it finds
// the proposal block in the raw report and decodes its JSON, then redacts
// each decoded content, ref and path once, and decides each entry with
// memory.DecideProposalKeep in report order. Decoding comes first, so a
// secret written without quotes is masked inside its field instead of
// breaking the block. Blob hashes are not redacted: memory.ValidBlobHash is
// their guard. Category and source kind are closed lists.
//
// Only the first memory.MaxProposalEntriesExamined entries are decoded, and
// an entry with more than memory.MaxSourcesPerItem sources is dropped before
// its fields are copied or redacted. Findings are the sum of the findings of
// the fields redacted: those of the entries examined and decoded within the
// source cap. A secret elsewhere in the report (outside the block, or in an
// entry not examined) is not counted, because nothing from there is kept;
// storing the report itself, redacted, is spec 003's.
//
// It is pure: it performs no I/O. Its result holds only redacted field text,
// enums, counts and validated hashes.
func Extract(report string, redactor redaction.Redactor, normalizer SearchNormalizer) Extraction {
	var extraction Extraction
	var entries []json.RawMessage
	extraction.Block, entries = readBlock(report)
	extraction.Entries = len(entries)
	dropped := make(map[memory.ProposalKeepKind]int)
	// seen holds the valid search text identities of the entries kept or
	// dropped only for the limit; an invalid identity matches nothing
	// (memory.SameSearchText), so it is never added.
	seen := make(map[memory.SearchTextIdentity]bool)
	for index, raw := range entries {
		facts := memory.ProposalKeepFacts{Position: index, Kept: len(extraction.Proposals)}
		var proposal Proposal
		var identity memory.SearchTextIdentity
		if index < memory.MaxProposalEntriesExamined {
			entry, decoded := decodeEntry(raw)
			facts.Decoded, facts.Category, facts.SourceCount = decoded, memory.RecordCategory(entry.Category), len(entry.Sources)
			// An entry over the source cap is decided on these cheap facts:
			// its sources are neither copied nor redacted.
			if decoded && facts.SourceCount <= memory.MaxSourcesPerItem {
				var findings []redaction.Finding
				proposal, findings = redactProposal(proposalOf(entry), redactor)
				extraction.Findings = redaction.MergeFindings(extraction.Findings, findings)
				// Duplicates compare the redacted text, as stored.
				identity = normalizer.SearchTextIdentity(proposal.Content)
				facts.Content, facts.Sources = proposal.Content, proposal.Sources
				facts.RepeatsEarlierEntry = seen[identity]
			}
			proposal.Entry = index + 1
		}
		decision := memory.DecideProposalKeep(facts)
		counted := decision.Kind == memory.ProposalKeep || (decision.Kind == memory.ProposalOverLimit && facts.Decoded)
		if counted && memory.SameSearchText(identity, identity) {
			seen[identity] = true
		}
		if decision.Kind == memory.ProposalKeep {
			extraction.Proposals = append(extraction.Proposals, proposal)
			continue
		}
		dropped[decision.Kind]++
	}
	for _, kind := range memory.ProposalKeepKinds() {
		if dropped[kind] > 0 {
			extraction.Dropped = append(extraction.Dropped, DropCount{Kind: kind, Count: dropped[kind]})
		}
	}
	return extraction
}

// redactProposal redacts a decoded entry's content and every source ref and
// path, once. Blob hashes are machine identities with a strict grammar
// (memory.ValidBlobHash) and stay as given; the rule refuses anything else.
func redactProposal(proposal Proposal, redactor redaction.Redactor) (Proposal, []redaction.Finding) {
	content := redactor.Redact(proposal.Content)
	proposal.Content = content.Text
	findings := content.Findings
	for index, source := range proposal.Sources {
		ref, path := redactor.Redact(source.Ref), redactor.Redact(source.Path)
		proposal.Sources[index].Ref, proposal.Sources[index].Path = ref.Text, path.Text
		findings = redaction.MergeFindings(findings, ref.Findings, path.Findings)
	}
	return proposal, findings
}

// readBlock finds the one proposal block (reportblock.Find) and decodes its
// envelope, {"proposals": [...]}, refusing any other field and trailing data.
func readBlock(text string) (reportblock.Kind, []json.RawMessage) {
	kind, body := reportblock.Find(text, BlockInfo)
	if kind != reportblock.Read {
		return kind, nil
	}
	var envelope struct {
		Proposals *[]json.RawMessage `json:"proposals"`
	}
	if !reportblock.DecodeObject([]byte(body), &envelope, 1) || envelope.Proposals == nil {
		return reportblock.Malformed, nil
	}
	return reportblock.Read, *envelope.Proposals
}

type entryJSON struct {
	Category string       `json:"category"`
	Content  string       `json:"content"`
	Sources  []sourceJSON `json:"sources"`
}

type sourceJSON struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Path     string `json:"path"`
	BlobHash string `json:"blob_hash"`
}

// decodeEntry decodes one entry. It reports false for anything but an object
// of the format's fields with their types; the rule then drops the entry.
func decodeEntry(raw json.RawMessage) (entryJSON, bool) {
	var entry entryJSON
	if !reportblock.DecodeObject(raw, &entry, entryKeyDepth) {
		return entryJSON{}, false
	}
	return entry, true
}

// proposalOf copies a decoded entry into a proposal.
func proposalOf(entry entryJSON) Proposal {
	proposal := Proposal{Category: memory.RecordCategory(entry.Category), Content: entry.Content}
	for _, source := range entry.Sources {
		proposal.Sources = append(proposal.Sources, memory.RecordSource{
			Kind: memory.SourceKind(source.Kind), Ref: source.Ref, Path: source.Path, BlobHash: source.BlobHash,
		})
	}
	return proposal
}

// entryKeyDepth checks an entry's own fields and those of its sources.
const entryKeyDepth = 2

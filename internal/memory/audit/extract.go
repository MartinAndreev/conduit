// Package audit owns the memory audit of task 5.5, domain part: the material
// an audit agent reviews (one page of the project's active and stale
// records), reading the agent's suggestions to archive or delete records from
// its report and recording them as agent opinions, the pending suggestions a
// human sees, and the human's confirmation of one suggestion at a time.
// Running the agent, the command and where its report comes from are spec
// 003's.
package audit

import (
	"encoding/json"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/reportblock"
)

// BlockInfo is the info string of the fenced code block that carries an
// audit agent's suggestions.
const BlockInfo = "conduit-memory-audit"

// Suggestion is one kept entry. The identities are checked against their
// grammar and are unchanged by redaction; the reason is redacted.
type Suggestion struct {
	// Entry is the entry's 1-based position in the block.
	Entry       int
	RecordID    string
	VersionID   string
	ContentHash string
	Action      memory.Suggestion
	Reason      string
}

// DropCount counts the entries dropped for one memory.SuggestionKeepKind.
type DropCount struct {
	Kind  memory.SuggestionKeepKind
	Count int
}

// Extraction is what an audit report suggests. Findings count what
// redaction replaced in the reasons Extract redacted, by kind, never the
// values. Entries counts the block's entries; Suggestions are the kept ones
// in report order, and Dropped the dropped ones by kind, in
// memory.SuggestionKeepKinds order, without zero counts. It holds nothing
// else from the report: no raw text, field or field name, no identity that
// failed its check, and no decoder error.
type Extraction struct {
	Block       reportblock.Kind
	Findings    []redaction.Finding
	Entries     int
	Suggestions []Suggestion
	Dropped     []DropCount
}

// Extract reads an audit agent's report with the proposals' block rules
// (reportblock): a report over memory.MaxReportBytes is refused whole
// (TooLarge); otherwise the one conduit-memory-audit block is found in the
// raw report and its JSON decoded, then each decoded entry's reason is
// redacted once and the entry decided with memory.DecideSuggestionKeep in
// report order. The record ID, version ID and content hash are machine
// identities: they are not redacted but must fit their grammar and pass the
// redactor unchanged and without a finding, or the entry is dropped.
//
// Only the first memory.MaxSuggestionEntriesExamined entries are decoded.
// Findings are the sum of the reasons' findings of the entries examined and
// decoded; a secret elsewhere in the report is not counted, because nothing
// from there is kept.
//
// It is pure: it performs no I/O.
func Extract(report string, redactor redaction.Redactor) Extraction {
	var extraction Extraction
	var entries []json.RawMessage
	extraction.Block, entries = readBlock(report)
	extraction.Entries = len(entries)
	dropped := make(map[memory.SuggestionKeepKind]int)
	// seen holds the records named by the entries kept or dropped only for
	// the limit; their identities are valid.
	seen := make(map[string]bool)
	for index, raw := range entries {
		facts := memory.SuggestionKeepFacts{Position: index, Kept: len(extraction.Suggestions)}
		var suggestion Suggestion
		if index < memory.MaxSuggestionEntriesExamined {
			entry, decoded := decodeEntry(raw)
			facts.Decoded = decoded
			if decoded {
				reason := redactor.Redact(entry.Reason)
				extraction.Findings = redaction.MergeFindings(extraction.Findings, reason.Findings)
				suggestion = Suggestion{
					RecordID: entry.RecordID, VersionID: entry.VersionID, ContentHash: entry.ContentHash,
					Action: memory.Suggestion(entry.Action), Reason: reason.Text,
				}
				facts.RecordID, facts.VersionID, facts.ContentHash = suggestion.RecordID, suggestion.VersionID, suggestion.ContentHash
				facts.IdentityChangedByRedaction = changedByRedaction(redactor, suggestion.RecordID, suggestion.VersionID, suggestion.ContentHash)
				facts.Action, facts.Reason = suggestion.Action, suggestion.Reason
				facts.RepeatsEarlierRecord = seen[suggestion.RecordID]
			}
			suggestion.Entry = index + 1
		}
		decision := memory.DecideSuggestionKeep(facts)
		if decision.Kind == memory.SuggestionKeep || (decision.Kind == memory.SuggestionOverLimit && facts.Decoded) {
			seen[suggestion.RecordID] = true
		}
		if decision.Kind == memory.SuggestionKeep {
			extraction.Suggestions = append(extraction.Suggestions, suggestion)
			continue
		}
		dropped[decision.Kind]++
	}
	for _, kind := range memory.SuggestionKeepKinds() {
		if dropped[kind] > 0 {
			extraction.Dropped = append(extraction.Dropped, DropCount{Kind: kind, Count: dropped[kind]})
		}
	}
	return extraction
}

// changedByRedaction reports whether the redactor changes any identity or
// reports a finding in one: such a value is a secret, never an identity.
func changedByRedaction(redactor redaction.Redactor, identities ...string) bool {
	for _, identity := range identities {
		result := redactor.Redact(identity)
		if result.Text != identity || len(result.Findings) > 0 {
			return true
		}
	}
	return false
}

// readBlock finds the one audit block (reportblock.Find) and decodes its
// envelope, {"suggestions": [...]}, refusing any other field and trailing
// data.
func readBlock(report string) (reportblock.Kind, []json.RawMessage) {
	kind, body := reportblock.Find(report, BlockInfo)
	if kind != reportblock.Read {
		return kind, nil
	}
	var envelope struct {
		Suggestions *[]json.RawMessage `json:"suggestions"`
	}
	if !reportblock.DecodeObject([]byte(body), &envelope, 1) || envelope.Suggestions == nil {
		return reportblock.Malformed, nil
	}
	return reportblock.Read, *envelope.Suggestions
}

type entryJSON struct {
	RecordID    string `json:"record_id"`
	VersionID   string `json:"version_id"`
	ContentHash string `json:"content_hash"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
}

// decodeEntry decodes one entry. It reports false for anything but an object
// of the format's fields with their types, each at most once; the rule then
// drops the entry.
func decodeEntry(raw json.RawMessage) (entryJSON, bool) {
	var entry entryJSON
	if !reportblock.DecodeObject(raw, &entry, 1) {
		return entryJSON{}, false
	}
	return entry, true
}

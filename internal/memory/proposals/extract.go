// Package proposals owns stage 2 of project memory: reading an agent's
// proposals from its report and recording them, and the human review of
// proposals (the review queue and batch decisions). Where the report comes
// from is spec 003; this package takes its text.
package proposals

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
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

// BlockKind is what the report held, read as a whole.
type BlockKind string

const (
	// BlockAbsent: no proposal block. The report proposes nothing; this is
	// not an error.
	BlockAbsent BlockKind = "absent"
	// BlockRead: exactly one block, which decoded; its entries were decided.
	BlockRead BlockKind = "read"
	// BlockMultiple: more than one proposal block, counting an unclosed one.
	// None is read, because none can be told to be the report's.
	BlockMultiple BlockKind = "multiple"
	// BlockMalformed: one block that is unclosed, is not one JSON object,
	// has a field other than "proposals", or whose "proposals" is missing or
	// not an array. Nothing is read.
	BlockMalformed BlockKind = "malformed"
	// BlockTooLarge: the report is over memory.MaxReportBytes. It is refused
	// as a whole: nothing is scanned, decoded or redacted.
	BlockTooLarge BlockKind = "too_large"
)

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
	Block     BlockKind
	Findings  []redaction.Finding
	Entries   int
	Proposals []Proposal
	Dropped   []DropCount
}

// Extract reads an agent's report (Martin, 2026-11-13): a report over
// memory.MaxReportBytes is refused whole (BlockTooLarge). Otherwise it finds
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
	if len(report) > memory.MaxReportBytes {
		return Extraction{Block: BlockTooLarge}
	}
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

// readBlock finds the one proposal block and decodes its envelope,
// {"proposals": [...]}, refusing any other field and trailing data.
func readBlock(text string) (BlockKind, []json.RawMessage) {
	bodies, unclosed := proposalBlocks(text)
	switch blocks := len(bodies); {
	case unclosed && blocks > 0, blocks > 1:
		return BlockMultiple, nil
	case unclosed:
		return BlockMalformed, nil
	case blocks == 0:
		return BlockAbsent, nil
	}
	var envelope struct {
		Proposals *[]json.RawMessage `json:"proposals"`
	}
	if !decodeObject([]byte(bodies[0]), &envelope, 1) || envelope.Proposals == nil {
		return BlockMalformed, nil
	}
	return BlockRead, *envelope.Proposals
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
	if !decodeObject(raw, &entry, entryKeyDepth) {
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

// decodeObject decodes exactly one JSON object into target, refusing unknown
// fields, repeated fields and trailing data. Field names match as
// encoding/json matches them, ignoring case, so a repeat is a name equal to
// an earlier one of the same object under that folding. Objects nested
// deeper than keyDepth are not checked for repeats here: the envelope checks
// only its own fields, and each entry is decoded, and checked, on its own.
// The decoder's error is dropped: it can quote report text.
func decodeObject(data []byte, target any, keyDepth int) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) || !uniqueKeys(data, keyDepth) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

// entryKeyDepth checks an entry's own fields and those of its sources.
const entryKeyDepth = 2

// maxNesting bounds the objects and arrays open at once in a proposal block,
// counted from the block's top value. The format needs 5 (envelope,
// proposals, entry, sources, source); anything deeper is malformed, and the
// bound keeps the key check's work and memory per level fixed.
const maxNesting = 8

// keyFrame is one open object or array of the key check.
type keyFrame struct {
	object    bool
	expectKey bool
	// objectsAround counts the objects that enclose this one.
	objectsAround int
	names         map[string]bool
}

// uniqueKeys reports whether data's first value nests at most maxNesting
// objects and arrays and no object of it, down to keyDepth levels of object
// nesting, names a field twice under encoding/json's case folding. It walks
// the tokens iteratively, so no input can deepen the call stack. Invalid
// JSON reports false; decoding refuses it anyway.
func uniqueKeys(data []byte, keyDepth int) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var stack []keyFrame
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			if len(stack) == maxNesting {
				return false
			}
			frame := keyFrame{object: token == json.Delim('{'), expectKey: true}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				frame.objectsAround = top.objectsAround
				if top.object {
					frame.objectsAround++
				}
			}
			if frame.object {
				frame.names = make(map[string]bool)
			}
			stack = append(stack, frame)
			continue
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
		default:
			if top := len(stack) - 1; top >= 0 && stack[top].object && stack[top].expectKey {
				folded := foldName(token.(string))
				if stack[top].objectsAround < keyDepth && stack[top].names[folded] {
					return false
				}
				stack[top].names[folded], stack[top].expectKey = true, false
				continue
			}
		}
		// A value ended: the enclosing object expects its next key.
		if len(stack) == 0 {
			return true
		}
		if top := len(stack) - 1; stack[top].object {
			stack[top].expectKey = true
		}
	}
}

// foldName maps every rune to the smallest rune of its simple case folding
// orbit, so two names are equal exactly when strings.EqualFold, the matching
// encoding/json uses, finds them equal (for example "ſ" and "s").
func foldName(name string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			smallest = min(smallest, folded)
		}
		return smallest
	}, name)
}

// fence is an open fenced code block.
type fence struct {
	char      byte
	length    int
	proposals bool
}

// proposalBlocks returns the bodies of the closed fenced code blocks whose
// info string is exactly BlockInfo, and whether such a block is still open at
// the end of the text. Fences follow CommonMark: up to three spaces of
// indentation, three or more backticks or tildes, and a closing fence of the
// same character at least as long with nothing after it. A fence inside
// another block is that block's content. LF, CRLF and a lone CR end a line.
func proposalBlocks(text string) ([]string, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var bodies, body []string
	var open *fence
	for _, line := range strings.Split(text, "\n") {
		switch {
		case open == nil:
			if opened, ok := openingFence(line); ok {
				open, body = &opened, nil
			}
		case closesFence(line, *open):
			if open.proposals {
				bodies = append(bodies, strings.Join(body, "\n"))
			}
			open = nil
		default:
			body = append(body, line)
		}
	}
	return bodies, open != nil && open.proposals
}

// fenceRun splits a line into its fence character run, after at most three
// spaces of indentation, and the rest of the line. The run is empty when the
// line does not start with three or more backticks or tildes.
func fenceRun(line string) (string, string) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return "", ""
	}
	line = line[indent:]
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return "", ""
	}
	length := len(line) - len(strings.TrimLeft(line, line[:1]))
	if length < 3 {
		return "", ""
	}
	return line[:length], line[length:]
}

func openingFence(line string) (fence, bool) {
	run, info := fenceRun(line)
	if run == "" || (run[0] == '`' && strings.Contains(info, "`")) {
		return fence{}, false
	}
	return fence{char: run[0], length: len(run), proposals: strings.TrimSpace(info) == BlockInfo}, true
}

func closesFence(line string, open fence) bool {
	run, rest := fenceRun(line)
	return run != "" && run[0] == open.char && len(run) >= open.length && strings.TrimSpace(rest) == ""
}

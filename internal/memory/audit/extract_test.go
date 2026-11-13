package audit

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MartinAndreev/conduit/internal/memory"
	"github.com/MartinAndreev/conduit/internal/memory/redaction"
	"github.com/MartinAndreev/conduit/internal/memory/reportblock"
)

// fakeToken has the shape of a GitHub token, which the policy masks in place
// and which fits the ID grammar.
const fakeToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"

func hashOf(text string) string { return memory.ContentHash(text) }

func block(body string) string {
	return "Прегледах паметта.\n\n```conduit-memory-audit\n" + body + "\n```\n"
}

func entry(recordID, action, reason string) string {
	return fmt.Sprintf(`{"record_id":%q,"version_id":%q,"content_hash":%q,"action":%q,"reason":%q}`,
		recordID, recordID+"-v", hashOf(recordID), action, reason)
}

func suggestionsOf(entries ...string) string {
	return block(`{"suggestions":[` + strings.Join(entries, ",") + `]}`)
}

func distinctEntries(n int) []string {
	entries := make([]string, n)
	for index := range entries {
		entries[index] = entry(fmt.Sprintf("record-%d", index+1), "archive", "остаряло")
	}
	return entries
}

type extractCase struct {
	name    string
	report  string
	block   reportblock.Kind
	entries int
	kept    []int
	dropped []DropCount
}

func dropped(kind memory.SuggestionKeepKind, count int) []DropCount {
	return []DropCount{{Kind: kind, Count: count}}
}

func runExtractCases(t *testing.T, tests []extractCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.report, redaction.Policy{})
			entries := make([]int, len(got.Suggestions))
			for index, suggestion := range got.Suggestions {
				entries[index] = suggestion.Entry
			}
			if got.Block != tt.block || got.Entries != tt.entries || !slices.Equal(entries, tt.kept) || !slices.Equal(got.Dropped, tt.dropped) {
				t.Fatalf("extraction = %+v\nwant block %s, %d entries, kept %v, dropped %v", got, tt.block, tt.entries, tt.kept, tt.dropped)
			}
		})
	}
}

func TestExtractBlocks(t *testing.T) {
	valid := `{"suggestions":[` + entry("r1", "archive", "остаряло") + `]}`
	tests := []extractCase{
		{name: "no block", report: "Нищо за предлагане.", block: reportblock.Absent},
		{name: "empty report", report: "", block: reportblock.Absent},
		{name: "proposal block is not an audit block", report: "```conduit-memory\n" + valid + "\n```", block: reportblock.Absent},
		{name: "info string with another word", report: "```conduit-memory-audit json\n" + valid + "\n```", block: reportblock.Absent},
		{name: "one block", report: block(valid), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "tilde fence and spacing", report: "  ~~~~  conduit-memory-audit \n" + valid + "\n~~~~~\n", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "CRLF lines", report: "```conduit-memory-audit\r\n" + valid + "\r\n```\r\n", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "lone CR lines", report: "```conduit-memory-audit\r" + valid + "\r```", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "four spaces is not a fence", report: "    ```conduit-memory-audit\n" + valid + "\n    ```", block: reportblock.Absent},
		{name: "quoted inside another block", report: "````markdown\n" + block(valid) + "````\n", block: reportblock.Absent},
		{name: "next to a proposal block", report: "```conduit-memory\n{}\n```\n" + block(valid), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "two blocks", report: block(valid) + block(valid), block: reportblock.Multiple},
		{name: "closed and unclosed", report: block(valid) + "```conduit-memory-audit\n" + valid, block: reportblock.Multiple},
		{name: "unclosed", report: "```conduit-memory-audit\n" + valid, block: reportblock.Malformed},
		{name: "invalid JSON", report: block(`{"suggestions":[`), block: reportblock.Malformed},
		{name: "empty body", report: block(""), block: reportblock.Malformed},
		{name: "unknown top-level field", report: block(`{"suggestions":[],"note":"x"}`), block: reportblock.Malformed},
		{name: "proposals envelope", report: block(`{"proposals":[]}`), block: reportblock.Malformed},
		{name: "missing suggestions", report: block(`{}`), block: reportblock.Malformed},
		{name: "null suggestions", report: block(`{"suggestions":null}`), block: reportblock.Malformed},
		{name: "suggestions not an array", report: block(`{"suggestions":{}}`), block: reportblock.Malformed},
		{name: "top-level array", report: block(`[` + entry("r1", "archive", "x") + `]`), block: reportblock.Malformed},
		{name: "trailing data", report: block(valid + ` {}`), block: reportblock.Malformed},
		{name: "repeated suggestions", report: block(`{"suggestions":[],"Suggestions":[]}`), block: reportblock.Malformed},
		{name: "empty suggestions", report: block(`{"suggestions":[]}`), block: reportblock.Read},
	}
	runExtractCases(t, tests)
}

func TestExtractEntries(t *testing.T) {
	hash := hashOf("r1")
	tests := []extractCase{
		{name: "delete", report: suggestionsOf(entry("r1", "delete", "грешно")), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "fields in another case", report: suggestionsOf(`{"RECORD_ID":"r1","Version_Id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "non-object entries", report: suggestionsOf(`"r1"`, `null`, `5`, `[]`), block: reportblock.Read, entries: 4, dropped: dropped(memory.SuggestionMalformedEntry, 4)},
		{name: "unknown field", report: suggestionsOf(`{"record_id":"r1","version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x","confidence":1}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionMalformedEntry, 1)},
		{name: "repeated field", report: suggestionsOf(`{"record_id":"r1","record_id":"r2","version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionMalformedEntry, 1)},
		{name: "repeated field under folding", report: suggestionsOf(`{"reason":"a","REASON":"b","record_id":"r1","version_id":"v1","content_hash":"` + hash + `","action":"archive"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionMalformedEntry, 1)},
		{name: "nested value", report: suggestionsOf(`{"record_id":{"id":"r1"},"version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionMalformedEntry, 1)},
		{name: "wrong type", report: suggestionsOf(`{"record_id":5,"version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionMalformedEntry, 1)},
		{name: "missing record id", report: suggestionsOf(`{"version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionInvalidIdentity, 1)},
		{name: "record id with a dot", report: suggestionsOf(entry("r.1", "archive", "x")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionInvalidIdentity, 1)},
		{name: "record id over 128", report: suggestionsOf(`{"record_id":"` + strings.Repeat("r", 129) + `","version_id":"v1","content_hash":"` + hash + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionInvalidIdentity, 1)},
		{name: "uppercase content hash", report: suggestionsOf(`{"record_id":"r1","version_id":"v1","content_hash":"` + strings.ToUpper(hash) + `","action":"archive","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionInvalidIdentity, 1)},
		{name: "secret-shaped record id", report: suggestionsOf(entry(fakeToken, "archive", "x")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionInvalidIdentity, 1)},
		{name: "unknown action", report: suggestionsOf(entry("r1", "forget", "x")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionUnknownAction, 1)},
		{name: "action in another case", report: suggestionsOf(entry("r1", "Archive", "x")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionUnknownAction, 1)},
		{name: "missing action", report: suggestionsOf(`{"record_id":"r1","version_id":"v1","content_hash":"` + hash + `","reason":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionUnknownAction, 1)},
		{name: "empty reason", report: suggestionsOf(entry("r1", "delete", "")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionEmptyReason, 1)},
		{name: "blank reason", report: suggestionsOf(entry("r1", "delete", " \n ")), block: reportblock.Read, entries: 1, dropped: dropped(memory.SuggestionEmptyReason, 1)},
		{name: "every drop kind in rule order", report: suggestionsOf(append(distinctEntries(memory.MaxSuggestionsPerReport),
			entry("record-1", "delete", "пак"), entry("extra", "archive", "x"), `"x"`, entry("a.b", "archive", "x"), entry("c", "forget", "x"), entry("d", "delete", ""))...),
			block: reportblock.Read, entries: memory.MaxSuggestionsPerReport + 6, kept: seq(1, memory.MaxSuggestionsPerReport),
			dropped: []DropCount{
				{Kind: memory.SuggestionMalformedEntry, Count: 1}, {Kind: memory.SuggestionInvalidIdentity, Count: 1}, {Kind: memory.SuggestionUnknownAction, Count: 1},
				{Kind: memory.SuggestionEmptyReason, Count: 1}, {Kind: memory.SuggestionDuplicate, Count: 1}, {Kind: memory.SuggestionOverLimit, Count: 1},
			}},
	}
	runExtractCases(t, tests)
}

func seq(from, to int) []int {
	var values []int
	for value := from; value <= to; value++ {
		values = append(values, value)
	}
	return values
}

func TestExtractLimitAndDuplicates(t *testing.T) {
	limit := memory.MaxSuggestionsPerReport
	tests := []extractCase{
		{name: "limit", report: suggestionsOf(distinctEntries(limit + 2)...), block: reportblock.Read, entries: limit + 2, kept: seq(1, limit), dropped: dropped(memory.SuggestionOverLimit, 2)},
		{name: "exactly the limit", report: suggestionsOf(distinctEntries(limit)...), block: reportblock.Read, entries: limit, kept: seq(1, limit)},
		{name: "invalid entries take no place", report: suggestionsOf(append([]string{entry("x", "forget", "a"), `{}`}, distinctEntries(limit)...)...),
			block: reportblock.Read, entries: limit + 2, kept: seq(3, limit+2),
			dropped: []DropCount{{Kind: memory.SuggestionInvalidIdentity, Count: 1}, {Kind: memory.SuggestionUnknownAction, Count: 1}}},
		{name: "same record with another action", report: suggestionsOf(entry("r1", "archive", "a"), entry("r1", "delete", "b")),
			block: reportblock.Read, entries: 2, kept: []int{1}, dropped: dropped(memory.SuggestionDuplicate, 1)},
		{name: "same record with another version", report: suggestionsOf(entry("r1", "archive", "a"),
			`{"record_id":"r1","version_id":"other","content_hash":"`+hashOf("x")+`","action":"archive","reason":"b"}`),
			block: reportblock.Read, entries: 2, kept: []int{1}, dropped: dropped(memory.SuggestionDuplicate, 1)},
		{name: "duplicates take no place", report: suggestionsOf(append([]string{entry("record-1", "delete", "x")}, distinctEntries(limit)...)...),
			block: reportblock.Read, entries: limit + 1, kept: append([]int{1}, seq(3, limit+1)...), dropped: dropped(memory.SuggestionDuplicate, 1)},
		{name: "repeat of an entry over the limit", report: suggestionsOf(append(distinctEntries(limit+1), entry(fmt.Sprintf("record-%d", limit+1), "delete", "x"))...),
			block: reportblock.Read, entries: limit + 2, kept: seq(1, limit),
			dropped: []DropCount{{Kind: memory.SuggestionDuplicate, Count: 1}, {Kind: memory.SuggestionOverLimit, Count: 1}}},
		{name: "repeat of an invalid entry is kept", report: suggestionsOf(entry("r1", "forget", "a"), entry("r1", "archive", "a")),
			block: reportblock.Read, entries: 2, kept: []int{2}, dropped: dropped(memory.SuggestionUnknownAction, 1)},
	}
	runExtractCases(t, tests)
}

func TestExtractDecodesTheSuggestion(t *testing.T) {
	report := suggestionsOf(`{"record_id":"record-0a1b","version_id":"version-2","content_hash":"` + hashOf("text") + `","action":"delete","reason":"Решението е отменено"}`)
	got := Extract(report, redaction.Policy{})
	want := []Suggestion{{Entry: 1, RecordID: "record-0a1b", VersionID: "version-2", ContentHash: hashOf("text"), Action: memory.SuggestDelete, Reason: "Решението е отменено"}}
	if got.Block != reportblock.Read || got.Entries != 1 || !slices.Equal(got.Suggestions, want) || got.Findings != nil || got.Dropped != nil {
		t.Fatalf("extraction = %+v", got)
	}
}

// The block is decoded from the raw report and each decoded reason is
// redacted once; a secret outside the block is not counted. An unquoted
// secret assignment is masked inside its reason, and a JSON escape does not
// hide it.
func TestExtractRedactsTheReason(t *testing.T) {
	report := "Ползвах " + fakeToken + ".\n" + suggestionsOf(
		entry("r1", "archive", "токенът "+fakeToken+" изтече"),
		entry("r2", "delete", "password: hunter2"),
		`{"record_id":"r3","version_id":"r3-v","content_hash":"`+hashOf("r3")+`","action":"delete","reason":"password=hunter3"}`,
	)
	got := Extract(report, redaction.Policy{})
	wantFindings := []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 1}, {Kind: redaction.KindSecretAssignment, Count: 2}}
	if got.Block != reportblock.Read || len(got.Suggestions) != 3 || !slices.Equal(redaction.MergeFindings(nil, got.Findings), redaction.MergeFindings(nil, wantFindings)) {
		t.Fatalf("extraction = %+v", got)
	}
	if got.Suggestions[0].Reason != "токенът [REDACTED:github_token] изтече" || got.Suggestions[1].Reason != "password: [REDACTED:secret_assignment]" {
		t.Fatalf("suggestions = %+v", got.Suggestions)
	}
	for _, secret := range []string{fakeToken, "hunter2", "hunter3"} {
		wantNoRawValue(t, got, secret)
	}
}

// wantNoRawValue fails when value appears in any rendering of the result,
// including Go syntax (%#v), which prints every field.
func wantNoRawValue(t *testing.T, got Extraction, value string) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{string(encoded), fmt.Sprintf("%+v", got), fmt.Sprintf("%#v", got)} {
		if strings.Contains(rendered, value) {
			t.Fatalf("raw value survived extraction: %s", rendered)
		}
	}
}

// Nothing unmasked leaves Extract on any path.
func TestExtractLeaksNothingOnAnyPath(t *testing.T) {
	secret := "hunter2"
	field := "password: " + secret
	reports := map[string]string{
		"malformed block":    block(`{"suggestions":[` + entry("r1", "archive", field) + `],"note":"` + field + `"}`),
		"invalid JSON":       block(`{"suggestions":[` + entry("r1", "archive", field)),
		"unclosed":           "```conduit-memory-audit\n" + `{"suggestions":[` + entry("r1", "archive", field) + `]}`,
		"multiple":           suggestionsOf(entry("r1", "archive", field)) + suggestionsOf(entry("r1", "archive", field)),
		"malformed entry":    suggestionsOf(`{"record_id":"r1","reason":"` + field + `","` + secret + `":1}`),
		"secret as an id":    suggestionsOf(`{"record_id":"` + secret + `x.y","version_id":"v","content_hash":"` + hashOf("a") + `","action":"archive","reason":"` + field + `"}`),
		"token as an id":     suggestionsOf(entry(fakeToken, "archive", "x")),
		"secret as a hash":   suggestionsOf(`{"record_id":"r1","version_id":"v","content_hash":"` + secret + `","action":"archive","reason":"x"}`),
		"secret as action":   suggestionsOf(entry("r1", secret, field)),
		"duplicate":          suggestionsOf(entry("r1", "archive", "a"), entry("r1", "delete", field)),
		"over limit":         suggestionsOf(append(distinctEntries(memory.MaxSuggestionsPerReport), entry("last", "delete", field))...),
		"past examined":      suggestionsOf(append(distinctEntries(memory.MaxSuggestionEntriesExamined), entry("last", "delete", field))...),
		"outside the block":  field + "\n" + suggestionsOf(entry("r1", "archive", "a")),
		"too large":          suggestionsOf(entry("r1", "archive", field)) + strings.Repeat("x", memory.MaxReportBytes),
		"kept with a secret": suggestionsOf(entry("r1", "archive", field)),
	}
	for name, report := range reports {
		t.Run(name, func(t *testing.T) {
			got := Extract(report, redaction.Policy{})
			wantNoRawValue(t, got, secret)
			if name == "token as an id" {
				wantNoRawValue(t, got, fakeToken)
			}
		})
	}
}

// countingRedactor counts the texts it redacts.
type countingRedactor struct {
	calls *int
	redaction.Redactor
}

func (c countingRedactor) Redact(text string) redaction.Result {
	*c.calls++
	return c.Redactor.Redact(text)
}

// A report over memory.MaxReportBytes is refused whole, before any scanning,
// decoding or redaction; exactly the limit is read.
func TestExtractRefusesATooLargeReport(t *testing.T) {
	valid := suggestionsOf(entry("r1", "archive", "токен "+fakeToken))
	padded := func(size int) string {
		return strings.Repeat("x", size-len(valid)-1) + "\n" + valid
	}
	calls := 0
	atLimit := Extract(padded(memory.MaxReportBytes), countingRedactor{&calls, redaction.Policy{}})
	// One redaction of the reason and three identity checks.
	if atLimit.Block != reportblock.Read || len(atLimit.Suggestions) != 1 || calls != 4 ||
		!slices.Equal(atLimit.Findings, []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 1}}) {
		t.Fatalf("at the limit: %+v after %d redactions", atLimit, calls)
	}
	calls = 0
	over := Extract(padded(memory.MaxReportBytes+1), countingRedactor{&calls, redaction.Policy{}})
	if !reflect.DeepEqual(over, Extraction{Block: reportblock.TooLarge}) || calls != 0 {
		t.Fatalf("over the limit: %#v after %d redactions", over, calls)
	}
}

// Nesting deeper than reportblock.MaxNesting makes the block malformed,
// wherever it is, even in an entry past the examined ones.
func TestExtractRefusesDeepNesting(t *testing.T) {
	deep := strings.Repeat("[", reportblock.MaxNesting) + strings.Repeat("]", reportblock.MaxNesting)
	tests := []extractCase{
		{name: "in an entry", report: suggestionsOf(`{"record_id":` + deep + `}`), block: reportblock.Malformed},
		{name: "past the examined entries", report: suggestionsOf(append(distinctEntries(memory.MaxSuggestionEntriesExamined), deep)...), block: reportblock.Malformed},
	}
	runExtractCases(t, tests)
}

// Only the first MaxSuggestionEntriesExamined entries are decoded; the rest
// are over the limit, so a block of as many entries as fit the report limit
// is read quickly, with the real policy.
func TestExtractBoundsTheWork(t *testing.T) {
	entries := make([]string, 1_200)
	for index := range entries {
		entries[index] = entry(fmt.Sprintf("r%d", index), "archive", "x")
	}
	report := suggestionsOf(entries...)
	if len(report) > memory.MaxReportBytes {
		t.Fatalf("report of %d bytes is over the limit", len(report))
	}
	start := time.Now()
	got := Extract(report, redaction.Policy{})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("extraction took %v", elapsed)
	}
	if got.Entries != len(entries) || len(got.Suggestions) != memory.MaxSuggestionsPerReport ||
		!slices.Equal(got.Dropped, dropped(memory.SuggestionOverLimit, len(entries)-memory.MaxSuggestionsPerReport)) {
		t.Fatalf("extraction: %d entries, %d kept, dropped %v", got.Entries, len(got.Suggestions), got.Dropped)
	}
	// Past the examined entries even an invalid one is only over the limit,
	// and a repeat of an examined record is not compared.
	examined := append(distinctEntries(memory.MaxSuggestionEntriesExamined), `"x"`, entry("record-1", "delete", "x"))
	got = Extract(suggestionsOf(examined...), redaction.Policy{})
	if !slices.Equal(got.Dropped, dropped(memory.SuggestionOverLimit, memory.MaxSuggestionEntriesExamined-memory.MaxSuggestionsPerReport+2)) {
		t.Fatalf("dropped = %v", got.Dropped)
	}
}

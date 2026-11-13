package proposals

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

// fakeToken has the shape of a GitHub token, which the policy masks in place.
const fakeToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"

// lowerNormalizer folds case like storage's normalization version 1.
type lowerNormalizer struct{}

func (lowerNormalizer) SearchTextIdentity(content string) memory.SearchTextIdentity {
	return memory.SearchTextIdentity{NormalizationVersion: 1, Digest: strings.ToLower(content)}
}

func block(body string) string {
	return "Готово.\n\n```conduit-memory\n" + body + "\n```\n"
}

func entry(category, content string) string {
	return fmt.Sprintf(`{"category":%q,"content":%q}`, category, content)
}

func proposalsOf(entries ...string) string {
	return block(`{"proposals":[` + strings.Join(entries, ",") + `]}`)
}

func distinctEntries(n int) []string {
	entries := make([]string, n)
	for index := range entries {
		entries[index] = entry("fact", fmt.Sprintf("факт %d", index+1))
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

func dropped(kind memory.ProposalKeepKind, count int) []DropCount {
	return []DropCount{{Kind: kind, Count: count}}
}

func TestExtractBlocks(t *testing.T) {
	valid := `{"proposals":[` + entry("decision", "Миграциите са адитивни") + `]}`
	tests := []extractCase{
		{name: "no block", report: "Готово, без бележки.", block: reportblock.Absent},
		{name: "empty report", report: "", block: reportblock.Absent},
		{name: "other info string", report: "```json\n" + valid + "\n```", block: reportblock.Absent},
		{name: "info string with another word", report: "```conduit-memory json\n" + valid + "\n```", block: reportblock.Absent},
		{name: "one block", report: block(valid), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "info string spacing", report: "  ```   conduit-memory  \n" + valid + "\n```", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "tilde fence with longer close", report: "~~~~conduit-memory\n" + valid + "\n~~~~~~ \n", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "CRLF lines", report: "```conduit-memory\r\n" + valid + "\r\n```\r\n", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "lone CR lines", report: "```conduit-memory\r" + valid + "\r```", block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "four spaces is not a fence", report: "    ```conduit-memory\n" + valid + "\n    ```", block: reportblock.Absent},
		{name: "backtick in a backtick info string", report: "```conduit-memory`\n" + valid + "\n```", block: reportblock.Absent},
		{name: "two-character run is not a fence", report: "``conduit-memory\n" + valid + "\n``", block: reportblock.Absent},
		{name: "shorter closing run is content", report: "````conduit-memory\n" + valid + "\n```\n````", block: reportblock.Malformed},
		{name: "other character does not close", report: "```conduit-memory\n" + valid + "\n~~~\n```", block: reportblock.Malformed},
		{name: "closing fence with text is content", report: "```conduit-memory\n" + valid + "\n``` x\n```", block: reportblock.Malformed},
		{name: "quoted inside another block", report: "````markdown\n" + block(valid) + "````\n", block: reportblock.Absent},
		{name: "after another block", report: "```go\nx := 1\n```\n" + block(valid), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "two blocks", report: block(valid) + block(valid), block: reportblock.Multiple},
		{name: "closed and unclosed", report: block(valid) + "```conduit-memory\n" + valid, block: reportblock.Multiple},
		{name: "unclosed", report: "```conduit-memory\n" + valid, block: reportblock.Malformed},
		{name: "unclosed other block", report: "```text\n" + valid, block: reportblock.Absent},
		{name: "invalid JSON", report: block(`{"proposals":[`), block: reportblock.Malformed},
		{name: "empty body", report: block(""), block: reportblock.Malformed},
		{name: "unknown top-level field", report: block(`{"proposals":[],"note":"x"}`), block: reportblock.Malformed},
		{name: "missing proposals", report: block(`{}`), block: reportblock.Malformed},
		{name: "null proposals", report: block(`{"proposals":null}`), block: reportblock.Malformed},
		{name: "proposals not an array", report: block(`{"proposals":{}}`), block: reportblock.Malformed},
		{name: "top-level array", report: block(`[` + entry("fact", "x") + `]`), block: reportblock.Malformed},
		{name: "top-level null", report: block(`null`), block: reportblock.Malformed},
		{name: "trailing data", report: block(valid + ` {}`), block: reportblock.Malformed},
		{name: "non-string key", report: block(`{"proposals":[],5:1}`), block: reportblock.Malformed},
		{name: "empty proposals", report: block(`{"proposals":[]}`), block: reportblock.Read},
	}
	runExtractCases(t, tests)
}

func TestExtractEntries(t *testing.T) {
	file := `{"kind":"file","path":"a.go","blob_hash":"0fd0bcfb44f83e7d5ac7a8922578276b9af48746"}`
	withSources := func(content string, sources ...string) string {
		return fmt.Sprintf(`{"category":"gotcha","content":%q,"sources":[%s]}`, content, strings.Join(sources, ","))
	}
	tests := []extractCase{
		{name: "with sources", report: proposalsOf(withSources("a", file, `{"kind":"spec","ref":"specs/012"}`)), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "unknown entry field", report: proposalsOf(`{"category":"fact","content":"a","trust":"human_approved"}`, entry("fact", "b")),
			block: reportblock.Read, entries: 2, kept: []int{2}, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "unknown source field", report: proposalsOf(withSources("a", `{"kind":"spec","ref":"s","url":"x"}`)),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "non-object entries", report: proposalsOf(`"text"`, `null`, `5`, `[]`), block: reportblock.Read, entries: 4, dropped: dropped(memory.ProposalMalformedEntry, 4)},
		{name: "wrong content type", report: proposalsOf(`{"category":"fact","content":5}`), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "source not an object", report: proposalsOf(withSources("a", `"a.go"`)), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "unknown category", report: proposalsOf(entry("architecture", "a")), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidCategory, 1)},
		{name: "missing category", report: proposalsOf(`{"content":"a"}`), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidCategory, 1)},
		{name: "unknown source kind", report: proposalsOf(withSources("a", `{"kind":"url","ref":"x"}`)), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidSource, 1)},
		{name: "file without path", report: proposalsOf(withSources("a", `{"kind":"file","blob_hash":"b"}`)), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidSource, 1)},
		{name: "file without hash", report: proposalsOf(withSources("a", `{"kind":"file","path":"a.go"}`)), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidSource, 1)},
		{name: "null source", report: proposalsOf(withSources("a", `null`)), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalInvalidSource, 1)},
		{name: "empty content", report: proposalsOf(entry("fact", " \n ")), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalEmptyContent, 1)},
		{name: "missing content", report: proposalsOf(`{"category":"fact"}`), block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalEmptyContent, 1)},
		{name: "every drop kind in rule order", report: proposalsOf(append(distinctEntries(5),
			entry("fact", "факт 1"), entry("fact", "шести"), `"x"`, entry("x", "y"), entry("fact", ""), withSources("a", `{"kind":"x"}`))...),
			block: reportblock.Read, entries: 11, kept: []int{1, 2, 3, 4, 5},
			dropped: []DropCount{
				{Kind: memory.ProposalMalformedEntry, Count: 1}, {Kind: memory.ProposalInvalidCategory, Count: 1}, {Kind: memory.ProposalInvalidSource, Count: 1},
				{Kind: memory.ProposalEmptyContent, Count: 1}, {Kind: memory.ProposalDuplicate, Count: 1}, {Kind: memory.ProposalOverLimit, Count: 1},
			}},
	}
	runExtractCases(t, tests)
}

func TestExtractLimitAndDuplicates(t *testing.T) {
	tests := []extractCase{
		{name: "limit", report: proposalsOf(distinctEntries(7)...), block: reportblock.Read, entries: 7, kept: []int{1, 2, 3, 4, 5}, dropped: dropped(memory.ProposalOverLimit, 2)},
		{name: "exactly the limit", report: proposalsOf(distinctEntries(5)...), block: reportblock.Read, entries: 5, kept: []int{1, 2, 3, 4, 5}},
		{name: "invalid entries take no place", report: proposalsOf(append([]string{entry("x", "a"), `{}`}, distinctEntries(5)...)...),
			block: reportblock.Read, entries: 7, kept: []int{3, 4, 5, 6, 7},
			dropped: []DropCount{{Kind: memory.ProposalInvalidCategory, Count: 2}}},
		{name: "duplicate by normalized text", report: proposalsOf(entry("fact", "Памет"), entry("gotcha", "ПАМЕТ")),
			block: reportblock.Read, entries: 2, kept: []int{1}, dropped: dropped(memory.ProposalDuplicate, 1)},
		{name: "duplicates take no place", report: proposalsOf(append([]string{entry("fact", "факт 1")}, distinctEntries(5)...)...),
			block: reportblock.Read, entries: 6, kept: []int{1, 3, 4, 5, 6}, dropped: dropped(memory.ProposalDuplicate, 1)},
		{name: "repeat of an entry over the limit", report: proposalsOf(append(distinctEntries(6), entry("fact", "факт 6"))...),
			block: reportblock.Read, entries: 7, kept: []int{1, 2, 3, 4, 5},
			dropped: []DropCount{{Kind: memory.ProposalDuplicate, Count: 1}, {Kind: memory.ProposalOverLimit, Count: 1}}},
		{name: "repeat of an invalid entry is kept", report: proposalsOf(entry("x", "a"), entry("fact", "a")),
			block: reportblock.Read, entries: 2, kept: []int{2}, dropped: dropped(memory.ProposalInvalidCategory, 1)},
	}
	runExtractCases(t, tests)
}

func runExtractCases(t *testing.T, tests []extractCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.report, redaction.Policy{}, lowerNormalizer{})
			entries := make([]int, len(got.Proposals))
			for index, proposal := range got.Proposals {
				entries[index] = proposal.Entry
			}
			if got.Block != tt.block || got.Entries != tt.entries || !slices.Equal(entries, tt.kept) || !slices.Equal(got.Dropped, tt.dropped) {
				t.Fatalf("extraction = %+v\nwant block %s, %d entries, kept %v, dropped %v", got, tt.block, tt.entries, tt.kept, tt.dropped)
			}
		})
	}
}

func TestExtractDecodesTheProposal(t *testing.T) {
	report := proposalsOf(`{"category":"gotcha","content":"Turso оставя COMMIT отворен","sources":[` +
		`{"kind":"file","path":"internal/storage/turso/memory_write.go","blob_hash":"4bff34222a43b71631d0bc0a180aa03863cef75b"},{"kind":"spec","ref":"specs/012"},` +
		`{"kind":"event","ref":"event-1"},{"kind":"assignment","ref":"assignment-1"}]}`)
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	want := Proposal{Entry: 1, Category: memory.CategoryGotcha, Content: "Turso оставя COMMIT отворен", Sources: []memory.RecordSource{
		{Kind: memory.SourceFile, Path: "internal/storage/turso/memory_write.go", BlobHash: "4bff34222a43b71631d0bc0a180aa03863cef75b"},
		{Kind: memory.SourceSpec, Ref: "specs/012"},
		{Kind: memory.SourceEvent, Ref: "event-1"},
		{Kind: memory.SourceAssignment, Ref: "assignment-1"},
	}}
	if len(got.Proposals) != 1 || got.Proposals[0].Entry != want.Entry || got.Proposals[0].Category != want.Category ||
		got.Proposals[0].Content != want.Content || !slices.Equal(got.Proposals[0].Sources, want.Sources) || got.Findings != nil {
		t.Fatalf("extraction = %+v", got)
	}
}

// The block is decoded from the raw report and each decoded content, ref and
// path is redacted once (Martin, 2026-11-13). Findings are those fields'; a
// secret outside the block is not stored by this path and not counted.
func TestExtractRedactsDecodedFields(t *testing.T) {
	report := "Ползвах " + fakeToken + ".\n" + proposalsOf(
		entry("gotcha", "Токенът "+fakeToken+" изтича"),
		fmt.Sprintf(`{"category":"fact","content":"a","sources":[{"kind":"spec","ref":"specs/%s"}]}`, fakeToken),
	)
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	if got.Block != reportblock.Read || len(got.Proposals) != 2 || !slices.Equal(got.Findings, []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 2}}) {
		t.Fatalf("extraction = %+v", got)
	}
	if got.Proposals[0].Content != "Токенът [REDACTED:github_token] изтича" || got.Proposals[1].Sources[0].Ref != "specs/[REDACTED:github_token]" {
		t.Fatalf("proposals = %+v", got.Proposals)
	}
	wantNoRawValue(t, got, fakeToken)
}

// An unquoted secret assignment no longer breaks the block: it is masked
// inside its field, to the field's end of line, and counted; the other
// proposals are kept unaffected.
func TestExtractMasksAnUnquotedSecretInsideItsField(t *testing.T) {
	report := proposalsOf(entry("fact", "password: hunter2"), entry("fact", "друго"), entry("gotcha", "трето\nвтори ред"))
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	if got.Block != reportblock.Read || got.Entries != 3 || len(got.Proposals) != 3 || got.Dropped != nil ||
		!slices.Equal(got.Findings, []redaction.Finding{{Kind: redaction.KindSecretAssignment, Count: 1}}) {
		t.Fatalf("extraction = %+v", got)
	}
	if got.Proposals[0].Content != "password: [REDACTED:secret_assignment]" ||
		got.Proposals[1].Content != "друго" || got.Proposals[2].Content != "трето\nвтори ред" {
		t.Fatalf("proposals = %+v", got.Proposals)
	}
	wantNoRawValue(t, got, "hunter2")
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

// Nothing unmasked leaves Extract on any path: a malformed block, a
// malformed entry, a repeated or over-limit entry, an entry over the source
// cap, an unknown field name and a too-large report keep no raw text.
func TestExtractLeaksNothingOnAnyPath(t *testing.T) {
	secret := "hunter2"
	field := `password: ` + secret
	manySources := make([]string, memory.MaxSourcesPerItem+1)
	for index := range manySources {
		manySources[index] = `{"kind":"spec","ref":"` + field + `"}`
	}
	reports := map[string]string{
		"malformed block":   block(`{"proposals":[` + entry("fact", field) + `],"note":"` + field + `"}`),
		"invalid JSON":      block(`{"proposals":[` + entry("fact", field)),
		"unclosed":          "```conduit-memory\n" + `{"proposals":[` + entry("fact", field) + `]}`,
		"multiple":          proposalsOf(entry("fact", field)) + proposalsOf(entry("fact", field)),
		"malformed entry":   proposalsOf(`{"category":"fact","content":"` + field + `","` + secret + `":1}`),
		"invalid category":  proposalsOf(entry(secret, field)),
		"invalid source":    proposalsOf(`{"category":"fact","content":"a","sources":[{"kind":"` + secret + `","ref":"` + field + `"}]}`),
		"blob hash":         proposalsOf(`{"category":"fact","content":"a","sources":[{"kind":"file","path":"a.go","blob_hash":"` + secret + `"}]}`),
		"duplicates":        proposalsOf(entry("fact", field), entry("fact", field)),
		"over limit":        proposalsOf(append(distinctEntries(memory.MaxProposalsPerReport), entry("fact", field))...),
		"over source cap":   proposalsOf(`{"category":"fact","content":"` + field + `","sources":[` + strings.Join(manySources, ",") + `]}`),
		"past examined":     proposalsOf(append(distinctEntries(memory.MaxProposalEntriesExamined), entry("fact", field))...),
		"outside the block": field + "\n" + proposalsOf(entry("fact", "a")),
		"too large":         proposalsOf(entry("fact", field)) + strings.Repeat("x", memory.MaxReportBytes),
	}
	for name, report := range reports {
		t.Run(name, func(t *testing.T) {
			wantNoRawValue(t, Extract(report, redaction.Policy{}, lowerNormalizer{}), secret)
		})
	}
}

// A report over memory.MaxReportBytes is refused whole, before any
// scanning, decoding or redaction; exactly the limit is read.
func TestExtractRefusesATooLargeReport(t *testing.T) {
	valid := proposalsOf(entry("fact", "токен "+fakeToken))
	padded := func(size int) string {
		return strings.Repeat("x", size-len(valid)-1) + "\n" + valid
	}
	calls := 0
	atLimit := Extract(padded(memory.MaxReportBytes), countingRedactor{&calls, redaction.Policy{}}, lowerNormalizer{})
	if len(padded(memory.MaxReportBytes)) != 256*1024 || atLimit.Block != reportblock.Read || len(atLimit.Proposals) != 1 ||
		!slices.Equal(atLimit.Findings, []redaction.Finding{{Kind: redaction.KindGitHubToken, Count: 1}}) || calls != 1 {
		t.Fatalf("at the limit: %+v after %d redactions", atLimit, calls)
	}
	calls = 0
	over := Extract(padded(memory.MaxReportBytes+1), countingRedactor{&calls, redaction.Policy{}}, lowerNormalizer{})
	if !reflect.DeepEqual(over, Extraction{Block: reportblock.TooLarge}) || calls != 0 {
		t.Fatalf("over the limit: %#v after %d redactions", over, calls)
	}
}

// A JSON escape (here of a letter of the key) hides a secret from the
// report's redaction; the decoded
// content, ref and path are redacted again before anything is compared or
// returned, and a secret posing as a blob hash fails its grammar. No form
// of the secret is anywhere in the result.
func TestExtractRedactsDecodedValues(t *testing.T) {
	escaped := `p\u0061ssword=hunter2`
	report := proposalsOf(
		`{"category":"fact","content":"`+escaped+`","sources":[{"kind":"spec","ref":"`+escaped+`"},{"kind":"file","path":"`+escaped+`","blob_hash":"`+strings.Repeat("a", 40)+`"}]}`,
		`{"category":"fact","content":"другo","sources":[{"kind":"file","path":"a.go","blob_hash":"`+escaped+`"}]}`,
	)
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	if got.Block != reportblock.Read || len(got.Proposals) != 1 || !slices.Equal(got.Dropped, dropped(memory.ProposalInvalidSource, 1)) ||
		!slices.Equal(got.Findings, []redaction.Finding{{Kind: redaction.KindSecretAssignment, Count: 3}}) {
		t.Fatalf("extraction = %+v", got)
	}
	masked := "password=[REDACTED:secret_assignment]"
	proposal := got.Proposals[0]
	if proposal.Content != masked || proposal.Sources[0].Ref != masked || proposal.Sources[1].Path != masked {
		t.Fatalf("proposal = %+v", proposal)
	}
	wantSecretAbsent(t, got)
}

// Two different hidden secrets that mask to the same text are duplicates,
// as their stored text would be.
func TestExtractComparesTheRedactedText(t *testing.T) {
	report := proposalsOf(entryJSONText(`p\u0061ssword=hunter2`), entryJSONText(`p\u0061ssword=second-secret`))
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	if len(got.Proposals) != 1 || !slices.Equal(got.Dropped, dropped(memory.ProposalDuplicate, 1)) ||
		!slices.Equal(got.Findings, []redaction.Finding{{Kind: redaction.KindSecretAssignment, Count: 2}}) {
		t.Fatalf("extraction = %+v", got)
	}
	wantSecretAbsent(t, got)
}

func entryJSONText(content string) string {
	return `{"category":"fact","content":"` + content + `"}`
}

func wantSecretAbsent(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "second-secret"} {
		if strings.Contains(string(encoded), secret) || strings.Contains(fmt.Sprintf("%+v", value), secret) {
			t.Fatalf("%s survived in %s", secret, encoded)
		}
	}
}

// Repeated fields, also under encoding/json's case folding, are malformed:
// the block at the top level, the entry inside it.
func TestExtractRefusesRepeatedFields(t *testing.T) {
	valid := entry("fact", "a")
	tests := []extractCase{
		{name: "repeated proposals", report: block(`{"proposals":[` + valid + `],"proposals":[]}`), block: reportblock.Malformed},
		{name: "proposals in another case", report: block(`{"proposals":[` + valid + `],"PROPOSALS":[]}`), block: reportblock.Malformed},
		{name: "proposals with a folding letter", report: block(`{"proposals":[` + valid + `],"propoſals":[]}`), block: reportblock.Malformed},
		{name: "repeated entry field", report: proposalsOf(`{"category":"fact","content":"a","Content":"b"}`, entry("fact", "c")),
			block: reportblock.Read, entries: 2, kept: []int{2}, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "repeated source field", report: proposalsOf(`{"category":"fact","content":"a","sources":[{"kind":"spec","ref":"x","REF":"y"}]}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "same field in different objects", report: proposalsOf(
			`{"category":"fact","content":"a","sources":[{"kind":"spec","ref":"x"},{"kind":"spec","ref":"y"}]}`), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "repeated entry fields do not spoil the block", report: proposalsOf(`{"content":"a","content":"b"}`, `{"content":"c","content":"d"}`),
			block: reportblock.Read, entries: 2, dropped: dropped(memory.ProposalMalformedEntry, 2)},
	}
	runExtractCases(t, tests)
}

// Only the first MaxProposalEntriesExamined entries are decoded; the rest
// are over the limit, so a block of as many entries as fit the report limit
// is read quickly, with the real policy.
func TestExtractBoundsTheWork(t *testing.T) {
	entries := make([]string, 6_000)
	for index := range entries {
		entries[index] = fmt.Sprintf(`{"category":"fact","content":"е%d"}`, index)
	}
	report := proposalsOf(entries...)
	if len(report) > memory.MaxReportBytes {
		t.Fatalf("report of %d bytes is over the limit", len(report))
	}
	start := time.Now()
	got := Extract(report, redaction.Policy{}, lowerNormalizer{})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("extraction took %v", elapsed)
	}
	if got.Entries != len(entries) || len(got.Proposals) != memory.MaxProposalsPerReport ||
		!slices.Equal(got.Dropped, dropped(memory.ProposalOverLimit, len(entries)-memory.MaxProposalsPerReport)) {
		t.Fatalf("extraction: %d entries, %d kept, dropped %v", got.Entries, len(got.Proposals), got.Dropped)
	}

	// Past the examined entries even an invalid one is only over the limit,
	// and a repeat of an examined one is not compared.
	examined := append(distinctEntries(memory.MaxProposalEntriesExamined), `"x"`, entry("fact", "факт 1"))
	got = Extract(proposalsOf(examined...), redaction.Policy{}, lowerNormalizer{})
	if !slices.Equal(got.Dropped, dropped(memory.ProposalOverLimit, memory.MaxProposalEntriesExamined-memory.MaxProposalsPerReport+2)) {
		t.Fatalf("dropped = %v", got.Dropped)
	}

	sources := make([]string, memory.MaxSourcesPerItem+1)
	for index := range sources {
		sources[index] = `{"kind":"spec","ref":"specs/012"}`
	}
	many := `{"category":"fact","content":"a","sources":[` + strings.Join(sources, ",") + `]}`
	got = Extract(proposalsOf(many), redaction.Policy{}, lowerNormalizer{})
	if !slices.Equal(got.Dropped, dropped(memory.ProposalInvalidSource, 1)) {
		t.Fatalf("dropped = %v", got.Dropped)
	}
}

// An entry whose identity storage cannot give matches nothing.
type blankNormalizer struct{}

func (blankNormalizer) SearchTextIdentity(string) memory.SearchTextIdentity {
	return memory.SearchTextIdentity{}
}

func TestExtractWithoutIdentitiesFindsNoDuplicates(t *testing.T) {
	got := Extract(proposalsOf(entry("fact", "a"), entry("fact", "a")), redaction.Policy{}, blankNormalizer{})
	if len(got.Proposals) != 2 || got.Dropped != nil {
		t.Fatalf("extraction = %+v", got)
	}
}

// countingRedactor counts its calls and redacts with inner.
type countingRedactor struct {
	calls *int
	inner redaction.Redactor
}

func (c countingRedactor) Redact(text string) redaction.Result {
	*c.calls++
	return c.inner.Redact(text)
}

// An entry over the source cap is dropped on its count: its fields are not
// redacted, and it adds no findings.
func TestExtractDropsAnEntryOverTheSourceCapBeforeRedactingIt(t *testing.T) {
	sources := make([]string, 5_000)
	for index := range sources {
		sources[index] = `{"kind":"spec","ref":"p\u0061ssword=hunter2"}`
	}
	many := `{"category":"fact","content":"много","sources":[` + strings.Join(sources, ",") + `]}`
	calls := 0
	got := Extract(proposalsOf(many, entry("fact", "едно")), countingRedactor{&calls, redaction.Policy{}}, lowerNormalizer{})
	if got.Block != reportblock.Read || !slices.Equal(got.Dropped, dropped(memory.ProposalInvalidSource, 1)) || len(got.Proposals) != 1 || got.Findings != nil {
		t.Fatalf("extraction = %+v", got)
	}
	// Only the kept entry's content: no per-source work, no whole report.
	if calls != 1 {
		t.Fatalf("redactions = %d", calls)
	}
	wantSecretAbsent(t, got)

	calls = 0
	got = Extract(proposalsOf(`{"category":"fact","content":"a","sources":[{"kind":"spec","ref":"r"},{"kind":"spec","ref":"s"}]}`), countingRedactor{&calls, redaction.Policy{}}, lowerNormalizer{})
	if len(got.Proposals) != 1 || calls != 1+2*2 {
		t.Fatalf("redactions = %d for %+v", calls, got)
	}
}

// Nesting deeper than reportblock.MaxNesting makes the block malformed, wherever it is,
// even inside an entry past the examined ones; the check is iterative, so
// any depth returns.
func TestExtractRefusesDeepNesting(t *testing.T) {
	deep := func(open, close string, depth int) string {
		return strings.Repeat(open, depth) + strings.Repeat(close, depth)
	}
	nestedObject := func(depth int) string {
		return strings.Repeat(`{"a":`, depth) + "1" + strings.Repeat("}", depth)
	}
	tests := []extractCase{
		{name: "deep arrays", report: block(`{"proposals":` + deep("[", "]", 100_000) + `}`), block: reportblock.Malformed},
		{name: "deep objects", report: block(`{"proposals":[` + nestedObject(40_000) + `]}`), block: reportblock.Malformed},
		{name: "deep inside an entry past the examined ones", report: proposalsOf(append(distinctEntries(memory.MaxProposalEntriesExamined),
			`{"category":"fact","content":"a","sources":`+deep("[", "]", 100_000)+`}`)...), block: reportblock.Malformed},
		{name: "one level past the limit", report: block(`{"proposals":[{"category":"fact","content":"a","sources":[{"ref":` + deep("[", "]", reportblock.MaxNesting-4) + `}]}]}`), block: reportblock.Malformed},
		{name: "at the limit is decoded", report: block(`{"proposals":[{"category":"fact","content":"a","sources":[{"ref":` + deep("[", "]", reportblock.MaxNesting-5) + `}]}]}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.report) > memory.MaxReportBytes {
				t.Fatalf("report of %d bytes is over the limit", len(tt.report))
			}
			got := Extract(tt.report, redaction.Policy{}, lowerNormalizer{})
			if got.Block != tt.block || got.Entries != tt.entries || !slices.Equal(got.Dropped, tt.dropped) {
				t.Fatalf("extraction = %+v", got)
			}
		})
	}
}

// Names are compared after JSON unescaping, so an escaped repeat is a
// repeat; surrogate pairs decode to one rune in names and values.
func TestExtractEscapedNamesAndSurrogatePairs(t *testing.T) {
	tests := []extractCase{
		{name: "escaped repeat of proposals", report: block(`{"proposals":[],"\u0070roposals":[]}`), block: reportblock.Malformed},
		{name: "escaped repeat of content", report: proposalsOf(`{"category":"fact","content":"a","c\u006fntent":"b"}`, entry("fact", "c")),
			block: reportblock.Read, entries: 2, kept: []int{2}, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "escaped field name alone", report: proposalsOf(`{"c\u0061tegory":"fact","content":"a"}`), block: reportblock.Read, entries: 1, kept: []int{1}},
		{name: "surrogate pair name is unknown", report: proposalsOf(`{"category":"fact","content":"a","\ud83d\ude00":"x"}`),
			block: reportblock.Read, entries: 1, dropped: dropped(memory.ProposalMalformedEntry, 1)},
		{name: "repeated surrogate pair names", report: block(`{"proposals":[],"\ud83d\ude00":1,"😀":2}`), block: reportblock.Malformed},
	}
	runExtractCases(t, tests)

	got := Extract(proposalsOf(`{"category":"fact","content":"\ud83d\ude00 и \ud83d"}`, entry("fact", "😀 и \ufffd")), redaction.Policy{}, lowerNormalizer{})
	if len(got.Proposals) != 1 || got.Proposals[0].Content != "😀 и \ufffd" || !slices.Equal(got.Dropped, dropped(memory.ProposalDuplicate, 1)) {
		t.Fatalf("extraction = %+v", got)
	}
}

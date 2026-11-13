package reportblock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/memory"
)

const info = "conduit-test"

func fenced(body string) string { return "```" + info + "\n" + body + "\n```\n" }

// The formats' own tests (proposals, audit) exercise these rules through
// their Extract; this table pins the shared scanner on its own.
func TestFind(t *testing.T) {
	tests := []struct {
		name   string
		report string
		kind   Kind
		body   string
	}{
		{"empty report", "", Absent, ""},
		{"no block", "done", Absent, ""},
		{"other info string", "```json\n{}\n```", Absent, ""},
		{"info string with another word", "```" + info + " json\n{}\n```", Absent, ""},
		{"prefix of another info string", "```conduit-test-more\n{}\n```", Absent, ""},
		{"one block", fenced(`{"a":1}`), Read, `{"a":1}`},
		{"empty body", fenced(""), Read, ""},
		{"multi-line body", fenced("{\n\"a\": 1\n}"), Read, "{\n\"a\": 1\n}"},
		{"info string spacing", "   ~~~  " + info + "  \nx\n~~~~", Read, "x"},
		{"CRLF and lone CR", "```" + info + "\r\nx\ry\r\n```", Read, "x\ny"},
		{"four spaces is not a fence", "    ```" + info + "\nx\n    ```", Absent, ""},
		{"two-character run", "``" + info + "\nx\n``", Absent, ""},
		{"backtick in a backtick info string", "```" + info + "`\nx\n```", Absent, ""},
		{"tilde info may hold a backtick", "~~~" + info + "\nx\n~~~", Read, "x"},
		{"shorter closing run is content", "````" + info + "\nx\n```\n````", Read, "x\n```"},
		{"other character does not close", "```" + info + "\nx\n~~~\n```", Read, "x\n~~~"},
		{"closing fence with text is content", "```" + info + "\nx\n``` y\n```", Read, "x\n``` y"},
		{"inside another block", "````markdown\n" + fenced("x") + "````\n", Absent, ""},
		{"after another block", "```go\nx := 1\n```\n" + fenced("x"), Read, "x"},
		{"two blocks", fenced("x") + fenced("y"), Multiple, ""},
		{"closed and unclosed", fenced("x") + "```" + info + "\ny", Multiple, ""},
		{"unclosed", "```" + info + "\nx", Malformed, ""},
		{"unclosed other block", "```text\nx", Absent, ""},
		{"at the size bound", fenced("x") + strings.Repeat(" ", memory.MaxReportBytes-len(fenced("x"))), Read, "x"},
		{"one byte over", fenced("x") + strings.Repeat(" ", memory.MaxReportBytes-len(fenced("x"))+1), TooLarge, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, body := Find(tt.report, info)
			if kind != tt.kind || body != tt.body {
				t.Fatalf("Find = %s %q, want %s %q", kind, body, tt.kind, tt.body)
			}
		})
	}
}

func deep(open, close string, levels int) string {
	return strings.Repeat(open, levels) + strings.Repeat(close, levels)
}

func TestDecodeObject(t *testing.T) {
	type entry struct {
		Name  string            `json:"name"`
		Items []json.RawMessage `json:"items"`
	}
	tests := []struct {
		name     string
		data     string
		keyDepth int
		ok       bool
	}{
		{"object", `{"name":"a","items":[{"x":1}]}`, 1, true},
		{"surrounding space", " \n{\"name\":\"a\"}\n ", 1, true},
		{"case-insensitive field", `{"NAME":"a"}`, 1, true},
		{"not an object", `["name"]`, 1, false},
		{"null", `null`, 1, false},
		{"invalid JSON", `{"name":`, 1, false},
		{"unknown field", `{"name":"a","other":1}`, 1, false},
		{"wrong type", `{"name":5}`, 1, false},
		{"trailing data", `{"name":"a"} {}`, 1, false},
		{"trailing garbage", `{"name":"a"} x`, 1, false},
		{"repeated field", `{"name":"a","name":"b"}`, 1, false},
		{"repeated field under folding", `{"name":"a","NAME":"b"}`, 1, false},
		{"escaped repeat", `{"name":"a","name":"b"}`, 1, false},
		{"repeat below the key depth is not checked", `{"items":[{"x":1,"x":2}]}`, 1, true},
		{"repeat at the key depth is refused", `{"items":[{"x":1,"x":2}]}`, 2, false},
		{"same name in sibling objects", `{"items":[{"x":1},{"x":2}]}`, 2, true},
		{"nesting at the bound", `{"items":[` + deep("[", "]", MaxNesting-2) + `]}`, 1, true},
		{"nesting past the bound", `{"items":[` + deep("[", "]", MaxNesting-1) + `]}`, 1, false},
		{"objects past the bound", `{"items":[` + deep(`{"a":`, "}", MaxNesting-2) + `]}`, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target entry
			if got := DecodeObject([]byte(tt.data), &target, tt.keyDepth); got != tt.ok {
				t.Fatalf("DecodeObject = %v, want %v", got, tt.ok)
			}
		})
	}
}

// Deep nesting is refused without recursion, whatever its depth.
func TestDecodeObjectRefusesDeepNestingIteratively(t *testing.T) {
	data := `{"items":[` + deep("[", "]", 1_000_000) + `]}`
	var target struct {
		Items []json.RawMessage `json:"items"`
	}
	if DecodeObject([]byte(data), &target, 1) {
		t.Fatal("deep nesting decoded")
	}
}

func TestFoldName(t *testing.T) {
	for _, pair := range [][2]string{{"name", "NAME"}, {"s", "ſ"}, {"k", "K"}, {"ж", "Ж"}} {
		if foldName(pair[0]) != foldName(pair[1]) {
			t.Errorf("%q and %q fold apart", pair[0], pair[1])
		}
	}
	if foldName("a") == foldName("b") {
		t.Fatal("distinct names fold together")
	}
}

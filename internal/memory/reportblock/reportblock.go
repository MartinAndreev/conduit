// Package reportblock reads one structured block out of an agent's final
// report: the fenced code block with a given info string, and a strict JSON
// object decoder for its content. Proposals (conduit-memory) and audit
// suggestions (conduit-memory-audit) share it, so both formats have the same
// fence, size, repeated-key and nesting rules. It performs no I/O and decides
// nothing about entries; each format's rule does.
package reportblock

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"

	"github.com/MartinAndreev/conduit/internal/memory"
)

// Kind is what the report held, read as a whole.
type Kind string

const (
	// Absent: no block. The report proposes nothing; this is not an error.
	Absent Kind = "absent"
	// Read: exactly one block, which decoded; its entries were decided.
	Read Kind = "read"
	// Multiple: more than one block, counting an unclosed one. None is read,
	// because none can be told to be the report's.
	Multiple Kind = "multiple"
	// Malformed: one block that is unclosed, is not one JSON object of the
	// format's envelope, or whose entry array is missing or not an array.
	// Nothing is read.
	Malformed Kind = "malformed"
	// TooLarge: the report is over memory.MaxReportBytes. It is refused as a
	// whole: nothing is scanned, decoded or redacted.
	TooLarge Kind = "too_large"
)

// Find returns the body of the one closed fenced code block whose info
// string is exactly info. It returns TooLarge for a report over
// memory.MaxReportBytes before scanning it, Absent without such a block,
// Multiple for more than one (counting an unclosed one), Malformed for one
// unclosed block, and Read with the body otherwise. The envelope is the
// caller's to decode.
func Find(report, info string) (Kind, string) {
	if len(report) > memory.MaxReportBytes {
		return TooLarge, ""
	}
	bodies, unclosed := blocks(report, info)
	switch count := len(bodies); {
	case unclosed && count > 0, count > 1:
		return Multiple, ""
	case unclosed:
		return Malformed, ""
	case count == 0:
		return Absent, ""
	}
	return Read, bodies[0]
}

// DecodeObject decodes exactly one JSON object into target, refusing unknown
// fields, repeated fields and trailing data. Field names match as
// encoding/json matches them, ignoring case, so a repeat is a name equal to
// an earlier one of the same object under that folding. Objects nested
// deeper than keyDepth levels of objects are not checked for repeats here: an
// envelope checks only its own fields (1), and each entry is decoded, and
// checked, on its own. Nesting deeper than MaxNesting objects and arrays
// refuses data wherever it is. The decoder's error is dropped: it can quote
// report text.
func DecodeObject(data []byte, target any, keyDepth int) bool {
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

// MaxNesting bounds the objects and arrays open at once in a block, counted
// from the block's top value. The proposal format needs 5 (envelope,
// proposals, entry, sources, source), the audit format 3; anything deeper is
// malformed, and the bound keeps the key check's work and memory per level
// fixed.
const MaxNesting = 8

// keyFrame is one open object or array of the key check.
type keyFrame struct {
	object    bool
	expectKey bool
	// objectsAround counts the objects that enclose this one.
	objectsAround int
	names         map[string]bool
}

// uniqueKeys reports whether data's first value nests at most MaxNesting
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
			if len(stack) == MaxNesting {
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
	char   byte
	length int
	wanted bool
}

// blocks returns the bodies of the closed fenced code blocks whose info
// string is exactly info, and whether such a block is still open at the end
// of the text. Fences follow CommonMark: up to three spaces of indentation,
// three or more backticks or tildes, and a closing fence of the same
// character at least as long with nothing after it. A fence inside another
// block is that block's content. LF, CRLF and a lone CR end a line.
func blocks(text, info string) ([]string, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var bodies, body []string
	var open *fence
	for _, line := range strings.Split(text, "\n") {
		switch {
		case open == nil:
			if opened, ok := openingFence(line, info); ok {
				open, body = &opened, nil
			}
		case closesFence(line, *open):
			if open.wanted {
				bodies = append(bodies, strings.Join(body, "\n"))
			}
			open = nil
		default:
			body = append(body, line)
		}
	}
	return bodies, open != nil && open.wanted
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

func openingFence(line, info string) (fence, bool) {
	run, rest := fenceRun(line)
	if run == "" || (run[0] == '`' && strings.Contains(rest, "`")) {
		return fence{}, false
	}
	return fence{char: run[0], length: len(run), wanted: strings.TrimSpace(rest) == info}, true
}

func closesFence(line string, open fence) bool {
	run, rest := fenceRun(line)
	return run != "" && run[0] == open.char && len(run) >= open.length && strings.TrimSpace(rest) == ""
}

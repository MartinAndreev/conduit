package redaction

import (
	"regexp"
	"slices"
	"strings"
)

// assignmentKey finds a key ending in a secret-like name and its "=", ":" or
// ":=". Groups: 1 the quote closing a quoted key, 2 the space before the
// operator, 3 the operator, 4 the space after it. The space may contain line
// breaks; assignmentValues decides when that is allowed. The character before
// the key is checked in code, so a search may start anywhere in the text.
var assignmentKey = regexp.MustCompile(`(?i)(?:[\w.-]*[_.-])?` + secretKeyNames + `(["']?)(\s*)(:=|[:=])(\s*)`)

// secretKeyNames are the key endings that mark a secret assignment. Vendor
// token rules share them to stop before a key glued to a token.
const secretKeyNames = `(?:password|passwd|secret|secret[_-]?key|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key)`

// nonCredentialLiterals are language literals that cannot be a credential.
var nonCredentialLiterals = []string{"null", "nil", "none", "true", "false", "undefined", "~"}

// assignment is one key match.
type assignment struct {
	key        int // start of the key
	value      int // first non-space character after the operator
	operator   string
	opEnd      int // end of the operator
	quotedKey  bool
	breakAfter bool // a line break separates the operator from the value
}

// assignmentValues returns the value of every secret-like assignment:
//   - a quoted value (double, single or back quotes, or tripled double or
//     single quotes) runs to its closing delimiter, across lines; an
//     unterminated one runs to the end of the text rather than leak;
//   - after Go's ":=" only quoted values count, so scanner.Token() stays;
//   - after ":", a YAML block scalar header ("|" or ">") masks the following
//     lines indented more than the key's line;
//   - a value on a later line (key: then a line break) runs from its first
//     character over the following lines indented more than the key's line;
//     for an unquoted key it must itself be indented more, so a sibling key
//     is not a value;
//   - otherwise the value runs to the end of its line and over following
//     lines indented more than the key's line, unless that whole remainder is
//     a literal such as null.
//
// A line break between a key and its operator is allowed only for a quoted
// (JSON) key. LF, CRLF and a lone CR each end a line.
func assignmentValues(text string) []span {
	var spans []span
	for pos := 0; pos < len(text); {
		m := assignmentKey.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			break
		}
		start, end := pos+m[0], pos+m[1]
		if start > 0 && isKeyChar(text[start-1]) {
			pos = start + 1
			continue
		}
		a := assignment{
			key:        start,
			value:      end,
			operator:   text[pos+m[6] : pos+m[7]],
			opEnd:      pos + m[7],
			quotedKey:  m[3] > m[2],
			breakAfter: strings.ContainsAny(text[pos+m[8]:pos+m[9]], "\r\n"),
		}
		if !a.quotedKey && strings.ContainsAny(text[pos+m[4]:pos+m[5]], "\r\n") {
			pos = end
			continue
		}
		s, ok, resume := assignmentValue(text, a)
		if ok {
			spans = append(spans, s)
		}
		pos = max(end, resume)
	}
	return spans
}

// isKeyChar reports whether c can continue a key, so a key match preceded by it
// is only the tail of a longer, different key.
func isKeyChar(c byte) bool {
	return c == '_' || c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// assignmentValue returns the value of a, whether there is one, and where the
// key search resumes. Every case resumes past what it examined, so a long line
// is not rescanned per candidate key.
func assignmentValue(text string, a assignment) (span, bool, int) {
	i := a.value
	switch {
	case i == len(text):
		return span{}, false, i
	case text[i] == '"' || text[i] == '\'' || text[i] == '`':
		s := quotedValue(text, i)
		return s, true, s.end
	case a.operator == ":=":
		return span{}, false, i
	case a.breakAfter:
		return laterLineValue(text, a)
	case a.operator == ":" && blockScalarHeader(text, i):
		_, next := lineEnd(text, i)
		s, ok := blockScalarBody(text, next, lineIndent(text, a.key))
		return s, ok, max(next, s.end)
	case a.operator == "=" && i == a.opEnd && text[i] == '=':
		// "==" is a comparison, not an assignment; "= =x" assigns "=x".
		return span{}, false, i
	default:
		return restOfLine(text, i, lineIndent(text, a.key))
	}
}

// restOfLine masks the value at i up to the end of its line, without trailing
// spaces, and the following lines indented more than the key's line, which
// continue a YAML plain scalar. A remainder that is a literal, optionally
// closed by brackets, commas or a semicolon as in {"token": null}, is not a
// value when nothing continues it. Numbers are values.
func restOfLine(text string, i, keyIndent int) (span, bool, int) {
	end, next := lineEnd(text, i)
	value := span{i, i + len(strings.TrimRight(text[i:end], " \t"))}
	if more, ok := blockScalarBody(text, next, keyIndent); ok {
		value.end = more.end
		return value, true, value.end
	}
	if literalRemainder(text[value.start:value.end]) {
		return span{}, false, end
	}
	return value, true, value.end
}

func literalRemainder(value string) bool {
	bare := strings.TrimRight(value, " \t)]},;")
	return slices.ContainsFunc(nonCredentialLiterals, func(literal string) bool {
		return strings.EqualFold(bare, literal)
	})
}

// laterLineValue masks a value that starts on a line after its operator: that
// line and the following lines indented more than the key's line. For an
// unquoted key the first line must itself be indented more; otherwise it is a
// sibling and the key has no value.
func laterLineValue(text string, a assignment) (span, bool, int) {
	keyIndent := lineIndent(text, a.key)
	if !a.quotedKey && lineIndent(text, a.value) <= keyIndent {
		return span{}, false, a.value
	}
	end, next := lineEnd(text, a.value)
	first := a.value + len(strings.TrimRight(text[a.value:end], " \t"))
	if more, ok := blockScalarBody(text, next, keyIndent); ok {
		return span{a.value, more.end}, true, more.end
	}
	if literalRemainder(text[a.value:first]) {
		return span{}, false, first
	}
	return span{a.value, first}, true, first
}

// blockScalarHeader reports whether a YAML block scalar header starts at i: "|"
// or ">", at most one chomping and one indentation indicator in either order,
// then only spaces, a comment after a space, or the end of the line. It looks
// only at the header itself, never through a comment.
func blockScalarHeader(text string, i int) bool {
	if text[i] != '|' && text[i] != '>' {
		return false
	}
	j := i + 1
	j = skipIndicator(text, j, "+-", "123456789")
	j = skipIndicator(text, j, "123456789", "+-")
	k := j
	for k < len(text) && (text[k] == ' ' || text[k] == '\t') {
		k++
	}
	return k == len(text) || text[k] == '\r' || text[k] == '\n' || k > j && text[k] == '#'
}

// skipIndicator skips one character of first, then optionally one of second.
func skipIndicator(text string, j int, first, second string) int {
	if j < len(text) && strings.IndexByte(first, text[j]) >= 0 {
		j++
		if j < len(text) && strings.IndexByte(second, text[j]) >= 0 {
			j++
		}
	}
	return j
}

// quotedValue returns the whole value inside the quote opening at open. A
// tripled quote (Python, TOML) is a delimiter of its own, checked first so
// """x""" is not read as an empty string.
func quotedValue(text string, open int) span {
	delim := text[open : open+1]
	if triple := strings.Repeat(delim, 3); delim != "`" && strings.HasPrefix(text[open:], triple) {
		delim = triple
	}
	start := open + len(delim)
	return span{start, closingQuote(text, start, delim)}
}

// closingQuote returns the index of the delimiter closing a value that starts
// at start, or the end of the text when it is unterminated. A backslash
// escapes the next character except inside backticks, which are raw, and a
// doubled single quote is YAML's escaped quote.
func closingQuote(text string, start int, delim string) int {
	for j := start; j < len(text); j++ {
		switch {
		case text[j] == '\\' && delim != "`":
			j++
		case delim == "'" && strings.HasPrefix(text[j:], "''"):
			j++
		case strings.HasPrefix(text[j:], delim):
			return j
		}
	}
	return len(text)
}

// blockScalarBody returns the span from the first to the last non-blank line,
// starting at p, that is indented more than keyIndent. Blank lines inside the
// body are part of it; no such line means no body.
func blockScalarBody(text string, p, keyIndent int) (span, bool) {
	body := span{-1, -1}
	for p < len(text) {
		end, next := lineEnd(text, p)
		line := strings.TrimRight(text[p:end], " \t")
		content := strings.TrimLeft(line, " \t")
		if content != "" {
			indent := len(line) - len(content)
			if indent <= keyIndent {
				break
			}
			if body.start < 0 {
				body.start = p + indent
			}
			body.end = p + len(line)
		}
		p = next
	}
	return body, body.start >= 0
}

// lineEnd returns where the line at p ends and where the next one starts. LF,
// CRLF and a lone CR each end a line; the last line ends at the end of the text.
func lineEnd(text string, p int) (int, int) {
	n := strings.IndexAny(text[p:], "\r\n")
	if n < 0 {
		return len(text), len(text)
	}
	end := p + n
	if strings.HasPrefix(text[end:], "\r\n") {
		return end, end + 2
	}
	return end, end + 1
}

// lineIndent counts the spaces and tabs that start the line containing p.
func lineIndent(text string, p int) int {
	begin := strings.LastIndexAny(text[:p], "\r\n") + 1
	line := text[begin:]
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

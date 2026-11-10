package rules

import (
	"strings"
	"unicode"
)

// EquivalentDDL reports whether a definition stored in sqlite_schema is the
// statement that created it. Turso does not store the statement verbatim: it
// rewrites whitespace around punctuation and double-quotes the column name
// key; observed identically with tursogo v0.7.2 and v0.8.3. Both stores compare their owned DDL through this one definition.
func EquivalentDDL(stored, expected string) bool {
	return normalizedDDL(stored) == normalizedDDL(expected)
}

// Token separators remain significant: TEXT PRIMARY KEY must never become the
// custom type TEXTPRIMARYKEY. String literals and quoted identifiers are kept
// byte for byte, except the identifiers Turso is known to quote.
func normalizedDDL(statement string) string {
	characters := []rune(statement)
	var result strings.Builder
	previousWord, whitespace := false, false
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		if unicode.IsSpace(character) {
			whitespace = true
			continue
		}
		token, word, unquoted := string(character), isWordCharacter(character), false
		if character == '\'' || character == '"' || character == '`' {
			end := quotedEnd(characters, index)
			token, word = string(characters[index:end]), false
			if identifier, ok := plainIdentifier(token); ok {
				token, word, unquoted = identifier, true, true
			}
			index = end - 1
		}
		// Quotes delimit a token as whitespace does: "key"TEXT is key TEXT.
		if (whitespace || unquoted) && previousWord && word {
			result.WriteByte(' ')
		}
		result.WriteString(token)
		whitespace = unquoted
		previousWord = word
	}
	return result.String()
}

// quotedEnd returns the index after the quoted token starting at start. A
// doubled quote character is an escaped quote inside the token.
func quotedEnd(characters []rune, start int) int {
	quote := characters[start]
	for index := start + 1; index < len(characters); index++ {
		if characters[index] != quote {
			continue
		}
		if index+1 < len(characters) && characters[index+1] == quote {
			index++
			continue
		}
		return index + 1
	}
	return len(characters)
}

// tursoQuotedIdentifiers are the names Turso was observed to store quoted
// although our statements did not quote them. Only these are unquoted: in
// SQLite a double-quoted word may fall back to a string literal, so "NULL" or
// "TRUE" is not NULL or TRUE. A name Turso newly quotes makes a fresh
// database fail its own validation, so this list cannot silently go stale.
var tursoQuotedIdentifiers = []string{"key"}

func plainIdentifier(token string) (string, bool) {
	for _, name := range tursoQuotedIdentifiers {
		if token == `"`+name+`"` {
			return name, true
		}
	}
	return "", false
}

func isWordCharacter(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$'
}

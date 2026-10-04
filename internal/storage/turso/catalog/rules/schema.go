// Package rules owns deterministic catalog schema compatibility decisions.
package rules

import (
	"strconv"
	"strings"
	"unicode"
)

type SchemaKind string

const (
	SchemaCompatible SchemaKind = "compatible"
	SchemaCorrupt    SchemaKind = "corrupt"
	SchemaFuture     SchemaKind = "future"
)

type SchemaDecision struct {
	Kind   SchemaKind
	Reason string
}

func DecideMarker(key, actual, expected string) SchemaDecision {
	if actual == expected {
		return SchemaDecision{SchemaCompatible, "Catalog marker matches."}
	}
	if key == "schema_version" {
		version, err := strconv.Atoi(actual)
		supported, supportedErr := strconv.Atoi(expected)
		if err == nil && supportedErr == nil && version > supported {
			return SchemaDecision{SchemaFuture, "Catalog schema version is newer than supported."}
		}
	}
	return SchemaDecision{SchemaCorrupt, "Catalog marker is incompatible: " + key + "."}
}

// Exact owned DDL preserves constraints, not just column names. A differently
// shaped format requires a future version; this adapter never repairs it.
func DecideTable(actual, expected string) SchemaDecision {
	if normalizedDDL(actual) == normalizedDDL(expected) {
		return SchemaDecision{SchemaCompatible, "Catalog table and constraints match."}
	}
	return SchemaDecision{SchemaCorrupt, "Catalog table or constraints are incompatible."}
}

// Turso rewrites whitespace around punctuation. Token separators remain
// significant: TEXT PRIMARY KEY must never become the custom type TEXTPRIMARYKEY.
// Quoted identifiers and string literals are retained byte for byte.
func normalizedDDL(statement string) string {
	var result strings.Builder
	var quote rune
	previousWord, whitespace := false, false
	for _, character := range statement {
		if quote != 0 {
			result.WriteRune(character)
			if character == quote {
				quote = 0
			}
			continue
		}
		if unicode.IsSpace(character) {
			whitespace = true
			continue
		}
		word := unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$'
		if whitespace && previousWord && word {
			result.WriteByte(' ')
		}
		if character == '\'' || character == '"' || character == '`' {
			quote = character
		}
		result.WriteRune(character)
		whitespace = false
		previousWord = word
	}
	return result.String()
}

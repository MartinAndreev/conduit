// Package rules owns deterministic catalog schema compatibility decisions.
package rules

import (
	"strconv"

	enginerules "github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine/rules"
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
	if enginerules.EquivalentDDL(actual, expected) {
		return SchemaDecision{SchemaCompatible, "Catalog table and constraints match."}
	}
	return SchemaDecision{SchemaCorrupt, "Catalog table or constraints are incompatible."}
}

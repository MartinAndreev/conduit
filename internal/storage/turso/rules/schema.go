package rules

import (
	"fmt"
	"slices"
	"strings"

	enginerules "github.com/MartinAndreev/conduit/internal/storage/turso/internal/engine/rules"
)

type SchemaCompatibilityKind string

const (
	SchemaCompatible     SchemaCompatibilityKind = "compatible"
	SchemaMarkerMismatch SchemaCompatibilityKind = "marker_mismatch"
	SchemaObjectMismatch SchemaCompatibilityKind = "object_mismatch"
)

type SchemaCompatibilityDecision struct {
	Kind   SchemaCompatibilityKind
	Reason string
}

// DecideSchemaMarker evaluates one successfully read marker. Keeping evaluation
// incremental preserves rejection before later database reads.
func DecideSchemaMarker(key, actual, expected string) SchemaCompatibilityDecision {
	if actual != expected {
		return SchemaCompatibilityDecision{Kind: SchemaMarkerMismatch, Reason: fmt.Sprintf("invalid %s", key)}
	}
	return SchemaCompatibilityDecision{Kind: SchemaCompatible, Reason: "schema marker matches"}
}

// DecideSchemaObjects requires every expected table and index to be stored
// with exactly its expected definition, so a table missing a UNIQUE, FOREIGN
// KEY or CHECK constraint, or a missing index, is refused. Stored objects the
// expectation does not name are not judged, as in the project catalog.
func DecideSchemaObjects(expected, stored []string) SchemaCompatibilityDecision {
	for _, statement := range expected {
		if !slices.ContainsFunc(stored, func(definition string) bool { return enginerules.EquivalentDDL(definition, statement) }) {
			return SchemaCompatibilityDecision{Kind: SchemaObjectMismatch, Reason: fmt.Sprintf("%s is missing or differs from the expected definition", objectName(statement))}
		}
	}
	return SchemaCompatibilityDecision{Kind: SchemaCompatible, Reason: "schema objects match their definitions"}
}

// objectName names the object of a CREATE TABLE or CREATE INDEX statement for
// the decision reason only.
func objectName(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) < 3 {
		return statement
	}
	return strings.ToLower(fields[1]) + " " + strings.TrimSuffix(fields[2], "(")
}

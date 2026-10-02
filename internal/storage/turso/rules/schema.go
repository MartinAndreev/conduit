package rules

import "fmt"

type SchemaCompatibilityKind string

const (
	SchemaCompatible     SchemaCompatibilityKind = "compatible"
	SchemaMarkerMismatch SchemaCompatibilityKind = "marker_mismatch"
	SchemaColumnMissing  SchemaCompatibilityKind = "column_missing"
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

// DecideSchemaColumns preserves required-column order after a table read succeeds.
func DecideSchemaColumns(table string, required []string, found map[string]bool) SchemaCompatibilityDecision {
	for _, column := range required {
		if !found[column] {
			return SchemaCompatibilityDecision{Kind: SchemaColumnMissing, Reason: fmt.Sprintf("missing %s.%s", table, column)}
		}
	}
	return SchemaCompatibilityDecision{Kind: SchemaCompatible, Reason: "required schema columns are present"}
}

package turso

import "strings"

// searchNormalizationVersion identifies normalizeSearchText and is stored with
// every memory version's search text. Rows written under another version must
// be renormalized before queries normalized by this version are compared.
const searchNormalizationVersion = 1

// normalizeSearchText is Unicode lowercase computed in Go at write time and for
// queries alike, because Turso's lower() and case-insensitive LIKE fold ASCII
// only. It does no stemming.
func normalizeSearchText(text string) string { return strings.ToLower(text) }

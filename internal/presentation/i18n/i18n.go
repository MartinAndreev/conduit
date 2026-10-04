// Package i18n shares the presentation copy used by the terminal and desktop.
package i18n

import (
	_ "embed"
	"encoding/json"
	"strings"
)

type Locale string

const (
	English   Locale = "en"
	Bulgarian Locale = "bg"
)

//go:embed en.json
var englishJSON []byte

//go:embed bg.json
var bulgarianJSON []byte

// Catalogs remain private. Their embedded source is validated by package tests.
var english, englishError = decode(englishJSON)
var bulgarian, bulgarianError = decode(bulgarianJSON)

func decode(data []byte) (map[string]string, error) {
	var catalog map[string]string
	err := json.Unmarshal(data, &catalog)
	return catalog, err
}

// Normalize accepts language tags such as bg-BG and en_US. English is the default.
func Normalize(language string) Locale {
	tag := strings.ToLower(strings.TrimSpace(language))
	tag = strings.ReplaceAll(tag, "_", "-")
	if strings.SplitN(tag, "-", 2)[0] == string(Bulgarian) {
		return Bulgarian
	}
	return English
}

// T returns English for unsupported locales or missing translations. An unknown
// key is returned unchanged so a missing entry remains visible to the developer.
// Formatting belongs to the consumer; both catalogs use the same placeholders.
func T(locale Locale, key string) string {
	if Normalize(string(locale)) == Bulgarian && bulgarianError == nil {
		if text, ok := bulgarian[key]; ok {
			return text
		}
	}
	if englishError == nil {
		if text, ok := english[key]; ok {
			return text
		}
	}
	return key
}

// Catalog returns an independent map for GUI serialization. Mutating the map
// cannot alter translations used by another view or application instance.
func Catalog(locale Locale) map[string]string {
	result := make(map[string]string, len(english))
	for key := range english {
		result[key] = T(locale, key)
	}
	return result
}

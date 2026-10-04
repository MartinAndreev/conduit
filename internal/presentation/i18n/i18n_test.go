package i18n

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestCatalogParityAndFormatting(t *testing.T) {
	if englishError != nil || bulgarianError != nil {
		t.Fatalf("invalid embedded catalog: English=%v Bulgarian=%v", englishError, bulgarianError)
	}
	if len(english) == 0 || len(english) != len(bulgarian) {
		t.Fatalf("catalog sizes: en=%d bg=%d", len(english), len(bulgarian))
	}
	placeholders := regexp.MustCompile(`%[ds]`)
	for key, text := range english {
		translated, ok := bulgarian[key]
		if !ok || strings.TrimSpace(text) == "" || strings.TrimSpace(translated) == "" {
			t.Errorf("missing or empty translation: %s", key)
			continue
		}
		if !reflect.DeepEqual(placeholders.FindAllString(text, -1), placeholders.FindAllString(translated, -1)) {
			t.Errorf("placeholder mismatch: %s", key)
		}
	}
	for _, locale := range []Locale{English, Bulgarian} {
		for _, tc := range []struct {
			key  string
			args []any
		}{
			{"startup.steps", []any{2, 4}},
			{"startup.steps_count", []any{2, 4}},
			{"label.configuration", []any{"/home/user/.conduit/config.yaml"}},
			{"diagnostic.unknown", []any{"future_code"}},
		} {
			formatted := fmt.Sprintf(T(locale, tc.key), tc.args...)
			if strings.Contains(formatted, "%!") {
				t.Errorf("invalid format %s/%s: %s", locale, tc.key, formatted)
			}
		}
	}
}

func TestNormalizeAndFallback(t *testing.T) {
	for _, tc := range []struct {
		language string
		want     Locale
	}{
		{"", English}, {"en", English}, {"EN-us", English}, {"fr", English},
		{"bg", Bulgarian}, {" BG-bg ", Bulgarian}, {"bg_BG", Bulgarian},
	} {
		if got := Normalize(tc.language); got != tc.want {
			t.Errorf("Normalize(%q)=%q want %q", tc.language, got, tc.want)
		}
	}
	if got := T(Locale("unsupported"), "action.cancel"); got != "Cancel" {
		t.Fatalf("English fallback=%q", got)
	}
	if got := T(Bulgarian, "action.cancel"); got != "Отказ" {
		t.Fatalf("Bulgarian=%q", got)
	}
	if got := T(Bulgarian, "missing.key"); got != "missing.key" {
		t.Fatalf("unknown key=%q", got)
	}
	if got := Catalog(Locale("unsupported")); !reflect.DeepEqual(got, Catalog(English)) {
		t.Fatal("catalog did not fall back to English")
	}
}

func TestCatalogCopiesCannotMutateTranslations(t *testing.T) {
	for _, locale := range []Locale{English, Bulgarian} {
		first := Catalog(locale)
		original := T(locale, "action.retry")
		first["action.retry"] = "changed"
		delete(first, "startup.title")
		first["injected.key"] = "injected"
		next := Catalog(locale)
		if next["action.retry"] != original || T(locale, "action.retry") != original {
			t.Fatal("catalog mutation changed shared translations")
		}
		if next["startup.title"] == "" || next["injected.key"] != "" {
			t.Fatal("catalog maps share mutation")
		}
	}
}

func TestStartupDiagnosticCoverage(t *testing.T) {
	for _, code := range []string{
		"home_unavailable", "configuration_invalid", "configuration_unreadable", "configuration_future_schema", "configuration_publish_failed",
		"harness_missing", "harness_override_missing", "harness_probe_failed", "harness_probe_timeout", "harness_version_unknown", "harness_probe_cleanup_unconfirmed",
		"final_configuration_invalid", "final_configuration_unreadable", "final_configuration_future_schema", "final_configuration_absent",
		"operation_id_failed", "startup_dependencies_missing", "configuration_publish_invalid_result", "configuration_creation_rejected",
	} {
		for _, locale := range []Locale{English, Bulgarian} {
			key := "diagnostic." + code
			if T(locale, key) == key {
				t.Errorf("missing diagnostic %s/%s", locale, code)
			}
		}
	}
}

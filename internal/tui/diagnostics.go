package tui

import (
	"fmt"
	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"strings"
)

func diagnosticText(locale i18n.Locale, d startup.Diagnostic) string {
	key := "diagnostic." + d.Code
	text := i18n.T(locale, key)
	if text == key {
		text = fmt.Sprintf(i18n.T(locale, "diagnostic.unknown"), safeText(d.Code))
	}
	if d.Harness != "" {
		text = string(d.Harness) + ": " + text
	}
	return text
}

// Terminal controls from externally supplied paths/codes must never become ANSI.
func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || (r >= 128 && r < 160) {
			return -1
		}
		return r
	}, text)
}

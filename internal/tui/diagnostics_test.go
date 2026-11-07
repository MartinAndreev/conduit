package tui

import (
	"strings"
	"testing"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
)

func TestMigrationDiagnosticsRender(t *testing.T) {
	unconfirmed := diagnosticText(i18n.English, startup.Diagnostic{Code: "configuration_migration_unconfirmed"})
	if !strings.Contains(unconfirmed, "replaced") || strings.Contains(unconfirmed, "unchanged") {
		t.Fatal(unconfirmed)
	}
	deferred := diagnosticText(i18n.Bulgarian, startup.Diagnostic{Code: "configuration_migration_deferred"})
	if !strings.Contains(deferred, "непроменен") {
		t.Fatal(deferred)
	}
}

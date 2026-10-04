package views

import (
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAssetsAndLocale(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.English, i18n.Bulgarian} {
		h := Handler(locale)
		page := httptest.NewRecorder()
		h.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
		if page.Code != 200 {
			t.Fatal(page.Code)
		}
		body := page.Body.String()
		if !strings.Contains(body, i18n.T(locale, "startup.title")) {
			t.Fatal("catalog unavailable")
		}
		for _, path := range []string{"/assets/style.css", "/assets/progress.js", "/assets/bridge.js"} {
			asset := httptest.NewRecorder()
			h.ServeHTTP(asset, httptest.NewRequest("GET", path, nil))
			if asset.Code != 200 || asset.Body.Len() == 0 {
				t.Fatalf("local asset %s missing", path)
			}
		}
		if strings.Contains(body, "https://") || strings.Contains(body, "http://") {
			t.Fatal("remote asset reference")
		}
	}
}

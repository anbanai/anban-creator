package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func setupResourceHandlerTest(t *testing.T) *fiber.App {
	t.Helper()
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewResourceHandler(&logger)
	app := fiber.New()
	app.Get("/api/v1/resources/themes/:name/preview", h.PreviewTheme)
	return app
}

// TestPreviewTheme_RendersHTML: a known theme renders to non-empty WeChat HTML
// via the deterministic renderer (no LLM). Confirms the preview endpoint is
// usable by the template editor's iframe.
func TestPreviewTheme_RendersHTML(t *testing.T) {
	app := setupResourceHandlerTest(t)

	req := httptest.NewRequest("GET", "/api/v1/resources/themes/autumn-warm/preview", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatalf("rendered HTML body is empty")
	}
	if !strings.Contains(string(body), `<body style="margin:0;">`) {
		t.Errorf("preview HTML should zero browser body margin, got: %s", string(body)[:min(160, len(body))])
	}
	if !strings.Contains(string(body), "<") {
		t.Errorf("rendered body does not look like HTML: %q", string(body)[:min(80, len(body))])
	}
}

// TestPreviewTheme_UnknownThemeReturns404.
func TestPreviewTheme_UnknownThemeReturns404(t *testing.T) {
	app := setupResourceHandlerTest(t)

	req := httptest.NewRequest("GET", "/api/v1/resources/themes/does-not-exist/preview", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown theme", resp.StatusCode)
	}
}

package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func TestBillingAdminReportsRequireAdminAuthAndExposeIntegerAccounting(t *testing.T) {
	f := newBillingHandlerFixture(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	identity, err := model.ProviderCostBaseIdentityKey(model.BillingProviderCostIdentityProviderRequest, "", "volcengine_ark", "seedance", "admin-report-cost")
	if err != nil {
		t.Fatal(err)
	}
	event := &model.BillingProviderCostEvent{
		ID: uuid.NewString(), EventKind: model.BillingProviderCostEventKindBase, IdentityKind: model.BillingProviderCostIdentityProviderRequest,
		ProviderRequestID: "admin-report-cost", Provider: "volcengine_ark", Model: "seedance", CatalogID: "cost-v1",
		IdempotencyScope: "admin-report-cost", IdempotencyKey: "admin-report-cost", BaseIdentityKey: &identity,
		RequestFingerprint: strings.Repeat("a", 64), Source: model.BillingProviderCostSourceProviderResponse,
		Status: model.BillingProviderCostStatusReconciled, CostMicroCNY: 123_000,
		UsageEvidence: datatypes.JSON(`{"kind":"video_output"}`), CalculationSnapshot: datatypes.JSON(`{"version":1}`), CreatedAt: now,
	}
	if err := f.db.Create(event).Error; err != nil {
		t.Fatal(err)
	}
	margin := service.NewMarginService(repository.NewBillingMarginRepository(f.db), f.repo, &f.bundle, service.MarginServiceOptions{})
	admin := NewBillingAdminHandler(margin)
	app := fiber.New()
	app.Get("/api/admin/billing/costs", f.handler.AdminAuth, admin.Costs)
	app.Get("/api/admin/billing/margins", f.handler.AdminAuth, admin.Margins)
	app.Get("/api/admin/billing/reconciliation", f.handler.AdminAuth, admin.Reconciliation)

	unauthorized, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/admin/billing/costs", nil))
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/billing/costs?group_by=provider", nil)
	req.Header.Set("X-Admin-API-Key", f.adminKey)
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	text := string(body)
	if response.StatusCode != http.StatusOK || !strings.Contains(text, `"provider_cost_micro_cny":123000`) || !strings.Contains(text, `"provider_cost_cny":"0.123000"`) {
		t.Fatalf("cost report status/body = %d %s", response.StatusCode, text)
	}
	if strings.Contains(text, `"provider_cost_micro_cny":"123000"`) {
		t.Fatalf("integer accounting field was encoded as text: %s", text)
	}

	badWindow := httptest.NewRequest(http.MethodGet, "/api/admin/billing/margins?from=bad", nil)
	badWindow.Header.Set("X-Admin-API-Key", f.adminKey)
	badResponse, err := app.Test(badWindow)
	if err != nil {
		t.Fatal(err)
	}
	if badResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid report window status = %d", badResponse.StatusCode)
	}

	reconcile := httptest.NewRequest(http.MethodGet, "/api/admin/billing/reconciliation", nil)
	reconcile.Header.Set("X-Admin-API-Key", f.adminKey)
	reconcileResponse, err := app.Test(reconcile)
	if err != nil {
		t.Fatal(err)
	}
	reconcileBody, _ := io.ReadAll(reconcileResponse.Body)
	if reconcileResponse.StatusCode != http.StatusOK || !strings.Contains(string(reconcileBody), `"missing_facts":0`) {
		t.Fatalf("reconciliation status/body = %d %s", reconcileResponse.StatusCode, string(reconcileBody))
	}
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func TestBillingAdminTopUpUsesLoggedInAdministratorAndRecordsReplay(t *testing.T) {
	f := newBillingHandlerFixture(t)
	admin, err := f.repo.Users().FindByID(context.Background(), billingHandlerInviterID)
	if err != nil {
		t.Fatal(err)
	}
	admin.IsAdmin = true
	if err := f.repo.Users().Update(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: f.inviteeID}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: billingHandlerInviterID}); err != nil {
		t.Fatal(err)
	}

	adminHandler := NewBillingAdminHandler(nil, BillingAdminHandlerOptions{Repo: f.repo, Referrals: service.NewBillingReferralService(f.repo, service.NewBillingWalletService(f.repo, &f.bundle, service.BillingWalletOptions{}), &f.bundle, service.BillingReferralOptions{}), CatalogID: f.bundle.Products.CatalogID})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", billingHandlerInviterID); return c.Next() })
	app.Post("/api/v1/admin/billing/topups", adminHandler.TopUp)

	body := `{"user_id":"` + f.inviteeID + `","credits":10000,"external_source_id":"cashier-20261012-001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/topups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		t.Fatalf("topup status = %d, want 200: %#v", resp.StatusCode, body)
	}
	var first map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	if first["data"].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("first result = %#v", first)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/topups", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d, want 200", resp.StatusCode)
	}
	var replay map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if replay["data"].(map[string]any)["status"] != "replayed" {
		t.Fatalf("replay result = %#v", replay)
	}

	operations, err := f.repo.Billing().ListTopUpOperations(context.Background(), repository.BillingTopUpOperationFilter{ExternalSourceID: "cashier-20261012-001"}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 2 {
		t.Fatalf("operations = %#v", operations)
	}
	if operations[0].Status != model.BillingTopUpOperationStatusReplayed && operations[1].Status != model.BillingTopUpOperationStatusReplayed {
		t.Fatalf("replay operation = %#v", operations)
	}
}

func TestBillingAdminInvalidCreditsAreRecordedAsFailed(t *testing.T) {
	f := newBillingHandlerFixture(t)
	admin, err := f.repo.Users().FindByID(context.Background(), billingHandlerInviterID)
	if err != nil {
		t.Fatal(err)
	}
	admin.IsAdmin = true
	if err := f.repo.Users().Update(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	adminHandler := NewBillingAdminHandler(nil, BillingAdminHandlerOptions{Repo: f.repo, CatalogID: f.bundle.Products.CatalogID})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", billingHandlerInviterID); return c.Next() })
	app.Post("/topups", adminHandler.TopUp)

	req := httptest.NewRequest(http.MethodPost, "/topups", strings.NewReader(`{"user_id":"`+f.inviteeID+`","credits":0,"external_source_id":"invalid-credits-001"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid credits status = %d, want 400", resp.StatusCode)
	}
	operations, err := f.repo.Billing().ListTopUpOperations(context.Background(), repository.BillingTopUpOperationFilter{ExternalSourceID: "invalid-credits-001"}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 1 || operations[0].Status != model.BillingTopUpOperationStatusFailed || operations[0].Credits != 0 {
		t.Fatalf("invalid credits operation = %#v", operations)
	}
}

func TestBillingAdminEndpointsRejectNonAdministrators(t *testing.T) {
	f := newBillingHandlerFixture(t)
	adminHandler := NewBillingAdminHandler(nil, BillingAdminHandlerOptions{Repo: f.repo, CatalogID: f.bundle.Products.CatalogID})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", f.inviteeID); return c.Next() })
	app.Get("/users", adminHandler.Users)
	app.Post("/topups", adminHandler.TopUp)
	app.Get("/topups", adminHandler.TopUpOperations)
	app.Get("/entries", adminHandler.Entries)

	for _, path := range []string{"/users?q=test", "/topups", "/entries"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", path, resp.StatusCode)
		}
	}
	post := httptest.NewRequest(http.MethodPost, "/topups", strings.NewReader(`{"user_id":"`+f.inviteeID+`","credits":1,"external_source_id":"non-admin-001"}`))
	post.Header.Set("Content-Type", "application/json")
	response, err := app.Test(post)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /topups status = %d, want 403", response.StatusCode)
	}
}

func TestBillingAdminTopUpConflictCreatesFailedAuditRow(t *testing.T) {
	f := newBillingHandlerFixture(t)
	admin := mustBillingAdmin(t, f)
	if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: f.inviteeID}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: admin.ID}); err != nil {
		t.Fatal(err)
	}
	h := newAdminBillingTopUpHandler(f, f.bundle.Products.CatalogID)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", admin.ID); return c.Next() })
	app.Post("/topups", h.TopUp)
	post := func(body string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/topups", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	first := post(`{"user_id":"` + f.inviteeID + `","credits":10000,"external_source_id":"cashier-conflict-001"}`)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", first.StatusCode)
	}
	conflict := post(`{"user_id":"` + f.inviteeID + `","credits":11000,"external_source_id":"cashier-conflict-001"}`)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", conflict.StatusCode)
	}
	rows, err := f.repo.Billing().ListTopUpOperations(context.Background(), repository.BillingTopUpOperationFilter{ExternalSourceID: "cashier-conflict-001"}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	if rows[0].Status != model.BillingTopUpOperationStatusFailed && rows[1].Status != model.BillingTopUpOperationStatusFailed {
		t.Fatalf("conflict audit rows = %#v", rows)
	}
}

func TestBillingAdminTopUpFailuresRemainAuditable(t *testing.T) {
	tests := []struct {
		name       string
		catalogID  string
		userID     string
		credits    int64
		externalID string
		prepare    func(*testing.T, *billingHandlerFixture)
		wantCode   string
	}{
		{name: "missing user", catalogID: "retail-handler-v1", userID: "00000000-0000-0000-0000-000000000099", credits: 10000, externalID: "missing-user-001", wantCode: "billing_user_not_found"},
		{name: "missing catalog", catalogID: "missing-catalog", userID: billingHandlerInviteeID, credits: 10000, externalID: "missing-catalog-001", wantCode: "billing_catalog_not_found", prepare: func(t *testing.T, f *billingHandlerFixture) {
			t.Helper()
			if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: f.inviteeID}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ledger anomaly", catalogID: "retail-handler-v1", userID: billingHandlerInviteeID, credits: 10000, externalID: "ledger-anomaly-001", wantCode: "billing_ledger_invalid", prepare: func(t *testing.T, f *billingHandlerFixture) {
			t.Helper()
			if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: f.inviteeID, DebtCredits: 1}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBillingHandlerFixture(t)
			admin := mustBillingAdmin(t, f)
			if tt.prepare != nil {
				tt.prepare(t, f)
			}
			h := newAdminBillingTopUpHandler(f, tt.catalogID)
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error { c.Locals("user_id", admin.ID); return c.Next() })
			app.Post("/topups", h.TopUp)
			body := `{"user_id":"` + tt.userID + `","credits":` + fmt.Sprint(tt.credits) + `,"external_source_id":"` + tt.externalID + `"}`
			req := httptest.NewRequest(http.MethodPost, "/topups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("status = 200, want failure")
			}
			rows, err := f.repo.Billing().ListTopUpOperations(context.Background(), repository.BillingTopUpOperationFilter{ExternalSourceID: tt.externalID}, 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Status != model.BillingTopUpOperationStatusFailed || rows[0].ErrorCode != tt.wantCode {
				t.Fatalf("audit rows = %#v, want failed %s", rows, tt.wantCode)
			}
		})
	}
}

func TestBillingAdminRecordQueriesFilterAndPaginate(t *testing.T) {
	f := newBillingHandlerFixture(t)
	admin := mustBillingAdmin(t, f)
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	rows := []model.BillingTopUpOperation{
		{ID: "op-filter-1", UserID: f.inviteeID, AdminUserID: admin.ID, Credits: 100, ExternalSourceType: "manual_api", ExternalSourceID: "cashier-search-a", CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("a", 64), IdempotencyScope: "admin-topup", IdempotencyKey: "filter-a", Status: model.BillingTopUpOperationStatusFailed, CreatedAt: start.Add(time.Minute), UpdatedAt: start.Add(time.Minute)},
		{ID: "op-filter-2", UserID: f.inviteeID, AdminUserID: admin.ID, Credits: 200, ExternalSourceType: "manual_api", ExternalSourceID: "cashier-search-b", CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("b", 64), IdempotencyScope: "admin-topup", IdempotencyKey: "filter-b", Status: model.BillingTopUpOperationStatusFailed, CreatedAt: start.Add(2 * time.Minute), UpdatedAt: start.Add(2 * time.Minute)},
		{ID: "op-filter-3", UserID: f.inviteeID, AdminUserID: admin.ID, Credits: 300, ExternalSourceType: "manual_api", ExternalSourceID: "cashier-search-c", CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("c", 64), IdempotencyScope: "admin-topup", IdempotencyKey: "filter-c", Status: model.BillingTopUpOperationStatusSucceeded, CreatedAt: start.Add(3 * time.Minute), UpdatedAt: start.Add(3 * time.Minute)},
		{ID: "op-filter-4", UserID: admin.ID, AdminUserID: admin.ID, Credits: 400, ExternalSourceType: "manual_api", ExternalSourceID: "cashier-search-d", CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("d", 64), IdempotencyScope: "admin-topup", IdempotencyKey: "filter-d", Status: model.BillingTopUpOperationStatusFailed, CreatedAt: start.Add(4 * time.Minute), UpdatedAt: start.Add(4 * time.Minute)},
	}
	for i := range rows {
		if err := f.repo.Billing().CreateTopUpOperation(context.Background(), &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []model.BillingWalletEntry{
		{ID: "entry-filter-topup", UserID: f.inviteeID, EventKind: model.BillingWalletEventKindTopUp, CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("e", 64), SourceType: billingStringPtr("manual_api"), SourceID: billingStringPtr("entry-filter-source"), IdempotencyScope: "entry-filter", IdempotencyKey: "topup", CreatedAt: start},
		{ID: "entry-filter-charge", UserID: f.inviteeID, EventKind: model.BillingWalletEventKindCharge, CreatedAt: start.Add(time.Minute)},
		{ID: "entry-filter-other-user", UserID: admin.ID, EventKind: model.BillingWalletEventKindTopUp, CatalogID: f.bundle.Products.CatalogID, RequestFingerprint: strings.Repeat("f", 64), SourceType: billingStringPtr("manual_api"), SourceID: billingStringPtr("entry-filter-other"), IdempotencyScope: "entry-filter", IdempotencyKey: "other-topup", CreatedAt: start.Add(2 * time.Minute)},
	} {
		if err := f.db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
	}

	h := NewBillingAdminHandler(nil, BillingAdminHandlerOptions{Repo: f.repo, CatalogID: f.bundle.Products.CatalogID})
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", admin.ID); return c.Next() })
	app.Get("/topups", h.TopUpOperations)
	app.Get("/entries", h.Entries)
	query := "/topups?user_id=" + f.inviteeID + "&status=failed&external_source_id=cashier-search&from=" + start.Format(time.RFC3339) + "&to=" + start.Add(3*time.Minute).Format(time.RFC3339) + "&offset=1&limit=1"
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, query, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("topups status = %d", resp.StatusCode)
	}
	var topUps map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&topUps); err != nil {
		t.Fatal(err)
	}
	data := topUps["data"].(map[string]any)
	items := data["items"].([]any)
	if len(items) != 1 || data["total"] != float64(2) || items[0].(map[string]any)["id"] != "op-filter-1" {
		t.Fatalf("filtered topups = %#v", data)
	}
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/entries?user_id="+f.inviteeID+"&event_kind=topup&limit=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entries status = %d", resp.StatusCode)
	}
	var entries map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	entryData := entries["data"].(map[string]any)
	entryItems := entryData["items"].([]any)
	if len(entryItems) != 1 || entryData["total"] != float64(1) || entryItems[0].(map[string]any)["id"] != "entry-filter-topup" {
		t.Fatalf("filtered entries = %#v", entryData)
	}
}

func mustBillingAdmin(t *testing.T, f *billingHandlerFixture) *model.User {
	t.Helper()
	admin, err := f.repo.Users().FindByID(context.Background(), billingHandlerInviterID)
	if err != nil {
		t.Fatal(err)
	}
	admin.IsAdmin = true
	if err := f.repo.Users().Update(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	return admin
}

func newAdminBillingTopUpHandler(f *billingHandlerFixture, catalogID string) *BillingAdminHandler {
	wallet := service.NewBillingWalletService(f.repo, &f.bundle, service.BillingWalletOptions{})
	referrals := service.NewBillingReferralService(f.repo, wallet, &f.bundle, service.BillingReferralOptions{})
	return NewBillingAdminHandler(nil, BillingAdminHandlerOptions{Repo: f.repo, Referrals: referrals, CatalogID: catalogID})
}

func billingStringPtr(value string) *string { return &value }

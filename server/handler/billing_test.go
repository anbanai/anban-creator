package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	billingHandlerInviterID = "30000000-0000-4000-8000-000000000001"
	billingHandlerInviteeID = "30000000-0000-4000-8000-000000000002"
)

func TestBillingHandler(t *testing.T) {
	t.Run("admin auth accepts exactly one canonical credential", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		tests := []struct {
			name    string
			headers map[string]string
			status  int
		}{
			{name: "admin header", headers: map[string]string{"X-Admin-API-Key": f.adminKey}, status: http.StatusNoContent},
			{name: "bearer", headers: map[string]string{"Authorization": "Bearer " + f.adminKey}, status: http.StatusNoContent},
			{name: "raw authorization", headers: map[string]string{"Authorization": f.adminKey}, status: http.StatusUnauthorized},
			{name: "basic authorization", headers: map[string]string{"Authorization": "Basic " + f.adminKey}, status: http.StatusUnauthorized},
			{name: "empty bearer", headers: map[string]string{"Authorization": "Bearer"}, status: http.StatusUnauthorized},
			{name: "double spaced bearer", headers: map[string]string{"Authorization": "Bearer  " + f.adminKey}, status: http.StatusUnauthorized},
			{name: "both credentials", headers: map[string]string{"X-Admin-API-Key": f.adminKey, "Authorization": "Bearer " + f.adminKey}, status: http.StatusUnauthorized},
			{name: "wrong admin header", headers: map[string]string{"X-Admin-API-Key": "wrong"}, status: http.StatusUnauthorized},
			{name: "wrong bearer", headers: map[string]string{"Authorization": "Bearer wrong"}, status: http.StatusUnauthorized},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				app := fiber.New()
				app.Get("/", f.handler.AdminAuth, func(c fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				for name, value := range tt.headers {
					req.Header.Set(name, value)
				}
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != tt.status {
					t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
				}
			})
		}
	})

	t.Run("service errors keep exact public identities", func(t *testing.T) {
		tests := []struct {
			name   string
			err    error
			status int
			code   int
			msg    string
		}{
			{name: "SKU not found", err: service.ErrBillingSKUNotFound, status: http.StatusNotFound, code: 40401, msg: "billing_sku_not_found"},
			{name: "catalog not found", err: service.ErrBillingCatalogNotFound, status: http.StatusNotFound, code: 40402, msg: "billing_catalog_not_found"},
			{name: "user not found", err: service.ErrBillingUserNotFound, status: http.StatusNotFound, code: 40403, msg: "billing_user_not_found"},
			{name: "charge conflict", err: service.ErrBillingConflict, status: http.StatusConflict, code: 40901, msg: "billing_charge_conflict"},
			{name: "quote expired", err: service.ErrBillingQuoteExpired, status: http.StatusGone, code: 41001, msg: "billing_quote_expired"},
			{name: "debt outstanding", err: service.ErrBillingDebtOutstanding, status: http.StatusPaymentRequired, code: 40201, msg: "billing_debt_outstanding"},
			{name: "task insufficient", err: service.ErrBillingInsufficientForTask, status: http.StatusPaymentRequired, code: 40202, msg: "billing_insufficient_for_task"},
			{name: "standalone insufficient", err: service.ErrBillingInsufficientForStandaloneOperation, status: http.StatusPaymentRequired, code: 40203, msg: "billing_insufficient_for_standalone_operation"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				app := fiber.New()
				app.Get("/", func(c fiber.Ctx) error { return writeBillingServiceError(c, tt.err) })
				resp := billingTestRequest(t, app, http.MethodGet, "/", nil, "")
				assertBillingHTTPMessage(t, resp, tt.status, tt.code, tt.msg)
			})
		}
	})

	t.Run("wallet absent is zero and read only", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		resp := f.publicRequest(t, http.MethodGet, "/api/billing/wallet", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		data := billingResponseData(t, resp)
		assertJSONNumbers(t, data, map[string]float64{"paid": 0, "promotional": 0, "debt": 0, "balance": 0})
		if _, err := f.repo.Billing().FindAccount(context.Background(), f.inviteeID); !errorsIsRecordNotFound(err) {
			t.Fatalf("wallet read created account or returned unexpected error: %v", err)
		}
	})

	t.Run("wallet distinct buckets and invalid projection", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{
			UserID: f.inviteeID, PaidCredits: 2_000, PromotionalCredits: 300, DebtCredits: 500,
		}); err != nil {
			t.Fatal(err)
		}
		resp := f.publicRequest(t, http.MethodGet, "/api/billing/wallet", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		assertJSONNumbers(t, billingResponseData(t, resp), map[string]float64{"paid": 2_000, "promotional": 300, "debt": 500, "balance": 1_800})

		if err := f.db.Exec("PRAGMA ignore_check_constraints = ON").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Model(&model.BillingWalletAccount{}).Where("user_id = ?", f.inviteeID).
			Updates(map[string]any{"paid_credits": int64(^uint64(0) >> 1), "promotional_credits": int64(1)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Exec("PRAGMA ignore_check_constraints = OFF").Error; err != nil {
			t.Fatal(err)
		}
		resp = f.publicRequest(t, http.MethodGet, "/api/billing/wallet", nil)
		assertBillingHTTP(t, resp, http.StatusInternalServerError, BillingCodeLedgerInvalid)
	})

	t.Run("quote is fixed and errors are typed", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		body := map[string]any{
			"operation": "task.article", "request_fingerprint": strings.Repeat("a", 64),
			"idempotency_scope": "quote", "idempotency_key": "quote-1",
		}
		resp := f.publicRequest(t, http.MethodPost, "/api/billing/quotes", body)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		data := billingResponseData(t, resp)
		assertJSONNumbers(t, data, map[string]float64{"price_credits": 500})
		for _, field := range []string{"id", "catalog_id", "sku_id", "sku_snapshot", "expires_at"} {
			if _, ok := data[field]; !ok {
				t.Fatalf("quote response missing %s: %#v", field, data)
			}
		}
		resp = f.publicRequest(t, http.MethodPost, "/api/billing/quotes", map[string]any{"operation": "task.article"})
		assertBillingHTTP(t, resp, http.StatusBadRequest, BillingCodeInvalid)
		resp = f.publicRequest(t, http.MethodPost, "/api/billing/quotes", map[string]any{
			"operation": "missing", "request_fingerprint": strings.Repeat("b", 64), "idempotency_scope": "quote", "idempotency_key": "missing",
		})
		assertBillingHTTP(t, resp, http.StatusNotFound, BillingCodeSKUNotFound)
		body["operation"] = "mcp.generate_image"
		resp = f.publicRequest(t, http.MethodPost, "/api/billing/quotes", body)
		assertBillingHTTP(t, resp, http.StatusConflict, BillingCodeChargeConflict)
	})

	t.Run("admin authentication topup replay transactions and referral summary", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		for _, userID := range []string{billingHandlerInviterID, billingHandlerInviteeID} {
			if err := f.repo.Billing().CreateAccount(context.Background(), &model.BillingWalletAccount{UserID: userID}); err != nil {
				t.Fatal(err)
			}
		}
		body := f.topUpBody("payment-1", 10_000)
		resp := f.adminRequest(t, "", body)
		assertBillingHTTP(t, resp, http.StatusUnauthorized, BillingCodeUnauthorized)
		resp = f.adminRequest(t, "wrong", body)
		assertBillingHTTP(t, resp, http.StatusUnauthorized, BillingCodeUnauthorized)
		resp = f.adminRequest(t, f.adminKey, body)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		first := billingResponseData(t, resp)
		resp = f.adminRequest(t, f.adminKey, body)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		second := billingResponseData(t, resp)
		if first["entry_id"] != second["entry_id"] || first["referral_issue_id"] != second["referral_issue_id"] {
			t.Fatalf("topup replay drifted: first=%#v second=%#v", first, second)
		}
		body["credits"] = float64(10_001)
		resp = f.adminRequest(t, f.adminKey, body)
		assertBillingHTTP(t, resp, http.StatusConflict, BillingCodeChargeConflict)

		resp = f.publicRequest(t, http.MethodGet, "/api/billing/referral", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		referral := billingResponseData(t, resp)
		if referral["invite_code"] != "INVITEE" || !strings.Contains(referral["invite_link"].(string), "INVITEE") {
			t.Fatalf("referral link response = %#v", referral)
		}
		program := referral["program"].(map[string]any)
		if program["id"] != "referral-handler-v1" || referral["status"] != "issued" {
			t.Fatalf("referral summary = %#v", referral)
		}

		resp = f.publicRequest(t, http.MethodGet, "/api/billing/transactions?offset=0&limit=1", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		page1 := billingResponseData(t, resp)
		items1 := page1["items"].([]any)
		if len(items1) != 1 || page1["limit"] != float64(1) || page1["total"].(float64) != 2 {
			t.Fatalf("transaction page 1 = %#v", page1)
		}
		resp = f.publicRequest(t, http.MethodGet, "/api/billing/transactions?offset=1&limit=1", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		items2 := billingResponseData(t, resp)["items"].([]any)
		if items1[0].(map[string]any)["id"] == items2[0].(map[string]any)["id"] {
			t.Fatalf("stable pages repeated entry: %#v %#v", items1, items2)
		}
		for _, query := range []string{"?offset=-1&limit=20", "?offset=0&limit=0", "?offset=0&limit=101"} {
			resp = f.publicRequest(t, http.MethodGet, "/api/billing/transactions"+query, nil)
			assertBillingHTTP(t, resp, http.StatusBadRequest, BillingCodeInvalid)
		}
	})

	t.Run("admin topup requires every canonical identity field", func(t *testing.T) {
		for _, field := range []string{"external_source_type", "external_source_id", "catalog_id", "request_fingerprint", "idempotency_scope", "idempotency_key"} {
			t.Run(field, func(t *testing.T) {
				f := newBillingHandlerFixture(t)
				body := f.topUpBody("missing-"+field, 10_000)
				delete(body, field)
				resp := f.adminRequest(t, f.adminKey, body)
				assertBillingHTTPMessage(t, resp, http.StatusBadRequest, BillingCodeInvalid, "billing_invalid")
			})
		}
	})

	t.Run("admin topup rejects invalid provenance before wallet mutation", func(t *testing.T) {
		const missingUserID = "30000000-0000-4000-8000-000000000099"
		tests := []struct {
			name    string
			prepare func(*testing.T, *billingHandlerFixture, map[string]any)
			status  int
			code    int
			msg     string
		}{
			{
				name: "missing user", status: http.StatusNotFound, code: BillingCodeUserNotFound, msg: "billing_user_not_found",
				prepare: func(_ *testing.T, _ *billingHandlerFixture, body map[string]any) { body["user_id"] = missingUserID },
			},
			{
				name: "missing catalog", status: http.StatusNotFound, code: BillingCodeCatalogNotFound, msg: "billing_catalog_not_found",
				prepare: func(_ *testing.T, _ *billingHandlerFixture, body map[string]any) {
					body["catalog_id"] = "retail-missing-v1"
				},
			},
			{
				name: "unpublished catalog", status: http.StatusNotFound, code: BillingCodeCatalogNotFound, msg: "billing_catalog_not_found",
				prepare: func(t *testing.T, f *billingHandlerFixture, body map[string]any) {
					body["catalog_id"] = "retail-draft-v1"
					if err := f.repo.Billing().CreateCatalogVersion(context.Background(), &model.BillingCatalogVersion{
						CatalogID: "retail-draft-v1", Currency: "credits", Status: "draft", PublishedAt: time.Now().UTC(), Snapshot: []byte(`{}`), CreatedAt: time.Now().UTC(),
					}); err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "invalid user UUID", status: http.StatusBadRequest, code: BillingCodeInvalid, msg: "billing_invalid",
				prepare: func(_ *testing.T, _ *billingHandlerFixture, body map[string]any) { body["user_id"] = "not-a-uuid" },
			},
			{
				name: "overlong source type", status: http.StatusBadRequest, code: BillingCodeInvalid, msg: "billing_invalid",
				prepare: func(_ *testing.T, _ *billingHandlerFixture, body map[string]any) {
					body["external_source_type"] = strings.Repeat("s", 41)
				},
			},
			{
				name: "overlong request ID", status: http.StatusBadRequest, code: BillingCodeInvalid, msg: "billing_invalid",
				prepare: func(_ *testing.T, _ *billingHandlerFixture, body map[string]any) {
					body["request_id"] = strings.Repeat("r", 129)
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newBillingHandlerFixture(t)
				body := f.topUpBody("rejected-"+tt.name, 10_000)
				tt.prepare(t, f, body)
				resp := f.adminRequest(t, f.adminKey, body)
				assertBillingHTTPMessage(t, resp, tt.status, tt.code, tt.msg)

				for _, userID := range []string{f.inviteeID, missingUserID} {
					if _, err := f.repo.Billing().FindAccount(context.Background(), userID); !errorsIsRecordNotFound(err) {
						t.Fatalf("rejected topup created wallet for %s: %v", userID, err)
					}
					entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), userID, 0, 10)
					if err != nil || len(entries) != 0 {
						t.Fatalf("rejected topup entries for %s = %+v, %v", userID, entries, err)
					}
				}
			})
		}
	})

	t.Run("capped referral summary is read only", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		if err := f.repo.Billing().CreateReferralIssue(context.Background(), &model.BillingReferralIssue{
			ID: uuid.NewString(), ProgramID: "referral-handler-v1", CatalogID: "promotion-handler-v1",
			InviteeUserID: f.inviteeID, InviterUserID: billingHandlerInviterID, QualifyingTopUpEntryID: uuid.NewString(),
			RequestFingerprint: strings.Repeat("e", 64), Status: model.BillingReferralStatusCapped,
		}); err != nil {
			t.Fatal(err)
		}
		resp := f.publicRequest(t, http.MethodGet, "/api/billing/referral", nil)
		assertBillingHTTP(t, resp, http.StatusOK, 0)
		if got := billingResponseData(t, resp)["status"]; got != "capped" {
			t.Fatalf("referral status = %#v, want capped", got)
		}
		if _, err := f.repo.Billing().FindAccount(context.Background(), f.inviteeID); !errorsIsRecordNotFound(err) {
			t.Fatalf("read-only referral created wallet: %v", err)
		}
		entries, err := f.repo.Billing().ListEntriesByUser(context.Background(), f.inviteeID, 0, 10)
		if err != nil || len(entries) != 0 {
			t.Fatalf("read-only referral entries = %+v, %v", entries, err)
		}
	})

	t.Run("public DTOs contain no internal cost vocabulary", func(t *testing.T) {
		f := newBillingHandlerFixture(t)
		for _, endpoint := range []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/billing/wallet", nil},
			{http.MethodGet, "/api/billing/transactions?offset=0&limit=20", nil},
			{http.MethodGet, "/api/billing/referral", nil},
			{http.MethodPost, "/api/billing/quotes", map[string]any{"operation": "task.article", "request_fingerprint": strings.Repeat("c", 64), "idempotency_scope": "quote", "idempotency_key": "safe-dto"}},
		} {
			resp := f.publicRequest(t, endpoint.method, endpoint.path, endpoint.body)
			assertBillingHTTP(t, resp, http.StatusOK, 0)
			encoded := strings.ToLower(string(readResponseBody(t, resp)))
			for _, forbidden := range []string{"provider_cost", "token", "model", "budget", "multiplier", "bonus", "total_cost_usd"} {
				if strings.Contains(encoded, forbidden) {
					t.Fatalf("%s %s leaked %q: %s", endpoint.method, endpoint.path, forbidden, encoded)
				}
			}
		}
		for _, dto := range []any{BillingWalletResponse{}, BillingTransactionResponse{}, BillingQuoteResponse{}} {
			typeOf := reflect.TypeOf(dto)
			for index := 0; index < typeOf.NumField(); index++ {
				field := typeOf.Field(index)
				name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
				for _, forbidden := range []string{"provider", "cost", "token", "model", "budget", "multiplier", "bonus"} {
					if strings.Contains(name, forbidden) {
						t.Fatalf("public DTO %s field %s contains forbidden %q", typeOf.Name(), field.Name, forbidden)
					}
				}
			}
		}
	})
}

type billingHandlerFixture struct {
	repo      repository.Repository
	db        *gorm.DB
	handler   *BillingHandler
	inviteeID string
	adminKey  string
	bundle    serverbilling.Bundle
}

func newBillingHandlerFixture(t *testing.T) *billingHandlerFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_busy_timeout=10000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	bundle := billingHandlerBundle()
	catalog := service.NewBillingCatalogService(repo, &bundle, service.BillingCatalogOptions{Now: func() time.Time { return now }, QuoteTTL: 5 * time.Minute})
	if _, err := catalog.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	wallet := service.NewBillingWalletService(repo, &bundle, service.BillingWalletOptions{Now: func() time.Time { return now }})
	referrals := service.NewBillingReferralService(repo, wallet, &bundle, service.BillingReferralOptions{Now: func() time.Time { return now }})
	for _, user := range []model.User{
		{ID: billingHandlerInviterID, Email: "inviter@billing.test", Password: "x", InviteCode: "INVITER"},
		{ID: billingHandlerInviteeID, Email: "invitee@billing.test", Password: "x", InviteCode: "INVITEE", InvitedBy: billingHandlerInviterID},
	} {
		user := user
		if err := repo.Users().Create(context.Background(), &user); err != nil {
			t.Fatal(err)
		}
	}
	logger := zerolog.New(io.Discard)
	const adminKey = "billing-handler-admin-secret"
	return &billingHandlerFixture{
		repo: repo, db: db, inviteeID: billingHandlerInviteeID, adminKey: adminKey, bundle: bundle,
		handler: NewBillingHandler(repo, catalog, referrals, &bundle, BillingHandlerOptions{
			AdminAPIKey: adminKey, InviteBaseURL: "https://creator.anbanai.com/register?invite=",
		}, &logger),
	}
}

func billingHandlerBundle() serverbilling.Bundle {
	return serverbilling.Bundle{
		Policy: serverbilling.PolicyCatalog{
			Version: "2026-07-19", CreditsPerCNY: 1_000,
			TaskAdmission: serverbilling.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  serverbilling.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         serverbilling.TopUpPolicy{RepayDebtFirst: true}, Promotions: serverbilling.PromotionsPolicy{MayRepayDebt: false},
		},
		Products: serverbilling.ProductCatalog{CatalogID: "retail-handler-v1", Currency: "credits", SKUs: []serverbilling.SKUConfig{
			{ID: "task.article.v1", Operation: "task.article", ChargePolicy: "task_admission", PriceCredits: 500, Delivery: "article"},
			{ID: "image.cover.v1", Operation: "mcp.generate_image", Route: "image.cover", ChargePolicy: "accepted_task_operation", PriceCredits: 100, Delivery: "image"},
		}},
		Promotions: serverbilling.PromotionCatalog{CatalogID: "promotion-handler-v1", Programs: []serverbilling.ReferralProgram{{
			ID: "referral-handler-v1", Trigger: "invitee_first_paid_topup", MinimumTopUpCNY: 10_000_000,
			InviterCredits: 1_000, InviteeCredits: 1_000, ExpiresAfter: 30 * 24 * time.Hour, MaxInviterRewards: 10,
		}}},
	}
}

func (f *billingHandlerFixture) publicRequest(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", f.inviteeID); return c.Next() })
	app.Get("/api/billing/wallet", f.handler.Wallet)
	app.Get("/api/billing/transactions", f.handler.Transactions)
	app.Post("/api/billing/quotes", f.handler.CreateQuote)
	app.Get("/api/billing/referral", f.handler.Referral)
	return billingTestRequest(t, app, method, path, body, "")
}

func (f *billingHandlerFixture) adminRequest(t *testing.T, key string, body any) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Post("/api/admin/billing/topups", f.handler.AdminAuth, f.handler.AdminTopUp)
	return billingTestRequest(t, app, http.MethodPost, "/api/admin/billing/topups", body, key)
}

func (f *billingHandlerFixture) topUpBody(sourceID string, credits int64) map[string]any {
	return map[string]any{
		"user_id": f.inviteeID, "credits": credits, "external_source_type": "manual_api", "external_source_id": sourceID,
		"catalog_id": f.bundle.Products.CatalogID, "request_fingerprint": strings.Repeat("d", 64),
		"idempotency_scope": "admin-topup", "idempotency_key": sourceID,
	}
}

func billingTestRequest(t *testing.T, app *fiber.App, method, path string, body any, adminKey string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if adminKey != "" {
		req.Header.Set("X-Admin-API-Key", adminKey)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func assertBillingHTTP(t *testing.T, resp *http.Response, status, code int) {
	t.Helper()
	if resp.StatusCode != status {
		body := readResponseBody(t, resp)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, body)
	}
	var envelope map[string]any
	if err := json.Unmarshal(readResponseBody(t, resp), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["code"] != float64(code) {
		t.Fatalf("code = %#v, want %d; response=%#v", envelope["code"], code, envelope)
	}
	resp.Body = io.NopCloser(bytes.NewReader(mustJSON(t, envelope)))
}

func assertBillingHTTPMessage(t *testing.T, resp *http.Response, status, code int, msg string) {
	t.Helper()
	if resp.StatusCode != status {
		body := readResponseBody(t, resp)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, body)
	}
	var envelope map[string]any
	if err := json.Unmarshal(readResponseBody(t, resp), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["code"] != float64(code) || envelope["msg"] != msg {
		t.Fatalf("response = %#v, want code=%d msg=%q", envelope, code, msg)
	}
}

func billingResponseData(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(readResponseBody(t, resp), &envelope); err != nil {
		t.Fatal(err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %#v", envelope["data"])
	}
	return data
}

func readResponseBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func assertJSONNumbers(t *testing.T, data map[string]any, want map[string]float64) {
	t.Helper()
	for key, value := range want {
		if data[key] != value {
			t.Fatalf("%s = %#v, want %v; data=%#v", key, data[key], value, data)
		}
	}
}

func errorsIsRecordNotFound(err error) bool { return err == gorm.ErrRecordNotFound }

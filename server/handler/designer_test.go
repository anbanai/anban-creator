package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type designerPendingUploadRepo struct {
	uploads map[string]*model.PendingUpload
}

func (r *designerPendingUploadRepo) CreatePendingUpload(_ context.Context, upload *model.PendingUpload) error {
	copy := *upload
	r.uploads[upload.ID] = &copy
	return nil
}

func (r *designerPendingUploadRepo) FindPendingUploadByID(_ context.Context, id string) (*model.PendingUpload, error) {
	upload := r.uploads[id]
	if upload == nil {
		return nil, service.ErrPendingUploadNotFound
	}
	copy := *upload
	return &copy, nil
}

func (r *designerPendingUploadRepo) FinalizePendingUploadClaims(_ context.Context, claims []model.PendingUploadClaim, finalizedAt time.Time) error {
	for _, claim := range claims {
		upload := r.uploads[claim.UploadID]
		allowed := false
		for _, purpose := range claim.AllowedPurposes {
			allowed = allowed || upload != nil && upload.Purpose == purpose
		}
		if upload == nil || upload.UserID != claim.UserID || upload.Key != claim.Key || claim.FinalizedKey == "" || !allowed {
			return model.ErrPendingUploadClaimRejected
		}
		if upload.Status == model.PendingUploadStatusFinalized {
			if upload.FinalizedKey != claim.FinalizedKey {
				return model.ErrPendingUploadClaimRejected
			}
			continue
		}
		if upload.Status != model.PendingUploadStatusPending || !upload.ExpiresAt.After(finalizedAt) {
			return model.ErrPendingUploadClaimRejected
		}
		upload.Status = model.PendingUploadStatusFinalized
		upload.FinalizedKey = claim.FinalizedKey
		upload.FinalizedAt = &finalizedAt
	}
	return nil
}

func (*designerPendingUploadRepo) FindPendingUploadsForCleanup(context.Context, time.Time, time.Time, int) ([]*model.PendingUpload, error) {
	return nil, nil
}
func (*designerPendingUploadRepo) ClaimPendingUploadExpiration(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (*designerPendingUploadRepo) CompletePendingUploadExpiration(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (*designerPendingUploadRepo) ReopenPendingUploadExpiration(context.Context, string, string) (bool, error) {
	return false, nil
}

func setupDesignerHandlerTest(t *testing.T) (*fiber.App, *DesignerHandler, *gorm.DB) {
	t.Helper()
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enabled := true
	cfg := &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				Designer: map[string]srvconfig.ImageGenerationRouteConfig{
					"test-openai": {
						Alias:    "Test OpenAI",
						Enabled:  true,
						Provider: "wangcai_openai",
						Model:    "gpt-image-2",
						Capabilities: service.DesignerProviderCapabilities{
							QualityLevels:      []string{"auto", "low", "medium", "high"},
							SizePresets:        []string{"auto", "1024x1024", "1536x1024", "1024x1536"},
							DefaultSize:        "auto",
							MaxBatch:           10,
							MaxReferenceImages: 16,
							SupportsReference:  true,
							SupportsMask:       true,
							OutputFormats:      []string{"png", "jpeg", "webp"},
							HasBackground:      true,
							HasCompression:     true,
						},
					},
				},
			},
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			CurrencyRates: map[string]srvconfig.CurrencyRate{"USD": {ToCNY: 7.2}},
			ImageGeneration: map[string]srvconfig.ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:      srvconfig.ImagePricingTypeOpenAIUsage,
					Currency:         "USD",
					Unit:             1_000_000,
					RequireUsage:     true,
					TextInput:        5,
					TextCachedInput:  1.25,
					ImageInput:       8,
					ImageCachedInput: 2,
					ImageOutput:      30,
					EstimateTable: map[string]map[string]srvconfig.FlexibleFloat{
						"1024x1024": {"medium": srvconfig.FlexibleFloat(0.053)},
					},
				},
			},
		},
		Billing: srvconfig.BillingConfig{CreditsPerCNY: 1000, MinimumChargeCredits: 1},
		ImageAPI: srvconfig.ImageAPIConfig{
			Designer: map[string]*appconfig.ImageAPI{
				"test-openai": {
					Alias:    "Test OpenAI",
					Enable:   &enabled,
					Provider: "wangcai_openai",
					Model:    "gpt-image-2",
				},
			},
		},
	}
	db := setupTaskHandlerTestDB(t)
	designerSvc := service.NewDesignerService(db, nil, nil, cfg, nil, &logger)
	handler := NewDesignerHandler(designerSvc, &logger)

	app := fiber.New()
	app.Get("/designer/providers", handler.GetProviders)
	return app, handler, db
}

func TestDesignerProvidersUsesStandardResponseEnvelope(t *testing.T) {
	app, _, _ := setupDesignerHandlerTest(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/designer/providers", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	defer resp.Body.Close()

	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := rawBody["code"]; !ok {
		t.Fatalf("response missing standard code field: %s", string(mustMarshalJSON(t, rawBody)))
	}
	if _, ok := rawBody["msg"]; !ok {
		t.Fatalf("response missing standard msg field: %s", string(mustMarshalJSON(t, rawBody)))
	}
	var body Response
	if err := json.Unmarshal(mustMarshalJSON(t, rawBody), &body); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("code = %d, want 0", body.Code)
	}
	if body.Msg != "success" {
		t.Fatalf("msg = %q, want success", body.Msg)
	}
	raw, err := json.Marshal(body.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var providers []service.DesignerProviderInfo
	if err := json.Unmarshal(raw, &providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(providers) != 1 || providers[0].ID != "test-openai" {
		t.Fatalf("providers = %+v", providers)
	}
	if providers[0].Idx != 0 {
		t.Fatalf("providers[0].Idx = %d, want 0", providers[0].Idx)
	}
	if providers[0].ProviderKey != "wangcai_openai" || providers[0].Route != "image_generation.designer.test-openai" {
		t.Fatalf("provider route fields = %+v", providers[0])
	}
	if providers[0].Capabilities.MaxBatch != 10 || !providers[0].Capabilities.SupportsReference || len(providers[0].Capabilities.QualityLevels) == 0 {
		t.Fatalf("capabilities = %+v", providers[0].Capabilities)
	}
	if providers[0].Capabilities.DefaultSize != "auto" {
		t.Fatalf("default size = %q, want auto for GPT Image", providers[0].Capabilities.DefaultSize)
	}
	if len(providers[0].Capabilities.SizePresets) == 0 || providers[0].Capabilities.SizePresets[0] != "auto" {
		t.Fatalf("size presets = %+v, want auto first for GPT Image", providers[0].Capabilities.SizePresets)
	}
	if providers[0].Pricing.PricingType != srvconfig.ImagePricingTypeOpenAIUsage || !providers[0].Pricing.RequiresUsage {
		t.Fatalf("pricing = %+v", providers[0].Pricing)
	}
	if providers[0].Credits != 0 {
		t.Fatalf("legacy credits = %d, want 0 for dynamic GPT Image 2", providers[0].Credits)
	}
}

func TestDesignerGenerateRejectsInvalidReferenceWithGenericClientError(t *testing.T) {
	_, h, _ := setupDesignerHandlerTest(t)
	userID := uuid.NewString()
	app := fiber.New()
	app.Post("/designer/generate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Generate(c)
	})

	resp := postJSON(t, app, "/designer/generate", `{
		"prompt":"edit","provider_id":"test-openai","quality":"medium","size":"1024x1024","n":1,
		"reference_file_ids":["*"]
	}`)
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest || body.Msg != "designer reference is invalid or unavailable" || strings.Contains(body.Msg, "*") {
		t.Fatalf("status=%d response=%#v; want generic reference rejection", resp.StatusCode, body)
	}
}

func TestDesignerGenerateRedactsReferenceRepositoryFailure(t *testing.T) {
	_, h, db := setupDesignerHandlerTest(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	userID := uuid.NewString()
	app := fiber.New()
	app.Post("/designer/generate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Generate(c)
	})

	resp := postJSON(t, app, "/designer/generate", `{
		"prompt":"edit","provider_id":"test-openai","quality":"medium","size":"1024x1024","n":1,
		"reference_file_ids":["`+uuid.NewString()+`"]
	}`)
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError || body.Msg != "failed to create generation" || strings.Contains(body.Msg, "database is closed") {
		t.Fatalf("status=%d response=%#v; want redacted infrastructure failure", resp.StatusCode, body)
	}
}

func TestDesignerGenerateMapsProjectOwnershipErrors(t *testing.T) {
	_, handler, db := setupDesignerHandlerTest(t)
	userID := uuid.NewString()
	otherUserID := uuid.NewString()
	foreignProjectID := uuid.NewString()
	if err := db.Create(&model.Project{
		ID: foreignProjectID, UserID: otherUserID, Platform: model.PlatformArticle,
		Name: "Foreign", Status: model.ProjectStatusActive,
	}).Error; err != nil {
		t.Fatalf("create foreign project: %v", err)
	}
	app := fiber.New()
	app.Post("/designer/generate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Generate(c)
	})

	for _, tc := range []struct {
		name      string
		projectID string
		wantCode  int
	}{
		{name: "missing", projectID: uuid.NewString(), wantCode: fiber.StatusNotFound},
		{name: "foreign", projectID: foreignProjectID, wantCode: fiber.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := mustMarshalJSON(t, service.DesignerGenerateRequest{
				ProjectID: tc.projectID, Prompt: "a cat", ProviderID: "test-openai",
				Quality: "medium", Size: "1024x1024", N: 1,
			})
			req := httptest.NewRequest(http.MethodPost, "/designer/generate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantCode {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d body=%s, want %d", resp.StatusCode, responseBody, tc.wantCode)
			}
		})
	}
}

func TestRegisterDesignerReference(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	pending := &designerPendingUploadRepo{uploads: map[string]*model.PendingUpload{}}
	logger := zerolog.New(io.Discard)
	userID := uuid.NewString()
	otherUserID := uuid.NewString()
	now := time.Now()
	type uploadFixture struct {
		id, userID, purpose, sourceKey, finalKey string
		status                                   string
	}
	fixtures := []uploadFixture{
		{id: "pending-ok", userID: userID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + userID + "/pending-ok/reference.png", finalKey: "uploads/finalized/" + userID + "/pending-ok/reference.png", status: model.PendingUploadStatusPending},
		{id: "final-ok", userID: userID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + userID + "/final-ok/reference.png", finalKey: "uploads/finalized/" + userID + "/final-ok/reference.png", status: model.PendingUploadStatusFinalized},
		{id: "wrong-purpose", userID: userID, purpose: service.DirectUploadPurposeAIEntryAttachment, sourceKey: "uploads/pending/" + userID + "/wrong-purpose/reference.png", finalKey: "", status: model.PendingUploadStatusPending},
		{id: "other-user", userID: otherUserID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + otherUserID + "/other-user/reference.png", finalKey: "", status: model.PendingUploadStatusPending},
		{id: "legacy-final", userID: userID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + userID + "/legacy-final/reference.png", finalKey: "", status: model.PendingUploadStatusFinalized},
		{id: "oversize", userID: userID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + userID + "/oversize/reference.png", finalKey: "uploads/finalized/" + userID + "/oversize/reference.png", status: model.PendingUploadStatusFinalized},
		{id: "backend", userID: userID, purpose: service.DirectUploadPurposeDesignerReference, sourceKey: "uploads/pending/" + userID + "/backend/reference.png", finalKey: "uploads/finalized/" + userID + "/backend/reference.png", status: model.PendingUploadStatusFinalized},
	}
	for _, fixture := range fixtures {
		if err := pending.CreatePendingUpload(context.Background(), &model.PendingUpload{
			ID: fixture.id, UserID: fixture.userID, Purpose: fixture.purpose, Key: fixture.sourceKey, FinalizedKey: fixture.finalKey,
			FileName: "reference.png", ContentType: "image/png", Size: 9, Status: fixture.status, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("create upload %s: %v", fixture.id, err)
		}
	}
	store := pendingUploadStatStore(pending)
	store.data = map[string][]byte{
		fixtures[0].sourceKey: []byte("pending"), fixtures[1].finalKey: []byte("finalized"),
		fixtures[5].finalKey: []byte("too-large"), fixtures[6].finalKey: []byte("backend"),
	}
	store.objects = map[string]*storage.ObjectInfo{
		fixtures[1].finalKey: {Key: fixtures[1].finalKey, Size: 9, ContentType: "image/png", ETag: "final"},
		fixtures[5].finalKey: {Key: fixtures[5].finalKey, Size: 9, ContentType: "image/png", ETag: "oversize"},
		fixtures[6].finalKey: {Key: fixtures[6].finalKey, Size: 9, ContentType: "image/png", ETag: "backend"},
	}
	designerSvc := service.NewDesignerService(db, nil, nil, nil, store, &logger)
	h := NewDesignerHandler(designerSvc, &logger)
	h.SetDirectUploadDependencies(pending, store)
	app := fiber.New()
	app.Post("/designer/register-reference", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.RegisterReference(c)
	})

	request := func(id, key string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/designer/register-reference", bytes.NewBufferString(`{"upload_id":"`+id+`","key":"`+key+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, fiber.TestConfig{})
		if err != nil {
			t.Fatalf("request %s: %v", id, err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response %s: %v", id, err)
		}
		return resp, string(body)
	}

	for _, success := range []struct{ id, key, finalKey string }{{"pending-ok", fixtures[0].sourceKey, fixtures[0].finalKey}, {"final-ok", fixtures[1].finalKey, fixtures[1].finalKey}} {
		store.read = nil
		store.readMax = nil
		store.uploaded = nil
		resp, body := request(success.id, success.key)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("%s status=%d body=%s", success.id, resp.StatusCode, body)
		}
		var envelope struct {
			Data struct {
				FileID   string `json:"file_id"`
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err != nil {
			t.Fatalf("decode %s: %v", success.id, err)
		}
		if envelope.Data.FileID == "" || envelope.Data.Filename != "reference.png" || envelope.Data.Size != 9 {
			t.Fatalf("%s response=%s", success.id, body)
		}
		if len(store.read) != 1 || store.read[0] != success.finalKey || len(store.readMax) != 1 || store.readMax[0] != 10*1024*1024 {
			t.Fatalf("%s reads=%#v max=%#v, want only bounded final key %q", success.id, store.read, store.readMax, success.finalKey)
		}
		if len(store.uploaded) != 0 {
			t.Fatalf("%s duplicated reference bytes to %#v", success.id, store.uploaded)
		}
		reference, err := repository.NewDesignerReferenceRepository(db).FindByIDAndUserID(context.Background(), envelope.Data.FileID, userID)
		if err != nil || reference.StorageKey != success.finalKey || reference.FileName != "reference.png" || reference.ContentType != "image/png" || reference.Size != 9 {
			t.Fatalf("%s durable reference = %#v err=%v", success.id, reference, err)
		}
	}
	foundPending, err := pending.FindPendingUploadByID(context.Background(), "pending-ok")
	if err != nil || foundPending.Status != model.PendingUploadStatusFinalized || foundPending.FinalizedKey != fixtures[0].finalKey {
		t.Fatalf("pending upload not finalized to immutable key: upload=%#v err=%v", foundPending, err)
	}

	for _, rejected := range []struct{ name, id, key string }{
		{"wrong purpose", "wrong-purpose", fixtures[2].sourceKey},
		{"cross user", "other-user", fixtures[3].sourceKey},
		{"mismatched key", "pending-ok", "uploads/pending/attacker/reference.png"},
		{"legacy finalized", "legacy-final", fixtures[4].sourceKey},
	} {
		t.Run(rejected.name, func(t *testing.T) {
			resp, body := request(rejected.id, rejected.key)
			if resp.StatusCode != fiber.StatusBadRequest && resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("status=%d body=%s, want 400/403", resp.StatusCode, body)
			}
		})
	}

	store.readErr = storage.ErrObjectExceedsMaxSize
	resp, body := request("oversize", fixtures[5].finalKey)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("oversize status=%d body=%s", resp.StatusCode, body)
	}
	store.readErr = errors.New("OSS secret backend detail")
	resp, body = request("backend", fixtures[6].finalKey)
	if resp.StatusCode != fiber.StatusInternalServerError || strings.Contains(body, "OSS secret backend detail") {
		t.Fatalf("backend status=%d body=%s", resp.StatusCode, body)
	}
}

func TestUploadDesignerReferenceFromURLRedactsBackendErrors(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	userID := uuid.NewString()
	const backendSecret = "OSS endpoint secret: request signature"
	store := &fakeStorageProvider{readErr: errors.New(backendSecret)}
	logger := zerolog.New(io.Discard)
	designerSvc := service.NewDesignerService(db, nil, nil, nil, store, &logger)
	h := NewDesignerHandler(designerSvc, &logger)
	app := fiber.New()
	app.Post("/designer/upload-reference-from-url", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.UploadReferenceFromURL(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/designer/upload-reference-from-url", strings.NewReader(`{"url":"/api/v1/files/`+userID+`/designer/result.png"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError || body.Msg != "failed to upload reference" || strings.Contains(body.Msg, backendSecret) {
		t.Fatalf("status=%d response=%#v; want redacted backend error", resp.StatusCode, body)
	}
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}

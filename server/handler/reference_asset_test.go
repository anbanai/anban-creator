package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func TestReferenceAssetErrorMapper(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{name: "invalid selection", err: service.ErrReferenceImageSelectionInvalid, wantStatus: fiber.StatusBadRequest, wantText: "reference image selection is invalid"},
		{name: "purpose mismatch", err: service.ErrReferenceAssetPurposeMismatch, wantStatus: fiber.StatusBadRequest, wantText: "reference image purpose is not allowed"},
		{name: "invalid metadata", err: service.ErrReferenceAssetInvalidMetadata, wantStatus: fiber.StatusBadRequest, wantText: "reference image metadata is invalid"},
		{name: "invalid request", err: errReferenceAssetRequestInvalid, wantStatus: fiber.StatusBadRequest, wantText: "reference image request is invalid"},
		{name: "legacy field", err: errRemovedReferenceImageField, wantStatus: fiber.StatusBadRequest, wantText: "use reference_image"},
		{name: "opaque forbidden", err: service.ErrReferenceAssetForbidden, wantStatus: fiber.StatusForbidden, wantText: "reference image is not accessible"},
		{name: "concurrent finalization", err: service.ErrReferenceAssetConcurrentFinalization, wantStatus: fiber.StatusConflict, wantText: "reference image upload is being finalized"},
		{name: "expired session", err: service.ErrReferenceAssetExpired, wantStatus: fiber.StatusGone, wantText: "reference image upload has expired"},
		{name: "storage unavailable", err: service.ErrReferenceAssetUnavailable, wantStatus: fiber.StatusServiceUnavailable, wantText: "reference image storage is temporarily unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				return respondReferenceAssetError(c, nil, tt.err)
			})
			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if resp.StatusCode != tt.wantStatus || !strings.Contains(string(body), tt.wantText) {
				t.Fatalf("status/body = %d, %s; want %d containing %q", resp.StatusCode, body, tt.wantStatus, tt.wantText)
			}
		})
	}
}

func TestRejectLegacyReferenceImageURL(t *testing.T) {
	tests := []struct {
		name                string
		body                string
		wantErr             bool
		wantLegacy          bool
		wantImageCapability bool
	}{
		{name: "absent", body: `{"reference_image":{"asset_id":"asset-1"}}`},
		{name: "url", body: `{"reference_image_url":"https://example.com/ref.png"}`, wantErr: true, wantLegacy: true},
		{name: "empty", body: `{"reference_image_url":""}`, wantErr: true, wantLegacy: true},
		{name: "null field", body: `{"reference_image_url":null}`, wantErr: true, wantLegacy: true},
		{name: "uppercase", body: `{"REFERENCE_IMAGE_URL":null}`, wantErr: true, wantLegacy: true},
		{name: "mixed case", body: `{"Reference_Image_Url":""}`, wantErr: true, wantLegacy: true},
		{name: "duplicate case keys", body: `{"reference_image_url":null,"REFERENCE_IMAGE_URL":""}`, wantErr: true, wantLegacy: true},
		{name: "removed image model field", body: `{"image_model_key":"standard_image"}`, wantErr: true, wantImageCapability: true},
		{name: "removed nested image model field", body: `{"ecommerce_defaults":{"IMAGE_MODEL_KEY":"standard_image"}}`, wantErr: true, wantImageCapability: true},
		{name: "empty body", body: ``, wantErr: true},
		{name: "malformed JSON", body: `{"reference_image_url":`, wantErr: true},
		{name: "null body", body: `null`, wantErr: true},
		{name: "array", body: `[]`, wantErr: true},
		{name: "string", body: `"reference_image_url"`, wantErr: true},
		{name: "number", body: `42`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectRemovedRequestFields([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, errReferenceAssetRequestInvalid) {
				t.Fatalf("error = %v, want errReferenceAssetRequestInvalid", err)
			}
			if tt.wantLegacy && (!errors.Is(err, errRemovedReferenceImageField) || !strings.Contains(err.Error(), "use reference_image")) {
				t.Fatalf("legacy error = %q, want migration hint", err)
			}
			if tt.wantImageCapability && !strings.Contains(err.Error(), "use image_capability_key") {
				t.Fatalf("removed image model error = %q, want migration hint", err)
			}
		})
	}
}

func TestImageModelKeyIsRejectedByPublicWriteAPIs(t *testing.T) {
	logger := zerolog.New(io.Discard)
	taskHandler := NewTaskHandler(nil, &logger)
	planHandler := NewPlanHandler(nil, &logger)
	projectHandler := NewProjectHandler(nil, &logger)

	tests := []struct {
		name string
		path string
		body string
		run  fiber.Handler
	}{
		{name: "task", path: "/tasks", body: `{"image_model_key":"standard_image"}`, run: taskHandler.Create},
		{name: "plan", path: "/plans", body: `{"image_model_key":"standard_image"}`, run: planHandler.Create},
		{name: "nested project default", path: "/projects", body: `{"ecommerce_defaults":{"image_model_key":"standard_image"}}`, run: projectHandler.Create},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Post(tt.path, func(c fiber.Ctx) error {
				c.Locals("user_id", "user-1")
				return tt.run(c)
			})
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(string(body), "use image_capability_key") {
				t.Fatalf("status/body = %d, %s; want 400 with capability migration hint", resp.StatusCode, body)
			}
		})
	}
}

func TestReferenceAssetErrorMapperTreatsMalformedJSONAsBadRequest(t *testing.T) {
	requestErr := rejectRemovedRequestFields([]byte(`{"reference_image_url":`))
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return respondReferenceAssetError(c, nil, requestErr)
	})
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestReferenceAssetErrorMapperRedactsUnknownErrors(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return respondReferenceAssetError(c, nil, errors.New("database secret"))
	})
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusInternalServerError || strings.Contains(string(body), "database secret") {
		t.Fatalf("status/body = %d, %s", resp.StatusCode, body)
	}
}

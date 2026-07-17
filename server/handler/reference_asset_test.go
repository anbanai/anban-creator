package handler

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
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
		{name: "legacy field", err: errLegacyReferenceImageURL, wantStatus: fiber.StatusBadRequest, wantText: "use reference_image"},
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
		name       string
		body       string
		wantErr    bool
		wantLegacy bool
	}{
		{name: "absent", body: `{"reference_image":{"asset_id":"asset-1"}}`},
		{name: "url", body: `{"reference_image_url":"https://example.com/ref.png"}`, wantErr: true, wantLegacy: true},
		{name: "empty", body: `{"reference_image_url":""}`, wantErr: true, wantLegacy: true},
		{name: "null field", body: `{"reference_image_url":null}`, wantErr: true, wantLegacy: true},
		{name: "uppercase", body: `{"REFERENCE_IMAGE_URL":null}`, wantErr: true, wantLegacy: true},
		{name: "mixed case", body: `{"Reference_Image_Url":""}`, wantErr: true, wantLegacy: true},
		{name: "duplicate case keys", body: `{"reference_image_url":null,"REFERENCE_IMAGE_URL":""}`, wantErr: true, wantLegacy: true},
		{name: "empty body", body: ``, wantErr: true},
		{name: "malformed JSON", body: `{"reference_image_url":`, wantErr: true},
		{name: "null body", body: `null`, wantErr: true},
		{name: "array", body: `[]`, wantErr: true},
		{name: "string", body: `"reference_image_url"`, wantErr: true},
		{name: "number", body: `42`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectLegacyReferenceImageURL([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, errReferenceAssetRequestInvalid) {
				t.Fatalf("error = %v, want errReferenceAssetRequestInvalid", err)
			}
			if tt.wantLegacy && (!errors.Is(err, errLegacyReferenceImageURL) || !strings.Contains(err.Error(), "use reference_image")) {
				t.Fatalf("legacy error = %q, want migration hint", err)
			}
		})
	}
}

func TestReferenceAssetErrorMapperTreatsMalformedJSONAsBadRequest(t *testing.T) {
	requestErr := rejectLegacyReferenceImageURL([]byte(`{"reference_image_url":`))
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

package handler

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

func TestRewriteFinalizedUploadURLsCoversCurrentConsumers(t *testing.T) {
	const (
		pending = "https://cdn.example.com/uploads/pending/user-1/upload-1/input.mp4"
		final   = "https://cdn.example.com/uploads/finalized/user-1/upload-1/input.mp4"
	)
	rewrites := map[string]string{pending: final}
	values := []string{pending, "https://external.example.com/image.png"}
	cfg := &model.VideoTaskConfig{References: []model.VideoReferenceAsset{{URL: pending}}}
	input := &model.VideoInput{References: []model.VideoReferenceAsset{{URL: pending}}}
	montage := &model.MontageInput{SourceAssets: []model.MontageAsset{{URL: pending}}}

	if got := rewriteFinalizedUploadURL(pending, rewrites); got != final {
		t.Fatalf("scalar rewrite = %q", got)
	}
	rewriteFinalizedUploadURLSlice(values, rewrites)
	rewriteFinalizedVideoReferenceURLs(rewrites, cfg, input, nil, nil)
	rewriteFinalizedMontageAssetURLs(montage, rewrites)
	if values[0] != final || values[1] != "https://external.example.com/image.png" || cfg.References[0].URL != final || input.References[0].URL != final || montage.SourceAssets[0].URL != final {
		t.Fatalf("rewritten values: values=%#v cfg=%#v input=%#v montage=%#v", values, cfg, input, montage)
	}
}

func TestRespondPendingUploadFinalizeErrorClassifiesAndRedacts(t *testing.T) {
	const secret = "uploads/pending/user-1/upload-1/object.png at oss-secret.example.com"
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "access denied", err: fmt.Errorf("%w: %s", service.ErrUploadSessionAccessDenied, secret), wantStatus: fiber.StatusBadRequest},
		{name: "expired", err: fmt.Errorf("%w: %s", service.ErrUploadSessionExpired, secret), wantStatus: fiber.StatusBadRequest},
		{name: "not pending", err: fmt.Errorf("%w: %s", service.ErrUploadSessionStateConflict, secret), wantStatus: fiber.StatusBadRequest},
		{name: "invalid object metadata", err: fmt.Errorf("%w: %s", service.ErrUploadSessionObjectInvalid, secret), wantStatus: fiber.StatusBadRequest},
		{name: "backend error", err: fmt.Errorf("storage backend failed: %s", secret), wantStatus: fiber.StatusInternalServerError},
		{name: "timeout", err: context.DeadlineExceeded, wantStatus: fiber.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				return respondUploadSessionFinalizeError(c, nil, tt.err)
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", resp.StatusCode, body, tt.wantStatus)
			}
			if strings.Contains(string(body), secret) || strings.Contains(string(body), "oss-secret") {
				t.Fatalf("response leaked backend detail: %s", body)
			}
		})
	}
}

func TestFinalizePendingURLCallersUseSharedResponder(t *testing.T) {
	expectedCalls := map[string]int{
		"task.go":     4,
		"plan.go":     6,
		"project.go":  2,
		"template.go": 2,
	}
	for fileName, wantCalls := range expectedCalls {
		t.Run(fileName, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(fileName), nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", fileName, err)
			}
			finalizeCalls := 0
			responderCalls := 0
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				switch ident.Name {
				case "finalizeUploadSessionURLs":
					finalizeCalls++
				case "respondUploadSessionFinalizeError":
					responderCalls++
				}
				return true
			})
			if finalizeCalls != wantCalls || responderCalls != wantCalls {
				t.Fatalf("calls finalize=%d responder=%d, want %d of each", finalizeCalls, responderCalls, wantCalls)
			}
		})
	}
}

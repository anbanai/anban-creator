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

	"github.com/anbanai/anban-creator/server/service"
)

func TestRespondPendingUploadFinalizeErrorClassifiesAndRedacts(t *testing.T) {
	const secret = "uploads/pending/user-1/upload-1/object.png at oss-secret.example.com"
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "access denied", err: fmt.Errorf("%w: %s", service.ErrPendingUploadAccessDenied, secret), wantStatus: fiber.StatusBadRequest},
		{name: "expired", err: fmt.Errorf("%w: %s", service.ErrPendingUploadExpired, secret), wantStatus: fiber.StatusBadRequest},
		{name: "not pending", err: fmt.Errorf("%w: %s", service.ErrPendingUploadNotPending, secret), wantStatus: fiber.StatusBadRequest},
		{name: "invalid object metadata", err: fmt.Errorf("%w: %s", service.ErrPendingUploadObjectInvalid, secret), wantStatus: fiber.StatusBadRequest},
		{name: "backend error", err: fmt.Errorf("storage backend failed: %s", secret), wantStatus: fiber.StatusInternalServerError},
		{name: "timeout", err: context.DeadlineExceeded, wantStatus: fiber.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				return respondPendingUploadFinalizeError(c, nil, tt.err)
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
			calls := 0
			ast.Inspect(file, func(node ast.Node) bool {
				ifStmt, ok := node.(*ast.IfStmt)
				if !ok || !ifInitializesCall(ifStmt, "finalizePendingURLs") {
					return true
				}
				calls++
				if !ifReturnsCall(ifStmt, "respondPendingUploadFinalizeError") {
					t.Errorf("finalizePendingURLs call at %s is not wired to shared responder", fileName)
				}
				return true
			})
			if calls != wantCalls {
				t.Fatalf("finalizePendingURLs calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func ifInitializesCall(ifStmt *ast.IfStmt, name string) bool {
	assign, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == name
}

func ifReturnsCall(ifStmt *ast.IfStmt, name string) bool {
	if len(ifStmt.Body.List) != 1 {
		return false
	}
	ret, ok := ifStmt.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == name
}

package handler

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

func setupFileHandlerTest(userID string) *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeStorageProvider{data: map[string][]byte{
		"user-1/designer/gen-1/0.png":                []byte("png-bytes"),
		"uploads/projects/user-1/ref.png":            []byte("project-png"),
		"uploads/references/user-1/source.png":       []byte("ref-png"),
		"user-2/designer/gen-2/0.png":                []byte("other-user-png"),
		"user-1/anban-creator_ref_fileid_source.png": []byte("ref-bytes"),
	}}
	h := NewFileHandler(store, &logger)

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	})
	app.Get("/files/*", h.ServeFile)
	return app
}

func TestServeFile_AllowsOwnPendingUploadThroughRepository(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	key := "uploads/pending/user-1/upload-1/ref.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: []byte("pending-png")}}
	pending := &fakeProjectPendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"upload-1": {
			ID:        "upload-1",
			UserID:    "user-1",
			Purpose:   service.DirectUploadPurposeProjectReference,
			Key:       key,
			PublicURL: "/api/v1/files/" + key,
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: time.Now().Add(time.Minute),
		},
	}}
	h := NewFileHandler(store, &logger)
	h.SetPendingUploadRepository(pending)

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	app.Get("/files/*", h.ServeFile)

	resp, err := app.Test(httptest.NewRequest("GET", "/files/"+key, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(store.read) != 1 || store.read[0] != key {
		t.Fatalf("store.Read keys = %v, want [%s]", store.read, key)
	}
	if len(pending.finalized) != 0 {
		t.Fatalf("ServeFile finalized pending uploads = %v, want none", pending.finalized)
	}
}

func TestServeFile_AllowsDesignerPathForOwner(t *testing.T) {
	app := setupFileHandlerTest("user-1")

	resp, err := app.Test(httptest.NewRequest("GET", "/files/user-1/designer/gen-1/0.png", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeFile_RejectsDesignerPathForOtherUser(t *testing.T) {
	app := setupFileHandlerTest("user-1")

	resp, err := app.Test(httptest.NewRequest("GET", "/files/user-2/designer/gen-2/0.png", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServeFile_StillAllowsUploadsPath(t *testing.T) {
	app := setupFileHandlerTest("user-1")

	for _, path := range []string{
		"/files/uploads/projects/user-1/ref.png",
		"/files/uploads/references/user-1/source.png",
	} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("request %s failed: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("path %s: status = %d, want 200 (regression)", path, resp.StatusCode)
		}
	}
}

func TestServeFile_RejectsUnauthenticatedRequest(t *testing.T) {
	app := setupFileHandlerTest("")

	resp, err := app.Test(httptest.NewRequest("GET", "/files/user-1/designer/gen-1/0.png", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServeFile_RejectsReferenceFilePath(t *testing.T) {
	// Paths like "{userID}/anban-creator_ref_..." are reference files saved by
	// uploadReferenceFromUrl. They are NOT served by /api/v1/files/* — clients
	// never load them directly. Make sure we don't accidentally allow them.
	app := setupFileHandlerTest("user-1")

	resp, err := app.Test(httptest.NewRequest("GET", "/files/user-1/anban-creator_ref_fileid_source.png", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403 (reference files should not be served directly)", resp.StatusCode)
	}
}

func TestServeFile_RejectsPathTraversalAttempt(t *testing.T) {
	// filepath.Clean collapses ".." segments before the ownership check runs,
	// and ServeFile also rejects paths containing ".." outright. Any traversal
	// attempt must be rejected with 400/403 (blocked at security layer) OR 404
	// (literal percent-encoded path doesn't match a real storage key). The key
	// invariant: no 200, no access to another user's file.
	app := setupFileHandlerTest("user-1")

	for _, path := range []string{
		"/files/user-1/designer/../../user-2/designer/gen-2/0.png",
		"/files/user-1/designer/%2e%2e/%2e%2e/user-2/designer/gen-2/0.png",
		"/files/uploads/references/user-1/../../../etc/passwd",
	} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("request %s failed: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == fiber.StatusOK {
			t.Fatalf("traversal path %s: status = 200, must be rejected", path)
		}
		if resp.StatusCode < 400 {
			t.Fatalf("traversal path %s: status = %d, must be 4xx", path, resp.StatusCode)
		}
	}
}

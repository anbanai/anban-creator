package handler

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func setupFileHandlerTest(userID string) *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeStorageProvider{data: map[string][]byte{
		"user-1/designer/gen-1/0.png":              []byte("png-bytes"),
		"uploads/channels/user-1/ref.png":          []byte("channel-png"),
		"uploads/references/user-1/source.png":     []byte("ref-png"),
		"user-2/designer/gen-2/0.png":              []byte("other-user-png"),
		"user-1/anbanwriter_ref_fileid_source.png": []byte("ref-bytes"),
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
		"/files/uploads/channels/user-1/ref.png",
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
	// Paths like "{userID}/anbanwriter_ref_..." are reference files saved by
	// uploadReferenceFromUrl. They are NOT served by /api/v1/files/* — clients
	// never load them directly. Make sure we don't accidentally allow them.
	app := setupFileHandlerTest("user-1")

	resp, err := app.Test(httptest.NewRequest("GET", "/files/user-1/anbanwriter_ref_fileid_source.png", nil))
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

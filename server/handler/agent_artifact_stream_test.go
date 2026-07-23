package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAgentArtifactStreamUsesExecutionAuthAndStreamsBody(t *testing.T) {
	app, _, task, _, token, rawAPIKey, store := setupExecutionScopedAgentApp(t)
	body := "artifact-body"
	sum := sha256.Sum256([]byte(body))
	request := func(credential string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/agent/artifacts/content", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "text/markdown")
		req.Header.Set("X-Anban-Artifact-Path", "output/article.md")
		req.Header.Set("X-Anban-Artifact-Size", "13")
		req.Header.Set("X-Anban-Artifact-SHA256", hex.EncodeToString(sum[:]))
		return req
	}

	resp, err := app.Test(request(token))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream status/body = %d/%s", resp.StatusCode, responseBody)
	}
	if string(store.uploadedBody) != body || !strings.Contains(store.uploadedKey, "/tasks/"+task.ID+"/executions/") {
		t.Fatalf("stored key/body = %q/%q", store.uploadedKey, store.uploadedBody)
	}

	resp, err = app.Test(request(rawAPIKey))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("legacy API key stream status = %d, want 403", resp.StatusCode)
	}
}

func TestAgentArtifactStreamHandlerDoesNotBufferRequestBody(t *testing.T) {
	source, err := os.ReadFile("agent_artifact_stream.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "c.Request().BodyStream()") {
		t.Fatal("stream handler must pass Fiber's request BodyStream directly")
	}
	if strings.Contains(text, "c.Body()") || strings.Contains(text, "Bind().Body") {
		t.Fatal("stream handler must not buffer or bind the request body")
	}
}

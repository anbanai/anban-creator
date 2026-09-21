package handler

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHypitArtifactExtendsOnlyValidatedStreamReadDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, kind, path string
		wantOK           bool
	}{{"hypit archive", model.PlatformHypit, "output/project.zip", true}, {"ordinary artifact", model.PlatformArticle, "output/article.md", false}} {
		t.Run(tc.name, func(t *testing.T) {
			app, repo, task, _, token, _, store := setupExecutionScopedAgentAppForPack(t, tc.kind, "")
			if tc.kind == model.PlatformHypit {
				task.HypitRuntimeSnapshot = datatypes.JSON(`{"limits":{"max_project_bytes":2147483648}}`)
				if err := repo.Tasks().Update(t.Context(), task); err != nil {
					t.Fatal(err)
				}
			}
			app.Server().ReadTimeout = 50 * time.Millisecond
			app.Server().MaxRequestBodySize = 1024
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
			t.Cleanup(func() { _ = app.Shutdown(); <-done })
			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			body := strings.Repeat("x", 16<<10)
			hash := sha256.Sum256([]byte(body))
			mime := "text/markdown"
			if tc.kind == model.PlatformHypit {
				mime = "application/zip"
			}
			_, err = fmt.Fprintf(conn, "POST /agent/artifacts/content HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nAuthorization: Bearer %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nX-Anban-Artifact-Path: %s\r\nX-Anban-Artifact-Size: %d\r\nX-Anban-Artifact-SHA256: %s\r\n\r\n%s", token, mime, len(body), tc.path, len(body), hex.EncodeToString(hash[:]), body[:8192])
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			_, _ = io.WriteString(conn, body[8192:])
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				if tc.wantOK {
					t.Fatal(err)
				}
				return
			}
			defer resp.Body.Close()
			payload, _ := io.ReadAll(resp.Body)
			if (resp.StatusCode == http.StatusOK) != tc.wantOK {
				t.Fatalf("status %d body %s", resp.StatusCode, payload)
			}
			if tc.wantOK && string(store.uploadedBody) != body {
				t.Fatal("stream body changed")
			}
		})
	}
}

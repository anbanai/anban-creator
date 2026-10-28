package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskShareHandlerExpiryAndSafeResponses(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskShare{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := repository.New(db)
	if err := repo.Tasks().Create(t.Context(), &model.Task{ID: "task", UserID: "owner", AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration}); err != nil {
		t.Fatal(err)
	}
	h := NewTaskShareHandler(service.NewTaskShareService(repo, nil))
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return c.Next() })
	app.Get("/tasks/:id/share", h.Get)
	app.Put("/tasks/:id/share", h.Put)
	app.Delete("/tasks/:id/share", h.Revoke)
	app.Get("/public/shares/:token", h.Public)
	for _, body := range []string{`{}`, `null`, `{"expires_in_days":0}`, `{"expires_in_days":366}`, `{"expires_in_days":1.5}`, `{"expires_in_days":"7"}`, `{"expires_in_days":null}`} {
		req := httptest.NewRequest("PUT", "/tasks/task/share", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "owner")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: status %d, headers %v", body, resp.StatusCode, resp.Header)
		}
	}
	for _, user := range []string{"", "foreign", "owner"} {
		req := httptest.NewRequest("GET", "/tasks/task/share", nil)
		req.Header.Set("X-User-ID", user)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		want := map[string]int{"": 401, "foreign": 404, "owner": 200}[user]
		if resp.StatusCode != want {
			t.Fatalf("user %q: status %d, body %s", user, resp.StatusCode, data)
		}
		if user == "owner" {
			var envelope struct {
				Data service.TaskShareStatus `json:"data"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil || envelope.Data.Status != "none" {
				t.Fatalf("none response: %s, %v", data, err)
			}
		}
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/public/shares/unknown", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 || resp.Header.Get("X-Robots-Tag") != "noindex, nofollow" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("public = %d, %v", resp.StatusCode, resp.Header)
	}
}

func TestTaskShareHandlerAnonymousContentAndImage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.TaskFile{}, &model.TaskShare{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := repository.New(db)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(t.Context(), &model.Task{ID: "task", UserID: "owner", ProjectID: "project", AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, Title: "Shared article", Prompt: "private prompt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(t.Context(), &model.TaskExecution{ID: "execution", TaskID: "task", Attempt: 1, Status: model.TaskExecutionSucceeded, ManifestStatus: model.TaskExecutionManifestDelivered, AgentPackID: model.AgentIDArticle, AgentPackVersion: "1.0.0", AgentPackDigest: "frozen", AgentPackDeliveryContract: datatypes.JSON(`[{"role":"html","path":"output/05-article.html","mime_type":"text/html"},{"role":"cover","path":"output/cover.png","mime_type":"image/png"}]`)}); err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		id, path, mime string
		data           []byte
	}{
		{"body", "output/05-article.html", "text/html", []byte(`<p>Full content</p><img src="cover.png">`)},
		{"cover", "output/cover.png", "image/png", imageData.Bytes()},
	} {
		key := "uploads/users/owner/projects/project/tasks/task/executions/execution/artifacts/mcp/" + file.path
		if _, err := store.Upload(t.Context(), key, bytes.NewReader(file.data), file.mime); err != nil {
			t.Fatal(err)
		}
		if err := repo.TaskFiles().Create(t.Context(), &model.TaskFile{ID: file.id, TaskID: "task", ExecutionID: "execution", State: model.TaskFileStateDelivered, Role: model.FileRoleImage, FilePath: file.path, FileName: file.id, MimeType: file.mime, FileSize: int64(len(file.data)), OSSKey: key, StorageProvider: store.Name()}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewTaskShareHandler(service.NewTaskShareService(repo, store))
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return c.Next() })
	app.Put("/tasks/:id/share", h.Put)
	app.Delete("/tasks/:id/share", h.Revoke)
	app.Get("/public/shares/:token", h.Public)
	app.Get("/api/v1/public/shares/:token/images/:fileId", h.Image)
	request := httptest.NewRequest("PUT", "/tasks/task/share", strings.NewReader(`{"expires_in_days":1.0}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-User-ID", "owner")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	var managed struct {
		Data service.TaskShareStatus `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&managed); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || managed.Data.Status != "active" {
		t.Fatalf("creation = %d %#v", response.StatusCode, managed)
	}
	token := strings.TrimPrefix(managed.Data.SharePath, "/share/")
	response, err = app.Test(httptest.NewRequest("GET", "/public/shares/"+token, nil))
	if err != nil {
		t.Fatal(err)
	}
	var public struct {
		Data service.TaskShareContent `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || len(public.Data.Images) != 1 || !strings.Contains(public.Data.Body, public.Data.Images[0].URL) {
		t.Fatalf("public = %d %#v", response.StatusCode, public)
	}
	response, err = app.Test(httptest.NewRequest("GET", public.Data.Images[0].URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(data, imageData.Bytes()) || response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stream = %d %v, bytes=%d", response.StatusCode, response.Header, len(data))
	}
	request = httptest.NewRequest("DELETE", "/tasks/task/share", nil)
	request.Header.Set("X-User-ID", "owner")
	response, err = app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("revoke = %d", response.StatusCode)
	}
	response, err = app.Test(httptest.NewRequest("GET", public.Data.Images[0].URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 410 {
		t.Fatalf("revoked image = %d", response.StatusCode)
	}
}

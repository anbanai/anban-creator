package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestFeedbackHandlerTaskFeedbackEndpoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	task := &model.Task{ID: uuid.NewString(), UserID: "user-1", Type: "article", Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	h := NewFeedbackHandler(service.NewFeedbackService(repo, nil), nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", "user-1"); return c.Next() })
	app.Get("/tasks/:id/feedback", h.GetTaskFeedback)
	app.Put("/tasks/:id/feedback", h.UpsertTaskFeedback)

	getReq := httptest.NewRequest("GET", "/tasks/"+task.ID+"/feedback", nil)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatal(err)
	}
	if getResp.StatusCode != 200 {
		t.Fatalf("GET status = %d", getResp.StatusCode)
	}
	var getBody map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&getBody); err != nil {
		t.Fatal(err)
	}
	if value, ok := getBody["data"]; !ok || value != nil {
		t.Fatalf("GET data = %#v, want nil", value)
	}

	putReq := httptest.NewRequest("PUT", "/tasks/"+task.ID+"/feedback", strings.NewReader(`{"rating":4,"content":"不错"}`))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := app.Test(putReq)
	if err != nil {
		t.Fatal(err)
	}
	if putResp.StatusCode != 200 {
		t.Fatalf("PUT status = %d", putResp.StatusCode)
	}
	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	getAfterResume := httptest.NewRequest("GET", "/tasks/"+task.ID+"/feedback", nil)
	getAfterResumeResp, err := app.Test(getAfterResume)
	if err != nil {
		t.Fatal(err)
	}
	if getAfterResumeResp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("GET after task resumed status = %d, want %d", getAfterResumeResp.StatusCode, fiber.StatusBadRequest)
	}
}

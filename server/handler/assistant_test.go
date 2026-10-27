package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type assistantHandlerRuntime struct{}

func (assistantHandlerRuntime) Command(context.Context, string, any) error { return nil }
func (assistantHandlerRuntime) Close() error                               { return nil }

type assistantHandlerRuntimeFactory struct{ starts atomic.Int32 }

func (f *assistantHandlerRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (service.Runtime, error) {
	f.starts.Add(1)
	return assistantHandlerRuntime{}, nil
}

func newAssistantHandlerTestRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.Task{}, &model.AssistantCheckpoint{}, &model.AssistantSession{}, &model.AssistantSessionLease{}, &model.AssistantWorkspace{}, &model.AnalyticsState{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return repository.New(db)
}

func newAssistantHandlerTestApp(h *AssistantHandler) *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	app.Get("/assistant/bootstrap", h.Bootstrap)
	app.Get("/assistant/projects/:projectId/works", h.Works)
	app.Get("/assistant/checkpoint", h.LatestCheckpoint)
	app.Post("/assistant/sessions", h.Create)
	return app
}

func TestAssistantRecoveryHasNoLegacyWorkspaceSessionSurface(t *testing.T) {
	for _, target := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "handler", typ: reflect.TypeOf(&AssistantHandler{})},
		{name: "service", typ: reflect.TypeOf(&service.InteractiveService{})},
	} {
		t.Run(target.name, func(t *testing.T) {
			for _, method := range []string{
				"WorkspaceCreate",
				"WorkspaceMode",
				"WorkspaceEvents",
				"WorkspaceMessage",
				"WorkspaceAnswers",
				"WorkspaceCheckpoint",
				"WorkspaceCommit",
				"WorkspaceDelete",
				"CreateAssistantWorkspace",
				"SwitchAssistantWorkspaceMode",
				"SwitchAssistantWorkspaceModeWithContext",
				"AssistantWorkspaceSession",
				"WorkspaceState",
				"WorkspaceAnswer",
				"CloseAssistantWorkspace",
			} {
				if _, ok := target.typ.MethodByName(method); ok {
					t.Errorf("legacy workspace-session method %s remains on %s", method, target.name)
				}
			}
		})
	}
}

func TestInteractionMessageRejectsStaleModeFromAnotherTab(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.AssistantInteractionLegacyMigration{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := repository.New(db)
	factory := &assistantHandlerRuntimeFactory{}
	svc := service.NewAssistantInteractionService(repo, factory, time.Now)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", service.AssistantInteractionInput{ModeID: service.AssistantModeCreation}, "mode-route")
	if err != nil {
		t.Fatal(err)
	}
	h := NewAssistantHandler(nil, nil, repo)
	h.SetInteractionService(svc)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	app.Post("/assistant/interactions/:id/messages", h.InteractionMessage)
	request := httptest.NewRequest(http.MethodPost, "/assistant/interactions/"+snapshot.ID+"/messages", strings.NewReader(`{"mode_id":"profile","generation":1,"client_message_id":"old-tab-message","text":"这条消息来自旧标签"}`))
	request.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		ErrorCode string `json:"error_code"`
		Data      struct {
			ModeID string `json:"mode_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusConflict || body.ErrorCode != "assistant_interaction_mode_changed" || body.Data.ModeID != service.AssistantModeCreation {
		t.Fatalf("stale mode response = status %d body %#v, want mode-changed snapshot for creation", resp.StatusCode, body)
	}
	if factory.starts.Load() != 1 {
		t.Fatalf("runtime starts = %d, expected the original runtime only", factory.starts.Load())
	}
}

func TestAssistantBootstrapDoesNotStartRuntime(t *testing.T) {
	repo := newAssistantHandlerTestRepo(t)
	factory := &assistantHandlerRuntimeFactory{}
	svc := service.NewInteractiveServiceWithRuntime(repo, factory)
	svc.SetEnabled(true)
	app := newAssistantHandlerTestApp(NewAssistantHandler(svc, nil, repo))

	req := httptest.NewRequest(http.MethodGet, "/assistant/bootstrap", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("bootstrap status = %d, body = %s", resp.StatusCode, body)
	}
	var envelope struct {
		Data service.AssistantBootstrap `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Modes) != 3 || envelope.Data.DefaultModeID != service.AssistantModeProfile {
		t.Fatalf("bootstrap = %#v, want three modes and profile default", envelope.Data)
	}
	if got := factory.starts.Load(); got != 0 {
		t.Fatalf("runtime starts on bootstrap = %d, want 0", got)
	}
}

func TestAssistantWorksAndCheckpointRoutesUseSelectedScope(t *testing.T) {
	repo := newAssistantHandlerTestRepo(t)
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.Version = 1
	project := &model.Project{ID: "project-1", UserID: "user-1", Platform: "wechat", Name: "画像", Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(profile)}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.Tasks().Create(context.Background(), &model.Task{ID: fmt.Sprintf("work-%d", i), UserID: "user-1", ProjectID: project.ID, Type: "article", Status: model.TaskStatusCompleted, Title: fmt.Sprintf("作品 %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := &model.AssistantCheckpoint{
		ID: "checkpoint-1", UserID: "user-1", ModeID: service.AssistantModeReview, ProjectID: project.ID, ContentID: "work-0",
		SchemaVersion: "1.0", Payload: []byte(`{"review":{"summary":"草稿"}}`), Summary: "复盘草稿",
		Status: model.AssistantCheckpointDraft, SourceSessionID: "session-1", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.AssistantCheckpoints().UpsertBySourceSession(context.Background(), checkpoint); err != nil {
		t.Fatal(err)
	}

	svc := service.NewInteractiveServiceWithRuntime(repo, &assistantHandlerRuntimeFactory{})
	svc.SetEnabled(true)
	app := newAssistantHandlerTestApp(NewAssistantHandler(svc, nil, repo))

	worksResponse, err := app.Test(httptest.NewRequest(http.MethodGet, "/assistant/projects/project-1/works?limit=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer worksResponse.Body.Close()
	var worksEnvelope struct {
		Data service.AssistantWorkPage `json:"data"`
	}
	if err := json.NewDecoder(worksResponse.Body).Decode(&worksEnvelope); err != nil {
		t.Fatal(err)
	}
	if worksResponse.StatusCode != fiber.StatusOK || len(worksEnvelope.Data.Items) != 1 || !worksEnvelope.Data.HasMore || worksEnvelope.Data.NextCursor == nil {
		t.Fatalf("works response = %d %#v", worksResponse.StatusCode, worksEnvelope.Data)
	}

	checkpointResponse, err := app.Test(httptest.NewRequest(http.MethodGet, "/assistant/checkpoint?mode_id=review&project_id=project-1&content_id=work-0", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer checkpointResponse.Body.Close()
	var checkpointEnvelope struct {
		Data *service.AssistantCheckpointSummary `json:"data"`
	}
	if err := json.NewDecoder(checkpointResponse.Body).Decode(&checkpointEnvelope); err != nil {
		t.Fatal(err)
	}
	if checkpointResponse.StatusCode != fiber.StatusOK || checkpointEnvelope.Data == nil || checkpointEnvelope.Data.ID != checkpoint.ID {
		t.Fatalf("checkpoint response = %d %#v", checkpointResponse.StatusCode, checkpointEnvelope.Data)
	}
}

func TestAssistantCreateAcceptsModeContractAndRejectsAgentSelection(t *testing.T) {
	repo := newAssistantHandlerTestRepo(t)
	factory := &assistantHandlerRuntimeFactory{}
	svc := service.NewInteractiveServiceWithRuntime(repo, factory)
	svc.SetEnabled(true)
	app := newAssistantHandlerTestApp(NewAssistantHandler(svc, nil, repo))

	badReq := httptest.NewRequest(http.MethodPost, "/assistant/sessions", strings.NewReader(`{"mode_id":"profile","agent_id":"project-creator"}`))
	badReq.Header.Set("Content-Type", "application/json")
	badResp, err := app.Test(badReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = badResp.Body.Close()
	if badResp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("agent selection status = %d, want 400", badResp.StatusCode)
	}
	if got := factory.starts.Load(); got != 0 {
		t.Fatalf("runtime starts after invalid request = %d, want 0", got)
	}

	goodReq := httptest.NewRequest(http.MethodPost, "/assistant/sessions", strings.NewReader(`{"mode_id":"profile"}`))
	goodReq.Header.Set("Content-Type", "application/json")
	goodReq.Header.Set("Idempotency-Key", "start-profile-1")
	goodResp, err := app.Test(goodReq)
	if err != nil {
		t.Fatal(err)
	}
	defer goodResp.Body.Close()
	if goodResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(goodResp.Body)
		t.Fatalf("create status = %d, body = %s", goodResp.StatusCode, body)
	}
	deadline := time.Now().Add(time.Second)
	for factory.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := factory.starts.Load(); got != 1 {
		t.Fatalf("runtime starts after session create = %d, want 1", got)
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(goodResp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.AssistantSessions().FindByIDForUser(context.Background(), "user-1", envelope.Data.ID)
	if err != nil || persisted.Status != "starting" || persisted.ModeID != service.AssistantModeProfile {
		t.Fatalf("persisted assistant session = %#v, %v", persisted, err)
	}
	if _, err := repo.AssistantSessions().FindLeaseBySession(context.Background(), envelope.Data.ID); err != nil {
		t.Fatalf("active session lease lookup: %v", err)
	}
	if err := svc.Close("user-1", envelope.Data.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	persisted, err = repo.AssistantSessions().FindByIDForUser(context.Background(), "user-1", envelope.Data.ID)
	if err != nil || persisted.Status != "closed" || persisted.ClosedAt == nil {
		t.Fatalf("closed assistant session = %#v, %v", persisted, err)
	}
	if _, err := repo.AssistantSessions().FindLeaseBySession(context.Background(), envelope.Data.ID); err == nil {
		t.Fatal("session lease remains after close")
	}
}

func TestInteractiveErrorMapsAssistantErrorsToClientStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "mode unavailable", err: service.ErrAssistantModeUnavailable, want: fiber.StatusConflict},
		{name: "checkpoint conflict", err: service.ErrAssistantCheckpointConflict, want: fiber.StatusConflict},
		{name: "checkpoint not found", err: service.ErrAssistantCheckpointNotFound, want: fiber.StatusNotFound},
		{name: "runtime context too large", err: service.ErrAssistantRuntimeContextTooLarge, want: fiber.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/error", func(c fiber.Ctx) error { return interactiveError(c, errors.Join(errors.New("wrapped"), tc.err)) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/error", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestAssistantCheckpointResponseContainsStructuredPayload(t *testing.T) {
	repo := newAssistantHandlerTestRepo(t)
	svc := service.NewInteractiveServiceWithRuntime(repo, &assistantHandlerRuntimeFactory{})
	svc.SetEnabled(true)
	session, err := svc.CreateAssistantSessionWithIdempotency(context.Background(), "user-1", service.AssistantSessionInput{ModeID: service.AssistantModeProfile}, "structured-payload")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	h := NewAssistantHandler(svc, nil, repo)
	app := newAssistantHandlerTestApp(h)
	app.Post("/assistant/sessions/:id/checkpoint", h.Checkpoint)
	request := httptest.NewRequest(http.MethodPost, "/assistant/sessions/"+session.ID+"/checkpoint", strings.NewReader(`{"schema_version":"1.0","status":"draft","summary":"画像草稿","payload":{"project":{"name":"测试画像"},"ready":false}}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			ID            string         `json:"id"`
			Payload       map[string]any `json:"payload"`
			PayloadDigest string         `json:"payload_digest"`
			Summary       string         `json:"summary"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("checkpoint must encode payload as JSON object: %v", err)
	}
	if response.StatusCode != http.StatusOK || envelope.Data.ID != session.ID || envelope.Data.PayloadDigest == "" || envelope.Data.Summary != "画像草稿" {
		t.Fatalf("checkpoint response = %d %#v", response.StatusCode, envelope)
	}
	project, ok := envelope.Data.Payload["project"].(map[string]any)
	if !ok || project["name"] != "测试画像" || envelope.Data.Payload["ready"] != false {
		t.Fatalf("structured payload lost: %#v", envelope.Data.Payload)
	}
	stored, err := repo.AssistantCheckpoints().FindByIDForUser(context.Background(), "user-1", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(stored.Payload, &persisted); err != nil {
		t.Fatalf("persisted JSON payload invalid: %v", err)
	}
	if persisted["project"].(map[string]any)["name"] != "测试画像" {
		t.Fatalf("persisted payload = %#v", persisted)
	}
}

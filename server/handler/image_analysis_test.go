package handler

import (
	"io"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type handlerAnalysisEnqueuer struct {
	id         string
	generation int64
}

func (e *handlerAnalysisEnqueuer) EnqueueImageAnalysis(id string, generation int64) error {
	e.id, e.generation = id, generation
	return nil
}

func setupImageAnalysisHandlerTest(t *testing.T) (*fiber.App, repository.Repository, *handlerAnalysisEnqueuer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Template{}, &model.ImageAnalysisJob{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	enqueuer := &handlerAnalysisEnqueuer{}
	logger := zerolog.New(io.Discard)
	svc := service.NewImageAnalysisService(repo, nil, nil, enqueuer, nil, service.ImageAnalysisConfig{}, &logger)
	h := NewImageAnalysisHandler(svc, &logger)
	app := fiber.New()
	withUser := func(c fiber.Ctx) error {
		if userID := c.Get("X-User-ID"); userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	}
	app.Post("/image-analyses/:id/retry", withUser, h.Retry)
	app.Post("/image-analyses/:id/cancel", withUser, h.Cancel)
	return app, repo, enqueuer
}

func seedHandlerAnalysisJob(t *testing.T, repo repository.Repository, owner, status string) *model.ImageAnalysisJob {
	t.Helper()
	subjectID := uuid.NewString()
	if err := repo.Projects().Create(t.Context(), &model.Project{
		ID: subjectID, UserID: owner, Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	job := &model.ImageAnalysisJob{
		ID: uuid.NewString(), UserID: owner, Kind: model.ImageAnalysisKindProjectVisualStyle,
		SubjectType: model.ImageAnalysisSubjectProject, SubjectID: subjectID, SourceAssetID: uuid.NewString(),
		Generation: 1, Status: status, AttemptCount: 3,
	}
	if err := repo.ImageAnalyses().Create(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestImageAnalysisHandlerRetryAndCancelStateMachine(t *testing.T) {
	app, repo, enqueuer := setupImageAnalysisHandlerTest(t)
	failed := seedHandlerAnalysisJob(t, repo, "owner", model.ImageAnalysisStatusFailed)

	resp := doRequest(t, app, http.MethodPost, "/image-analyses/"+failed.ID+"/retry", "owner", nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("retry status = %d, body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["status"] != model.ImageAnalysisStatusQueued || data["attempt_count"] != float64(0) || data["can_retry"] != false {
		t.Fatalf("retry response = %#v", data)
	}
	if enqueuer.id != failed.ID || enqueuer.generation != 2 {
		t.Fatalf("enqueue = %q/%d, want %q/2", enqueuer.id, enqueuer.generation, failed.ID)
	}

	running := seedHandlerAnalysisJob(t, repo, "owner", model.ImageAnalysisStatusRunning)
	resp = doRequest(t, app, http.MethodPost, "/image-analyses/"+running.ID+"/cancel", "owner", nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("cancel status = %d, body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.ImageAnalysisStatusCancelled || persisted.Generation != 2 {
		t.Fatalf("cancelled job = %+v", persisted)
	}
}

func TestImageAnalysisHandlerRejectsUnauthorizedForeignAndInvalidTransitions(t *testing.T) {
	app, repo, _ := setupImageAnalysisHandlerTest(t)
	queued := seedHandlerAnalysisJob(t, repo, "owner", model.ImageAnalysisStatusQueued)

	tests := []struct {
		name   string
		path   string
		userID string
		want   int
	}{
		{name: "unauthorized", path: "/image-analyses/" + queued.ID + "/cancel", want: fiber.StatusUnauthorized},
		{name: "foreign", path: "/image-analyses/" + queued.ID + "/cancel", userID: "other", want: fiber.StatusForbidden},
		{name: "queued cannot retry", path: "/image-analyses/" + queued.ID + "/retry", userID: "owner", want: fiber.StatusConflict},
		{name: "missing", path: "/image-analyses/" + uuid.NewString() + "/retry", userID: "owner", want: fiber.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodPost, tt.path, tt.userID, nil)
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d; body=%v", resp.StatusCode, tt.want, decodeBody(t, resp))
			}
		})
	}
}

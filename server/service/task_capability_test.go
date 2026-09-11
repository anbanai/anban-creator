package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskServiceCreateManualFailsClosedWithoutImageCapabilityResolver(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetImageCapabilityResolver(nil)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Prompt: "春季穿搭",
	})
	if err == nil || !strings.Contains(err.Error(), "image capability resolver is unavailable") {
		t.Fatalf("CreateManual = %#v, %v; want unavailable image capability resolver rejection", tasks, err)
	}
}

func TestTaskServiceCreateFromPlanFailsClosedWithoutImageCapabilityResolver(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetImageCapabilityResolver(nil)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		ExecutionProfile: "effective", Prompt: "春季穿搭",
	}

	task, err := svc.CreateFromPlan(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "image capability resolver is unavailable") {
		t.Fatalf("CreateFromPlan = %#v, %v; want unavailable image capability resolver rejection", task, err)
	}
}

func TestTaskServiceCloneRevalidatesFrozenImageCapability(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"standard":     testImageCapabilityRoute("image.standard"),
				"professional": {Enabled: false, MinTier: "free"},
			},
		}},
	}))
	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ImageCapabilityKey: "professional",
	}
	freezeTestTaskImageCapability(t, source, "professional", testImageCapabilityRoute("image.professional"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if err == nil || !strings.Contains(err.Error(), `unknown image capability key "professional"`) {
		t.Fatalf("Clone = %#v, %v; want unavailable frozen capability rejection", clones, err)
	}
}

func TestTaskServiceCreateManualPersistsResolvedDefaultImageCapability(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "professional",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"professional": testImageCapabilityRoute("image.professional"),
			},
		}},
	}))

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Prompt: "春季穿搭",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ImageCapabilityKey != "professional" {
		t.Fatalf("created task capability = %#v, want professional", tasks)
	}
	persisted, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find persisted task: %v", err)
	}
	if persisted.ImageCapabilityKey != "professional" {
		t.Fatalf("persisted task capability = %q, want professional", persisted.ImageCapabilityKey)
	}
	snapshot := persisted.ImageCapabilitySnapshot.Data()
	if snapshot.Key != "professional" || snapshot.Digest == "" || snapshot.SchemaVersion != model.ImageCapabilitySnapshotSchemaVersion {
		t.Fatalf("persisted task capability snapshot = %#v", snapshot)
	}
}

func TestTaskServiceCreateManualMontagePersistsResolvedDefaultImageCapability(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "professional",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"professional": testImageCapabilityRoute("image.professional"),
			},
		}},
	}))

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective",
		ImageRatio: "9:16", MontageInput: &model.MontageInput{Brief: "新品发布短片"},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ImageCapabilityKey != "professional" || tasks[0].ImageRatio != "9:16" {
		t.Fatalf("created Montage image settings = %#v, want professional and 9:16", tasks)
	}
}

func TestTaskServiceCloneRejectsLegacySourceWithoutFrozenImageCapability(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"standard": testImageCapabilityRoute("image.standard"),
			},
		}},
	}))
	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ImageCapabilityKey: "",
	}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if clones != nil || err == nil || !strings.Contains(err.Error(), "frozen image capability") {
		t.Fatalf("Clone = %#v, %v; want missing frozen capability rejection", clones, err)
	}
}

func TestTaskServiceResumeRejectsLegacyTaskWithoutFrozenImageCapability(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusFailed, ImageCapabilityKey: "",
	}
	freezeTestTaskProfile(t, task)
	task.ImageCapabilityKey = ""
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "continue"})
	if resumed != nil || !errors.Is(err, ErrTaskResumeImageCapabilityMissing) {
		t.Fatalf("Resume = %#v, %v; want frozen image capability rejection", resumed, err)
	}
	persisted, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if persisted.Status != model.TaskStatusFailed || len(persisted.InputAttachments.Data()) != 0 {
		t.Fatalf("legacy task mutated by rejected resume: %#v", persisted)
	}
}

func TestTaskServiceResumeRevalidatesFrozenImageCapabilityBeforeMutation(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc.store = store
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"standard":     {Enabled: true, MinTier: "free"},
				"professional": {Enabled: false, MinTier: "free"},
			},
		}},
	}))
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusFailed, ImageCapabilityKey: "professional",
	}
	freezeTestTaskImageCapability(t, task, "professional", testImageCapabilityRoute("image.professional"))
	freezeTestTaskProfile(t, task)
	task.SetInputAttachments([]model.EntryAttachment{{Role: "brief", Text: "keep"}})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "continue",
		Files:  []ResumeTaskFile{{OriginalName: "new.txt", Reader: strings.NewReader("new")}},
	})
	if resumed != nil || err == nil || !strings.Contains(err.Error(), `unknown image capability key "professional"`) {
		t.Fatalf("Resume = %#v, %v; want disabled frozen capability rejection", resumed, err)
	}
	if len(store.files) != 0 {
		t.Fatalf("rejected resume uploaded files: %#v", store.files)
	}
	persisted, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if persisted.Status != model.TaskStatusFailed || len(persisted.InputAttachments.Data()) != 1 || persisted.InputAttachments.Data()[0].Text != "keep" {
		t.Fatalf("rejected resume mutated task: %#v", persisted)
	}
}

func TestTaskServiceListTitlesForUserChecksProjectOwnership(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)

	if _, err := svc.ListTitlesForUser(ctx, uuid.NewString(), projectID); err == nil {
		t.Fatal("ListTitlesForUser accepted a non-owner")
	}
	if _, err := svc.ListTitlesForUser(ctx, ownerID, projectID); err != nil {
		t.Fatalf("ListTitlesForUser owner: %v", err)
	}
}

func TestTaskServiceGetVisibleFilesForUserChecksTaskOwnership(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: ownerID, ProjectID: projectID, Quantity: 1, Prompt: "topic",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}

	if _, err := svc.GetVisibleFilesForUser(ctx, uuid.NewString(), tasks[0].ID); err == nil {
		t.Fatal("GetVisibleFilesForUser accepted a non-owner")
	}
	files, err := svc.GetVisibleFilesForUser(ctx, ownerID, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetVisibleFilesForUser owner: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %d, want 0", len(files))
	}
}

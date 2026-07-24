package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type taskCreationRepositoryOverride struct {
	repository.Repository
	projects   repository.ProjectRepository
	topicPools repository.TopicPoolRepository
}

func (r *taskCreationRepositoryOverride) Projects() repository.ProjectRepository {
	if r.projects != nil {
		return r.projects
	}
	return r.Repository.Projects()
}

func (r *taskCreationRepositoryOverride) TopicPools() repository.TopicPoolRepository {
	if r.topicPools != nil {
		return r.topicPools
	}
	return r.Repository.TopicPools()
}

type countingTopicPoolRepository struct {
	repository.TopicPoolRepository
	claimWithTaskCalls int
}

func (r *countingTopicPoolRepository) ClaimWithTask(ctx context.Context, userID, projectID, taskID string) (*model.TopicPool, error) {
	r.claimWithTaskCalls++
	return r.TopicPoolRepository.ClaimWithTask(ctx, userID, projectID, taskID)
}

func setupTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Plan{}, &model.Task{}, &model.TaskExecution{}, &model.User{},
		&model.LoginSession{}, &model.TaskFile{}, &model.Project{},
		&model.TopicPool{},
		&model.IlinkBinding{}, &model.IlinkNotification{}, &model.UploadSession{}, &model.Asset{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func setupTaskServiceWithEnqueuer(t *testing.T) (*TaskService, repository.Repository) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	return svc, repo
}

func TestTaskService_ResumeRequiresNASCapability(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeUnavailable) {
		t.Fatalf("Resume error = %v, want ErrTaskResumeUnavailable", err)
	}
}

// mockEnqueuer captures enqueued tasks without executing them.
type mockEnqueuer struct {
	enqueued  []string
	uniqueIDs map[string]struct{}
}

type cancelingFailTaskEnqueuer struct {
	cancel context.CancelFunc
	err    error
}

func (e cancelingFailTaskEnqueuer) Enqueue(string, []byte) error {
	e.cancel()
	return e.err
}

func (e cancelingFailTaskEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	e.cancel()
	return e.err
}

func (e cancelingFailTaskEnqueuer) EnqueueUnique(string, []byte, string) (bool, error) {
	e.cancel()
	return false, e.err
}

func (m *mockEnqueuer) Enqueue(taskType string, payload []byte) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error) {
	if m.uniqueIDs == nil {
		m.uniqueIDs = make(map[string]struct{})
	}
	if _, exists := m.uniqueIDs[uniqueKey]; exists {
		return false, nil
	}
	m.uniqueIDs[uniqueKey] = struct{}{}
	m.enqueued = append(m.enqueued, taskType)
	return true, nil
}

type resumeTestStorage struct {
	files        map[string][]byte
	deleted      []string
	uploadCount  int
	failUploadAt int
}

func (s *resumeTestStorage) Name() string { return "resume-test" }

func (s *resumeTestStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	s.uploadCount++
	if s.failUploadAt > 0 && s.uploadCount == s.failUploadAt {
		return nil, errors.New("object storage unavailable")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	s.files[key] = data
	return &storage.UploadResult{Key: key, URL: "https://storage.test/" + key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (s *resumeTestStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (s *resumeTestStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *resumeTestStorage) GetURL(key string) string { return "https://storage.test/" + key }

func (s *resumeTestStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := s.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (s *resumeTestStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.files, key)
	return nil
}

func (s *resumeTestStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *resumeTestStorage) HasCustomDomain() bool  { return true }
func (s *resumeTestStorage) IsOwnedURL(string) bool { return true }

type concurrentResumeStorage struct {
	mu      sync.Mutex
	files   map[string][]byte
	deleted []string
	uploads int
	release chan struct{}
}

func newConcurrentResumeStorage() *concurrentResumeStorage {
	return &concurrentResumeStorage{files: map[string][]byte{}, release: make(chan struct{})}
}

func (s *concurrentResumeStorage) Name() string { return "concurrent-resume-test" }

func (s *concurrentResumeStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.files[key] = data
	s.uploads++
	if s.uploads == 2 {
		close(s.release)
	}
	s.mu.Unlock()
	<-s.release
	return &storage.UploadResult{Key: key, URL: "https://storage.test/" + key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (s *concurrentResumeStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (s *concurrentResumeStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *concurrentResumeStorage) GetURL(key string) string { return "https://storage.test/" + key }
func (s *concurrentResumeStorage) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (s *concurrentResumeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, key)
	delete(s.files, key)
	return nil
}

func (s *concurrentResumeStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *concurrentResumeStorage) HasCustomDomain() bool  { return true }
func (s *concurrentResumeStorage) IsOwnedURL(string) bool { return true }

type fakePublishedTrackingService struct {
	calls []struct {
		userID string
		taskID string
	}
	err error
}

func (f *fakePublishedTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error {
	f.calls = append(f.calls, struct {
		userID string
		taskID string
	}{userID: userID, taskID: taskID})
	return f.err
}

func TestTaskService_FinalizeTitleUpdatesCanonicalTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	title, err := svc.FinalizeTitle(ctx, userID, task.ID, "  月薪5000和月薪5万的人，喝茶差距在哪  ")
	if err != nil {
		t.Fatalf("FinalizeTitle: %v", err)
	}
	if title != "月薪5000和月薪5万的人，喝茶差距在哪" {
		t.Fatalf("title = %q", title)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != title {
		t.Fatalf("stored title = %q, want %q", found.Title, title)
	}
}

func TestTaskService_FinalizeTitleRejectsInvalidTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "   ", "title is required"},
		{"too long", strings.Repeat("长", 201), "title must be <= 200 characters"},
		{"artifact", "图片内容规划", "artifact title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.FinalizeTitle(ctx, userID, task.ID, tt.title)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FinalizeTitle error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTaskService_FinalizeTitleRejectsForeignTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	projectID := createTestProject(t, repo, ownerID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    ownerID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, otherID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsUserKeyForUnownedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    "",
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, userID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateWithinProject(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	existing := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Title:     "新手咖啡豆怎么选",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	current := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	foreignProject := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	for _, task := range []*model.Task{existing, current, foreignProject} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, current.ID, " 新手 咖啡豆怎么选 ")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}

	if _, err := svc.FinalizeTitle(ctx, userID, foreignProject.ID, "新手咖啡豆怎么选"); err != nil {
		t.Fatalf("other project duplicate should be allowed: %v", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateEvenWhenCurrentTaskAlreadyHasTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-current-with-same-title",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusRunning,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, "task-current-with-same-title", "新手 咖啡豆怎么选")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}
}

func TestTaskService_CreateManualSnapshotsProjectConfig(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: DirectUploadPurposeProjectReference, StorageKey: "assets/users/" + userID + "/reference/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
	project := &model.Project{
		ID:                    uuid.New().String(),
		UserID:                userID,
		Platform:              model.PlatformArticle,
		Name:                  "旧项目名",
		Status:                model.ProjectStatusActive,
		Instructions:          "旧定位",
		Keywords:              "旧关键词",
		VisualStyle:           "旧视觉",
		ReferenceImageAssetID: asset.ID,
		ImageRatio:            "3:4",
		Writer:                "dan-koe",
		Theme:                 "autumn-warm",
		Author:                "旧署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "topic",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(tasks))
	}

	project.Name = "新项目名"
	project.Instructions = "新定位"
	project.VisualStyle = "新视觉"
	project.Author = "新署名"
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.ProjectName != "旧项目名" || snap.Instructions != "旧定位" ||
		snap.VisualStyle != "旧视觉" || snap.Author != "旧署名" {
		t.Fatalf("snapshot = %+v, want original project values", snap)
	}
	if snap.ReferenceImageAssetID != asset.ID || snap.ImageRatio != "3:4" {
		t.Fatalf("snapshot image fields = %q/%q", snap.ReferenceImageAssetID, snap.ImageRatio)
	}
}

func TestTaskService_CreateFromPlanSnapshotsProjectWithoutPlanStyleOverrides(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:           uuid.New().String(),
		UserID:       userID,
		Platform:     model.PlatformSeednote,
		Name:         "项目",
		Status:       model.ProjectStatusActive,
		Instructions: "项目定位",
		VisualStyle:  "项目视觉",
		Writer:       "project-writer",
		Theme:        "project-theme",
		Author:       "项目署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	plan := &model.Plan{
		ID:          uuid.New().String(),
		UserID:      userID,
		ProjectID:   project.ID,
		Type:        model.PlatformSeednote,
		Status:      model.PlanStatusActive,
		Prompt:      "topic",
		VisualStyle: "计划视觉不应进入任务",
		Writer:      "plan-writer",
		Theme:       "plan-theme",
		Author:      "计划署名",
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.VisualStyle != "项目视觉" || snap.Writer != "project-writer" ||
		snap.Theme != "project-theme" || snap.Author != "项目署名" {
		t.Fatalf("snapshot = %+v, want project values", snap)
	}
	if overrides := found.Overrides.Data(); overrides != (model.StyleOverrides{}) {
		t.Fatalf("overrides = %+v, want empty", overrides)
	}
	if found.PlanID == nil || *found.PlanID != plan.ID {
		t.Fatalf("plan_id = %v, want %q", found.PlanID, plan.ID)
	}
}

func TestTaskService_ListFiltersByPlanID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "项目",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	planA := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 A",
	}
	planB := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 B",
	}
	taskA, err := svc.CreateFromPlan(ctx, planA)
	if err != nil {
		t.Fatalf("CreateFromPlan A: %v", err)
	}
	if _, err := svc.CreateFromPlan(ctx, planB); err != nil {
		t.Fatalf("CreateFromPlan B: %v", err)
	}

	tasks, total, err := svc.List(ctx, userID, 0, 20, "", "", planA.ID)
	if err != nil {
		t.Fatalf("List by plan_id: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("filtered tasks len/total = %d/%d, want 1/1", len(tasks), total)
	}
	if tasks[0].ID != taskA.ID || tasks[0].PlanID == nil || *tasks[0].PlanID != planA.ID {
		t.Fatalf("filtered task = %+v, want task %s for plan %s", tasks[0], taskA.ID, planA.ID)
	}
}

func TestTaskService_CreateManualEcommerceTaskForcesSinglePackageWithoutCreditService(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-ecommerce-no-credit"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "做一组咖啡杯电商图",
		Quantity:  5,
		Ecommerce: &model.EcommerceConfig{
			SelectedModules: map[string]int{"main_images": 5},
			ProductPhotos:   []string{"https://cdn.example.com/cup.png"},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1 ecommerce package task", len(tasks))
	}
}

func TestTaskServiceCreateManualMontageStoresInputAndClampsQuantity(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	userID := "user-om"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Quantity:  3,
		MontageInput: &model.MontageInput{
			Brief:       "做一条新品发布短片",
			PipelineKey: "default",
			Preferences: model.MontagePreferences{
				AspectRatio:     "9:16",
				DurationSeconds: 30,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	got := tasks[0].MontageInput.Data()
	if got.Brief != "做一条新品发布短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskServiceCreateManualRejectsMontageInputForOtherPlatforms(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := "user-om-reject"
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "春季穿搭",
		MontageInput: &model.MontageInput{
			Brief: "错误平台",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage_input can only be set on montage tasks") {
		t.Fatalf("CreateManual error = %v, want montage input rejection", err)
	}
}

func TestTaskServiceCreateFromPlanMontageCopiesInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformMontage,
		Status:    model.PlanStatusActive,
		Prompt:    "计划提示",
	}
	plan.SetMontageInput(model.MontageInput{
		Brief:       "从计划生成发布会短片",
		PipelineKey: "default",
		Preferences: model.MontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 45,
		},
	})

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	got := task.MontageInput.Data()
	if got.Brief != "从计划生成发布会短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 45 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskService_ClearArtifactTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "图片内容规划",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "真实茶饮标题",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	count, err := svc.ClearArtifactTitles(ctx)
	if err != nil {
		t.Fatalf("ClearArtifactTitles: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	titles, err := svc.ListTitles(ctx, projectID)
	if err != nil {
		t.Fatalf("ListTitles: %v", err)
	}
	if len(titles) != 1 || titles[0] != "真实茶饮标题" {
		t.Fatalf("titles = %v, want [真实茶饮标题]", titles)
	}
}

func TestTaskService_CloneClonesCompletedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{
		ID:         uuid.New().String(),
		UserID:     userID,
		ProjectID:  projectID,
		Type:       model.PlatformArticle,
		Status:     model.TaskStatusCompleted,
		Prompt:     "finished topic",
		ImageRatio: "16:9",
		Goal:       "keep the same goal",
		GoalMode:   true,
	}
	src.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "original input", FileName: "brief.txt"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue old workspace", FileName: "latest.md"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: "resume/feedback.pdf", FileName: "feedback.pdf"},
	})
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	clone, err := svc.Clone(ctx, src.ID, CloneTaskParams{})
	if err != nil {
		t.Fatalf("Clone completed task: %v", err)
	}
	if clone.ID == src.ID {
		t.Fatal("clone reused the original task id")
	}
	if clone.Status != model.TaskStatusPending {
		t.Fatalf("clone status = %q, want %q", clone.Status, model.TaskStatusPending)
	}
	if clone.Prompt != src.Prompt || clone.ImageRatio != src.ImageRatio || clone.Goal != src.Goal || clone.GoalMode != src.GoalMode {
		t.Fatalf("clone config = prompt %q ratio %q goal %q mode %v, want source config", clone.Prompt, clone.ImageRatio, clone.Goal, clone.GoalMode)
	}
	if clone.InputSourceTaskID != src.ID {
		t.Fatalf("clone input source = %q, want %q", clone.InputSourceTaskID, src.ID)
	}
	cloneAttachments := clone.InputAttachments.Data()
	if len(cloneAttachments) != 1 || cloneAttachments[0].Role != "brief" || cloneAttachments[0].Text != "original input" {
		t.Fatalf("clone attachments = %#v, want only original task inputs", cloneAttachments)
	}
	foundSrc, err := repo.Tasks().FindByID(ctx, src.ID)
	if err != nil {
		t.Fatalf("find source task: %v", err)
	}
	if foundSrc.Status != model.TaskStatusCompleted {
		t.Fatalf("source status = %q, want completed", foundSrc.Status)
	}
	if len(foundSrc.InputAttachments.Data()) != 3 {
		t.Fatalf("source resume inputs were mutated: %#v", foundSrc.InputAttachments.Data())
	}
}

func TestTaskServiceClonePreservesRootInputSource(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{
		ID:                uuid.NewString(),
		UserID:            userID,
		ProjectID:         projectID,
		Type:              model.PlatformArticle,
		Status:            model.TaskStatusCompleted,
		Prompt:            "clone lineage",
		InputSourceTaskID: "root-task-id",
	}
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatal(err)
	}

	clone, err := svc.Clone(ctx, src.ID, CloneTaskParams{})
	if err != nil {
		t.Fatal(err)
	}
	if clone.InputSourceTaskID != "root-task-id" {
		t.Fatalf("clone input source = %q, want root-task-id", clone.InputSourceTaskID)
	}
}

func TestTaskServiceClonePreservesMontageInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	src := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformMontage,
		Status:    model.TaskStatusCompleted,
		Prompt:    "source prompt",
	}
	src.SetMontageInput(model.MontageInput{
		Brief:       "保留克隆输入",
		PipelineKey: "default",
		Preferences: model.MontagePreferences{
			AspectRatio:     "1:1",
			DurationSeconds: 20,
		},
	})
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	clone, err := svc.Clone(ctx, src.ID, CloneTaskParams{})
	if err != nil {
		t.Fatalf("Clone montage task: %v", err)
	}
	got := clone.MontageInput.Data()
	if got.Brief != "保留克隆输入" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.AspectRatio != "1:1" || got.Preferences.DurationSeconds != 20 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskService_ResumeReusesTaskAndPersistsPromptAndFiles(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc := NewTaskService(repo, enqueuer, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	completedAt := time.Now().Add(-time.Minute)
	resultJSON := `{"success":true}`
	workflowStatus := `{"current_stage":"done"}`
	task := &model.Task{
		ID:             uuid.New().String(),
		UserID:         userID,
		ProjectID:      projectID,
		Type:           model.PlatformArticle,
		Status:         model.TaskStatusCompleted,
		Prompt:         "finished topic",
		Progress:       100,
		Result:         &resultJSON,
		ErrorMessage:   "old error",
		CompletedAt:    &completedAt,
		WorkflowStatus: &workflowStatus,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "请基于现有草稿补充案例",
		Files: []ResumeTaskFile{
			{
				OriginalName: "客户 反馈.txt",
				Label:        "客户反馈",
				Reader:       strings.NewReader("feedback"),
				Size:         int64(len("feedback")),
			},
			{
				OriginalName: "客户 反馈.txt",
				Label:        "第二份反馈",
				Reader:       strings.NewReader("feedback-2"),
				Size:         int64(len("feedback-2")),
			},
		},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.ID != task.ID {
		t.Fatalf("resumed id = %q, want original %q", resumed.ID, task.ID)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	if resumed.CompletedAt != nil || resumed.Result != nil || resumed.ErrorMessage != "" || resumed.Progress != 0 || resumed.WorkflowStatus != nil {
		t.Fatalf("resume did not reset execution state: %+v", resumed)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.enqueued))
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	var latestText string
	var resumeFiles []model.EntryAttachment
	for _, attachment := range found.InputAttachments.Data() {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latestText = attachment.Text
		case model.EntryAttachmentRoleResumeFile:
			resumeFiles = append(resumeFiles, attachment)
		}
	}
	for _, want := range []string{"请基于现有草稿补充案例", "客户反馈", "客户 反馈.txt", "attachments/客户_反馈.txt", "attachments/客户_反馈_2.txt"} {
		if !strings.Contains(latestText, want) {
			t.Fatalf("resume latest missing %q:\n%s", want, latestText)
		}
	}
	if len(resumeFiles) != 2 || resumeFiles[0].FileName != "客户_反馈.txt" || resumeFiles[1].FileName != "客户_反馈_2.txt" || string(store.files[resumeFiles[0].Key]) != "feedback" || string(store.files[resumeFiles[1].Key]) != "feedback-2" {
		t.Fatalf("resume files = %#v", resumeFiles)
	}
}

func TestTaskServiceResumeRejectsUnfinishedCurrentFinalization(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusFailed, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed,
		FinalizationStatus: model.TaskExecutionFinalizationTask,
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeConflict) || resumed != nil {
		t.Fatalf("Resume unfinished finalization = task %v err %v, want nil/ErrTaskResumeConflict", resumed, err)
	}
	found, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusFailed || found.CurrentExecutionID == nil || *found.CurrentExecutionID != executionID {
		t.Fatalf("conflicting resume mutated task: status=%q current=%v", found.Status, found.CurrentExecutionID)
	}
}

func TestTaskServiceResumeClearsPreviousTerminalEvidence(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	resultJSON := `{"success":false,"cost_status":"reconciled"}`
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusFailed, CurrentExecutionID: &executionID, Result: &resultJSON,
		TerminalModelUsage: datatypes.NewJSONType([]model.ModelTokenUsage{{Provider: "provider", Model: "old", InputTokens: 9}}),
		CostStatus:         agent.CostStatusReconciled,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed,
		FinalizationStatus: model.TaskExecutionFinalizationDone,
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Result != nil || len(resumed.TerminalModelUsage.Data()) != 0 || resumed.CostStatus != "" {
		t.Fatalf("returned resumed task exposes old evidence: result=%v usage=%+v cost=%q", resumed.Result, resumed.TerminalModelUsage.Data(), resumed.CostStatus)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Result != nil || len(found.TerminalModelUsage.Data()) != 0 || found.CostStatus != "" {
		t.Fatalf("persisted resumed task exposes old evidence: result=%v usage=%+v cost=%q", found.Result, found.TerminalModelUsage.Data(), found.CostStatus)
	}
}

func TestTaskService_ResumePersistsFilesWithoutResultOrLocalWorkspace(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	store := &fakeTaskStorage{files: map[string][]byte{}}
	svc := NewTaskService(repo, enqueuer, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "finished remotely",
	}
	task.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "keep me", FileName: "brief.txt"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "old resume", FileName: "latest.md"},
	})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "继续补充 ACK 方案",
		Files: []ResumeTaskFile{{
			OriginalName: "补充 材料.txt",
			Label:        "补充材料",
			Reader:       strings.NewReader("remote resume file"),
			Size:         int64(len("remote resume file")),
		}},
	})
	if err != nil {
		t.Fatalf("Resume remote: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.enqueued))
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed: %v", err)
	}
	attachments := found.InputAttachments.Data()
	var latest, resumeFile, preserved bool
	for _, attachment := range attachments {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latest = strings.Contains(attachment.Text, "继续补充 ACK 方案") &&
				strings.Contains(attachment.Text, "attachments/补充_材料.txt")
		case model.EntryAttachmentRoleResumeFile:
			resumeFile = attachment.Key != "" && string(store.files[attachment.Key]) == "remote resume file"
		case "brief":
			preserved = attachment.Text == "keep me"
		}
		if attachment.Role == model.EntryAttachmentRoleResumeLatest && attachment.Text == "old resume" {
			t.Fatal("old resume attachment was not replaced")
		}
	}
	if !latest || !resumeFile || !preserved {
		t.Fatalf("attachments latest=%v resumeFile=%v preserved=%v: %#v", latest, resumeFile, preserved, attachments)
	}
}

func TestTaskService_ResumeStorageFailureCleansPartialUploads(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{}, failUploadAt: 2}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "继续",
		Files: []ResumeTaskFile{
			{OriginalName: "first.md", Reader: strings.NewReader("first")},
			{OriginalName: "second.md", Reader: strings.NewReader("second")},
		},
	})
	if !errors.Is(err, ErrTaskResumeStorageUnavailable) {
		t.Fatalf("Resume error = %v, want ErrTaskResumeStorageUnavailable", err)
	}
	if len(store.deleted) != 2 || len(store.files) != 0 {
		t.Fatalf("deleted = %v files = %v, want completed and failing upload keys cleaned", store.deleted, store.files)
	}
	if !strings.HasSuffix(store.deleted[0], "/first.md") || !strings.HasSuffix(store.deleted[1], "/second.md") {
		t.Fatalf("deleted keys = %v, want first.md and second.md", store.deleted)
	}
	found, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if found.Status != model.TaskStatusFailed || len(found.InputAttachments.Data()) != 0 {
		t.Fatalf("task changed after upload failure: status=%q attachments=%#v", found.Status, found.InputAttachments.Data())
	}
}

func TestTaskService_ResumeRejectsNonPortableFilenameAndCleansPartialUploads(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续", Files: []ResumeTaskFile{
		{OriginalName: "first.md", Reader: strings.NewReader("first")},
		{OriginalName: "CON.txt", Reader: strings.NewReader("reserved")},
	}})
	if err == nil {
		t.Fatal("Windows reserved resume filename accepted")
	}
	if len(store.files) != 0 || len(store.deleted) != 1 || !strings.HasSuffix(store.deleted[0], "/first.md") {
		t.Fatalf("partial portable-name failure cleanup files=%v deleted=%v", store.files, store.deleted)
	}
	found, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusFailed || len(found.InputAttachments.Data()) != 0 {
		t.Fatalf("task changed after portable-name failure: status=%q attachments=%#v", found.Status, found.InputAttachments.Data())
	}
}

func TestTaskService_ResumeDeletesSupersededResumeFilesAfterCAS(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{"resume/old.txt": []byte("old")}}
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}
	task.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "keep"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "old latest"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: "resume/old.txt", FileName: "old.txt"},
	})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "new prompt"}); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "resume/old.txt" {
		t.Fatalf("deleted = %v, want superseded resume file", store.deleted)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 2 || attachments[0].Role != "brief" || attachments[1].Role != model.EntryAttachmentRoleResumeLatest {
		t.Fatalf("attachments = %#v, want brief plus new latest", attachments)
	}
}

func TestTaskService_ResumeEnqueueFailureReturnsTaskToFailed(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	svc := NewTaskService(repo, cancelingFailTaskEnqueuer{cancel: cancel, err: errors.New("redis unavailable")}, nil, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if err == nil || !strings.Contains(err.Error(), "enqueue resumed task") {
		t.Fatalf("Resume error = %v, want enqueue failure", err)
	}
	found, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil {
		t.Fatalf("status=%q completed_at=%v, want retryable failed terminal task", found.Status, found.CompletedAt)
	}
	if !strings.Contains(found.ErrorMessage, "redis unavailable") {
		t.Fatalf("error_message = %q", found.ErrorMessage)
	}
}

func TestTaskService_ConcurrentResumeKeepsOnlyWinningUpload(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := newConcurrentResumeStorage()
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	errs := make(chan error, 2)
	for _, prompt := range []string{"first", "second"} {
		prompt := prompt
		go func() {
			_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
				Prompt: prompt,
				Files:  []ResumeTaskFile{{OriginalName: prompt + ".md", Reader: strings.NewReader(prompt)}},
			})
			errs <- err
		}()
	}
	var succeeded, conflicted int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrTaskResumeConflict):
			conflicted++
		default:
			t.Fatalf("unexpected Resume error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d, want one each", succeeded, conflicted)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.files) != 1 || len(store.deleted) != 1 {
		t.Fatalf("files=%v deleted=%v, want only winning upload", store.files, store.deleted)
	}
}

func TestTaskServiceLegacyProjectConcurrencyCapOverridesProjectLimit(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetProjectConcurrencyCap(1)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	project.MaxConcurrentTasks = 10
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}
	running := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "already running",
	}
	if err := repo.Tasks().Create(ctx, running); err != nil {
		t.Fatalf("create running task: %v", err)
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusPending,
		Prompt:    "next task",
	}
	if err := repo.Tasks().Create(ctx, pending); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	if err := svc.EnqueueExecution(ctx, pending, project); err != nil {
		t.Fatalf("EnqueueExecution: %v", err)
	}
	if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 0 {
		t.Fatalf("enqueued = %d, want 0 while legacy Kubernetes cap is 1", got)
	}
}

func TestTaskServiceRuntimeDispatcherHonorsConfiguredProjectConcurrencyCap(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetProjectConcurrencyCap(1)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.MaxConcurrentTasks = 10
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	var pending *model.Task
	for _, status := range []string{model.TaskStatusRunning, model.TaskStatusPending} {
		created := &model.Task{
			ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
			Type: model.PlatformArticle, Status: status,
		}
		if err := repo.Tasks().Create(ctx, created); err != nil {
			t.Fatal(err)
		}
		if status == model.TaskStatusPending {
			pending = created
		}
	}
	if err := svc.EnqueueExecution(ctx, pending, project); err != nil {
		t.Fatal(err)
	}
	if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 0 {
		t.Fatalf("enqueued = %d, want 0 while configured cap is reached", got)
	}
}

func TestTaskServiceProjectConcurrencyModes(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	project := &model.Project{MaxConcurrentTasks: 8}
	if got := svc.effectiveProjectMaxConcurrent(project); got != 8 {
		t.Fatalf("local/Docker configured limit = %d, want 8", got)
	}
	svc.SetProjectConcurrencyCap(1)
	if got := svc.effectiveProjectMaxConcurrent(project); got != 1 {
		t.Fatalf("configured runtime cap = %d, want 1", got)
	}
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	if got := svc.effectiveProjectMaxConcurrent(project); got != 1 {
		t.Fatalf("dispatcher configured cap = %d, want 1", got)
	}
	uncapped, _ := setupTaskServiceWithEnqueuer(t)
	uncapped.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	if got := uncapped.effectiveProjectMaxConcurrent(project); got != 8 {
		t.Fatalf("uncapped dispatcher limit = %d, want project limit 8", got)
	}
}

func TestTaskServiceResolveWorkspacePathDoesNotInspectServerFilesystem(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	for _, path := range []string{"output/cover.png", "/workspace/task-1/output/cover.png"} {
		got, ok := svc.ResolveWorkspacePath("task-1", path)
		if ok || got != path {
			t.Fatalf("ResolveWorkspacePath(%q) = %q, %v; want unchanged path and false", path, got, ok)
		}
	}
}

func TestTaskService_ResumeRejectsNoInputAndNonTerminal(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{})
	if !errors.Is(err, ErrTaskResumeNoInput) {
		t.Fatalf("Resume empty input error = %v, want ErrTaskResumeNoInput", err)
	}

	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	_, err = svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeNotTerminal) {
		t.Fatalf("Resume running task error = %v, want ErrTaskResumeNotTerminal", err)
	}
}

func TestTaskRepository_ResetTerminalTaskForResumeOnlyOneStatusSwap(t *testing.T) {
	_, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	first, err := repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID, nil)
	if err != nil {
		t.Fatalf("first ResetTerminalTaskForResume: %v", err)
	}
	if !first {
		t.Fatal("first ResetTerminalTaskForResume = false, want true")
	}
	second, err := repo.Tasks().ResetTerminalTaskForResume(ctx, task.ID, nil)
	if err != nil {
		t.Fatalf("second ResetTerminalTaskForResume: %v", err)
	}
	if second {
		t.Fatal("second ResetTerminalTaskForResume = true, want false after status changed")
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", found.Status)
	}
}

func TestTaskService_ResumePersistsPromptWithoutResultOrLocalWorkspace(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续完成原任务"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Role != model.EntryAttachmentRoleResumeLatest {
		t.Fatalf("input attachments = %#v, want one resume latest attachment", attachments)
	}
	if !strings.Contains(attachments[0].Text, "继续完成原任务") {
		t.Fatalf("resume latest = %q, want prompt", attachments[0].Text)
	}
}

func TestTaskService_CreateManual(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Test topic",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
	if task.UserID != userID {
		t.Errorf("UserID = %q, want %q", task.UserID, userID)
	}
	if task.ProjectID != projectID {
		t.Errorf("ProjectID = %q, want %q", task.ProjectID, projectID)
	}
	if task.Status != model.TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskStatusPending)
	}
	if task.Prompt != "Test topic" {
		t.Errorf("Prompt = %q, want %q", task.Prompt, "Test topic")
	}
}

func TestTaskService_CreateManual_NoProject(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    "user1",
		ProjectID: "",
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for empty project_id")
	}
}

func TestTaskService_CreateManual_WrongUser(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    "wrong-user",
		ProjectID: projectID,
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for wrong user")
	}
}

func TestTaskService_GetByID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Find me",
	})
	task := taskSlice[0]

	found, err := svc.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.ID != task.ID {
		t.Errorf("ID = %q, want %q", found.ID, task.ID)
	}
}

func TestTaskService_GetByID_NotFound(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestTaskService_Cancel(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	err := svc.Cancel(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	found, _ := svc.GetByID(context.Background(), task.ID)
	if found.Status != model.TaskStatusCancelled {
		t.Errorf("Status = %q, want %q", found.Status, model.TaskStatusCancelled)
	}
}

func TestTaskService_CancelEnqueuesIlinkNotification(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create ilink binding: %v", err)
	}
	log := zerolog.Nop()
	svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &log))

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	if err := svc.Cancel(context.Background(), task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	items, err := repo.IlinkNotifications().ListDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("outbox items = %d, want 1", len(items))
	}
	if items[0].TaskID != task.ID || items[0].TaskStatus != model.TaskStatusCancelled {
		t.Fatalf("unexpected notification: %+v", items[0])
	}
}

func TestTaskService_List(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 1",
	})
	svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 2",
	})

	tasks, total, err := svc.List(context.Background(), userID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total < 2 {
		t.Errorf("total = %d, want >= 2", total)
	}
	if len(tasks) < 2 {
		t.Errorf("len(tasks) = %d, want >= 2", len(tasks))
	}
}

func TestTaskService_List_ByStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Pending task",
	})
	task := taskSlice[0]
	svc.Cancel(context.Background(), task.ID)

	// Filter by pending — should find none
	tasks, total, err := svc.List(context.Background(), userID, 0, 10, model.TaskStatusPending, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("pending total = %d, want 0", total)
	}
	if len(tasks) != 0 {
		t.Errorf("pending tasks = %d, want 0", len(tasks))
	}

	// Filter by cancelled
	tasks, total, err = svc.List(context.Background(), userID, 0, 10, model.TaskStatusCancelled, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("cancelled total = %d, want 1", total)
	}
}

func TestTaskService_GetFiles(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	files, err := svc.GetFiles(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestTaskService_GetVisibleFilesIncludesCollectedAfterPublished(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	taskID := uuid.NewString()
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "successful", State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md"},
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
	}); err != nil {
		t.Fatal(err)
	}

	published, err := svc.GetFiles(ctx, taskID)
	if err != nil || len(published) != 1 || published[0].State != model.TaskFileStatePublished {
		t.Fatalf("published files = %#v, err=%v", published, err)
	}
	visible, err := svc.GetVisibleFiles(ctx, taskID)
	if err != nil {
		t.Fatalf("GetVisibleFiles: %v", err)
	}
	if len(visible) != 2 || visible[0].State != model.TaskFileStatePublished || visible[1].State != model.TaskFileStateCollected {
		t.Fatalf("visible files = %#v", visible)
	}
}

func TestTaskServiceCollectedFileIsDownloadableButExcludedFromZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = store
	ctx := context.Background()
	taskID := uuid.NewString()
	publishedUpload, err := store.Upload(ctx, "tasks/"+taskID+"/content.md", strings.NewReader("published"), "text/markdown")
	if err != nil {
		t.Fatal(err)
	}
	collectedUpload, err := store.Upload(ctx, "tasks/"+taskID+"/failure-state.json", strings.NewReader("failure"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	collectedID := uuid.NewString()
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "successful", State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md", OSSKey: publishedUpload.Key, FileSize: publishedUpload.Size, StorageProvider: store.Name()},
		{ID: collectedID, TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json", OSSKey: collectedUpload.Key, FileSize: collectedUpload.Size, StorageProvider: store.Name()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyFileBelongsToTask(ctx, taskID, collectedID); err != nil {
		t.Fatalf("VerifyFileBelongsToTask: %v", err)
	}
	stream, file, err := svc.GetFileStream(ctx, collectedID)
	if err != nil {
		t.Fatalf("GetFileStream: %v", err)
	}
	data, readErr := io.ReadAll(stream)
	stream.Close()
	if readErr != nil || string(data) != "failure" || file.State != model.TaskFileStateCollected {
		t.Fatalf("downloaded collected file=%#v data=%q err=%v", file, data, readErr)
	}

	buf, _, err := svc.DownloadZip(ctx, taskID)
	if err != nil {
		t.Fatalf("DownloadZip: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "content.md" {
		t.Fatalf("zip files = %#v, want only content.md", reader.File)
	}
}

func TestTaskService_SetPublishedCreatesSeednoteTracking(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished: %v", err)
	}

	if len(trackingSvc.calls) != 1 {
		t.Fatalf("tracking calls = %d, want 1", len(trackingSvc.calls))
	}
	if trackingSvc.calls[0].userID != userID || trackingSvc.calls[0].taskID != task.ID {
		t.Fatalf("tracking call = %+v", trackingSvc.calls[0])
	}
}

func TestTaskService_SetPublishedSkipsTrackingForNonSeednoteOrUnpublish(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished article: %v", err)
	}
	if err := svc.SetPublished(ctx, userID, task.ID, false); err != nil {
		t.Fatalf("SetPublished false: %v", err)
	}
	if len(trackingSvc.calls) != 0 {
		t.Fatalf("tracking calls = %d, want 0", len(trackingSvc.calls))
	}
}

func TestTaskService_DownloadTasksZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, otherUserID, model.PlatformSeednote)

	completed := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "A finished task",
		Title:     "Finished",
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
		Prompt:    "A pending task",
	}
	foreign := &model.Task{
		ID:        uuid.New().String(),
		UserID:    otherUserID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Foreign task",
	}
	for _, task := range []*model.Task{completed, pending, foreign} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	if _, err := svc.UploadTaskFileFromReader(ctx, completed.ID, userID, "output/article.md", strings.NewReader("# hello"), "text/markdown", 7); err != nil {
		t.Fatalf("upload completed task file: %v", err)
	}
	if _, err := svc.UploadTaskFileFromReader(ctx, foreign.ID, otherUserID, "output/secret.md", strings.NewReader("secret"), "text/markdown", 6); err != nil {
		t.Fatalf("upload foreign task file: %v", err)
	}

	buf, zipName, err := svc.DownloadTasksZip(ctx, userID, []string{completed.ID, pending.ID, foreign.ID})
	if err != nil {
		t.Fatalf("DownloadTasksZip: %v", err)
	}
	if !strings.HasPrefix(zipName, "tasks_export_") || !strings.HasSuffix(zipName, ".zip") {
		t.Fatalf("unexpected zip name: %s", zipName)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	var hasManifest bool
	var hasCompletedFile bool
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			hasManifest = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			manifest := string(data)
			var parsedManifest BulkDownloadZipManifest
			if err := json.Unmarshal(data, &parsedManifest); err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			for _, want := range []string{completed.ID, pending.ID, foreign.ID, "task_not_completed", "unavailable"} {
				if !strings.Contains(manifest, want) {
					t.Fatalf("manifest missing %q: %s", want, manifest)
				}
			}
			if strings.Contains(manifest, "forbidden") || strings.Contains(manifest, "task_not_found") {
				t.Fatalf("manifest uses distinguishable unavailable reasons: %s", manifest)
			}
			for _, task := range parsedManifest.Tasks {
				if task.TaskID == foreign.ID && (task.Title != "" || task.Status != "") {
					t.Fatalf("manifest leaked foreign task metadata: %+v", task)
				}
			}
		}
		if strings.HasSuffix(file.Name, "output/article.md") {
			hasCompletedFile = true
		}
		if strings.Contains(file.Name, "secret.md") {
			t.Fatalf("foreign file leaked into zip: %s", file.Name)
		}
	}
	if !hasManifest {
		t.Fatal("manifest.json missing")
	}
	if !hasCompletedFile {
		t.Fatal("completed task file missing")
	}
}

func TestTaskService_RebuildWorkflowStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "workflow task",
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	files := []struct {
		path    string
		body    string
		mime    string
		size    int64
		wantErr bool
	}{
		{"output/03-draft.md", "# Draft", "text/markdown", 7, false},
		{"output/04-final.md", "# Final", "text/markdown", 7, false},
		{"output/review.json", `{"overall_score":91,"readiness":"ready","strengths":["清晰"],"risks":[],"next_actions":["发布"]}`, "application/json", 92, false},
	}
	for _, file := range files {
		if _, err := svc.UploadTaskFileFromReader(ctx, task.ID, userID, file.path, strings.NewReader(file.body), file.mime, file.size); err != nil {
			t.Fatalf("upload %s: %v", file.path, err)
		}
	}

	if err := svc.RebuildWorkflowStatus(ctx, task.ID); err != nil {
		t.Fatalf("RebuildWorkflowStatus: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.WorkflowStatus == nil || *found.WorkflowStatus == "" {
		t.Fatal("workflow_status was not persisted")
	}

	var status WorkflowStatus
	if err := json.Unmarshal([]byte(*found.WorkflowStatus), &status); err != nil {
		t.Fatalf("unmarshal workflow status: %v", err)
	}
	if status.Review == nil {
		t.Fatal("expected review summary")
	}
	if status.Review.OverallScore != 91 {
		t.Fatalf("review score = %d, want 91", status.Review.OverallScore)
	}
}

func TestCreateManualClonesInputAttachments(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	input := []model.EntryAttachment{{
		Type:        "image",
		URL:         "/api/v1/files/product.png",
		FileName:    "product.png",
		ContentType: "image/png",
		Instruction: "保持包装和 Logo",
	}}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID:           userID,
		ProjectID:        projectID,
		Prompt:           "生成种草图文",
		Quantity:         1,
		InputAttachments: input,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	input[0].Instruction = "调用方后续修改"
	input[0].FileName = "mutated.png"

	returned := tasks[0].InputAttachments.Data()
	if len(returned) != 1 || returned[0].Instruction != "保持包装和 Logo" || returned[0].FileName != "product.png" {
		t.Fatalf("returned attachment snapshot = %#v", returned)
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	stored := found.InputAttachments.Data()
	if len(stored) != 1 || stored[0].Instruction != "保持包装和 Logo" || stored[0].FileName != "product.png" {
		t.Fatalf("persisted attachment snapshot = %#v", stored)
	}
}

func TestCreateFromPlanClonesAttachmentSnapshotPerTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "plantasksnapshot",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		CronExpr:  "0 9 * * *",
		Prompt:    "根据产品参考图生成种草内容",
	}
	plan.SetInputAttachments([]model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/product.png", Instruction: "保留原始包装",
	}})
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	taskA, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan A: %v", err)
	}
	taskB, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan B: %v", err)
	}

	plan.SetInputAttachments([]model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/product.png", Instruction: "计划后来修改",
	}})
	if err := repo.Plans().Update(ctx, plan); err != nil {
		t.Fatalf("update plan: %v", err)
	}

	for label, taskID := range map[string]string{"task A": taskA.ID, "task B": taskB.ID} {
		stored, err := repo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			t.Fatalf("find %s: %v", label, err)
		}
		got := stored.InputAttachments.Data()
		if len(got) != 1 || got[0].Instruction != "保留原始包装" {
			t.Fatalf("%s attachments = %#v, want original plan snapshot", label, got)
		}
	}
}

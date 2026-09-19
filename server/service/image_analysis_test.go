package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type imageAnalysisEnqueuerFake struct {
	calls []imageAnalysisEnqueueCall
	err   error
}

type imageAnalysisEnqueueCall struct {
	id         string
	generation int64
}

func (f *imageAnalysisEnqueuerFake) EnqueueImageAnalysis(id string, generation int64) error {
	f.calls = append(f.calls, imageAnalysisEnqueueCall{id: id, generation: generation})
	return f.err
}

type imageAnalysisStorageFake struct{ data map[string][]byte }

func (s *imageAnalysisStorageFake) Name() string { return "fake" }
func (s *imageAnalysisStorageFake) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (s *imageAnalysisStorageFake) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (s *imageAnalysisStorageFake) UploadURL(context.Context, string, string, int) (string, error) {
	return "", nil
}
func (s *imageAnalysisStorageFake) GetURL(string) string { return "" }
func (s *imageAnalysisStorageFake) Read(_ context.Context, key string) ([]byte, error) {
	return s.data[key], nil
}
func (s *imageAnalysisStorageFake) Delete(context.Context, string) error { return nil }
func (s *imageAnalysisStorageFake) DownloadURL(context.Context, string, int) (string, error) {
	return "", nil
}
func (s *imageAnalysisStorageFake) HasCustomDomain() bool  { return false }
func (s *imageAnalysisStorageFake) IsOwnedURL(string) bool { return false }

type imageAnalysisLLMFake struct {
	imageResult string
	textResult  string
	err         error
}

func (f *imageAnalysisLLMFake) Complete(context.Context, string, string) (string, error) {
	return f.textResult, f.err
}
func (f *imageAnalysisLLMFake) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return f.imageResult, f.err
}

func setupImageAnalysisService(t *testing.T) (*ImageAnalysisService, repository.Repository, *imageAnalysisEnqueuerFake, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Asset{}, &model.Project{}, &model.Template{}, &model.ImageAnalysisJob{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	enqueuer := &imageAnalysisEnqueuerFake{}
	logger := zerolog.New(io.Discard)
	svc := NewImageAnalysisService(repo, &imageAnalysisStorageFake{data: map[string][]byte{"image.png": {'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}}}, &imageAnalysisLLMFake{
		imageResult: "整体氛围：清透\n色彩色调：低饱和\n画面质感：细腻\n构图手法：居中\n光影特征：柔光\n信息密度：留白",
		textResult:  `{"allowed":true}`,
	}, enqueuer, nil, ImageAnalysisConfig{LeaseDuration: time.Minute}, &logger)
	return svc, repo, enqueuer, db
}

func seedAnalysisAsset(t *testing.T, repo repository.Repository, userID, purpose string) *model.Asset {
	t.Helper()
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: purpose, StorageKey: "image.png", FileName: "image.png", ContentType: "image/png", Size: 8, ETag: "etag"}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	return asset
}

func seedReplacementAnalysisAsset(t *testing.T, repo repository.Repository, userID, purpose string) *model.Asset {
	t.Helper()
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: purpose, StorageKey: uuid.NewString() + ".png", FileName: "replacement.png", ContentType: "image/png", Size: 8, ETag: uuid.NewString()}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestImageAnalysisCreateProjectJobAndEnqueueFailureKeepsQueued(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	enqueuer.err = context.DeadlineExceeded
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID

	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatalf("CreateProjectWithJob: %v", err)
	}
	if job == nil || job.Status != model.ImageAnalysisStatusQueued || job.Generation != 1 {
		t.Fatalf("job = %+v", job)
	}
	if _, err := repo.Projects().FindByID(t.Context(), project.ID); err != nil {
		t.Fatalf("project was rolled back after enqueue failure: %v", err)
	}
}

func TestImageAnalysisOldGenerationCannotOverwriteManualProjectStyle(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelForManualEdit(t.Context(), project.UserID, job.ID); err != nil {
		t.Fatal(err)
	}
	project, _ = repo.Projects().FindByID(t.Context(), project.ID)
	project.VisualStyle = "人工风格"
	project.VisualStyleSource = model.ImageAnalysisSourceManual
	if err := repo.Projects().Update(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	if err := svc.Process(t.Context(), job.ID, 1); err != nil {
		t.Fatalf("stale Process returned error: %v", err)
	}
	project, _ = repo.Projects().FindByID(t.Context(), project.ID)
	if project.VisualStyle != "人工风格" {
		t.Fatalf("visual_style = %q, want manual value", project.VisualStyle)
	}
}

func TestImageAnalysisTemplateActivatesOnlyAfterSuccessfulAnalysis(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	owner := uuid.NewString()
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := repo.Templates().FindByID(t.Context(), tmpl.ID)
	if persisted.IsActive || persisted.ReadinessStatus != model.TemplateReadinessAnalyzing {
		t.Fatalf("before analysis template = %+v", persisted)
	}
	if err := svc.Process(t.Context(), job.ID, job.Generation); err != nil {
		t.Fatal(err)
	}
	persisted, _ = repo.Templates().FindByID(t.Context(), tmpl.ID)
	if !persisted.IsActive || persisted.ReadinessStatus != model.TemplateReadinessReady || persisted.Prompt == "" {
		t.Fatalf("after analysis template = %+v", persisted)
	}
}

func TestImageAnalysisRecoverRequeuesQueuedAndExpiredRunningJobs(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	queued := newImageAnalysisJob("user-1", model.ImageAnalysisSubjectProject, uuid.NewString(), model.ImageAnalysisKindProjectVisualStyle, uuid.NewString())
	if err := repo.ImageAnalyses().Create(t.Context(), queued); err != nil {
		t.Fatal(err)
	}
	expired := newImageAnalysisJob("user-2", model.ImageAnalysisSubjectTemplate, uuid.NewString(), model.ImageAnalysisKindTemplatePrompt, uuid.NewString())
	expired.Status = model.ImageAnalysisStatusRunning
	expired.AttemptCount = 1
	lease := now.Add(-time.Minute)
	expired.LeaseExpiresAt = &lease
	if err := repo.ImageAnalyses().Create(t.Context(), expired); err != nil {
		t.Fatal(err)
	}

	if err := svc.Recover(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if len(enqueuer.calls) != 2 {
		t.Fatalf("enqueue calls = %+v, want queued and expired jobs", enqueuer.calls)
	}
	recovered, err := repo.ImageAnalyses().FindByID(t.Context(), expired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != 2 || recovered.Status != model.ImageAnalysisStatusQueued || recovered.AttemptCount != 1 || recovered.EnqueuedAt == nil {
		t.Fatalf("recovered expired job = %+v", recovered)
	}
}

func TestImageAnalysisRecoverRequeuesStaleEnqueuedJobWithNewGeneration(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.config.EnqueueStaleAfter = time.Minute

	job := newImageAnalysisJob("user-1", model.ImageAnalysisSubjectProject, uuid.NewString(), model.ImageAnalysisKindProjectVisualStyle, uuid.NewString())
	stale := now.Add(-2 * time.Minute)
	job.EnqueuedAt = &stale
	if err := repo.ImageAnalyses().Create(t.Context(), job); err != nil {
		t.Fatal(err)
	}

	if err := svc.Recover(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Generation != 2 || persisted.Status != model.ImageAnalysisStatusQueued || persisted.EnqueuedAt == nil || !persisted.EnqueuedAt.Equal(now) {
		t.Fatalf("recovered stale queued job = %+v", persisted)
	}
	if len(enqueuer.calls) != 1 || enqueuer.calls[0].generation != 2 {
		t.Fatalf("enqueue calls = %+v, want generation 2", enqueuer.calls)
	}
}

func TestImageAnalysisRecoverLeavesFreshEnqueuedJobAlone(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.config.EnqueueStaleAfter = time.Minute

	job := newImageAnalysisJob("user-1", model.ImageAnalysisSubjectProject, uuid.NewString(), model.ImageAnalysisKindProjectVisualStyle, uuid.NewString())
	fresh := now.Add(-30 * time.Second)
	job.EnqueuedAt = &fresh
	if err := repo.ImageAnalyses().Create(t.Context(), job); err != nil {
		t.Fatal(err)
	}

	if err := svc.Recover(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Generation != job.Generation || persisted.EnqueuedAt == nil || !persisted.EnqueuedAt.Equal(fresh) {
		t.Fatalf("fresh queued job changed = %+v", persisted)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueue calls = %+v, want none", enqueuer.calls)
	}
}

func TestImageAnalysisRecoverFailsExpiredJobAfterAttemptBudget(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	job := newImageAnalysisJob("user-1", model.ImageAnalysisSubjectProject, uuid.NewString(), model.ImageAnalysisKindProjectVisualStyle, uuid.NewString())
	job.Status = model.ImageAnalysisStatusRunning
	job.AttemptCount = imageAnalysisMaxAttempts
	lease := now.Add(-time.Minute)
	job.LeaseExpiresAt = &lease
	if err := repo.ImageAnalyses().Create(t.Context(), job); err != nil {
		t.Fatal(err)
	}

	if err := svc.Recover(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.ImageAnalysisStatusFailed || persisted.AttemptCount != imageAnalysisMaxAttempts || persisted.CompletedAt == nil {
		t.Fatalf("expired exhausted job = %+v", persisted)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueue calls = %+v, want none", enqueuer.calls)
	}
}

func TestImageAnalysisRecoverFailsExpiredTemplateAndMarksReadiness(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	owner := uuid.NewString()
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = model.ImageAnalysisStatusRunning
	job.AttemptCount = imageAnalysisMaxAttempts
	lease := now.Add(-time.Minute)
	job.LeaseExpiresAt = &lease
	if err := repo.ImageAnalyses().Update(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	enqueuer.calls = nil

	if err := svc.Recover(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.Templates().FindByID(t.Context(), tmpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ReadinessStatus != model.TemplateReadinessFailed || persisted.IsActive {
		t.Fatalf("template after exhausted lease = %+v", persisted)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueue calls = %+v, want none", enqueuer.calls)
	}
}

func TestImageAnalysisTemplateLookupErrorsRollbackTransitions(t *testing.T) {
	tests := []struct {
		name       string
		prepareJob func(*testing.T, *ImageAnalysisService, repository.Repository, *model.ImageAnalysisJob) *model.ImageAnalysisJob
		transition func(context.Context, *ImageAnalysisService, string, *model.ImageAnalysisJob) error
		wantStatus string
	}{
		{
			name: "cancel",
			prepareJob: func(_ *testing.T, _ *ImageAnalysisService, _ repository.Repository, job *model.ImageAnalysisJob) *model.ImageAnalysisJob {
				return job
			},
			transition: func(ctx context.Context, svc *ImageAnalysisService, owner string, job *model.ImageAnalysisJob) error {
				return svc.CancelForManualEdit(ctx, owner, job.ID)
			},
			wantStatus: model.ImageAnalysisStatusQueued,
		},
		{
			name: "retry",
			prepareJob: func(t *testing.T, _ *ImageAnalysisService, repo repository.Repository, job *model.ImageAnalysisJob) *model.ImageAnalysisJob {
				job.Status = model.ImageAnalysisStatusFailed
				if err := repo.ImageAnalyses().Update(t.Context(), job); err != nil {
					t.Fatal(err)
				}
				return job
			},
			transition: func(ctx context.Context, svc *ImageAnalysisService, owner string, job *model.ImageAnalysisJob) error {
				_, err := svc.Retry(ctx, owner, job.ID)
				return err
			},
			wantStatus: model.ImageAnalysisStatusFailed,
		},
		{
			name: "terminal failure",
			prepareJob: func(t *testing.T, svc *ImageAnalysisService, repo repository.Repository, job *model.ImageAnalysisJob) *model.ImageAnalysisJob {
				job.AttemptCount = imageAnalysisMaxAttempts - 1
				if err := repo.ImageAnalyses().Update(t.Context(), job); err != nil {
					t.Fatal(err)
				}
				claimed, ok, err := svc.claim(t.Context(), job.ID, job.Generation)
				if err != nil || !ok {
					t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
				}
				return claimed
			},
			transition: func(ctx context.Context, svc *ImageAnalysisService, _ string, job *model.ImageAnalysisJob) error {
				return svc.finishFailure(ctx, job, errors.New("provider unavailable"))
			},
			wantStatus: model.ImageAnalysisStatusRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, db := setupImageAnalysisService(t)
			owner := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: owner, Email: owner + "@example.com", Password: "x", InviteCode: strings.ToUpper(strings.ReplaceAll(owner, "-", ""))[:8], IsAdmin: true}); err != nil {
				t.Fatal(err)
			}
			asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
			tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
			job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
			if err != nil {
				t.Fatal(err)
			}
			job = tt.prepareJob(t, svc, repo, job)
			wantErr := errors.New("template lookup unavailable")
			if err := db.Callback().Query().Before("gorm:query").Register("test:fail_template_lookup", func(tx *gorm.DB) {
				if tx.Statement.Table == "templates" {
					tx.AddError(wantErr)
				}
			}); err != nil {
				t.Fatal(err)
			}

			if err := tt.transition(t.Context(), svc, owner, job); !errors.Is(err, wantErr) {
				t.Fatalf("transition error = %v, want %v", err, wantErr)
			}
			persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", persisted.Status, tt.wantStatus)
			}
		})
	}
}

func TestImageAnalysisTemplateActionsAllowAnyAdminAndRejectNonAdmin(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	ownerID := uuid.NewString()
	adminID := uuid.NewString()
	memberID := uuid.NewString()
	users := []*model.User{
		{ID: ownerID, Email: ownerID + "@example.com", Password: "x", InviteCode: "ANOWNER1", IsAdmin: true},
		{ID: adminID, Email: adminID + "@example.com", Password: "x", InviteCode: "ANADMIN1", IsAdmin: true},
		{ID: memberID, Email: memberID + "@example.com", Password: "x", InviteCode: "ANMEMBER", IsAdmin: false},
	}
	for _, user := range users {
		if err := repo.Users().Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	asset := seedAnalysisAsset(t, repo, ownerID, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: ownerID, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.CancelForManualEdit(t.Context(), adminID, job.ID); err != nil {
		t.Fatalf("other admin cancel: %v", err)
	}
	job, err = svc.Retry(t.Context(), adminID, job.ID)
	if err != nil {
		t.Fatalf("other admin retry: %v", err)
	}
	if job.Status != model.ImageAnalysisStatusQueued {
		t.Fatalf("status after admin retry = %s", job.Status)
	}

	if err := svc.CancelForManualEdit(t.Context(), memberID, job.ID); !errors.Is(err, ErrImageAnalysisForbidden) {
		t.Fatalf("member cancel error = %v, want forbidden", err)
	}
	job.Status = model.ImageAnalysisStatusFailed
	if err := repo.ImageAnalyses().Update(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Retry(t.Context(), memberID, job.ID); !errors.Is(err, ErrImageAnalysisForbidden) {
		t.Fatalf("member retry error = %v, want forbidden", err)
	}
}

func TestImageAnalysisThirdFailurePersistsSafeError(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	svc.client = &imageAnalysisLLMFake{err: errors.New("provider secret: upstream exploded")}
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= imageAnalysisMaxAttempts; attempt++ {
		err = svc.Process(t.Context(), job.ID, job.Generation)
		if attempt < imageAnalysisMaxAttempts && err == nil {
			t.Fatalf("attempt %d returned nil before terminal failure", attempt)
		}
	}
	if err != nil {
		t.Fatalf("terminal attempt returned %v, want persisted failure without retry", err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.ImageAnalysisStatusFailed || persisted.AttemptCount != imageAnalysisMaxAttempts {
		t.Fatalf("job = %+v", persisted)
	}
	if persisted.ErrorCode != "analysis_failed" || persisted.ErrorMessage != "图片识别失败，请稍后重试" || strings.Contains(persisted.ErrorMessage, "provider secret") {
		t.Fatalf("unsafe terminal error = %q / %q", persisted.ErrorCode, persisted.ErrorMessage)
	}
}

func TestImageAnalysisChangedProjectCannotReceiveClaimedResult(t *testing.T) {
	tests := []struct {
		name   string
		change func(*model.Project)
	}{
		{name: "source asset changed", change: func(project *model.Project) { project.ReferenceImageAssetID = uuid.NewString() }},
		{name: "manual style entered", change: func(project *model.Project) {
			project.VisualStyle = "人工视觉风格"
			project.VisualStyleSource = model.ImageAnalysisSourceManual
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, _ := setupImageAnalysisService(t)
			project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
			asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
			project.ReferenceImageAssetID = asset.ID
			job, err := svc.CreateProjectWithJob(t.Context(), project)
			if err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := svc.claim(t.Context(), job.ID, job.Generation)
			if err != nil || !ok {
				t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
			}
			persisted, _ := repo.Projects().FindByID(t.Context(), project.ID)
			tt.change(persisted)
			if err := repo.Projects().Update(t.Context(), persisted); err != nil {
				t.Fatal(err)
			}
			if err := svc.finishSuccess(t.Context(), claimed, "自动视觉风格"); err != nil {
				t.Fatal(err)
			}
			finished, _ := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
			if finished.Status != model.ImageAnalysisStatusSuperseded {
				t.Fatalf("status = %s, want superseded", finished.Status)
			}
			actual, _ := repo.Projects().FindByID(t.Context(), project.ID)
			if actual.VisualStyle == "自动视觉风格" {
				t.Fatal("stale result overwrote changed project")
			}
		})
	}
}

func TestImageAnalysisChangingProjectAssetRestartsGeneratedStyle(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	firstAsset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = firstAsset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Process(t.Context(), job.ID, job.Generation); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	generatedStyle := persisted.VisualStyle
	secondAsset := seedReplacementAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	persisted.ReferenceImageAssetID = secondAsset.ID
	persisted.ReferenceImageSet = true

	restarted, err := svc.UpdateProject(t.Context(), persisted, firstAsset.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if restarted == nil || restarted.Status != model.ImageAnalysisStatusQueued || restarted.Generation != 2 || restarted.PreviousResult != generatedStyle {
		t.Fatalf("restarted job = %+v", restarted)
	}
	if len(enqueuer.calls) < 2 || enqueuer.calls[len(enqueuer.calls)-1].generation != 2 {
		t.Fatalf("enqueue calls = %+v", enqueuer.calls)
	}
}

func TestImageAnalysisChangingProjectAssetPreservesManualStyle(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive, VisualStyle: "人工风格", VisualStyleSource: model.ImageAnalysisSourceManual}
	firstAsset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = firstAsset.ID
	if err := repo.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	secondAsset := seedReplacementAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = secondAsset.ID
	project.ReferenceImageSet = true

	restarted, err := svc.UpdateProject(t.Context(), project, firstAsset.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if restarted != nil {
		t.Fatalf("restarted job = %+v, want nil", restarted)
	}
	persisted, err := repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.VisualStyle != "人工风格" || persisted.VisualStyleSource != model.ImageAnalysisSourceManual {
		t.Fatalf("project style = %q/%q", persisted.VisualStyle, persisted.VisualStyleSource)
	}
}

func TestImageAnalysisChangingTemplateAssetRestartsGeneratedPrompt(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	logger := zerolog.New(io.Discard)
	templates := NewTemplateService(repo, &logger)
	templates.SetImageAnalysisService(svc)
	owner := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: owner, Email: owner + "@example.com", Password: "x", InviteCode: "TMPLASST", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	firstAsset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: firstAsset.ID, ActivateWhenReady: true}
	job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Process(t.Context(), job.ID, job.Generation); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.Templates().FindByID(t.Context(), tmpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	generatedPrompt := persisted.Prompt
	secondAsset := seedReplacementAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	updated, err := templates.UpdatePatch(t.Context(), tmpl.ID, owner, TemplatePatch{ThumbnailAssetID: &secondAsset.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageAnalysis == nil || updated.ImageAnalysis.Status != model.ImageAnalysisStatusQueued {
		t.Fatalf("image analysis = %+v", updated.ImageAnalysis)
	}
	restarted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Generation != 2 || restarted.PreviousResult != generatedPrompt {
		t.Fatalf("restarted job = %+v", restarted)
	}
	if updated.ReadinessStatus != model.TemplateReadinessAnalyzing || updated.IsActive {
		t.Fatalf("updated template = %+v", updated)
	}
	if len(enqueuer.calls) < 2 || enqueuer.calls[len(enqueuer.calls)-1].generation != 2 {
		t.Fatalf("enqueue calls = %+v", enqueuer.calls)
	}
}

func TestImageAnalysisManualTemplatePromptPreservesActivationIntent(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	logger := zerolog.New(io.Discard)
	templates := NewTemplateService(repo, &logger)
	templates.SetImageAnalysisService(svc)
	owner := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: owner, Email: owner + "@example.com", Password: "x", InviteCode: "TMPLMANU", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	if _, err := svc.CreateTemplateWithJob(t.Context(), tmpl); err != nil {
		t.Fatal(err)
	}
	prompt := "人工模板提示词"
	updated, err := templates.UpdatePatch(t.Context(), tmpl.ID, owner, TemplatePatch{Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ActivateWhenReady || !updated.IsActive || updated.ReadinessStatus != model.TemplateReadinessReady {
		t.Fatalf("updated template = %+v", updated)
	}
}

func TestImageAnalysisFinishSuccessReturnsSubjectLookupFailure(t *testing.T) {
	svc, repo, _, db := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := svc.claim(t.Context(), job.ID, job.Generation)
	if err != nil || !ok {
		t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
	}
	wantErr := errors.New("project lookup unavailable")
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_project_lookup", func(tx *gorm.DB) {
		if tx.Statement.Table == "projects" {
			tx.AddError(wantErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.finishSuccess(t.Context(), claimed, "自动视觉风格"); !errors.Is(err, wantErr) {
		t.Fatalf("finishSuccess error = %v, want %v", err, wantErr)
	}
}

func TestImageAnalysisDeletedTemplateSupersedesClaimedResult(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	owner := uuid.NewString()
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	job, err := svc.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := svc.claim(t.Context(), job.ID, job.Generation)
	if err != nil || !ok {
		t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
	}
	if err := repo.Templates().Delete(t.Context(), tmpl.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.finishSuccess(t.Context(), claimed, "自动模板提示词"); err != nil {
		t.Fatal(err)
	}
	finished, _ := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if finished.Status != model.ImageAnalysisStatusSuperseded {
		t.Fatalf("status = %s, want superseded", finished.Status)
	}
}

func TestImageAnalysisDeletingProjectSupersedesClaimedResult(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := svc.claim(t.Context(), job.ID, job.Generation)
	if err != nil || !ok {
		t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
	}
	acquired, err := repo.Projects().BeginDelete(t.Context(), project.ID)
	if err != nil || !acquired {
		t.Fatalf("BeginDelete = %v, %v", acquired, err)
	}
	if err := svc.finishSuccess(t.Context(), claimed, "不应写入的自动视觉风格"); err != nil {
		t.Fatal(err)
	}
	finished, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != model.ImageAnalysisStatusSuperseded {
		t.Fatalf("status = %s, want superseded", finished.Status)
	}
	persisted, err := repo.Projects().FindByID(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.VisualStyle != "" {
		t.Fatalf("visual_style = %q, want unchanged", persisted.VisualStyle)
	}
}

func TestImageAnalysisCancelledJobCanRetryWithNewGeneration(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelForManualEdit(t.Context(), project.UserID, job.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := svc.Retry(t.Context(), project.UserID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Generation != 3 || retried.Status != model.ImageAnalysisStatusQueued || retried.AttemptCount != 0 {
		t.Fatalf("retried job = %+v", retried)
	}
	last := enqueuer.calls[len(enqueuer.calls)-1]
	if last.id != job.ID || last.generation != 3 {
		t.Fatalf("last enqueue = %+v", last)
	}
}

func TestImageAnalysisUnrelatedProjectUpdateDoesNotRestartActiveJob(t *testing.T) {
	svc, repo, enqueuer, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	project.Name = "只修改名称"
	if _, err := svc.UpdateProject(t.Context(), project, "", false); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Generation != job.Generation || len(enqueuer.calls) != 1 {
		t.Fatalf("job restarted on unrelated update: generation=%d enqueues=%+v", persisted.Generation, enqueuer.calls)
	}
}

func TestImageAnalysisUnrelatedTemplateUpdatePreservesActiveJobState(t *testing.T) {
	analyses, repo, enqueuer, _ := setupImageAnalysisService(t)
	logger := zerolog.New(io.Discard)
	templates := NewTemplateService(repo, &logger)
	templates.SetImageAnalysisService(analyses)
	owner := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: owner, Email: owner + "@example.com", Password: "x", InviteCode: "METADATA", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	job, err := analyses.CreateTemplateWithJob(t.Context(), tmpl)
	if err != nil {
		t.Fatal(err)
	}

	name := "只修改名称"
	updated, err := templates.UpdatePatch(t.Context(), tmpl.ID, owner, TemplatePatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.ReadinessStatus != model.TemplateReadinessAnalyzing || updated.IsActive {
		t.Fatalf("updated template = %+v", updated)
	}
	if persisted.Generation != job.Generation || persisted.Status != model.ImageAnalysisStatusQueued || len(enqueuer.calls) != 1 {
		t.Fatalf("job changed on metadata update: %+v enqueues=%+v", persisted, enqueuer.calls)
	}
}

func TestImageAnalysisRemovingProjectAssetSupersedesActiveJob(t *testing.T) {
	svc, repo, _, _ := setupImageAnalysisService(t)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	job, err := svc.CreateProjectWithJob(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	project.ReferenceImageAssetID = ""
	if _, err := svc.UpdateProject(t.Context(), project, "", false); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ImageAnalyses().FindByID(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.ImageAnalysisStatusSuperseded || persisted.Generation != 2 {
		t.Fatalf("job after source removal = %+v", persisted)
	}
}

func TestProjectServiceManualStyleResponseIncludesSupersededAnalysis(t *testing.T) {
	analyses, repo, _, _ := setupImageAnalysisService(t)
	logger := zerolog.New(io.Discard)
	projects := NewProjectService(repo, &logger)
	projects.SetImageAnalysisService(analyses)
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformSeednote, Name: "项目", Status: model.ProjectStatusActive}
	asset := seedAnalysisAsset(t, repo, project.UserID, DirectUploadPurposeProjectReference)
	project.ReferenceImageAssetID = asset.ID
	if _, err := analyses.CreateProjectWithJob(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	updated, err := projects.Update(t.Context(), project.UserID, project.ID, &model.Project{
		VisualStyle: "人工视觉风格", VisualStyleSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageAnalysis == nil || updated.ImageAnalysis.Status != model.ImageAnalysisStatusSuperseded {
		t.Fatalf("image_analysis = %+v, want superseded", updated.ImageAnalysis)
	}
}

func TestTemplateServiceManualPromptSupersedesAnalysisAndCannotRetry(t *testing.T) {
	analyses, repo, _, _ := setupImageAnalysisService(t)
	logger := zerolog.New(io.Discard)
	templates := NewTemplateService(repo, &logger)
	templates.SetImageAnalysisService(analyses)
	owner := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: owner, Email: owner + "@example.com", Password: "x", InviteCode: "ANALYSIS", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	asset := seedAnalysisAsset(t, repo, owner, DirectUploadPurposeTemplateThumbnail)
	tmpl := &model.Template{ID: uuid.NewString(), UserID: owner, Type: model.TemplateTypeSeednote, Name: "模板", Category: model.SeednoteTemplateCategoryProduct, Visibility: "public", ThumbnailAssetID: asset.ID, ActivateWhenReady: true}
	if _, err := analyses.CreateTemplateWithJob(t.Context(), tmpl); err != nil {
		t.Fatal(err)
	}
	prompt := "人工模板提示词"
	updated, err := templates.UpdatePatch(t.Context(), tmpl.ID, owner, TemplatePatch{Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageAnalysis == nil || updated.ImageAnalysis.Status != model.ImageAnalysisStatusSuperseded || updated.ImageAnalysis.CanRetry {
		t.Fatalf("image_analysis = %+v, want non-retryable superseded", updated.ImageAnalysis)
	}
	if _, err := analyses.Retry(t.Context(), owner, updated.ImageAnalysis.ID); !errors.Is(err, ErrImageAnalysisNotRetryable) {
		t.Fatalf("retry manual template analysis error = %v, want not retryable", err)
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeAIEntryLLM struct {
	responses    []string
	usage        srvconfig.TokenUsage
	missingUsage bool
	calls        []struct {
		system string
		user   string
	}
}

type flakyAIEntryAssetRepository struct {
	repository.AssetRepository
	calls       int
	secondAsset *model.Asset
	secondErr   error
}

type aiEntryProjectErrorRepository struct {
	repository.ProjectRepository
	calls int
	err   error
}

func (r *aiEntryProjectErrorRepository) FindByID(ctx context.Context, id string) (*model.Project, error) {
	r.calls++
	if r.calls == 2 {
		return nil, r.err
	}
	return r.ProjectRepository.FindByID(ctx, id)
}

type aiEntryRepositoryOverride struct {
	repository.Repository
	projects repository.ProjectRepository
}

func (r *aiEntryRepositoryOverride) Projects() repository.ProjectRepository { return r.projects }

func (r *flakyAIEntryAssetRepository) FindOwnedByID(ctx context.Context, id, userID string) (*model.Asset, error) {
	r.calls++
	if r.calls == 1 {
		return r.AssetRepository.FindOwnedByID(ctx, id, userID)
	}
	if r.secondErr != nil {
		return nil, r.secondErr
	}
	return r.secondAsset, nil
}

func (f *fakeAIEntryLLM) CompleteResult(_ context.Context, systemPrompt, userPrompt string) (*LLMResult, error) {
	f.calls = append(f.calls, struct {
		system string
		user   string
	}{system: systemPrompt, user: userPrompt})
	usage := f.usage
	if !f.missingUsage && usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		usage = srvconfig.TokenUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14}
	}
	text := `{}`
	if len(f.responses) == 0 {
		return &LLMResult{Text: text, Model: "kimi-k2.7-code", Usage: usage}, nil
	}
	text = f.responses[0]
	f.responses = f.responses[1:]
	return &LLMResult{Text: text, Model: "kimi-k2.7-code", Usage: usage}, nil
}

type fakeProviderTokenCostRecorder struct {
	reconciled   []RecordProviderTokenCostRequest
	unreconciled []RecordProviderTokenUnreconciledRequest
}

func (f *fakeProviderTokenCostRecorder) CatalogID() string { return "catalog" }

func (f *fakeProviderTokenCostRecorder) RecordProviderTokenUsage(_ context.Context, req RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	f.reconciled = append(f.reconciled, req)
	return &model.BillingProviderCostEvent{}, nil
}

func (f *fakeProviderTokenCostRecorder) RecordProviderTokenUnreconciled(_ context.Context, req RecordProviderTokenUnreconciledRequest) (*model.BillingProviderCostEvent, error) {
	f.unreconciled = append(f.unreconciled, req)
	return &model.BillingProviderCostEvent{}, nil
}

func TestAIEntryServiceSubmitCreatesArticleTaskWithAttachments(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"写一篇新品发布公众号文章","notes":"优先参考第一张图"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "帮我写一篇新品发布公众号文章",
		Attachments: []model.EntryAttachment{{
			Type:        "image",
			URL:         "https://cdn.example.com/ref.png",
			FileName:    "ref.png",
			ContentType: "image/png",
			Size:        123,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Prompt != "写一篇新品发布公众号文章" {
		t.Fatalf("task prompt = %q", found.Prompt)
	}
	if found.ReferenceImageAssetID != "" {
		t.Fatalf("URL-only attachment became a task reference: asset=%q", found.ReferenceImageAssetID)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].FileName != "ref.png" {
		t.Fatalf("input attachments = %#v", attachments)
	}
	if len(llm.calls) != 1 || !strings.Contains(llm.calls[0].user, "帮我写一篇新品发布公众号文章") {
		t.Fatalf("llm calls = %#v", llm.calls)
	}
}

func TestAIEntryServiceSubmitCreatesMontageTaskWithImageSettings(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	taskSvc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "social-short",
		Preferences: model.MontagePreferences{
			DurationSeconds: 45,
		},
		DeliveryTargets: []string{"final_video"},
	})
	project.MontageDefaultsSet = true
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	taskSvc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
				"standard": testImageCapabilityRoute("image.standard"),
			},
		}},
	}))

	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"做一条新品发布短片"}`}}, nil, AIEntryModelConfig{}, &logger)
	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:             userID,
		ProjectID:          projectID,
		ExecutionProfile:   "effective",
		Text:               "做一条新品发布短片",
		Quantity:           5,
		ImageRatio:         "16:9",
		ImageCapabilityKey: "standard",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want one created Montage task", result)
	}
	task := result.Tasks[0]
	if task.Type != model.PlatformMontage || task.ImageCapabilityKey != "standard" || task.ImageRatio != "16:9" {
		t.Fatalf("task identity/image settings = type %q, capability %q, ratio %q", task.Type, task.ImageCapabilityKey, task.ImageRatio)
	}
	input := task.MontageInput.Data()
	if input.Brief != "做一条新品发布短片" || input.PipelineKey != "social-short" {
		t.Fatalf("montage input = %#v", input)
	}
	if input.Preferences.DurationSeconds != 45 {
		t.Fatalf("montage preferences = %#v", input.Preferences)
	}
	if len(input.DeliveryTargets) != 1 || input.DeliveryTargets[0] != "final_video" {
		t.Fatalf("delivery targets = %#v", input.DeliveryTargets)
	}
}

func TestAIEntryServiceSubmitMontageIgnoresInferredRatioWhenRequestOmitsIt(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	taskSvc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.ImageRatio = "16:9"
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	taskSvc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
				"standard": testImageCapabilityRoute("image.standard"),
			},
		}},
	}))

	entrySvc := NewAIEntryService(
		repo,
		taskSvc,
		&fakeAIEntryLLM{responses: []string{`{"prompt":"做一条新品发布短片","image_ratio":"1:1"}`}},
		nil,
		AIEntryModelConfig{},
		nil,
	)
	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:           userID,
		ProjectID:        projectID,
		ExecutionProfile: "effective",
		Text:             "做一条新品发布短片",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want one created Montage task", result)
	}
	if got := result.Tasks[0].ImageRatio; got != "16:9" {
		t.Fatalf("image_ratio = %q, want Studio project ratio 16:9", got)
	}
}

func TestAIEntryServiceSubmitPropagatesProfileAndSKUErrorsFromTaskCreation(t *testing.T) {
	sentinels := []error{
		ErrAgentProfileNotFound,
		ErrAgentProfileUnavailable,
		ErrAgentProfileAccessDenied,
		ErrAgentProfileSnapshotInvalid,
		ErrAgentProfileSnapshotConflict,
		ErrAgentProviderUnavailable,
		ErrAgentModelCostUnmapped,
		ErrBillingProfileSKUNotFound,
	}
	for _, sentinel := range sentinels {
		for _, wrapped := range []bool{false, true} {
			name := sentinel.Error()
			injected := sentinel
			if wrapped {
				name += "/wrapped"
				injected = fmt.Errorf("catalog context: %w", sentinel)
			}
			t.Run(name, func(t *testing.T) {
				db := setupTaskTestDB(t)
				baseRepo := repository.New(db)
				userID := uuid.NewString()
				projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
				projects := &aiEntryProjectErrorRepository{ProjectRepository: baseRepo.Projects(), err: injected}
				repo := &aiEntryRepositoryOverride{Repository: baseRepo, projects: projects}
				logger := zerolog.New(io.Discard)
				taskSvc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
				entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write"}`}}, nil, AIEntryModelConfig{}, &logger)

				result, err := entrySvc.Submit(t.Context(), AIEntrySubmitRequest{
					UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
				})
				if result != nil || !errors.Is(err, sentinel) {
					t.Fatalf("Submit = %#v, %v; want nil/%v", result, err, sentinel)
				}
			})
		}
	}
}

func TestAIEntryServiceSubmitKeepsUnrelatedTaskCreationFailureAsStatusError(t *testing.T) {
	db := setupTaskTestDB(t)
	baseRepo := repository.New(db)
	userID := uuid.NewString()
	projectID := createTestProject(t, baseRepo, userID, model.PlatformArticle)
	projects := &aiEntryProjectErrorRepository{ProjectRepository: baseRepo.Projects(), err: errors.New("database unavailable")}
	repo := &aiEntryRepositoryOverride{Repository: baseRepo, projects: projects}
	logger := zerolog.New(io.Discard)
	taskSvc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write"}`}}, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(t.Context(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
	})
	if err != nil || result == nil || result.Status != AIEntryStatusError {
		t.Fatalf("Submit = %#v, %v; want AIEntryStatusError/nil", result, err)
	}
}

func TestAIEntryUsesFinalizedAttachmentAssetAsTaskReference(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle, model.PlatformMoments} {
		t.Run(platform, func(t *testing.T) {
			taskSvc, repo := setupTaskServiceWithEnqueuer(t)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, platform)
			projectAsset := referenceAssetFixture("project-reference-"+platform, userID, DirectUploadPurposeProjectReference)
			if err := repo.Assets().Create(ctx, projectAsset); err != nil {
				t.Fatalf("create project asset: %v", err)
			}
			project, err := repo.Projects().FindByID(ctx, projectID)
			if err != nil {
				t.Fatal(err)
			}
			project.ReferenceImageAssetID = projectAsset.ID
			if err := repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			asset := referenceAssetFixture("entry-upload-"+platform, userID, DirectUploadPurposeAIEntryAttachment)
			if err := repo.Assets().Create(ctx, asset); err != nil {
				t.Fatalf("create asset: %v", err)
			}
			if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
				ID: asset.ID, UserID: userID, Purpose: DirectUploadPurposeAIEntryAttachment,
				StagingKey: "uploads/pending/" + userID + "/" + asset.ID + "/" + asset.FileName,
				FileName:   asset.FileName, ContentType: asset.ContentType, Size: asset.Size,
				Status: model.UploadSessionFinalized, AssetID: asset.ID, FinalizationETag: asset.ETag,
				ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatalf("create upload session: %v", err)
			}
			store := &referenceAssetStore{objects: map[string]*storage.ObjectInfo{
				asset.StorageKey: {Key: asset.StorageKey, Size: asset.Size, ContentType: asset.ContentType, ETag: asset.ETag},
			}}
			referenceSvc := NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"write content"}`}}
			logger := zerolog.New(io.Discard)
			entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
				UserID: userID, ProjectID: projectID, Text: "write content",
				Quantity:    2,
				Attachments: []model.EntryAttachment{{Type: "image", UploadID: asset.ID, URL: "https://staging.example.com/ref.png", FileName: asset.FileName, ContentType: asset.ContentType, Size: asset.Size}},
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if result.Status != AIEntryStatusCreated || len(result.Tasks) != 2 {
				t.Fatalf("result = %#v", result)
			}
			for _, task := range result.Tasks {
				found, err := repo.Tasks().FindByID(ctx, task.ID)
				if err != nil {
					t.Fatalf("find task: %v", err)
				}
				if found.ReferenceImageAssetID != asset.ID {
					t.Fatalf("reference asset = %q", found.ReferenceImageAssetID)
				}
				if task.ReferenceImage == nil || task.ReferenceImage.AssetID != asset.ID {
					t.Fatalf("reference asset view = %#v", task.ReferenceImage)
				}
			}
			if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
				t.Fatalf("signed keys = %#v, want direct attachment only", store.signedKeys)
			}
		})
	}
}

func TestAIEntryPresentsInheritedProjectReferenceBeforeTaskCreation(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture("project-reference", userID, DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.ReferenceImageAssetID = asset.ID
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	store := &referenceAssetStore{objects: map[string]*storage.ObjectInfo{}}
	referenceSvc := NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write article"}`}}, nil, AIEntryModelConfig{}, &logger)
	entrySvc.SetReferenceAssetService(referenceSvc)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective", UserID: userID, ProjectID: projectID, Text: "write article"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 || result.Tasks[0].ReferenceImage == nil || result.Tasks[0].ReferenceImage.AssetID != asset.ID {
		t.Fatalf("result = %#v", result)
	}
	persisted, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ReferenceImageAssetID != "" || persisted.ProjectSnapshot.Data().ReferenceImageAssetID != asset.ID {
		t.Fatalf("persisted reference = direct %q snapshot %#v", persisted.ReferenceImageAssetID, persisted.ProjectSnapshot.Data())
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v", store.signedKeys)
	}
}

func TestAIEntrySeednotePresentsInheritedProjectReferenceAndKeepsAttachments(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	asset := referenceAssetFixture("seednote-project-reference", userID, DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.ReferenceImageAssetID = asset.ID
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	store := &referenceAssetStore{objects: map[string]*storage.ObjectInfo{}}
	referenceSvc := NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write seednote"}`}}, nil, AIEntryModelConfig{}, &logger)
	entrySvc.SetReferenceAssetService(referenceSvc)
	attachment := model.EntryAttachment{Type: "image", URL: "/api/v1/files/product.png", FileName: "product.png", ContentType: "image/png", Instruction: "keep logo"}

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective", UserID: userID, ProjectID: projectID, Text: "write seednote", Attachments: []model.EntryAttachment{attachment}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 || result.Tasks[0].ReferenceImage == nil || result.Tasks[0].ReferenceImage.AssetID != asset.ID {
		t.Fatalf("result = %#v", result)
	}
	persisted, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ReferenceImageAssetID != "" || persisted.ProjectSnapshot.Data().ReferenceImageAssetID != asset.ID {
		t.Fatalf("persisted reference = direct %q snapshot %#v", persisted.ReferenceImageAssetID, persisted.ProjectSnapshot.Data())
	}
	attachments := persisted.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].URL != attachment.URL || attachments[0].Instruction != attachment.Instruction {
		t.Fatalf("attachments changed: %#v", attachments)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v", store.signedKeys)
	}
}

func TestAIEntryProjectReferenceSigningFailureDoesNotCreateOrCharge(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle, model.PlatformSeednote} {
		t.Run(platform, func(t *testing.T) {
			base := repository.New(setupTaskTestDB(t))
			ctx := context.Background()
			userID := uuid.NewString()
			logger := zerolog.New(io.Discard)
			if err := base.Users().Create(ctx, &model.User{ID: userID, OpenID: "ai-entry-project-reference-" + platform}); err != nil {
				t.Fatal(err)
			}
			topics := &countingTopicPoolRepository{TopicPoolRepository: base.TopicPools()}
			repo := &taskCreationRepositoryOverride{Repository: base, topicPools: topics}
			projectID := createTestProject(t, repo, userID, platform)
			asset := referenceAssetFixture("project-reference-"+platform, userID, DirectUploadPurposeProjectReference)
			seedReferenceAsset(t, repo, asset)
			project, err := repo.Projects().FindByID(ctx, projectID)
			if err != nil {
				t.Fatal(err)
			}
			project.ReferenceImageAssetID = asset.ID
			if err := repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			store := &referenceAssetStore{objects: map[string]*storage.ObjectInfo{}, downloadErr: errors.New("signer unavailable")}
			taskSvc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
			taskSvc.SetTopicPoolService(NewTopicPoolService(repo, &logger))
			referenceSvc := NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write content"}`}}, nil, AIEntryModelConfig{}, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective", UserID: userID, ProjectID: projectID, Text: "write content"})
			if result != nil || !errors.Is(err, ErrReferenceAssetUnavailable) {
				t.Fatalf("result/error = %#v/%v, want unavailable", result, err)
			}
			if topics.claimWithTaskCalls != 0 {
				t.Fatalf("topic claims = %d", topics.claimWithTaskCalls)
			}
			_, total, listErr := taskSvc.List(ctx, userID, 0, 10, "", "", "")
			if listErr != nil || total != 0 {
				t.Fatalf("tasks = %d, %v", total, listErr)
			}
		})
	}
}

func TestAIEntryPreservesSecondReferenceValidationErrorsBeforeCreationOrBilling(t *testing.T) {
	rootCause := errors.New("database shard secret")
	for _, tt := range []struct {
		name        string
		secondAsset func(*model.Asset) *model.Asset
		secondErr   error
		want        error
	}{
		{name: "forbidden", secondErr: model.ErrAssetNotFound, want: ErrReferenceAssetForbidden},
		{name: "purpose mismatch", secondAsset: func(asset *model.Asset) *model.Asset {
			copy := *asset
			copy.Purpose = DirectUploadPurposeTaskReference
			return &copy
		}, want: ErrReferenceAssetPurposeMismatch},
		{name: "unavailable", secondErr: rootCause, want: ErrReferenceAssetUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := repository.New(setupTaskTestDB(t))
			ctx := context.Background()
			userID := uuid.NewString()
			logger := zerolog.New(io.Discard)
			if err := base.Users().Create(ctx, &model.User{ID: userID, OpenID: "ai-entry-second-reference-" + tt.name}); err != nil {
				t.Fatal(err)
			}
			topics := &countingTopicPoolRepository{TopicPoolRepository: base.TopicPools()}
			asset := referenceAssetFixture("project-reference-"+tt.name, userID, DirectUploadPurposeProjectReference)
			seedReferenceAsset(t, base, asset)
			flakyAssets := &flakyAIEntryAssetRepository{AssetRepository: base.Assets(), secondErr: tt.secondErr}
			if tt.secondAsset != nil {
				flakyAssets.secondAsset = tt.secondAsset(asset)
			}
			repo := &referenceAssetRepositoryOverride{
				Repository: base,
				assets:     flakyAssets,
			}
			projectID := createTestProject(t, repo, userID, model.PlatformArticle)
			project, err := repo.Projects().FindByID(ctx, projectID)
			if err != nil {
				t.Fatal(err)
			}
			project.ReferenceImageAssetID = asset.ID
			if err := repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			repoWithTopics := &taskCreationRepositoryOverride{Repository: repo, topicPools: topics}
			store := &referenceAssetStore{objects: map[string]*storage.ObjectInfo{}}
			taskSvc := newTestTaskService(repoWithTopics, &mockEnqueuer{}, store, &logger, "", nil, nil)
			taskSvc.SetTopicPoolService(NewTopicPoolService(repoWithTopics, &logger))
			referenceSvc := NewReferenceAssetService(repoWithTopics, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			entrySvc := NewAIEntryService(repoWithTopics, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write article"}`}}, nil, AIEntryModelConfig{}, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective", UserID: userID, ProjectID: projectID, Text: "write article"})
			if result != nil || !errors.Is(err, tt.want) {
				t.Fatalf("result/error = %#v/%v, want %v", result, err, tt.want)
			}
			if tt.want == ErrReferenceAssetUnavailable && !errors.Is(err, rootCause) {
				t.Fatalf("error = %v, want root cause preserved", err)
			}
			if flakyAssets.calls != 2 {
				t.Fatalf("asset lookup calls = %d, want pre-present plus CreateManual validation", flakyAssets.calls)
			}
			if topics.claimWithTaskCalls != 0 {
				t.Fatalf("topic claims = %d", topics.claimWithTaskCalls)
			}
			_, total, listErr := taskSvc.List(ctx, userID, 0, 10, "", "", "")
			if listErr != nil || total != 0 {
				t.Fatalf("tasks = %d, %v", total, listErr)
			}
		})
	}
}

func TestAIEntryServiceSubmitDropsUnsafeLLMImageFields(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"写一篇新品发布文章","image_ratio":"2:1","image_capability_key":"custom"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇新品发布文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.ImageRatio != model.DefaultImageRatio(model.PlatformArticle) {
		t.Fatalf("image_ratio = %q, want invalid LLM ratio dropped and platform default frozen", found.ImageRatio)
	}
	if found.ImageCapabilityKey != "standard" {
		t.Fatalf("image_capability_key = %q, want untrusted LLM key ignored and server default frozen", found.ImageCapabilityKey)
	}
}

func TestAIEntryServiceSubmitUsesExplicitImageParametersForEveryTask(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierEnterprise
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}
	taskSvc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
				"standard":     testImageCapabilityRoute("image.standard"),
				"professional": testImageCapabilityRoute("image.professional"),
			},
		}},
	}))
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"写新品文章","image_ratio":"3:4","image_capability_key":"untrusted"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "写新品文章",
		Quantity: 2, ImageRatio: " 16:9 ", ImageCapabilityKey: " professional ",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 2 {
		t.Fatalf("result = %#v, want two created tasks", result)
	}
	if result.Message != "已创建 2 个任务。" {
		t.Fatalf("message = %q", result.Message)
	}
	for _, task := range result.Tasks {
		if task.ImageRatio != "16:9" || task.ImageCapabilityKey != "professional" {
			t.Fatalf("task image parameters = ratio %q, capability %q", task.ImageRatio, task.ImageCapabilityKey)
		}
	}
}

func TestAIEntryServiceSubmitRejectsUnauthorizedExplicitImageCapability(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	taskSvc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
				"standard": testImageCapabilityRoute("image.standard"),
				"professional": func() srvconfig.ImageGenerationRouteConfig {
					route := testImageCapabilityRoute("image.professional")
					route.MinTier = "enterprise"
					return route
				}(),
			},
		}},
	}))
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"write"}`}}
	costs := &fakeProviderTokenCostRecorder{}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
		ImageCapabilityKey: "professional",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusError || !strings.Contains(result.Message, "requires enterprise tier") {
		t.Fatalf("result = %#v, want tier authorization failure", result)
	}
	if len(llm.calls) != 0 || len(costs.reconciled) != 0 || len(costs.unreconciled) != 0 {
		t.Fatalf("LLM/cost side effects = calls %d, reconciled %d, unreconciled %d; want all zero", len(llm.calls), len(costs.reconciled), len(costs.unreconciled))
	}
	tasks, findErr := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if findErr != nil || len(tasks) != 0 {
		t.Fatalf("tasks = %#v, err=%v; want none", tasks, findErr)
	}
}

func TestAIEntryServiceSubmitFailsClosedWhenExplicitImageCapabilityResolverIsUnavailable(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	taskSvc.SetImageCapabilityResolver(nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"write"}`}}
	costs := &fakeProviderTokenCostRecorder{}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
		ImageCapabilityKey: "professional",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusError || !strings.Contains(result.Message, "图片能力服务暂不可用") {
		t.Fatalf("result = %#v, want unavailable capability resolver error", result)
	}
	if len(llm.calls) != 0 || len(costs.reconciled) != 0 || len(costs.unreconciled) != 0 {
		t.Fatalf("LLM/cost side effects = calls %d, reconciled %d, unreconciled %d; want all zero", len(llm.calls), len(costs.reconciled), len(costs.unreconciled))
	}
	tasks, findErr := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if findErr != nil || len(tasks) != 0 {
		t.Fatalf("tasks = %#v, err=%v; want none", tasks, findErr)
	}
}

func TestAIEntryServiceSubmitRejectsInvalidExplicitParametersBeforeIntentParsing(t *testing.T) {
	tests := []struct {
		name        string
		quantity    int
		imageRatio  string
		wantMessage string
	}{
		{name: "negative quantity", quantity: -1, wantMessage: "quantity must be between 1 and 5"},
		{name: "quantity above maximum", quantity: 6, wantMessage: "quantity must be between 1 and 5"},
		{name: "ratio unsupported by article project", imageRatio: "9:16", wantMessage: model.ValidImageRatioHint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskSvc, repo := setupTaskServiceWithEnqueuer(t)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, model.PlatformArticle)
			llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"write"}`}}
			costs := &fakeProviderTokenCostRecorder{}
			logger := zerolog.New(io.Discard)
			entrySvc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
				UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
				Quantity: tt.quantity, ImageRatio: tt.imageRatio,
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if result.Status != AIEntryStatusError || !strings.Contains(result.Message, tt.wantMessage) {
				t.Fatalf("result = %#v, want error containing %q", result, tt.wantMessage)
			}
			if len(llm.calls) != 0 || len(costs.reconciled) != 0 || len(costs.unreconciled) != 0 {
				t.Fatalf("LLM/cost side effects = calls %d, reconciled %d, unreconciled %d; want all zero", len(llm.calls), len(costs.reconciled), len(costs.unreconciled))
			}
			tasks, findErr := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
			if findErr != nil || len(tasks) != 0 {
				t.Fatalf("tasks = %#v, err=%v; want none", tasks, findErr)
			}
		})
	}
}

func TestAIEntryServiceSubmitNeedsConfigurationForEcommerceWithoutProductImage(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
	llm := &fakeAIEntryLLM{responses: []string{
		`{"prompt":"做一套保温杯电商主图","selling_points":"316 不锈钢，长效保温"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "做一套保温杯电商主图",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration {
		t.Fatalf("status = %q, want needs_configuration", result.Status)
	}
	wantActionURL := "/tasks?create=true&type=ecommerce&project_id=" + projectID + "&intent=new"
	if !strings.Contains(result.Message, "产品图") || result.ActionURL != wantActionURL {
		t.Fatalf("result = %#v, want product-photo hint and action URL", result)
	}
	tasks, total, err := taskSvc.List(ctx, userID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if total != 0 || len(tasks) != 0 {
		t.Fatalf("created tasks = %d/%d, want none", len(tasks), total)
	}
}

func TestAIEntryServiceSubmitCreatesEcommerceTaskFromKeyFirstImage(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"制作保温杯电商主图","selected_modules":{"main_images":1}}`}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)
	key := "uploads/pending/" + userID + "/image-upload/product.png"

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID: userID, ProjectID: projectID, Channel: "studio", Text: "制作保温杯电商主图",
		Attachments: []model.EntryAttachment{{
			Type: "image", UploadID: "image-upload", Key: key,
			FileName: "product.png", ContentType: "image/png", Size: 2048,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want created", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if photos := found.Ecommerce.Data().ProductPhotos; len(photos) != 1 || photos[0] != key {
		t.Fatalf("product photos = %#v, want key source", photos)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Key != key || attachments[0].URL != "" {
		t.Fatalf("input attachments = %#v, want key-first snapshot", attachments)
	}
}

func TestAIEntryServiceSubmitNormalizesEcommerceModules(t *testing.T) {
	t.Run("falls back to minimum legal default when parsed modules are invalid", func(t *testing.T) {
		taskSvc, repo := setupTaskServiceWithEnqueuer(t)
		ctx := context.Background()
		userID := uuid.NewString()
		projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
		llm := &fakeAIEntryLLM{responses: []string{
			`{"prompt":"做一套保温杯主图","selected_modules":{"bogus":9,"main_images":0}}`,
		}}
		logger := zerolog.New(io.Discard)
		entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
			UserID:    userID,
			ProjectID: projectID,
			Channel:   "studio",
			Text:      "做一套保温杯主图",
			Attachments: []model.EntryAttachment{{
				Type:        "image",
				URL:         "https://cdn.example.com/product.png",
				FileName:    "product.png",
				ContentType: "image/png",
				Size:        123,
			}},
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
		if err != nil {
			t.Fatalf("find task: %v", err)
		}
		modules := found.Ecommerce.Data().SelectedModules
		if len(modules) != 1 || modules["main_images"] != 1 {
			t.Fatalf("selected modules = %#v, want minimum main_images default", modules)
		}
	})

	t.Run("caps parsed module quantities to Studio-supported limits", func(t *testing.T) {
		taskSvc, repo := setupTaskServiceWithEnqueuer(t)
		ctx := context.Background()
		userID := uuid.NewString()
		projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)
		llm := &fakeAIEntryLLM{responses: []string{
			`{"prompt":"做一套保温杯商详","selected_modules":{"detail_page":99,"share_image":2}}`,
		}}
		logger := zerolog.New(io.Discard)
		entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
			UserID:    userID,
			ProjectID: projectID,
			Channel:   "studio",
			Text:      "做一套保温杯商详",
			Attachments: []model.EntryAttachment{{
				Type:        "image",
				URL:         "https://cdn.example.com/product.png",
				FileName:    "product.png",
				ContentType: "image/png",
				Size:        123,
			}},
		})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
		if err != nil {
			t.Fatalf("find task: %v", err)
		}
		modules := found.Ecommerce.Data().SelectedModules
		if len(modules) != 2 || modules["detail_page"] != 20 || modules["share_image"] != 2 {
			t.Fatalf("selected modules = %#v, want capped detail_page and preserved share_image", modules)
		}
	})
}

func TestAIEntryRepairsInvalidJSONWithSameServerInternalClient(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	llm := &fakeAIEntryLLM{responses: []string{
		`不是 JSON`,
		`{"prompt":"写一篇可露营咖啡壶种草笔记"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇可露营咖啡壶种草笔记",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated {
		t.Fatalf("status = %q, want created", result.Status)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("llm call count = %d, want repair retry", len(llm.calls))
	}
	if !strings.Contains(llm.calls[1].user, "只返回合法 JSON") {
		t.Fatalf("repair prompt = %q", llm.calls[1].user)
	}
}

func TestAIEntryRequiresServerInternalClientBeforeTaskCreation(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, nil, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration || result.ActionURL != "" || !strings.Contains(result.Message, "管理员") {
		t.Fatalf("result = %#v, want an operator-owned server internal configuration error", result)
	}
	count, countErr := repo.Tasks().CountByUserID(ctx, userID, "", "")
	if countErr != nil || count != 0 {
		t.Fatalf("task count = %d, err=%v", count, countErr)
	}
}

func TestAIEntryRecordsEverySuccessfulServerInternalResponse(t *testing.T) {
	llm := &fakeAIEntryLLM{responses: []string{"not-json", `{"prompt":"整理成文章"}`}}
	costs := &fakeProviderTokenCostRecorder{}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

	_, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", ExecutionProfile: "effective", Text: "整理成文章",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.reconciled) != 2 || len(costs.unreconciled) != 0 {
		t.Fatalf("costs=%#v/%#v", costs.reconciled, costs.unreconciled)
	}
	for _, req := range costs.reconciled {
		if req.TaskID != "" || req.Provider != "moonshot" || req.Model != "kimi-k2.7-code" || req.Usage.Input != 10 {
			t.Fatalf("cost request = %#v", req)
		}
		if !strings.HasPrefix(req.ProviderRequestID, "internal:server_internal:") || req.IdempotencyKey != req.ProviderRequestID || req.CatalogID != "catalog" {
			t.Fatalf("cost identity = %#v", req)
		}
	}
	if costs.reconciled[0].ProviderRequestID == costs.reconciled[1].ProviderRequestID {
		t.Fatalf("provider request IDs must be unique: %#v", costs.reconciled)
	}
}

func TestAIEntryRecordsMissingUsageAsUnreconciled(t *testing.T) {
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"整理成文章"}`}, missingUsage: true}
	costs := &fakeProviderTokenCostRecorder{}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

	_, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", ExecutionProfile: "effective", Text: "整理成文章",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.reconciled) != 0 || len(costs.unreconciled) != 1 {
		t.Fatalf("costs=%#v/%#v", costs.reconciled, costs.unreconciled)
	}
	req := costs.unreconciled[0]
	if req.TaskID != "" || req.Provider != "moonshot" || req.Model != "kimi-k2.7-code" || req.ReasonCode != model.BillingExecutionCostReasonMissingProviderUsage {
		t.Fatalf("unreconciled request = %#v", req)
	}
	if !strings.HasPrefix(req.ProviderRequestID, "internal:server_internal:") {
		t.Fatalf("provider request ID = %q", req.ProviderRequestID)
	}
}

func TestAIEntryRecordsTotalOnlyUsageAsUnreconciled(t *testing.T) {
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"整理成文章"}`}, usage: srvconfig.TokenUsage{TotalTokens: 42}}
	costs := &fakeProviderTokenCostRecorder{}
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	svc := NewAIEntryService(repo, taskSvc, llm, costs, AIEntryModelConfig{ProviderKey: "moonshot", Model: "kimi-k2.7-code"}, &logger)

	_, err := svc.Submit(context.Background(), AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", ExecutionProfile: "effective", Text: "整理成文章",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(costs.reconciled) != 0 || len(costs.unreconciled) != 1 {
		t.Fatalf("costs=%#v/%#v, want one unreconciled event", costs.reconciled, costs.unreconciled)
	}
}

func TestAIEntrySeednoteDoesNotPromoteFirstImageToReferenceAsset(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"生成新品种草图文"}`}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "参考产品图生成种草内容",
		Attachments: []model.EntryAttachment{{
			Type:        "image",
			URL:         "/api/v1/files/product.png",
			FileName:    "product.png",
			ContentType: "image/png",
			Instruction: "  保持包装和 Logo  ",
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.ReferenceImageAssetID != "" {
		t.Fatalf("reference asset = %q, want empty", found.ReferenceImageAssetID)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].URL != "/api/v1/files/product.png" || attachments[0].Instruction != "保持包装和 Logo" {
		t.Fatalf("input attachments = %#v", attachments)
	}
	if len(llm.calls) != 1 || !strings.Contains(llm.calls[0].user, "attachment 1 instruction: 保持包装和 Logo") {
		t.Fatalf("LLM prompt did not include attachment instruction: %#v", llm.calls)
	}
}

func TestAIEntryArticleAndMomentsDoNotPersistURLOnlyReference(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle, model.PlatformMoments} {
		t.Run(platform, func(t *testing.T) {
			taskSvc, repo := setupTaskServiceWithEnqueuer(t)
			ctx := context.Background()
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, platform)
			llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"生成内容"}`}}
			logger := zerolog.New(io.Discard)
			entrySvc := NewAIEntryService(repo, taskSvc, llm, nil, AIEntryModelConfig{}, &logger)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{ExecutionProfile: "effective",
				UserID:    userID,
				ProjectID: projectID,
				Channel:   "studio",
				Text:      "参考产品图生成内容",
				Attachments: []model.EntryAttachment{{
					Type:        "image",
					URL:         "/api/v1/files/product.png",
					FileName:    "product.png",
					ContentType: "image/png",
				}},
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if result.Status != AIEntryStatusCreated || len(result.Tasks) != 1 {
				t.Fatalf("result = %#v, want created task", result)
			}
			found, err := repo.Tasks().FindByID(ctx, result.Tasks[0].ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.ReferenceImageAssetID != "" {
				t.Fatalf("URL-only reference persisted: asset=%q", found.ReferenceImageAssetID)
			}
		})
	}
}

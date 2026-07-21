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

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeAIEntryLLM struct {
	responses []string
	calls     []struct {
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

func (f *fakeAIEntryLLM) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	f.calls = append(f.calls, struct {
		system string
		user   string
	}{system: systemPrompt, user: userPrompt})
	if len(f.responses) == 0 {
		return `{}`, nil
	}
	out := f.responses[0]
	f.responses = f.responses[1:]
	return out, nil
}

func (f *fakeAIEntryLLM) CompleteWithImage(context.Context, string, string, string) (string, error) {
	return "", nil
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
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
			entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
				UserID: userID, ProjectID: projectID, Text: "write content",
				Attachments: []model.EntryAttachment{{Type: "image", UploadID: asset.ID, URL: "https://staging.example.com/ref.png", FileName: asset.FileName, ContentType: asset.ContentType, Size: asset.Size}},
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if result.Status != AIEntryStatusCreated || result.Task == nil || result.Task.ReferenceImage == nil {
				t.Fatalf("result = %#v", result)
			}
			found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.ReferenceImageAssetID != asset.ID {
				t.Fatalf("reference asset = %q", found.ReferenceImageAssetID)
			}
			if result.Task.ReferenceImage.AssetID != asset.ID {
				t.Fatalf("reference asset view = %#v", result.Task.ReferenceImage)
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
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write article"}`}}, &logger)
	entrySvc.SetReferenceAssetService(referenceSvc)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{UserID: userID, ProjectID: projectID, Text: "write article"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil || result.Task.ReferenceImage == nil || result.Task.ReferenceImage.AssetID != asset.ID {
		t.Fatalf("result = %#v", result)
	}
	persisted, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
	entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write seednote"}`}}, &logger)
	entrySvc.SetReferenceAssetService(referenceSvc)
	attachment := model.EntryAttachment{Type: "image", URL: "/api/v1/files/product.png", FileName: "product.png", ContentType: "image/png", Instruction: "keep logo"}

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{UserID: userID, ProjectID: projectID, Text: "write seednote", Attachments: []model.EntryAttachment{attachment}})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil || result.Task.ReferenceImage == nil || result.Task.ReferenceImage.AssetID != asset.ID {
		t.Fatalf("result = %#v", result)
	}
	persisted, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
			taskSvc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
			taskSvc.SetTopicPoolService(NewTopicPoolService(repo, &logger))
			referenceSvc := NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			entrySvc := NewAIEntryService(repo, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write content"}`}}, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{UserID: userID, ProjectID: projectID, Text: "write content"})
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
			taskSvc := NewTaskService(repoWithTopics, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
			taskSvc.SetTopicPoolService(NewTopicPoolService(repoWithTopics, &logger))
			referenceSvc := NewReferenceAssetService(repoWithTopics, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			entrySvc := NewAIEntryService(repoWithTopics, taskSvc, &fakeAIEntryLLM{responses: []string{`{"prompt":"write article"}`}}, &logger)
			entrySvc.SetReferenceAssetService(referenceSvc)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{UserID: userID, ProjectID: projectID, Text: "write article"})
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
		`{"prompt":"写一篇新品发布文章","image_ratio":"2:1","image_model_key":"custom"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇新品发布文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.ImageRatio != "" {
		t.Fatalf("image_ratio = %q, want invalid LLM ratio dropped", found.ImageRatio)
	}
	if found.ImageModelKey != "" {
		t.Fatalf("image_model_key = %q, want LLM model key ignored", found.ImageModelKey)
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
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)
	key := "uploads/pending/" + userID + "/image-upload/product.png"

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID: userID, ProjectID: projectID, Channel: "studio", Text: "制作保温杯电商主图",
		Attachments: []model.EntryAttachment{{
			Type: "image", UploadID: "image-upload", Key: key,
			FileName: "product.png", ContentType: "image/png", Size: 2048,
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
		entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
		if result.Status != AIEntryStatusCreated || result.Task == nil {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
		entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

		result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
		if result.Status != AIEntryStatusCreated || result.Task == nil {
			t.Fatalf("result = %#v, want created task", result)
		}
		found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
		if err != nil {
			t.Fatalf("find task: %v", err)
		}
		modules := found.Ecommerce.Data().SelectedModules
		if len(modules) != 2 || modules["detail_page"] != 20 || modules["share_image"] != 2 {
			t.Fatalf("selected modules = %#v, want capped detail_page and preserved share_image", modules)
		}
	})
}

func TestAIEntryServiceSubmitRetriesJSONRepairOnce(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	llm := &fakeAIEntryLLM{responses: []string{
		`不是 JSON`,
		`{"prompt":"写一篇可露营咖啡壶种草笔记"}`,
	}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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

func TestAIEntryServiceSubmitRequiresLLM(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, nil, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
		UserID:    userID,
		ProjectID: projectID,
		Channel:   "studio",
		Text:      "写一篇文章",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != AIEntryStatusNeedsConfiguration || !strings.Contains(result.Message, "模型") || result.ActionURL == "" {
		t.Fatalf("result = %#v, want model configuration action", result)
	}
}

func TestAIEntrySeednoteDoesNotPromoteFirstImageToReferenceAsset(t *testing.T) {
	taskSvc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	llm := &fakeAIEntryLLM{responses: []string{`{"prompt":"生成新品种草图文"}`}}
	logger := zerolog.New(io.Discard)
	entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

	result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
	if result.Status != AIEntryStatusCreated || result.Task == nil {
		t.Fatalf("result = %#v, want created task", result)
	}
	found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
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
			entrySvc := NewAIEntryService(repo, taskSvc, llm, &logger)

			result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
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
			if result.Status != AIEntryStatusCreated || result.Task == nil {
				t.Fatalf("result = %#v, want created task", result)
			}
			found, err := repo.Tasks().FindByID(ctx, result.Task.ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.ReferenceImageAssetID != "" {
				t.Fatalf("URL-only reference persisted: asset=%q", found.ReferenceImageAssetID)
			}
		})
	}
}

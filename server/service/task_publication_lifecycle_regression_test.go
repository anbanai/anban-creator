package service

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agent"
	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestCompleteCloudExecutionProjectsPreflightPublicationBlock(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cover          bool
		code           string
		action         string
		invalidPackage bool
	}{
		{"publication service unavailable", false, "publication_service_unavailable", "retry_draft", false},
		{"cover missing", true, "cover_media_missing", "retry_visuals", false},
		{"extra cover metadata in package", false, "publication_package_invalid", "review_content", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			if tc.invalidPackage {
				var pkg map[string]any
				if err := json.Unmarshal(validTaskDeliveryFixtureBody("output/draft.json", "application/json"), &pkg); err != nil {
					t.Fatal(err)
				}
				pkg["cover"] = map[string]string{"file_path": "output/cover.png"}
				body, err := json.Marshal(pkg)
				if err != nil {
					t.Fatal(err)
				}
				addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/draft.json", "application/json", body)
			}
			task.ArticleWithCover = &tc.cover
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
				t.Fatal(err)
			}
			if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
				t.Fatal(err)
			}
			stored, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Outcome == nil || stored.Outcome.Publication.Code != tc.code || stored.Outcome.Publication.Action != tc.action || stored.Outcome.Publication.Attempted {
				t.Fatalf("preflight outcome = %#v", stored.Outcome)
			}
			draft := stored.Lifecycle.Data().Stages[3]
			if draft.State != model.TaskLifecycleStateBlocked || draft.LatestUpdate != stored.Outcome.Publication.Message {
				t.Fatalf("draft stage = %#v, want blocked with actual preflight reason %q", draft, stored.Outcome.Publication.Message)
			}
		})
	}
}

func TestFinalizePicturePublicationUsesCanonicalServerPackageSchema(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantStatus string
		wantCode   string
	}{
		{
			name:       "agent-only caption shape is rejected",
			body:       `{"status":"ready","title":"图片消息","caption":"图下注释","cover_path":"output/cover.png","image_paths":[]}`,
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "nested readiness and content shape is accepted",
			body:       `{"schema_version":"1.0","status":"ready","source":"wechat-picture-agent","data_at":"2026-10-06T00:00:00Z","missing":[],"title":"图片消息","digest":"摘要","content":"图下注释","cover_path":"output/cover.png","image_paths":[],"readiness":{"status":"ready"}}`,
			wantStatus: model.TaskExecutionDraftDeliveryBlocked,
			wantCode:   "publication_service_unavailable",
		},
		{
			name:       "valid package with blocked readiness is recoverable review block",
			body:       string(canonicalPicturePublicationPackage(t, "blocked", "blocked", []string{"visual_review"}, nil)),
			wantStatus: model.TaskExecutionDraftDeliveryBlocked,
			wantCode:   "semantic_review_blocked",
		},
		{
			name:       "agent notes field is rejected",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"notes": "legacy runtime metadata"})),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "root and readiness status must agree",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "blocked", []string{"visual_review"}, nil)),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "source is required to identify the producer",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"source": "legacy-agent"})),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "data timestamp must be RFC3339",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"data_at": "yesterday"})),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "missing must be an array",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"missing": nil})),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "wildcard image path is rejected",
			body:       string(canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"cover_path": "output/image_*.png"})),
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
		{
			name:       "missing readiness is a malformed package",
			body:       `{"schema_version":"1.0","title":"图片消息","digest":"摘要","content":"图下注释","cover_path":"output/cover.png","image_paths":[]}`,
			wantStatus: model.TaskExecutionDraftDeliveryFailed,
			wantCode:   "publication_package_invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution := setupCloudCompletionTest(t, false)
			task.Type = model.TaskTypeWechatPicture
			task.Channel = model.ChannelWechatPicture
			addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", []byte(tc.body))

			status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
			if err != nil {
				t.Fatal(err)
			}
			var result publicationDeliveryResult
			if err := json.Unmarshal(evidence, &result); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus || result.Code != tc.wantCode || result.Attempted {
				t.Fatalf("status=%q evidence=%s", status, evidence)
			}
		})
	}
}

func canonicalPicturePublicationPackage(t *testing.T, status, readiness string, missing []string, extra map[string]any) []byte {
	t.Helper()
	if missing == nil {
		missing = []string{}
	}
	pkg := map[string]any{
		"schema_version": "1.0",
		"status":         status,
		"source":         "wechat-picture-agent",
		"data_at":        "2026-10-06T00:00:00Z",
		"missing":        missing,
		"title":          "图片消息",
		"digest":         "摘要",
		"content":        "图下注释",
		"cover_path":     "output/cover.png",
		"image_paths":    []string{},
		"readiness":      map[string]string{"status": readiness},
	}
	for key, value := range extra {
		pkg[key] = value
	}
	body, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func picturePublicationPackageWithImages(t *testing.T, imagePaths []string) []byte {
	t.Helper()
	var pkg map[string]any
	if err := json.Unmarshal(canonicalPicturePublicationPackage(t, "ready", "ready", nil, nil), &pkg); err != nil {
		t.Fatal(err)
	}
	pkg["image_paths"] = imagePaths
	body, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFinalizePicturePublicationRespectsRequestedImageCountMode(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      map[string]any
		imagePaths []string
		wantStatus string
		wantCode   string
	}{
		{name: "legacy task stays exact", input: map[string]any{"picture_image_count": 2}, imagePaths: []string{}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "image_count_mismatch"},
		{name: "fixed count rejects fewer images", input: map[string]any{"picture_image_count": 2, "picture_image_count_mode": "exact"}, imagePaths: []string{}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "image_count_mismatch"},
		{name: "smart count accepts fewer images", input: map[string]any{"picture_image_count": 5, "picture_image_count_mode": "up_to"}, imagePaths: []string{}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "publication_service_unavailable"},
		{name: "smart count rejects more images", input: map[string]any{"picture_image_count": 1, "picture_image_count_mode": "up_to"}, imagePaths: []string{"output/image_01.png"}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "image_count_mismatch"},
		{name: "missing input accepts five total images", input: map[string]any{}, imagePaths: []string{"output/image_01.png", "output/image_02.png", "output/image_03.png", "output/image_04.png"}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "publication_service_unavailable"},
		{name: "missing input rejects more than five total images", input: map[string]any{}, imagePaths: []string{"output/image_01.png", "output/image_02.png", "output/image_03.png", "output/image_04.png", "output/image_05.png"}, wantStatus: model.TaskExecutionDraftDeliveryBlocked, wantCode: "image_count_mismatch"},
		{name: "unknown mode is invalid", input: map[string]any{"picture_image_count": 5, "picture_image_count_mode": "flexible"}, imagePaths: []string{}, wantStatus: model.TaskExecutionDraftDeliveryFailed, wantCode: "image_count_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution := setupCloudCompletionTest(t, false)
			task.Type = model.TaskTypeWechatPicture
			task.Channel = model.ChannelWechatPicture
			task.SetAgentInput(tc.input)
			addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", picturePublicationPackageWithImages(t, tc.imagePaths))

			status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
			if err != nil {
				t.Fatal(err)
			}
			var result publicationDeliveryResult
			if err := json.Unmarshal(evidence, &result); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus || result.Code != tc.wantCode || result.Attempted {
				t.Fatalf("status=%q evidence=%s", status, evidence)
			}
		})
	}
}

func TestFinalizePicturePublicationRejectsWildcardImagePath(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	task.Type = model.TaskTypeWechatPicture
	task.Channel = model.ChannelWechatPicture
	api := &fakeWechatPublicationAPI{draftListResponse: &appwechat.DraftBatchGetResponse{}, addResponse: &appwechat.DraftAddResponse{MediaID: "must-not-publish"}}
	logger := zerolog.New(io.Discard)
	svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger))
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", canonicalPicturePublicationPackage(t, "ready", "ready", nil, map[string]any{"image_paths": []string{"output/image_*.png"}}))
	status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliveryFailed || result.Code != "publication_package_invalid" || result.Attempted || api.addCalls != 0 {
		t.Fatalf("status=%q code=%q attempted=%t add_calls=%d", status, result.Code, result.Attempted, api.addCalls)
	}
}

func TestFinalizePicturePublicationRejectsNonImageTaskFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		role     string
		mimeType string
	}{
		{name: "non-image role and mime", role: model.FileRoleOther, mimeType: "application/json"},
		{name: "image role with non-image mime", role: model.FileRoleCover, mimeType: "application/json"},
		{name: "content role used as cover", role: model.FileRoleImage, mimeType: "image/png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, task, execution := setupCloudCompletionTest(t, false)
			task.Type = model.TaskTypeWechatPicture
			task.Channel = model.ChannelWechatPicture
			api := &fakeWechatPublicationAPI{
				draftListResponse: &appwechat.DraftBatchGetResponse{},
				addResponse:       &appwechat.DraftAddResponse{MediaID: "must-not-publish"},
			}
			logger := zerolog.New(io.Discard)
			svc.SetWechatPublicationService(NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
				return api, nil
			}, &logger))
			addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", canonicalPicturePublicationPackage(t, "ready", "ready", nil, nil))
			if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
				ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered,
				Role: tc.role, FilePath: "output/cover.png", FileName: "cover.png", MimeType: tc.mimeType,
				FileSize: 1, MediaID: "forged-media-id",
			}); err != nil {
				t.Fatal(err)
			}

			status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
			if err != nil {
				t.Fatal(err)
			}
			var result publicationDeliveryResult
			if err := json.Unmarshal(evidence, &result); err != nil {
				t.Fatal(err)
			}
			if status != model.TaskExecutionDraftDeliveryBlocked || result.Code != "image_media_invalid" || result.Attempted {
				t.Fatalf("status=%q evidence=%s", status, evidence)
			}
			if api.addCalls != 0 {
				t.Fatalf("WeChat draft calls=%d, want none", api.addCalls)
			}
		})
	}
}

func TestPictureImageCountRejectsNonIntegersAndNonFiniteValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
	}{
		{name: "fractional float", raw: float64(1.5)},
		{name: "nan", raw: math.NaN()},
		{name: "positive infinity", raw: math.Inf(1)},
		{name: "fractional json number", raw: json.Number("1.5")},
		{name: "large json integer", raw: json.Number("9223372036854775807")},
		{name: "zero", raw: float64(0)},
		{name: "too large", raw: float64(21)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := pictureImageCount(tc.raw); ok || got != 0 {
				t.Fatalf("pictureImageCount(%v) = (%d, %t), want (0, false)", tc.raw, got, ok)
			}
		})
	}
}

func TestFinalizePicturePublicationDeliversCanonicalPackageToWechat(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	task.Type = model.TaskTypeWechatPicture
	task.Channel = model.ChannelWechatPicture
	task.SetAgentInput(map[string]any{"picture_image_count": 1})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatPublicationAPI{
		draftListResponse: &appwechat.DraftBatchGetResponse{},
		addResponse:       &appwechat.DraftAddResponse{MediaID: "picture-draft-1"},
	}
	logger := zerolog.New(io.Discard)
	publication := NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) {
		return api, nil
	}, &logger)
	svc.SetWechatPublicationService(publication)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", canonicalPicturePublicationPackage(t, "ready", "ready", nil, nil))
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleCover, FilePath: "output/cover.png", FileName: "cover.png", MimeType: "image/png",
		FileSize: 1, MediaID: "image-media-1",
	}); err != nil {
		t.Fatal(err)
	}

	status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliverySucceeded || result.Status != status || !result.Attempted || result.Code != "" {
		t.Fatalf("status=%q evidence=%s", status, evidence)
	}
	if api.addCalls != 1 || len(api.addRequests) != 1 || len(api.addRequests[0].Articles) != 1 {
		t.Fatalf("WeChat newspic calls=%d requests=%#v", api.addCalls, api.addRequests)
	}
	article := api.addRequests[0].Articles[0]
	if article.ArticleType != "newspic" || article.Title != "图片消息" || article.Content != "图下注释" || article.ImageInfo == nil || len(article.ImageInfo.ImageList) != 1 || article.ImageInfo.ImageList[0].ImageMediaID != "image-media-1" {
		t.Fatalf("newspic request=%#v", article)
	}
}

func TestFinalizePicturePublicationUploadsGeneratedImagesAndLeavesWechatDraft(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	task.Type = model.TaskTypeWechatPicture
	task.Channel = model.ChannelWechatPicture
	task.SetAgentInput(map[string]any{"picture_image_count": 2})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	store := svc.store.(*fakeTaskStorage)
	imageBodies := map[string][]byte{
		"cover-bytes": {0x89, 'P', 'N', 'G'},
		"image-bytes": {0x89, 'P', 'N', 'G', 1},
	}
	coverKey, imageKey := "tasks/picture/cover.png", "tasks/picture/image_01.png"
	store.files[coverKey], store.files[imageKey] = imageBodies["cover-bytes"], imageBodies["image-bytes"]
	api := &fakeWechatPublicationAPI{
		draftListResponse: &appwechat.DraftBatchGetResponse{},
		addResponse:       &appwechat.DraftAddResponse{MediaID: "picture-draft-1"},
	}
	logger := zerolog.New(io.Discard)
	publicationSvc := NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return api, nil }, &logger)
	var uploads []string
	publicationSvc.SetMaterialUploader(func(_ context.Context, _ *model.Project, data []byte, filename string) (*appwechat.UploadMaterialResult, error) {
		if filename == "cover.png" && string(data) != string(imageBodies["cover-bytes"]) {
			t.Fatalf("cover upload bytes = %v", data)
		}
		if filename == "image_01.png" && string(data) != string(imageBodies["image-bytes"]) {
			t.Fatalf("content upload bytes = %v", data)
		}
		uploads = append(uploads, filename)
		return &appwechat.UploadMaterialResult{MediaID: "media-" + filename, WechatURL: "https://mmbiz.example/" + filename}, nil
	})
	svc.SetWechatPublicationService(publicationSvc)
	addCloudOutcomeArtifact(t, svc, repo, task, execution, "output/publish-package.json", "application/json", picturePublicationPackageWithImages(t, []string{"output/image_01.png"}))
	for _, file := range []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleCover, FilePath: "output/cover.png", FileName: "cover.png", MimeType: "image/png", FileSize: 4, OSSKey: coverKey, StorageProvider: store.Name()},
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleImage, FilePath: "output/image_01.png", FileName: "image_01.png", MimeType: "image/png", FileSize: 5, OSSKey: imageKey, StorageProvider: store.Name()},
	} {
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatal(err)
		}
	}

	status, evidence, err := svc.finalizePicturePublication(ctx, task, execution)
	if err != nil {
		t.Fatal(err)
	}
	var result publicationDeliveryResult
	if err := json.Unmarshal(evidence, &result); err != nil {
		t.Fatal(err)
	}
	if status != model.TaskExecutionDraftDeliverySucceeded || !result.Attempted || result.Code != "" {
		t.Fatalf("status=%q evidence=%s", status, evidence)
	}
	if got := strings.Join(uploads, ","); got != "cover.png,image_01.png" {
		t.Fatalf("material uploads = %q", got)
	}
	if api.addCalls != 1 || api.submitCalls != 0 {
		t.Fatalf("draft_add calls=%d formal publish calls=%d, want 1 and 0", api.addCalls, api.submitCalls)
	}
	article := api.addRequests[0].Articles[0]
	if article.ArticleType != "newspic" || article.ImageInfo == nil || len(article.ImageInfo.ImageList) != 2 ||
		article.ImageInfo.ImageList[0].ImageMediaID != "media-cover.png" || article.ImageInfo.ImageList[1].ImageMediaID != "media-image_01.png" {
		t.Fatalf("newspic request = %#v", article)
	}
	publication, err := repo.WechatPublications().FindByTaskID(ctx, task.ID)
	if err != nil || publication.DraftMediaID != "picture-draft-1" || publication.Status != model.WechatPublicationStatusDrafted {
		t.Fatalf("stored publication = %#v, err=%v", publication, err)
	}
}

func TestFinalizeCloudDraftDeliveryReplaysPersistedBlockIntoLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.TaskTypeWechatArticle)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	evidence := encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryFailed, evidence); err != nil || !won {
		t.Fatalf("record block: %v %v", won, err)
	}
	if err := svc.finalizeCloudDraftDelivery(ctx, task, execution, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStateBlocked {
		t.Fatal("persisted preflight result was not projected on replay")
	}
}

func TestCompleteFailedExecutionDoesNotRestoreSkippedPublicationStagesToPending(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(ctx, execution.ID, &agent.ExecutionResult{Success: false, RemoteArtifacts: true, Error: "generation failed"}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range stored.Lifecycle.Data().Stages[3:] {
		if stage.State != model.TaskLifecycleStateSkipped {
			t.Fatalf("publication stage after failed generation = %#v", stage)
		}
	}
}

func TestGetTaskRepairsStalePublicationLifecycleWithoutCreatingDraft(t *testing.T) {
	ctx := context.Background()
	svc, repo, task, execution := setupTaskLifecycleTest(t, model.TaskTypeWechatArticle)
	if _, err := svc.SetTaskProgressPlan(ctx, task.ID, execution.ID, validLifecyclePlan()); err != nil {
		t.Fatal(err)
	}
	evidence := encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryBlocked, "cover_media_missing", false, "retry_visuals")
	if won, err := repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", model.TaskExecutionDraftDeliveryBlocked, evidence); err != nil || !won {
		t.Fatalf("record preflight: %v, %v", won, err)
	}
	if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	found, err := svc.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatal("unauthorized task lookup mutated the lifecycle")
	}
	svc.RefreshTaskPublicationLifecycle(ctx, "another-user", found)
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || stored.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatalf("cross-owner refresh changed lifecycle: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	svc.RefreshTaskPublicationLifecycle(cancelled, task.UserID, found)
	if found.Lifecycle.Data().Stages[3].State != model.TaskLifecycleStatePending {
		t.Fatal("failed best-effort refresh changed the readable task")
	}
	for range 2 {
		found, err := svc.GetByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		svc.RefreshTaskPublicationLifecycle(ctx, task.UserID, found)
		draft := found.Lifecycle.Data().Stages[3]
		if draft.State != model.TaskLifecycleStateBlocked || draft.LatestUpdate != "封面尚未生成或上传完成，微信尚未收到请求。" {
			t.Fatalf("read draft stage = %#v", draft)
		}
	}
	if _, err := repo.WechatPublications().FindByTaskID(ctx, task.ID); err == nil {
		t.Fatal("reading task created a publication")
	}
}

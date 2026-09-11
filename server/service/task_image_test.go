package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appimage "github.com/anbanai/anban-creator/app/image"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type taskImageResolverFake struct {
	resolved        *ResolvedImageModel
	calls           int
	userID          string
	imageCapability model.ImageCapabilitySnapshot
	imageType       string
	referenceCount  int
}

func (f *taskImageResolverFake) ResolveFrozenImageModelForGeneration(_ context.Context, userID string, imageCapability model.ImageCapabilitySnapshot, imageType string, referenceCount int) (*ResolvedImageModel, error) {
	f.calls++
	f.userID, f.imageCapability, f.imageType, f.referenceCount = userID, imageCapability, imageType, referenceCount
	return f.resolved, nil
}

type taskImageGeneratorFake struct {
	result         *ImageResult
	calls          int
	analyzeCalls   int
	uploadCalls    int
	prompts        []string
	referencePaths []string
	referenceData  [][]byte
	readReferences bool
	resolvedSize   string
}

type failingTaskImageDeleteStorage struct {
	storage.Provider
	err error
}

func (s *failingTaskImageDeleteStorage) Delete(context.Context, string) error { return s.err }

func (f *taskImageGeneratorFake) GenerateImage(_ context.Context, _, _, prompt, _, outputPath, _ string, referencePaths []string, _ string, aspectRatio string, _ *ResolvedImageModel, _ *bool) (*ImageResult, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	f.referencePaths = append([]string(nil), referencePaths...)
	f.resolvedSize = aspectRatio
	if f.readReferences {
		for _, referencePath := range referencePaths {
			data, err := os.ReadFile(referencePath)
			if err != nil {
				return nil, err
			}
			f.referenceData = append(f.referenceData, data)
		}
	}
	copy := *f.result
	copy.FilePath = outputPath
	return &copy, nil
}

func (f *taskImageGeneratorFake) AnalyzeImage() { f.analyzeCalls++ }
func (f *taskImageGeneratorFake) UploadImage()  { f.uploadCalls++ }

type taskImageFixture struct {
	service     *TaskImageService
	wallet      *BillingWalletService
	db          *gorm.DB
	repo        repository.Repository
	resolver    *taskImageResolverFake
	generator   *taskImageGeneratorFake
	userID      string
	projectID   string
	taskID      string
	executionID string
}

func newTaskImageFixture(t *testing.T) *taskImageFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:8]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusRunning, ImageCapabilityKey: "standard", ImageRatio: "3:4",
		BillingCatalogID: "retail-task-image-v1", BillingSKUID: "task.seednote.effective", BillingPricingTier: string(model.TierFree),
	}
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true,
	}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}

	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := newTestTaskService(repo, nil, store, &logger, "", nil, nil)
	bundle := &serverbilling.Bundle{Products: serverbilling.ProductCatalog{
		CatalogID: "retail-task-image-v1", Currency: "credits",
		TierRatesPercent: map[string]int64{"free": 100, "pro": 90, "enterprise": 80},
		SKUs: []serverbilling.SKUConfig{
			{ID: "image.standard", Operation: "image.generate", Route: "image_generation.capabilities.standard", ChargePolicy: "image_operation", PriceCredits: 500, Delivery: "persisted_image"},
			{ID: "image.professional", Operation: "image.generate", Route: "image_generation.capabilities.professional", ChargePolicy: "image_operation", PriceCredits: 900, Delivery: "persisted_image"},
		},
	}, Economics: serverbilling.EconomicsConfig{CreditsPerCNY: 1_000}, Policy: serverbilling.PolicySnapshot{AcceptedTask: serverbilling.AcceptedTaskPolicy{
		ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true,
	}}}
	catalog := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: userID, PaidCredits: 1000}); err != nil {
		t.Fatal(err)
	}
	createBillingLot(t, repo, model.BillingCreditLot{ID: uuid.NewString(), UserID: userID,
		Kind: model.BillingCreditLotKindPaid, SourceType: "fixture", SourceID: uuid.NewString(),
		CatalogID: bundle.Products.CatalogID, OriginalCredits: 1000, AvailableCredits: 1000})
	wallet := NewBillingWalletService(repo, bundle, BillingWalletOptions{})
	taskSvc.SetBillingWalletService(wallet)

	generatedPath := filepath.Join(t.TempDir(), "generated.png")
	if err := os.WriteFile(generatedPath, taskImageTinyPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &taskImageResolverFake{resolved: &ResolvedImageModel{
		Provider: "openai", Model: "gpt-image-2", SelectionReason: "preferred",
		BillingSKU:        "image.standard",
		Key:               "standard",
		SupportsReference: true, MaxReferenceImages: 16,
	}}
	generator := &taskImageGeneratorFake{result: &ImageResult{
		LocalFilePath: generatedPath, OutputMIME: "image/png", Size: "3:4",
		Provider: "secret-provider", Model: "secret-model", RevisedPrompt: "secret prompt",
	}}
	return &taskImageFixture{
		service: NewTaskImageService(taskSvc, resolver, generator, catalog, &logger), wallet: wallet, db: db,
		repo: repo, resolver: resolver, generator: generator,
		userID: userID, projectID: projectID, taskID: taskID, executionID: executionID,
	}
}

func (f *taskImageFixture) request() GenerateTaskImageRequest {
	return GenerateTaskImageRequest{
		UserID: f.userID, ExecutionID: f.executionID, TaskID: f.taskID, ProjectID: f.projectID,
		Prompt: "draw a tea cover", ImageType: "cover", OutputPath: "output/cover.png", AspectRatio: "3:4",
	}
}

func TestGenerateTaskImagePersistsAndSettlesAtomically(t *testing.T) {
	f := newTaskImageFixture(t)
	asset, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	if asset.TaskFileID == "" || asset.FilePath != "output/cover.png" || asset.DownloadURL == "" ||
		asset.MimeType != "image/png" || asset.FileSize != int64(len(taskImageTinyPNG())) || asset.ContentHash != hashTaskFileContent(taskImageTinyPNG()) {
		t.Fatalf("asset = %#v", asset)
	}
	if f.generator.resolvedSize != "3:4" {
		t.Fatalf("provider size=%q, want request ratio", f.generator.resolvedSize)
	}
	var files, settlements int64
	if err := f.db.Model(&model.TaskFile{}).Count(&files).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil {
		t.Fatal(err)
	}
	if files != 1 || settlements != 1 {
		t.Fatalf("task files=%d settlements=%d, want 1/1", files, settlements)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v, want one processed image settlement", processed, err)
	}
	var account model.BillingWalletAccount
	if err := f.db.Where("user_id = ?", f.userID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.PaidCredits != 500 {
		t.Fatalf("paid credits = %d, want 500 after image charge", account.PaidCredits)
	}
	var charges []model.BillingCharge
	if err := f.db.Where("operation_task_id = ?", f.taskID).Find(&charges).Error; err != nil {
		t.Fatal(err)
	}
	if len(charges) != 1 || charges[0].SKUID != "image.standard" || charges[0].PriceCredits != 500 {
		t.Fatalf("standard image charges = %#v, want one image.standard charge at 500 credits", charges)
	}
}

func TestGenerateTaskImageRejectsFrozenCapabilityDriftBeforeProviderOrBilling(t *testing.T) {
	f := newTaskImageFixture(t)
	route := testImageCapabilityRoute("image.standard")
	cfg := &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities:      map[string]serverconfig.ImageGenerationRouteConfig{"standard": route},
	}}}
	f.service.resolver = NewImageCapabilityResolver(f.repo, cfg)
	route.BillingSKU = "image.professional"
	cfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route

	_, err := f.service.Generate(context.Background(), f.request())
	if !errors.Is(err, ErrTaskImageCapabilityConflict) {
		t.Fatalf("Generate drift error = %v, want ErrTaskImageCapabilityConflict", err)
	}
	if f.generator.calls != 0 {
		t.Fatalf("provider calls = %d, want 0 before frozen capability validation", f.generator.calls)
	}
	var files, settlements, charges int64
	_ = f.db.Model(&model.TaskFile{}).Count(&files).Error
	_ = f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error
	_ = f.db.Model(&model.BillingCharge{}).Count(&charges).Error
	if files != 0 || settlements != 0 || charges != 0 {
		t.Fatalf("drift side effects files=%d settlements=%d charges=%d, want 0/0/0", files, settlements, charges)
	}
}

func TestGenerateTaskImageRejectsRatioOutsideBusinessContractWithoutProviderCall(t *testing.T) {
	f := newTaskImageFixture(t)
	req := f.request()
	req.AspectRatio = "2:3"
	_, err := f.service.Generate(context.Background(), req)
	var ratioErr *ImageRatioNotAllowedError
	if !errors.As(err, &ratioErr) || ratioErr.RequestedRatio != "2:3" {
		t.Fatalf("ratio error = %#v, %v", ratioErr, err)
	}
	if f.generator.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", f.generator.calls)
	}
}

func TestGenerateTaskImageRejectsMissingOrAutoAspectRatioWithoutProviderCall(t *testing.T) {
	for _, ratio := range []string{"", "auto", "1024x1536"} {
		f := newTaskImageFixture(t)
		req := f.request()
		req.AspectRatio = ratio
		_, err := f.service.Generate(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "aspect_ratio") {
			t.Fatalf("Generate(%q) error = %v, want aspect_ratio validation", ratio, err)
		}
		if f.resolver.calls != 0 || f.generator.calls != 0 {
			t.Fatalf("resolver/provider calls = %d/%d, want 0/0", f.resolver.calls, f.generator.calls)
		}
	}
}

func TestGenerateTaskImageRejectsMissingFrozenCapabilityBeforeResolution(t *testing.T) {
	f := newTaskImageFixture(t)
	if err := f.db.Model(&model.Task{}).
		Where("id = ?", f.taskID).
		Update("image_capability_key", "").Error; err != nil {
		t.Fatal(err)
	}

	_, err := f.service.Generate(context.Background(), f.request())
	if err == nil || !strings.Contains(err.Error(), "frozen image capability") {
		t.Fatalf("Generate error = %v, want missing frozen capability rejection", err)
	}
	if f.resolver.calls != 0 || f.generator.calls != 0 {
		t.Fatalf("missing frozen capability reached resolver/provider: resolver=%d provider=%d", f.resolver.calls, f.generator.calls)
	}
}

func TestGenerateTaskImageInjectsStrictRatioRequirement(t *testing.T) {
	f := newTaskImageFixture(t)
	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	got := f.generator.prompts[0]
	for _, required := range []string{
		"draw a tea cover",
		"最终图片画布宽高比必须严格为 3:4",
		"该比例是交付要求，不是构图建议",
		"不得输出 2:3、9:16、近似比例、留白边框或内嵌画布",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("prompt missing %q: %s", required, got)
		}
	}
}

func TestAppendStrictImageRatioRequirementDoesNotForbidRequestedRatio(t *testing.T) {
	got := appendStrictImageRatioRequirement("draw a portrait video cover", "9:16")
	if !strings.Contains(got, "最终图片画布宽高比必须严格为 9:16") {
		t.Fatalf("prompt missing requested ratio: %s", got)
	}
	if strings.Contains(got, "不得输出 2:3、9:16") {
		t.Fatalf("prompt forbids its requested ratio: %s", got)
	}
}

func TestGenerateTaskImageRejectsAbsoluteReferencePathBeforeProviderCall(t *testing.T) {
	f := newTaskImageFixture(t)
	req := f.request()
	req.ReferencePaths = []string{filepath.Join(t.TempDir(), "host-secret.png")}

	if _, err := f.service.Generate(context.Background(), req); err == nil || !strings.Contains(err.Error(), "task-relative") {
		t.Fatalf("Generate absolute reference error = %v, want task-relative rejection", err)
	}
	if f.generator.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", f.generator.calls)
	}
}

func TestGenerateTaskImageMaterializesFrozenInputAttachment(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	imageBytes := taskImageTinyPNG()
	uploadID := uuid.NewString()
	key := "assets/users/" + f.userID + "/" + uploadID + "/input.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: uploadID, UserID: f.userID, Purpose: DirectUploadPurposeAIEntryAttachment,
		StagingKey: "uploads/pending/" + f.userID + "/" + uploadID + "/input.png",
		FileName:   "input.png", ContentType: "image/png", Size: int64(len(imageBytes)),
		Status: model.UploadSessionFinalized, AssetID: uploadID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: uploadID, UserID: f.userID, Purpose: DirectUploadPurposeAIEntryAttachment,
		StorageKey: key, FileName: "input.png", ContentType: "image/png",
		Size: int64(len(imageBytes)), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().UpdateInputAttachments(ctx, f.taskID, []model.EntryAttachment{{
		AssetID: uploadID, Type: "image", FileName: "input.png",
		ContentType: "image/png", Size: int64(len(imageBytes)),
	}}); err != nil {
		t.Fatal(err)
	}
	f.generator.readReferences = true
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/input-attachments/attachment_01_input.png"}

	if _, err := f.service.Generate(ctx, req); err != nil {
		t.Fatalf("Generate with task input attachment: %v", err)
	}
	if len(f.generator.referencePaths) != 1 || !filepath.IsAbs(f.generator.referencePaths[0]) || len(f.generator.referenceData) != 1 || !bytes.Equal(f.generator.referenceData[0], imageBytes) {
		t.Fatalf("materialized references paths=%#v data=%d", f.generator.referencePaths, len(f.generator.referenceData))
	}
	if _, err := os.Stat(f.generator.referencePaths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialized reference still exists after generation: %v", err)
	}
}

func TestGenerateTaskImageRejectsFrozenInputAttachmentWithoutUniqueAssetID(t *testing.T) {
	tests := []struct {
		name       string
		attachment model.EntryAttachment
	}{
		{
			name: "upload id fallback",
			attachment: model.EntryAttachment{
				Type: "image", UploadID: "asset-1", FileName: "input.png",
				ContentType: "image/png", Size: int64(len(taskImageTinyPNG())),
			},
		},
		{
			name: "asset id with asserted key",
			attachment: model.EntryAttachment{
				AssetID: "asset-1", Type: "image", Key: "assets/users/user-1/asset-1/input.png", FileName: "input.png",
				ContentType: "image/png", Size: int64(len(taskImageTinyPNG())),
			},
		},
		{
			name: "asset id with asserted url",
			attachment: model.EntryAttachment{
				AssetID: "asset-1", Type: "image", URL: "/api/v1/files/assets/users/user-1/asset-1/input.png", FileName: "input.png",
				ContentType: "image/png", Size: int64(len(taskImageTinyPNG())),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTaskImageFixture(t)
			ctx := context.Background()
			assetID := "asset-1"
			key := "assets/users/" + f.userID + "/" + assetID + "/input.png"
			imageBytes := taskImageTinyPNG()
			if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.Assets().Create(ctx, &model.Asset{
				ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeAIEntryAttachment,
				StorageKey: key, FileName: "input.png", ContentType: "image/png",
				Size: int64(len(imageBytes)), ETag: "immutable",
			}); err != nil {
				t.Fatal(err)
			}
			attachment := tt.attachment
			if attachment.AssetID != "" {
				attachment.AssetID = assetID
			}
			if attachment.UploadID != "" {
				attachment.UploadID = assetID
			}
			if attachment.Key != "" {
				attachment.Key = key
			}
			if err := f.repo.Tasks().UpdateInputAttachments(ctx, f.taskID, []model.EntryAttachment{attachment}); err != nil {
				t.Fatal(err)
			}
			req := f.request()
			req.ReferencePaths = []string{".anban-creator/input-attachments/attachment_01_input.png"}

			if _, err := f.service.Generate(ctx, req); err == nil || !strings.Contains(err.Error(), "unique immutable asset identity") {
				t.Fatalf("Generate ambiguous attachment error = %v, want unique identity rejection", err)
			}
			if f.generator.calls != 0 {
				t.Fatalf("provider calls = %d, want 0", f.generator.calls)
			}
		})
	}
}

func TestGenerateTaskImageMaterializesCurrentResumeImageAttachment(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	imageBytes := taskImageTinyPNG()
	key := "uploads/users/" + f.userID + "/projects/" + f.projectID + "/tasks/" + f.taskID + "/resume/20260910/attachments/resume.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().UpdateInputAttachments(ctx, f.taskID, []model.EntryAttachment{{
		Type: "image", Role: model.EntryAttachmentRoleResumeFile, Key: key, FileName: "resume.png",
		ContentType: "image/png", Size: int64(len(imageBytes)),
	}}); err != nil {
		t.Fatal(err)
	}
	f.generator.readReferences = true
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/resume/attachments/resume.png"}

	if _, err := f.service.Generate(ctx, req); err != nil {
		t.Fatalf("Generate with resume image attachment: %v", err)
	}
	if len(f.generator.referenceData) != 1 || !bytes.Equal(f.generator.referenceData[0], imageBytes) {
		t.Fatalf("materialized resume reference data = %#v", f.generator.referenceData)
	}
}

func TestGenerateTaskImageRejectsUnauthorizedResumeImageAttachments(t *testing.T) {
	tests := []struct {
		name                string
		keyFor              func(*taskImageFixture) string
		fileName            string
		content             []byte
		declaredContentType string
		wantMessage         string
	}{
		{
			name: "foreign task namespace",
			keyFor: func(f *taskImageFixture) string {
				return "uploads/users/other-user/projects/" + f.projectID + "/tasks/" + f.taskID + "/resume/20260910/attachments/resume.png"
			},
			fileName:    "resume.png",
			content:     taskImageTinyPNG(),
			wantMessage: "outside the task resume namespace",
		},
		{
			name: "storage filename mismatch",
			keyFor: func(f *taskImageFixture) string {
				return "uploads/users/" + f.userID + "/projects/" + f.projectID + "/tasks/" + f.taskID + "/resume/20260910/attachments/other.png"
			},
			fileName:    "resume.png",
			content:     taskImageTinyPNG(),
			wantMessage: "outside the task resume namespace",
		},
		{
			name: "declared image contains text",
			keyFor: func(f *taskImageFixture) string {
				return "uploads/users/" + f.userID + "/projects/" + f.projectID + "/tasks/" + f.taskID + "/resume/20260910/attachments/resume.png"
			},
			fileName:    "resume.png",
			content:     []byte("this is not an image"),
			wantMessage: "content is not an image",
		},
		{
			name: "declared MIME differs from raster bytes",
			keyFor: func(f *taskImageFixture) string {
				return "uploads/users/" + f.userID + "/projects/" + f.projectID + "/tasks/" + f.taskID + "/resume/20260910/attachments/resume.png"
			},
			fileName:            "resume.png",
			content:             taskImageTinyPNG(),
			declaredContentType: "image/jpeg",
			wantMessage:         "declared MIME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTaskImageFixture(t)
			ctx := context.Background()
			key := tt.keyFor(f)
			if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(tt.content), "image/png"); err != nil {
				t.Fatal(err)
			}
			contentType := tt.declaredContentType
			if contentType == "" {
				contentType = "image/png"
			}
			if err := f.repo.Tasks().UpdateInputAttachments(ctx, f.taskID, []model.EntryAttachment{{
				Type: "image", Role: model.EntryAttachmentRoleResumeFile, Key: key, FileName: tt.fileName,
				ContentType: contentType, Size: int64(len(tt.content)),
			}}); err != nil {
				t.Fatal(err)
			}
			req := f.request()
			req.ReferencePaths = []string{".anban-creator/resume/attachments/" + tt.fileName}

			_, err := f.service.Generate(ctx, req)
			if err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Generate error = %v, want %q", err, tt.wantMessage)
			}
			if f.generator.calls != 0 {
				t.Fatalf("generator calls = %d, want 0", f.generator.calls)
			}
		})
	}
}

func TestGenerateTaskImageMaterializesFrozenEcommerceProduct(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	imageBytes := taskImageTinyPNG()
	assetID := uuid.NewString()
	key := "assets/users/" + f.userID + "/" + assetID + "/product.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeEcommercePhoto,
		StorageKey: key, FileName: "product.png", ContentType: "image/png",
		Size: int64(len(imageBytes)), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().UpdateInputAttachments(ctx, f.taskID, []model.EntryAttachment{{
		AssetID: assetID, Type: "image", FileName: "product.png",
		ContentType: "image/png", Size: int64(len(imageBytes)),
	}}); err != nil {
		t.Fatal(err)
	}
	f.generator.readReferences = true
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/input-attachments/attachment_01_product.png"}

	if _, err := f.service.Generate(ctx, req); err != nil {
		t.Fatalf("Generate with frozen ecommerce product: %v", err)
	}
	if len(f.generator.referenceData) != 1 || !bytes.Equal(f.generator.referenceData[0], imageBytes) {
		t.Fatalf("materialized ecommerce reference data = %#v", f.generator.referenceData)
	}
}

func TestGenerateTaskImageRejectsInheritedProjectStyleReference(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	assetID := uuid.NewString()
	key := "assets/users/" + f.userID + "/" + assetID + "/style.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(taskImageTinyPNG()), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeProjectReference,
		StorageKey: key, FileName: "style.png", ContentType: "image/png",
		Size: int64(len(taskImageTinyPNG())), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := f.repo.Tasks().FindByID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformSeednote, ReferenceImageAssetID: assetID})
	if err := f.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/project-style-reference.png"}

	if _, err := f.service.Generate(ctx, req); err == nil || !strings.Contains(err.Error(), "project style reference") {
		t.Fatalf("Generate project style reference error = %v, want prompt-only rejection", err)
	}
	if f.generator.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", f.generator.calls)
	}
}

func TestGenerateTaskImageRejectsInheritedProjectStyleReferenceForMontage(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	assetID := uuid.NewString()
	imageBytes := taskImageTinyPNG()
	key := "assets/users/" + f.userID + "/" + assetID + "/portrait.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeProjectReference,
		StorageKey: key, FileName: "portrait.png", ContentType: "image/png",
		Size: int64(len(imageBytes)), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := f.repo.Tasks().FindByID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Type = model.PlatformMontage
	task.ImageRatio = "9:16"
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformMontage, ReferenceImageAssetID: assetID})
	if err := f.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	generatedPath := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(generatedPath, taskImagePNG(9, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	f.generator.result.LocalFilePath = generatedPath
	req := f.request()
	req.AspectRatio = "9:16"
	req.ReferencePaths = []string{".anban-creator/project-style-reference.png"}

	if _, err := f.service.Generate(ctx, req); err == nil || !strings.Contains(err.Error(), "project style reference") {
		t.Fatalf("Generate with Montage project style reference = %v, want prompt-only rejection", err)
	}
	if f.generator.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", f.generator.calls)
	}
}

func TestGenerateTaskImageMaterializesDirectTaskReference(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	assetID := uuid.NewString()
	imageBytes := taskImageTinyPNG()
	key := "assets/users/" + f.userID + "/" + assetID + "/reference.png"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeTaskReference,
		StorageKey: key, FileName: "reference.png", ContentType: "image/png",
		Size: int64(len(imageBytes)), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := f.repo.Tasks().FindByID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.ReferenceImageAssetID = assetID
	if err := f.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	f.generator.readReferences = true
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/task-reference.png"}

	if _, err := f.service.Generate(ctx, req); err != nil {
		t.Fatalf("Generate with direct task reference: %v", err)
	}
	if len(f.generator.referenceData) != 1 || !bytes.Equal(f.generator.referenceData[0], imageBytes) {
		t.Fatalf("reference data = %#v", f.generator.referenceData)
	}
}

func TestGenerateTaskImagePreservesDirectJPEGReferenceMIMEForProvider(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	assetID := uuid.NewString()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.NRGBA{G: 0xff, A: 0xff})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	imageBytes := encoded.Bytes()
	key := "assets/users/" + f.userID + "/" + assetID + "/reference.jpg"
	if _, err := f.service.tasks.Storage().Upload(ctx, key, bytes.NewReader(imageBytes), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Assets().Create(ctx, &model.Asset{
		ID: assetID, UserID: f.userID, Purpose: DirectUploadPurposeTaskReference,
		StorageKey: key, FileName: "reference.jpg", ContentType: "image/jpeg",
		Size: int64(len(imageBytes)), ETag: "immutable",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := f.repo.Tasks().FindByID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.ReferenceImageAssetID = assetID
	if err := f.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	f.generator.readReferences = true
	req := f.request()
	req.ReferencePaths = []string{".anban-creator/task-reference.png"}

	if _, err := f.service.Generate(ctx, req); err != nil {
		t.Fatalf("Generate with direct JPEG reference: %v", err)
	}
	if len(f.generator.referenceData) != 1 || !bytes.Equal(f.generator.referenceData[0], imageBytes) {
		t.Fatalf("reference data = %#v", f.generator.referenceData)
	}
	if len(f.generator.referencePaths) != 1 || filepath.Ext(f.generator.referencePaths[0]) != ".jpg" {
		t.Fatalf("provider reference path = %#v, want a .jpg suffix", f.generator.referencePaths)
	}
}

func TestGenerateTaskImageRejectsStaleExecutionBeforeResolution(t *testing.T) {
	f := newTaskImageFixture(t)
	req := f.request()
	req.ExecutionID = uuid.NewString()

	if _, err := f.service.Generate(context.Background(), req); err == nil || !strings.Contains(err.Error(), "execution") {
		t.Fatalf("Generate stale execution error = %v", err)
	}
	if f.resolver.calls != 0 || f.generator.calls != 0 {
		t.Fatalf("stale execution reached resolver/provider: resolver=%d provider=%d", f.resolver.calls, f.generator.calls)
	}
}

func TestGenerateTaskImageRejectsMissingExecutionIdentityBeforeResolution(t *testing.T) {
	f := newTaskImageFixture(t)
	req := f.request()
	req.ExecutionID = ""

	if _, err := f.service.Generate(context.Background(), req); err == nil || !strings.Contains(err.Error(), "execution identity") {
		t.Fatalf("Generate missing execution identity error = %v", err)
	}
	if f.resolver.calls != 0 || f.generator.calls != 0 {
		t.Fatalf("missing execution identity reached resolver/provider: resolver=%d provider=%d", f.resolver.calls, f.generator.calls)
	}
}

func TestGenerateTaskImageRejectsExactRatioMismatchBeforePersistenceOrCharge(t *testing.T) {
	f := newTaskImageFixture(t)
	wrongPath := filepath.Join(t.TempDir(), "wrong.png")
	if err := os.WriteFile(wrongPath, taskImagePNG(2, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	f.generator.result.LocalFilePath = wrongPath
	f.generator.result.Width, f.generator.result.Height = 2, 3
	_, err := f.service.Generate(context.Background(), f.request())
	var mismatch *ImageRatioMismatchError
	if !errors.As(err, &mismatch) || mismatch.RequestedRatio != "3:4" || mismatch.ActualWidth != 2 || mismatch.ActualHeight != 3 || mismatch.CapabilityKey != "standard" {
		t.Fatalf("mismatch = %#v, err=%v", mismatch, err)
	}
	var files, settlements int64
	_ = f.db.Model(&model.TaskFile{}).Count(&files).Error
	_ = f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error
	if files != 0 || settlements != 0 {
		t.Fatalf("files=%d settlements=%d, want 0/0", files, settlements)
	}
	if _, statErr := os.Stat(wrongPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("mismatched output still exists: %v", statErr)
	}
}

func TestGenerateTaskImageNormalizesNearestProviderRatioBeforePersistence(t *testing.T) {
	f := newTaskImageFixture(t)
	nearestPath := filepath.Join(t.TempDir(), "nearest.png")
	if err := os.WriteFile(nearestPath, taskImagePNG(8, 12), 0o644); err != nil {
		t.Fatal(err)
	}
	f.generator.result.LocalFilePath = nearestPath
	f.generator.result.Width, f.generator.result.Height = 8, 12

	asset, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatalf("Generate nearest provider ratio: %v", err)
	}
	if asset.TaskFileID == "" {
		t.Fatalf("normalized asset = %#v", asset)
	}
	width, height, err := appimage.GetImageDimensions(nearestPath)
	if err != nil {
		t.Fatal(err)
	}
	if width != 6 || height != 8 {
		t.Fatalf("normalized dimensions = %dx%d, want 6x8", width, height)
	}
}

func TestGenerateTaskImageRejectsBillingSKURouteMismatchWithoutProviderCall(t *testing.T) {
	for _, tt := range []struct {
		name, column, value, wantOperation, wantRoute string
	}{
		{
			name: "operation mismatch", column: "operation", value: "task.article",
			wantOperation: "task.article", wantRoute: "image_generation.capabilities.standard",
		},
		{
			name: "route mismatch", column: "route", value: "image_generation.capabilities.professional",
			wantOperation: "image.generate", wantRoute: "image_generation.capabilities.professional",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newTaskImageFixture(t)
			if err := f.db.Model(&model.BillingSKU{}).
				Where("catalog_id = ? AND sk_uid = ?", "retail-task-image-v1", "image.standard").
				Update(tt.column, tt.value).Error; err != nil {
				t.Fatal(err)
			}

			_, err := f.service.Generate(context.Background(), f.request())
			var mismatch *ImageCapabilityBillingSKUError
			if !errors.As(err, &mismatch) {
				t.Fatalf("Generate() error = %T %v, want ImageCapabilityBillingSKUError", err, err)
			}
			if mismatch.BillingSKU != "image.standard" || mismatch.Operation != tt.wantOperation ||
				mismatch.Route != tt.wantRoute || mismatch.ExpectedOperation != "image.generate" ||
				mismatch.ExpectedRoute != "image_generation.capabilities.standard" {
				t.Fatalf("billing SKU mismatch = %#v", mismatch)
			}
			if f.generator.calls != 0 {
				t.Fatalf("provider calls = %d, want 0", f.generator.calls)
			}
		})
	}
}

func TestCropTaskImageReadsCurrentExecutionFileAndPersistsOutput(t *testing.T) {
	f := newTaskImageFixture(t)
	var encoded bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			source.Set(x, y, color.White)
		}
	}
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	input := encoded.Bytes()
	if _, err := f.service.tasks.UploadExecutionTaskFileFromReader(
		context.Background(), f.taskID, f.userID, f.executionID, "output/source.png",
		bytes.NewReader(input), "image/png", int64(len(input)),
	); err != nil {
		t.Fatal(err)
	}
	ops := NewTaskImageOperationsService(f.service.tasks, nil, nil, nil, TaskImageOperationsConfig{}, nil)
	result, err := ops.Crop(context.Background(), CropTaskImageRequest{
		UserID: f.userID, ExecutionID: f.executionID, TaskID: f.taskID,
		InputPath: "output/source.png", OutputPath: "output/cropped.png",
		TargetWidth: 20, TargetHeight: 10, Anchor: "center",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FilePath != "output/cropped.png" || result.Width != 20 || result.Height != 10 || result.DownloadURL == "" {
		t.Fatalf("crop result = %#v", result)
	}
	if _, err := ops.Crop(context.Background(), CropTaskImageRequest{
		UserID: f.userID, ExecutionID: f.executionID, TaskID: f.taskID,
		InputPath: filepath.Join(t.TempDir(), "source.png"), OutputPath: "output/invalid.png",
		TargetWidth: 20, TargetHeight: 10,
	}); err == nil {
		t.Fatal("absolute input path was accepted")
	}
}

func TestGenerateTaskImageWrongPolicyFailsSettlementWithoutCharge(t *testing.T) {
	f := newTaskImageFixture(t)
	if err := f.db.Model(&model.BillingSKU{}).
		Where("catalog_id = ? AND sk_uid = ?", "retail-task-image-v1", "image.standard").
		Update("policy", "standalone_operation").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v, want one permanently failed settlement", processed, err)
	}
	var settlement model.BillingSettlementOutbox
	if err := f.db.First(&settlement).Error; err != nil {
		t.Fatal(err)
	}
	if settlement.Status != "failed" || !strings.Contains(settlement.LastError, "SKU policy") {
		t.Fatalf("settlement status=%q error=%q", settlement.Status, settlement.LastError)
	}
	var charges int64
	if err := f.db.Model(&model.BillingCharge{}).Count(&charges).Error; err != nil || charges != 0 {
		t.Fatalf("charges=%d err=%v, want no charge", charges, err)
	}
}

func TestGenerateTaskImageReplaysIdenticalSemanticRequest(t *testing.T) {
	f := newTaskImageFixture(t)
	first, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Generate(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	if *first != *second || f.generator.calls != 1 {
		t.Fatalf("first=%#v second=%#v generator calls=%d", first, second, f.generator.calls)
	}
	var settlements int64
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
}

func TestGenerateTaskImageChangedPromptCreatesNewOperation(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	originalBytes := append([]byte(nil), taskImageTinyPNG()...)
	original := f.request()
	first, err := f.service.Generate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.generator.result.LocalFilePath, taskImagePNG(6, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := f.request()
	changed.Prompt = "draw a different tea cover"
	second, err := f.service.Generate(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := f.service.Generate(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if f.generator.calls != 2 {
		t.Fatalf("generator calls=%d, want 2", f.generator.calls)
	}
	if first.DownloadURL == second.DownloadURL {
		t.Fatalf("distinct paid operations share mutable delivery URL %q", first.DownloadURL)
	}
	if replayed.DownloadURL != first.DownloadURL {
		t.Fatalf("replayed download URL=%q, want original %q", replayed.DownloadURL, first.DownloadURL)
	}
	operationID, fingerprint, err := taskImageOperationIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	replayFile, _, err := f.service.tasks.FindExecutionTaskFileSettlement(
		ctx, f.taskID, f.executionID, operationID, fingerprint, "image", taskImageSettlementScope,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayBytes, err := f.service.tasks.Storage().Read(ctx, replayFile.OSSKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(replayBytes) != string(originalBytes) {
		t.Fatalf("replayed bytes=%q, want original image bytes", replayBytes)
	}
	var settlements int64
	if err := f.db.Model(&model.BillingSettlementOutbox{}).Count(&settlements).Error; err != nil || settlements != 2 {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
}

func TestTaskDeleteRemovesEveryImmutableImageOperationObject(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.generator.result.LocalFilePath, taskImagePNG(6, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := f.request()
	changed.Prompt = "draw a different tea cover"
	if _, err := f.service.Generate(ctx, changed); err != nil {
		t.Fatal(err)
	}

	var settlements []model.BillingSettlementOutbox
	if err := f.db.Where("task_id = ?", f.taskID).Order("created_at ASC").Find(&settlements).Error; err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(settlements))
	for _, settlement := range settlements {
		var snapshot struct {
			OperationObject taskFileOperationObjectSnapshot `json:"operation_object"`
		}
		if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, snapshot.OperationObject.OSSKey)
	}
	if len(keys) != 2 || keys[0] == "" || keys[1] == "" || keys[0] == keys[1] {
		t.Fatalf("operation object keys = %v, want two unique keys", keys)
	}
	collectedKey := "uploads/users/" + f.userID + "/tasks/" + f.taskID + "/collected.md"
	cleanupKey := "uploads/users/" + f.userID + "/tasks/" + f.taskID + "/superseded.md"
	if _, err := f.service.tasks.Storage().Upload(ctx, collectedKey, strings.NewReader("collected"), "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.tasks.Storage().Upload(ctx, cleanupKey, strings.NewReader("superseded"), "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: "collected-execution",
		State: model.TaskFileStateCollected, Role: model.FileRoleMarkdown,
		FilePath: "output/collected.md", FileName: "collected.md", MimeType: "text/markdown",
		FileSize: 9, ContentHash: strings.Repeat("a", 64), OSSKey: collectedKey,
		CleanupOSSKey: cleanupKey, StorageProvider: "local",
	}); err != nil {
		t.Fatal(err)
	}
	keys = append(keys, collectedKey, cleanupKey)
	if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if won, err := f.repo.TaskExecutions().Transition(ctx, f.executionID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded,
		model.ExecutionTransition{
			FinalizationStatus: model.TaskExecutionFinalizationDone,
			CleanupStatus:      model.TaskExecutionCleanupDone,
		}); err != nil || !won {
		t.Fatalf("complete execution cleanup: won=%v err=%v", won, err)
	}
	if err := f.service.tasks.Delete(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, err := f.service.tasks.Storage().Read(ctx, key); err == nil {
			t.Errorf("immutable operation object %q still exists after task deletion", key)
		}
	}
}

func TestTaskDeleteRetainsReferencesWhenOperationObjectDeletionFails(t *testing.T) {
	f := newTaskImageFixture(t)
	ctx := context.Background()
	if _, err := f.service.Generate(ctx, f.request()); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	f.service.tasks.store = &failingTaskImageDeleteStorage{
		Provider: f.service.tasks.store,
		err:      errors.New("storage unavailable"),
	}
	if err := f.service.tasks.Delete(ctx, f.taskID); err == nil {
		t.Fatal("task deletion succeeded despite operation object cleanup failure")
	}
	if _, err := f.repo.Tasks().FindByID(ctx, f.taskID); err != nil {
		t.Fatalf("task reference was removed after cleanup failure: %v", err)
	}
	var files int64
	if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Count(&files).Error; err != nil || files != 1 {
		t.Fatalf("task files after cleanup failure = %d, %v; want one retained row", files, err)
	}
}

func TestTaskDeleteRejectsUnknownTaskFileStorageProvider(t *testing.T) {
	for _, provider := range []string{"", "other"} {
		t.Run("provider="+provider, func(t *testing.T) {
			f := newTaskImageFixture(t)
			ctx := context.Background()
			if _, err := f.service.Generate(ctx, f.request()); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.Tasks().UpdateStatus(ctx, f.taskID, model.TaskStatusCompleted); err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Update("storage_provider", provider).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.service.tasks.Delete(ctx, f.taskID); err == nil {
				t.Fatal("task deletion accepted an unknown task-file storage provider")
			}
			if _, err := f.repo.Tasks().FindByID(ctx, f.taskID); err != nil {
				t.Fatalf("task reference was removed after provider mismatch: %v", err)
			}
			var files int64
			if err := f.db.Model(&model.TaskFile{}).Where("task_id = ?", f.taskID).Count(&files).Error; err != nil || files != 1 {
				t.Fatalf("task files after provider mismatch = %d, %v; want one retained row", files, err)
			}
		})
	}
}

func TestGenerateTaskImageDoesNotAnalyzeOrUpload(t *testing.T) {
	f := newTaskImageFixture(t)
	if _, err := f.service.Generate(context.Background(), f.request()); err != nil {
		t.Fatal(err)
	}
	if f.generator.analyzeCalls != 0 || f.generator.uploadCalls != 0 {
		t.Fatalf("analyze/upload calls=%d/%d", f.generator.analyzeCalls, f.generator.uploadCalls)
	}
}

func TestTaskImageAssetDoesNotExposeProviderMetadata(t *testing.T) {
	raw, err := json.Marshal(TaskImageAsset{
		TaskFileID: "file-1", FilePath: "output/cover.png", DownloadURL: "/files/cover.png",
		MimeType: "image/png", FileSize: 123, ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"provider", "model", "selection_reason", "response_type", "revised_prompt", "billing"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("asset exposes %q: %s", forbidden, raw)
		}
	}
}

func taskImageTinyPNG() []byte {
	return taskImagePNG(3, 4)
}

func taskImagePNG(width, height int) []byte {
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height)))
	return encoded.Bytes()
}

func hashTaskFileContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

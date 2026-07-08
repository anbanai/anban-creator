package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appimage "github.com/anbanai/anban-creator/app/image"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDesignerBillingTest(t *testing.T) (*DesignerService, repository.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	enabled := true
	cfg := &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				Designer: map[string]srvconfig.ImageGenerationRouteConfig{
					"gpt_image_2": {Alias: "GPT Image 2", Enabled: true, Provider: "wangcai_openai", Model: "gpt-image-2"},
				},
			},
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			CurrencyRates: map[string]srvconfig.CurrencyRate{"USD": {ToCNY: 7.2}},
			ImageGeneration: map[string]srvconfig.ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:      srvconfig.ImagePricingTypeOpenAIUsage,
					Currency:         "USD",
					Unit:             1_000_000,
					RequireUsage:     true,
					TextInput:        5,
					TextCachedInput:  1.25,
					ImageInput:       8,
					ImageCachedInput: 2,
					ImageOutput:      30,
					EstimateTable: map[string]map[string]srvconfig.FlexibleFloat{
						"1024x1024": {"medium": srvconfig.FlexibleFloat(0.053)},
					},
				},
			},
		},
		Billing: srvconfig.BillingConfig{
			CreditsPerCNY:         1000,
			TierMultipliers:       map[string]float64{"free": 1.30, "pro": 1.15, "enterprise": 1.00},
			DefaultUserMultiplier: 1,
			MinimumChargeCredits:  1,
		},
		ImageAPI: srvconfig.ImageAPIConfig{
			Designer: map[string]*appconfig.ImageAPI{
				"gpt_image_2": {Alias: "GPT Image 2", Enable: &enabled, Provider: "openai", Model: "gpt-image-2"},
			},
		},
	}
	return NewDesignerService(db, nil, NewCreditService(repo, &srvconfig.CreditsConfig{}, &logger), cfg, nil, &logger), repo, db
}

func TestDesignerCreateGenerationPreDeductsGPTImage2Estimate(t *testing.T) {
	svc, repo, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          1,
	})
	if err != nil {
		t.Fatalf("CreateGenerationRecord() error = %v", err)
	}
	if created.EstimatedCredits != 497 || created.BillingMode != srvconfig.ImagePricingTypeOpenAIUsage {
		t.Fatalf("created = %+v, want estimated=497 billing=openai_image_usage", created)
	}

	var gen model.ImageGeneration
	if err := db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if gen.EstimatedCost != 497 || gen.Cost != 497 || gen.BillingStatus != "estimated" {
		t.Fatalf("generation billing fields = %+v", gen)
	}

	tx, err := repo.Credits().FindDeductionByOperationID(ctx, created.GenerationID)
	if err != nil {
		t.Fatalf("find deduction: %v", err)
	}
	if tx.Amount != -497 {
		t.Fatalf("deduction amount = %d, want -497", tx.Amount)
	}
	var metadata model.CreditTransactionMetadata
	if err := json.Unmarshal(tx.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.Provider != "wangcai_openai" || metadata.Model != "gpt-image-2" || metadata.Route != "image_generation.designer.gpt_image_2" {
		t.Fatalf("metadata identity = %#v", metadata)
	}
	if metadata.BaseCredits != 382 || metadata.FinalCredits != 497 {
		t.Fatalf("metadata credits = %#v", metadata)
	}
	if metadata.PriceSnapshot["pricing_type"] != srvconfig.ImagePricingTypeOpenAIUsage {
		t.Fatalf("price snapshot = %#v", metadata.PriceSnapshot)
	}
}

func TestDesignerCreateGenerationAllowsGPTImageAutoSizeWithEstimate(t *testing.T) {
	svc, repo, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a tall milk carton poster",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "auto",
		N:          1,
	})
	if err != nil {
		t.Fatalf("CreateGenerationRecord() error = %v", err)
	}
	if created.EstimatedCredits != 497 || created.BillingMode != srvconfig.ImagePricingTypeOpenAIUsage {
		t.Fatalf("created = %+v, want estimated=497 billing=openai_image_usage", created)
	}

	var gen model.ImageGeneration
	if err := db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if gen.Size != "auto" {
		t.Fatalf("generation size = %q, want auto", gen.Size)
	}
	if gen.EstimatedCost != 497 || gen.Cost != 497 || gen.BillingStatus != "estimated" {
		t.Fatalf("generation billing fields = %+v", gen)
	}
}

func TestDesignerCreateGenerationUsesProviderIDAsRuntimeSourceOfTruth(t *testing.T) {
	svc, _, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repository.New(db), 5000)

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		Provider:   "gemini",
		Model:      "wrong-model",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          1,
	})
	if err != nil {
		t.Fatalf("CreateGenerationRecord() error = %v", err)
	}

	var gen model.ImageGeneration
	if err := db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if gen.Provider != "openai" || gen.Model != "gpt-image-2" {
		t.Fatalf("generation provider/model = %s/%s, want backend route openai/gpt-image-2", gen.Provider, gen.Model)
	}
}

func TestDesignerCreateGenerationRejectsUnknownProviderID(t *testing.T) {
	svc, repo, _ := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	_, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "missing",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          1,
	})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want provider_id rejection", err)
	}
}

func TestDesignerCreateGenerationValidatesProviderCapabilities(t *testing.T) {
	svc, repo, _ := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	_, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "512x512",
		N:          1,
	})
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("error = %v, want unsupported size rejection", err)
	}

	_, err = svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          11,
	})
	if err == nil || !strings.Contains(err.Error(), "max_batch") {
		t.Fatalf("error = %v, want max_batch rejection", err)
	}
}

func TestDesignerExecuteGenerationFailsAndRefundsWhenGPTImage2UsageMissing(t *testing.T) {
	svc, repo, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          1,
	})
	if err != nil {
		t.Fatalf("CreateGenerationRecord() error = %v", err)
	}
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return fakeImageProvider{result: &appimage.GenerateResult{
			URL:          "data:image/png;base64,ZmFrZQ==",
			Model:        "gpt-image-2",
			Size:         "1024x1024",
			ResponseType: "url",
		}}, nil
	}

	svc.ExecuteGeneration(ctx, created.GenerationID)

	var gen model.ImageGeneration
	if err := db.Preload("Results").First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if gen.Status != model.ImageGenerationStatusFailed {
		t.Fatalf("status = %q, want failed", gen.Status)
	}
	if gen.BillingStatus != "usage_required_failed" || gen.FinalCost != 0 || gen.Cost != 0 {
		t.Fatalf("generation billing fields = %+v", gen)
	}
	if len(gen.Results) != 0 {
		t.Fatalf("results len = %d, want 0 because unbillable GPT Image 2 output must not be delivered", len(gen.Results))
	}
	txs, err := repo.Credits().FindByUserID(ctx, userID, 0, 10)
	if err != nil {
		t.Fatalf("find transactions: %v", err)
	}
	if len(txs) != 2 || txs[0].Amount != 497 || txs[1].Amount != -497 {
		t.Fatalf("transactions newest-first = %+v, want refund +497 and deduction -497", txs)
	}
}

func TestDesignerExecuteGenerationSettlesGPTImage2FromUsageBeforeDeliveringResult(t *testing.T) {
	svc, repo, db := setupDesignerBillingTest(t)
	ctx := context.Background()
	userID := createCreditTestUser(t, repo, 5000)

	created, err := svc.CreateGenerationRecord(ctx, userID, DesignerGenerateRequest{
		ProjectID:  "default",
		Prompt:     "a cat",
		ProviderID: "gpt_image_2",
		Quality:    "medium",
		Size:       "1024x1024",
		N:          1,
	})
	if err != nil {
		t.Fatalf("CreateGenerationRecord() error = %v", err)
	}
	svc.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return fakeImageProvider{result: &appimage.GenerateResult{
			URL:          "data:image/png;base64,ZmFrZQ==",
			Model:        "gpt-image-2",
			Size:         "1024x1024",
			ResponseType: "url",
			Usage: &appimage.ImageGenerationUsage{
				TextInputTokens:   20,
				ImageInputTokens:  100,
				ImageOutputTokens: 1767,
				TotalTokens:       1887,
			},
		}}, nil
	}

	svc.ExecuteGeneration(ctx, created.GenerationID)

	var gen model.ImageGeneration
	if err := db.Preload("Results").First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if gen.Status != model.ImageGenerationStatusCompleted {
		t.Fatalf("status = %q, want completed: error=%s", gen.Status, gen.Error)
	}
	if gen.BillingStatus != "settled" || gen.EstimatedCost != 497 || gen.FinalCost != 506 || gen.Cost != 506 {
		t.Fatalf("generation billing fields = %+v", gen)
	}
	if gen.TextInputTokens != 20 || gen.ImageInputTokens != 100 || gen.ImageOutputTokens != 1767 || gen.TotalTokens != 1887 {
		t.Fatalf("usage fields = %+v", gen)
	}
	if len(gen.Results) != 1 {
		t.Fatalf("results len = %d, want 1 after settlement", len(gen.Results))
	}
	txs, err := repo.Credits().FindByUserID(ctx, userID, 0, 10)
	if err != nil {
		t.Fatalf("find transactions: %v", err)
	}
	if len(txs) != 2 || txs[0].Amount != -9 || txs[1].Amount != -497 {
		t.Fatalf("transactions newest-first = %+v, want supplemental -9 and estimate -497", txs)
	}
}

type fakeImageProvider struct {
	result *appimage.GenerateResult
	err    error
}

func (p fakeImageProvider) Name() string { return "openai" }

func (p fakeImageProvider) Generate(context.Context, string, *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
	return p.result, p.err
}

func (p fakeImageProvider) Capabilities() *appimage.ProviderCapabilities {
	return &appimage.ProviderCapabilities{}
}

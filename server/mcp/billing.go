package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

// billingServices holds dependencies for billing operations.
var billSvc *billingServices

// mcpLog is the package-level logger for MCP tool diagnostics.
var mcpLog *zerolog.Logger

type billingServices struct {
	creditSvc      *service.CreditService
	modelConfigSvc *service.ModelConfigService
	config         *config.Config
}

// SetBillingServices initializes billing dependencies. Called from MCP setup.
func SetBillingServices(creditSvc *service.CreditService, modelConfigSvc *service.ModelConfigService, cfg *config.Config) {
	billSvc = &billingServices{
		creditSvc:      creditSvc,
		modelConfigSvc: modelConfigSvc,
		config:         cfg,
	}
}

// SetLogger sets the package-level logger for MCP tool diagnostics.
func SetLogger(log *zerolog.Logger) {
	mcpLog = log
}

// maybeDeduct handles model operation billing with these rules:
// 1. Missing/system/admin auth -> skip
// 2. BYOK actually used for this call -> skip
// 3. Unpriced operation -> skip
// 4. Otherwise -> deduct by model pricing from config
func maybeDeduct(ctx context.Context, userID, opType, provider, mdl string, count int, taskID ...string) error {
	task := ""
	if len(taskID) > 0 {
		task = taskID[0]
	}
	return maybeDeductForResolvedModel(ctx, userID, opType, provider, mdl, count, task, "")
}

func maybeDeductUnderstandingTokens(ctx context.Context, userID, taskID, opType string, usage config.TokenUsage) (int, error) {
	if billSvc == nil || billSvc.creditSvc == nil || billSvc.config == nil {
		logBillingSkip(userID, opType, "no_credit_or_config_service")
		return 0, nil
	}
	if usage.TotalTokens <= 0 {
		return 0, fmt.Errorf("%s token usage is required for billing", opType)
	}
	if userID == "" || userID == "system" || isAdminCall(ctx) {
		logBillingSkip(userID, opType, "admin_or_system")
		return 0, nil
	}
	var provider, modelName string
	switch opType {
	case model.CreditTypeImageUnderstanding:
		provider = billSvc.config.ImageUnderstanding.ProviderKey
		modelName = billSvc.config.ImageUnderstanding.Model
	case model.CreditTypeVideoUnderstanding:
		provider = billSvc.config.VideoUnderstanding.ProviderKey
		modelName = billSvc.config.VideoUnderstanding.Model
	default:
		return 0, fmt.Errorf("unsupported understanding op type %s", opType)
	}
	if provider == "" || modelName == "" {
		return 0, fmt.Errorf("%s model route is not configured", opType)
	}
	tier, err := billSvc.creditSvc.GetUserTier(ctx, userID)
	if err != nil {
		return 0, err
	}
	cost, err := billSvc.config.CalculateTokenModelCredits(provider, modelName, usage, string(tier), 1)
	if err != nil {
		return 0, err
	}
	if taskID != "" {
		if err := validateBillingTask(ctx, userID, taskID); err != nil {
			return 0, err
		}
	}
	priceSnapshot := map[string]any{}
	if data, err := json.Marshal(cost.PriceSnapshot); err == nil {
		_ = json.Unmarshal(data, &priceSnapshot)
	}
	metadata := model.CreditTransactionMetadata{
		Provider:          provider,
		Model:             modelName,
		Route:             opType,
		InputTokens:       usage.InputTokens,
		CachedInputTokens: usage.CachedInputTokens,
		OutputTokens:      usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
		BaseCredits:       cost.BaseCredits,
		TierMultiplier:    cost.TierMultiplier,
		UserMultiplier:    cost.UserMultiplier,
		FinalCredits:      cost.FinalCredits,
		PriceSnapshot:     priceSnapshot,
	}
	operationID := fmt.Sprintf("%s:%s:%d:%d", opType, taskID, usage.TotalTokens, time.Now().UnixNano())
	_, err = billSvc.creditSvc.DeductForOperationWithMetadata(ctx, userID, opType, cost.FinalCredits, metadata, operationID, taskID)
	if err != nil {
		return 0, err
	}
	return cost.FinalCredits, nil
}

func maybeDeductImageGenerationUsage(ctx context.Context, userID, taskID, route, provider, modelName string, usage config.ImageGenerationUsage) (int, error) {
	if billSvc == nil || billSvc.creditSvc == nil || billSvc.config == nil {
		logBillingSkip(userID, model.CreditTypeImageGen, "no_credit_or_config_service")
		return 0, nil
	}
	if usage.TotalTokens <= 0 || usage.ImageOutputTokens <= 0 {
		return 0, fmt.Errorf("%s usage is required for billing", modelName)
	}
	if userID == "" || userID == "system" || isAdminCall(ctx) {
		logBillingSkip(userID, model.CreditTypeImageGen, "admin_or_system")
		return 0, nil
	}
	if provider == "" || modelName == "" {
		return 0, fmt.Errorf("image generation billing route is not configured")
	}
	if taskID != "" {
		if err := validateBillingTask(ctx, userID, taskID); err != nil {
			return 0, err
		}
	}
	tier, err := billSvc.creditSvc.GetUserTier(ctx, userID)
	if err != nil {
		return 0, err
	}
	cost, err := billSvc.config.CalculateImageGenerationUsageCredits(provider, modelName, usage, string(tier), 1)
	if err != nil {
		return 0, err
	}
	priceSnapshot := map[string]any{}
	if data, err := json.Marshal(cost.PriceSnapshot); err == nil {
		_ = json.Unmarshal(data, &priceSnapshot)
	}
	metadata := model.CreditTransactionMetadata{
		Provider:               provider,
		Model:                  modelName,
		Route:                  route,
		TextInputTokens:        usage.TextInputTokens,
		TextCachedInputTokens:  usage.TextCachedInputTokens,
		ImageInputTokens:       usage.ImageInputTokens,
		ImageCachedInputTokens: usage.ImageCachedInputTokens,
		ImageOutputTokens:      usage.ImageOutputTokens,
		TotalTokens:            usage.TotalTokens,
		BaseCredits:            cost.BaseCredits,
		TierMultiplier:         cost.TierMultiplier,
		UserMultiplier:         cost.UserMultiplier,
		FinalCredits:           cost.FinalCredits,
		PriceSnapshot:          priceSnapshot,
	}
	operationID := fmt.Sprintf("%s:%s:%d:%d", model.CreditTypeImageGen, taskID, usage.TotalTokens, time.Now().UnixNano())
	_, err = billSvc.creditSvc.DeductForOperationWithMetadata(ctx, userID, model.CreditTypeImageGen, cost.FinalCredits, metadata, operationID, taskID)
	if err != nil {
		return 0, err
	}
	return cost.FinalCredits, nil
}

func maybeDeductForResolvedModel(ctx context.Context, userID, opType, provider, mdl string, count int, taskID, modelSource string) error {
	if billSvc == nil || billSvc.creditSvc == nil {
		logBillingSkip(userID, opType, "no_credit_service")
		return nil
	}
	if userID == "" {
		logBillingSkip(userID, opType, "admin_static_key")
		return nil
	}
	if userID == "system" {
		logBillingSkip(userID, opType, "system_user")
		return nil
	}
	if isAdminCall(ctx) {
		logBillingSkip(userID, opType, "admin_static_key")
		return nil
	}
	if isByok(ctx, userID, opType, provider, mdl, modelSource) {
		logBillingSkip(userID, opType, "byok")
		return nil
	}
	if billSvc.config == nil {
		logBillingSkip(userID, opType, "unpriced")
		return nil
	}

	var cost int
	if opType == model.CreditTypeImageGen {
		cost = imageGenCredits(provider, mdl)
	} else if opType == model.CreditTypeVideoGen {
		cost = videoGenCredits()
	} else {
		var ok bool
		cost, ok = billSvc.config.Credits.ModelCost(opType, provider, mdl)
		if !ok {
			logBillingSkip(userID, opType, "unpriced")
			return nil // no pricing configured = free
		}
	}
	if cost == 0 {
		logBillingSkip(userID, opType, "unpriced")
		return nil
	}

	opArgs := []string{""}
	if taskID != "" {
		if err := validateBillingTask(ctx, userID, taskID); err != nil {
			return err
		}
		opArgs = append(opArgs, taskID)
	}
	_, err := billSvc.creditSvc.DeductForOperation(ctx, userID, opType, cost*count, opArgs...)
	return err
}

func validateBillingTask(ctx context.Context, userID, taskID string) error {
	if svcs == nil || svcs.TaskSvc == nil || taskID == "" {
		return nil
	}
	task, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("validate billing task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("validate billing task: task does not belong to user")
	}
	return nil
}

func logBillingSkip(userID, opType, reason string) {
	if mcpLog == nil {
		return
	}
	mcpLog.Debug().
		Str("user_id", userID).
		Str("op_type", opType).
		Str("reason", reason).
		Msg("MCP billing skipped")
}

func videoGenCredits() int {
	return 0
}

// imageGenCredits looks up per-image credit cost from ImageAPI configs.
func imageGenCredits(provider, mdl string) int {
	if billSvc == nil || billSvc.config == nil {
		return 0
	}
	cfg := billSvc.config.ImageAPI
	if cfg.Cover != nil && cfg.Cover.Provider == provider && cfg.Cover.Model == mdl {
		return cfg.Cover.Credits
	}
	if cfg.Content != nil && cfg.Content.Provider == provider && cfg.Content.Model == mdl {
		return cfg.Content.Credits
	}
	for _, d := range cfg.Designer {
		if d != nil && d.Provider == provider && d.Model == mdl {
			return d.Credits
		}
	}
	if cost, ok := billSvc.config.Credits.ModelCost(model.CreditTypeImageGen, provider, mdl); ok {
		return cost
	}
	return 0
}

// isByok checks if the user has their own model configured (BYOK).
func isByok(ctx context.Context, userID, opType, provider, mdl, modelSource string) bool {
	if billSvc.modelConfigSvc == nil {
		return false
	}
	switch opType {
	case model.CreditTypeImageGen:
		if modelSource != "" {
			return modelSource == "user_custom"
		}
		cfg := billSvc.modelConfigSvc.GetEffectiveImageConfig(ctx, userID)
		if cfg == nil {
			return false
		}
		if cfg.Cover != nil && cfg.Cover.Provider == provider && cfg.Cover.Model == mdl {
			return true
		}
		return cfg.Content != nil && cfg.Content.Provider == provider && cfg.Content.Model == mdl
	case model.CreditTypeVideoGen:
		return false
	case model.CreditTypeArticleWrite, model.CreditTypeConvert,
		model.CreditTypeTopicResearch,
		model.CreditTypeSEO, model.CreditTypeOutline:
		// GetEffectiveWritingConfig already checks all required fields (base_url + api_key + model).
		_, _, configuredModel, ok := billSvc.modelConfigSvc.GetEffectiveWritingConfig(ctx, userID)
		return ok && configuredModel == mdl
	}
	return false
}

// resolveImageModel returns the effective image provider/model for a user.
func resolveImageModel(ctx context.Context, userID string) (provider, mdl string) {
	provider, mdl, _ = resolveImageModelWithSource(ctx, userID)
	return provider, mdl
}

func resolveImageModelWithSource(ctx context.Context, userID string) (provider, mdl, source string) {
	if billSvc == nil || billSvc.config == nil {
		return "", "", ""
	}
	if billSvc.modelConfigSvc != nil {
		if cfg := billSvc.modelConfigSvc.GetEffectiveImageConfig(ctx, userID); cfg != nil {
			if mcpLog != nil {
				mcpLog.Info().
					Str("user_id", userID).
					Str("provider", cfg.Cover.Provider).
					Str("model", cfg.Cover.Model).
					Str("source", "user_override").
					Msg("MCP tool using user custom image model")
			}
			return cfg.Cover.Provider, cfg.Cover.Model, "user_custom"
		}
	}
	if billSvc.config.ImageAPI.Cover != nil {
		return billSvc.config.ImageAPI.Cover.Provider, billSvc.config.ImageAPI.Cover.Model, "system_default"
	}
	return "", "", ""
}

// resolveImageBillingModel mirrors generate_image's provider selection for
// billing/logging. A persisted task image_model_key must win over the server
// default; otherwise a user choosing GPT Image can be billed/logged as the
// default Volcengine model while generation uses OpenAI.
func resolveImageBillingModel(ctx context.Context, userID, imageModelKey string) (provider, mdl, source string, err error) {
	if imageModelKey != "" && billSvc != nil {
		if billSvc.modelConfigSvc != nil {
			cfg, src, err := billSvc.modelConfigSvc.ResolveImageConfigForTaskKey(ctx, userID, imageModelKey)
			if err != nil {
				return "", "", "", err
			}
			if cfg != nil {
				if cfg.Cover != nil && cfg.Cover.Provider != "" {
					return cfg.Cover.Provider, cfg.Cover.Model, src, nil
				}
				if cfg.Content != nil && cfg.Content.Provider != "" {
					return cfg.Content.Provider, cfg.Content.Model, src, nil
				}
			}
		}
		if billSvc.config != nil {
			for _, p := range billSvc.config.ImagePresets {
				if p.Key == imageModelKey {
					return p.Provider, p.Model, "preset:" + p.Key, nil
				}
			}
		}
		return "", "", "", fmt.Errorf("unknown image model key %q", imageModelKey)
	}
	provider, mdl, source = resolveImageModelWithSource(ctx, userID)
	return provider, mdl, source, nil
}

// resolveEcommerceImageProvider returns the provider/model the agent's
// generate_image calls will actually use for this e-commerce task, so the agent
// can adapt its reference-image strategy to the provider's capability rather
// than a fixed per-module split:
//   - openai: pass all product photos as reference images (≤16) for fidelity;
//   - volcengine/seedream: single anchor reference + the product-bible text
//     block — reusing one reference across many images repeats the scene (see
//     the Seedream strong-i2i limitation).
//
// Resolution mirrors generate_image's generation path: Task.ImageModelKey (a
// system image_preset or "custom", chosen by the user at task creation) wins,
// else the user override, else the server image generation cover default. Returns
// ("","") only when nothing is configured.
func resolveEcommerceImageProvider(ctx context.Context, userID string, task *model.Task) (provider, mdl string) {
	if task != nil && task.ImageModelKey != "" && billSvc != nil && billSvc.modelConfigSvc != nil {
		if cfg, _, err := billSvc.modelConfigSvc.ResolveImageConfigForTaskKey(ctx, userID, task.ImageModelKey); err == nil && cfg != nil {
			if cfg.Cover != nil && cfg.Cover.Provider != "" {
				return cfg.Cover.Provider, cfg.Cover.Model
			}
			if cfg.Content != nil && cfg.Content.Provider != "" {
				return cfg.Content.Provider, cfg.Content.Model
			}
		}
	}
	return resolveImageModel(ctx, userID)
}

// resolveTextModel returns the effective text model for a user.
func resolveTextModel(ctx context.Context, userID string) (provider, mdl string) {
	if billSvc == nil || billSvc.config == nil {
		return "", ""
	}
	if billSvc.modelConfigSvc != nil {
		if _, _, m, ok := billSvc.modelConfigSvc.GetEffectiveWritingConfig(ctx, userID); ok {
			if mcpLog != nil {
				mcpLog.Info().
					Str("user_id", userID).
					Str("model", m).
					Str("source", "user_override").
					Msg("MCP tool using user custom text model")
			}
			return "", m
		}
	}
	return "", billSvc.config.Writing.Model
}

// billingError converts an ErrInsufficientCredits into an MCP error result.
// If mcpLog is set, it also logs the error for diagnostics.
func billingError(opType string, err error) *mcp.CallToolResult {
	if mcpLog != nil && !errors.Is(err, service.ErrInsufficientCredits) {
		mcpLog.Error().Err(err).Str("tool", opType).Msg("MCP tool failed")
	}
	if errors.Is(err, service.ErrInsufficientCredits) {
		return errorResult("积分不足，请前往 https://creator.anbanai.com 充值")
	}
	return errorResult(opType + ": " + err.Error())
}

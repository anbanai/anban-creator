package mcp

import (
	"context"
	"errors"

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

// maybeDeduct handles model operation billing with three rules:
// 1. Managed key (task execution) -> skip
// 2. BYOK (user's own model) -> skip
// 3. Otherwise -> deduct by model pricing from config
func maybeDeduct(ctx context.Context, userID, opType, provider, mdl string, count int) error {
	if billSvc == nil || billSvc.creditSvc == nil {
		return nil
	}
	if isManagedCall(ctx) {
		return nil
	}
	if isByok(ctx, userID, opType) {
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
			return nil // no pricing configured = free
		}
	}
	if cost == 0 {
		return nil
	}

	_, err := billSvc.creditSvc.DeductForOperation(ctx, userID, opType, cost*count)
	return err
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
	return 0
}

// isByok checks if the user has their own model configured (BYOK).
func isByok(ctx context.Context, userID, opType string) bool {
	if billSvc.modelConfigSvc == nil {
		return false
	}
	switch opType {
	case model.CreditTypeImageGen:
		return billSvc.modelConfigSvc.HasCompleteImageOverride(ctx, userID)
	case model.CreditTypeVideoGen:
		return false
	case model.CreditTypeArticleWrite, model.CreditTypeConvert,
		model.CreditTypeTopicResearch,
		model.CreditTypeSEO, model.CreditTypeOutline:
		// GetEffectiveWritingConfig already checks all required fields (base_url + api_key + model).
		_, _, _, ok := billSvc.modelConfigSvc.GetEffectiveWritingConfig(ctx, userID)
		return ok
	}
	return false
}

// resolveImageModel returns the effective image provider/model for a user.
func resolveImageModel(ctx context.Context, userID string) (provider, mdl string) {
	if billSvc == nil || billSvc.config == nil {
		return "", ""
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
			return cfg.Cover.Provider, cfg.Cover.Model
		}
	}
	if billSvc.config.ImageAPI.Cover != nil {
		return billSvc.config.ImageAPI.Cover.Provider, billSvc.config.ImageAPI.Cover.Model
	}
	return "", ""
}

// resolveImageBillingModel mirrors generate_image's provider selection for
// billing/logging. A task/argument image_model_key must win over the server
// default; otherwise a user choosing GPT Image can be billed/logged as the
// default Volcengine model while generation uses OpenAI.
func resolveImageBillingModel(ctx context.Context, userID, imageModelKey string) (provider, mdl string) {
	if imageModelKey != "" && billSvc != nil {
		if billSvc.modelConfigSvc != nil {
			if cfg, _ := billSvc.modelConfigSvc.ResolveImageConfigForKey(ctx, userID, imageModelKey); cfg != nil {
				if cfg.Cover != nil && cfg.Cover.Provider != "" {
					return cfg.Cover.Provider, cfg.Cover.Model
				}
				if cfg.Content != nil && cfg.Content.Provider != "" {
					return cfg.Content.Provider, cfg.Content.Model
				}
			}
		}
		if billSvc.config != nil {
			for _, p := range billSvc.config.ImagePresets {
				if p.Key == imageModelKey {
					return p.Provider, p.Model
				}
			}
		}
	}
	return resolveImageModel(ctx, userID)
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
// else the user override, else the server image_api.cover default. Returns
// ("","") only when nothing is configured.
func resolveEcommerceImageProvider(ctx context.Context, userID string, task *model.Task) (provider, mdl string) {
	if task != nil && task.ImageModelKey != "" && billSvc != nil && billSvc.modelConfigSvc != nil {
		if cfg, _ := billSvc.modelConfigSvc.ResolveImageConfigForKey(ctx, userID, task.ImageModelKey); cfg != nil {
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

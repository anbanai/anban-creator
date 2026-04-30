package mcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
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
	cost, ok := billSvc.config.Credits.ModelCost(opType, provider, mdl)
	if !ok {
		return nil // no pricing configured = free
	}
	_, err := billSvc.creditSvc.DeductForOperation(ctx, userID, opType, cost*count)
	return err
}

// isByok checks if the user has their own model configured (BYOK).
func isByok(ctx context.Context, userID, opType string) bool {
	if billSvc.modelConfigSvc == nil {
		return false
	}
	switch opType {
	case model.CreditTypeImageGen:
		return billSvc.modelConfigSvc.HasCompleteImageOverride(ctx, userID)
	case model.CreditTypeArticleWrite, model.CreditTypeConvert,
		model.CreditTypeHumanize, model.CreditTypeTopicResearch,
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

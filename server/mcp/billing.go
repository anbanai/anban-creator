package mcp

import (
	"context"
	"fmt"

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
	capabilityResolver *service.ImageCapabilityResolver
	config             *config.Config
}

// SetBillingServices initializes billing dependencies. Called from MCP setup.
func SetBillingServices(capabilityResolver *service.ImageCapabilityResolver, cfg *config.Config) {
	billSvc = &billingServices{
		capabilityResolver: capabilityResolver,
		config:             cfg,
	}
}

// SetLogger sets the package-level logger for MCP tool diagnostics.
func SetLogger(log *zerolog.Logger) {
	mcpLog = log
}

func validateBillingTask(ctx context.Context, userID, taskID string) error {
	if taskID == "" {
		return nil
	}
	if svcs != nil && svcs.TaskSvc != nil {
		task, err := svcs.TaskSvc.GetByID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("validate billing task: %w", err)
		}
		if task.UserID != userID {
			return fmt.Errorf("validate billing task: task does not belong to user")
		}
		return nil
	}
	return nil
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
	if cfg, ok := billSvc.config.ImageAPIForCapability(""); ok && cfg.API != nil {
		return cfg.API.Provider, cfg.API.Model, "capability:" + billSvc.config.ModelRoutes.ImageGeneration.DefaultCapability
	}
	return "", "", ""
}

// resolveEcommerceImageProvider returns the provider/model the agent's
// generate_image calls will actually use for this e-commerce task, so the agent
// can adapt its reference-image strategy to the provider's capability rather
// than a fixed per-module split:
//   - openai: pass relevant product photos as reference images (≤16) for fidelity;
//   - volcengine/seedream: pass the relevant product-photo subset supported by
//     the configured provider limit, plus the product-bible text block. Avoid
//     unrelated refs because Seedream's strong i2i can over-lock the scene.
//
// Resolution mirrors generate_image's frozen task capability.
func resolveEcommerceImageProvider(ctx context.Context, userID string, task *model.Task) (provider, mdl string) {
	if task != nil && billSvc != nil && billSvc.capabilityResolver != nil {
		if cfg, _, err := billSvc.capabilityResolver.ResolveImageConfigForTaskKey(ctx, userID, task.ImageCapabilityKey); err == nil && cfg != nil {
			if cfg.API != nil && cfg.API.Provider != "" {
				return cfg.API.Provider, cfg.API.Model
			}
		}
	}
	return resolveImageModel(ctx, userID)
}

func billingError(opType string, err error) *mcp.CallToolResult {
	if mcpLog != nil {
		mcpLog.Error().Err(err).Str("tool", opType).Msg("MCP tool failed")
	}
	return errorResult(opType + ": " + err.Error())
}

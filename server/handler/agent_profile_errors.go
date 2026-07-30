package handler

import (
	"errors"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

const (
	AgentProfileCodeNotFound         = 46001
	AgentProfileCodeUnavailable      = 46002
	AgentProfileCodeAccessDenied     = 46003
	AgentProfileCodeSnapshotInvalid  = 46004
	AgentProfileCodeSnapshotConflict = 46005
	AgentProfileCodeProviderMissing  = 46006
	AgentProfileCodeCostUnmapped     = 46007
	AgentProfileCodeSKUMissing       = 46008
)

func respondAgentProfileError(c fiber.Ctx, err error) (bool, error) {
	type mapping struct {
		target error
		status int
		code   int
		msg    string
		hint   string
	}
	for _, item := range []mapping{
		{service.ErrAgentProfileAccessDenied, fiber.StatusForbidden, AgentProfileCodeAccessDenied, "agent_profile_access_denied", "Upgrade the account tier or select an accessible execution profile."},
		{service.ErrAgentProfileSnapshotConflict, fiber.StatusConflict, AgentProfileCodeSnapshotConflict, "agent_profile_snapshot_conflict", "Retry with a newly created task or quote."},
		{service.ErrAgentProfileNotFound, fiber.StatusBadRequest, AgentProfileCodeNotFound, "invalid_agent_execution_profile", "Select an execution profile returned by the capability API."},
		{service.ErrAgentProfileSnapshotInvalid, fiber.StatusBadRequest, AgentProfileCodeSnapshotInvalid, "agent_profile_snapshot_invalid", "Create the task again after the profile configuration is corrected."},
		{service.ErrAgentProfileUnavailable, fiber.StatusUnprocessableEntity, AgentProfileCodeUnavailable, "agent_profile_unavailable", "Select another available execution profile."},
		{service.ErrAgentProviderUnavailable, fiber.StatusUnprocessableEntity, AgentProfileCodeProviderMissing, "agent_provider_unavailable", "Try again after the configured model provider is available."},
		{service.ErrAgentModelCostUnmapped, fiber.StatusUnprocessableEntity, AgentProfileCodeCostUnmapped, "agent_model_cost_unmapped", "Try again after model pricing is configured."},
		{service.ErrBillingProfileSKUNotFound, fiber.StatusUnprocessableEntity, AgentProfileCodeSKUMissing, "billing_profile_sku_not_found", "Select a profile and task type available in the current billing catalog."},
	} {
		if errors.Is(err, item.target) {
			return true, c.Status(item.status).JSON(Response{Code: item.code, Msg: item.msg, Data: fiber.Map{"hint": item.hint}})
		}
	}
	return false, nil
}

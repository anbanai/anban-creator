package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/service"
)

const (
	agentArtifactPathHeader   = "X-Anban-Artifact-Path"
	agentArtifactSizeHeader   = "X-Anban-Artifact-Size"
	agentArtifactSHA256Header = "X-Anban-Artifact-SHA256"
)

// StreamArtifactContent handles execution-authenticated artifact bodies without
// materializing the request body in Fiber or in application memory.
func (h *AgentHandler) StreamArtifactContent(c fiber.Ctx) error {
	if h.taskSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "task service unavailable")
	}
	userID := h.authenticatedUserID(c)
	executionID := h.authenticatedExecutionID(c)
	taskID, _ := c.Locals(agentTaskIDContextKey).(string)
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return Error(c, fiber.StatusForbidden, "execution credentials are required")
	}

	declaredSize, err := strconv.ParseInt(strings.TrimSpace(c.Get(agentArtifactSizeHeader)), 10, 64)
	if err != nil || declaredSize <= 0 {
		return Error(c, fiber.StatusBadRequest, "valid artifact size is required")
	}
	result, err := h.taskSvc.StreamTaskArtifact(c.Context(), taskID, userID, executionID, service.TaskArtifactStreamRequest{
		RelativePath: strings.TrimSpace(c.Get(agentArtifactPathHeader)),
		ContentType:  strings.TrimSpace(c.Get("Content-Type")),
		Size:         declaredSize,
		SHA256:       strings.TrimSpace(c.Get(agentArtifactSHA256Header)),
		Body:         c.Request().BodyStream(),
	})
	if err == nil {
		return Success(c, result)
	}
	if isAgentTaskAccessError(err) {
		return Error(c, fiber.StatusForbidden, "task access denied")
	}
	switch {
	case errors.Is(err, service.ErrTaskArtifactExecutionConflict):
		return Error(c, fiber.StatusConflict, "task execution is no longer current")
	case errors.Is(err, service.ErrTaskArtifactInvalid):
		return Error(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrTaskArtifactUnavailable):
		if h.logger != nil {
			h.logger.Warn().Err(err).Str("task_id", taskID).Msg("stream agent artifact storage unavailable")
		}
		return Error(c, fiber.StatusServiceUnavailable, "task artifact storage temporarily unavailable")
	default:
		if h.logger != nil {
			h.logger.Error().Err(err).Str("task_id", taskID).Msg("stream agent artifact failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to stream task artifact")
	}
}

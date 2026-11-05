package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type archestraClient interface {
	ListAgents(ctx context.Context) ([]service.ArchestraAgent, error)
	SendMessage(ctx context.Context, agentID, contextID, taskID, text string, decisions []service.ApprovalDecision) (service.ArchestraSendResult, error)
	GetTask(ctx context.Context, agentID, taskID string) (service.ArchestraTask, error)
	CancelTask(ctx context.Context, agentID, taskID string) (service.ArchestraTask, error)
}

type ArchestraAdminHandler struct {
	repo   repository.Repository
	client archestraClient
	logger *zerolog.Logger
}

func NewArchestraAdminHandler(repo repository.Repository, client archestraClient, logger *zerolog.Logger) *ArchestraAdminHandler {
	return &ArchestraAdminHandler{repo: repo, client: client, logger: logger}
}

type archestraMessageRequest struct {
	ContextID string `json:"context_id"`
	TaskID    string `json:"task_id"`
	Text      string `json:"text"`
}

type archestraApprovalRequest struct {
	AgentID    string `json:"agent_id"`
	ContextID  string `json:"context_id"`
	ApprovalID string `json:"approval_id"`
	Approved   bool   `json:"approved"`
}

type archestraTaskRequest struct {
	AgentID string `json:"agent_id"`
}

func (h *ArchestraAdminHandler) requireAdmin(c fiber.Ctx) (string, error) {
	userID := strings.TrimSpace(GetUserID(c))
	if userID == "" {
		return "", Error(c, fiber.StatusUnauthorized, "authentication required")
	}
	if h.repo == nil {
		return "", Error(c, fiber.StatusServiceUnavailable, "Archestra service is not configured")
	}
	user, err := h.repo.Users().FindByID(c.Context(), userID)
	if errors.Is(err, gorm.ErrRecordNotFound) || user == nil {
		return "", Error(c, fiber.StatusUnauthorized, "authentication required")
	}
	if err != nil {
		return "", Error(c, fiber.StatusInternalServerError, "internal error")
	}
	if !user.IsAdmin {
		return "", Forbidden(c, "administrator access required")
	}
	if h.client == nil {
		return "", Error(c, fiber.StatusServiceUnavailable, "Archestra service is not configured")
	}
	return userID, nil
}

func (h *ArchestraAdminHandler) audit(userID, agentID, operation, result string) {
	if h.logger == nil {
		return
	}
	h.logger.Info().Str("admin_user_id", userID).Str("agent_id", strings.TrimSpace(agentID)).Str("operation", operation).Str("result", result).Msg("Archestra admin proxy operation")
}

func (h *ArchestraAdminHandler) Agents(c fiber.Ctx) error {
	userID, err := h.requireAdmin(c)
	if err != nil {
		return err
	}
	agents, err := h.client.ListAgents(c.Context())
	if err != nil {
		h.audit(userID, "", "list_agents", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	h.audit(userID, "", "list_agents", "success")
	return Success(c, agents)
}

func (h *ArchestraAdminHandler) Message(c fiber.Ctx) error {
	agentID := strings.TrimSpace(c.Params("agentId"))
	userID, err := h.requireAdmin(c)
	if err != nil {
		return err
	}
	var req archestraMessageRequest
	if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		return Error(c, fiber.StatusBadRequest, "text is required")
	}
	result, err := h.client.SendMessage(c.Context(), agentID, strings.TrimSpace(req.ContextID), strings.TrimSpace(req.TaskID), req.Text, nil)
	if err != nil {
		h.audit(userID, agentID, "send_message", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	h.audit(userID, agentID, "send_message", "success")
	return Success(c, archestraSendResponse(result))
}

func (h *ArchestraAdminHandler) Task(c fiber.Ctx) error {
	agentID := strings.TrimSpace(c.Query("agent_id"))
	taskID := strings.TrimSpace(c.Params("taskId"))
	userID, err := h.requireAdmin(c)
	if err != nil {
		return err
	}
	if agentID == "" || taskID == "" {
		return Error(c, fiber.StatusBadRequest, "agent_id and task_id are required")
	}
	task, err := h.client.GetTask(c.Context(), agentID, taskID)
	if err != nil {
		h.audit(userID, agentID, "get_task", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	h.audit(userID, agentID, "get_task", "success")
	return Success(c, task)
}

func (h *ArchestraAdminHandler) Approval(c fiber.Ctx) error {
	var req archestraApprovalRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request")
	}
	agentID := strings.TrimSpace(req.AgentID)
	taskID := strings.TrimSpace(c.Params("taskId"))
	approvalID := strings.TrimSpace(req.ApprovalID)
	userID, err := h.requireAdmin(c)
	if err != nil {
		return err
	}
	if agentID == "" || taskID == "" || approvalID == "" {
		return Error(c, fiber.StatusBadRequest, "agent_id, task_id and approval_id are required")
	}
	task, err := h.client.GetTask(c.Context(), agentID, taskID)
	if err != nil {
		h.audit(userID, agentID, "approval", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	validApproval := false
	for _, approval := range task.ApprovalRequests {
		if approval.ID == approvalID && !approval.Resolved {
			validApproval = true
			break
		}
	}
	if !validApproval {
		return Error(c, fiber.StatusBadRequest, "approval request is not active")
	}
	contextID := strings.TrimSpace(req.ContextID)
	if contextID == "" {
		contextID = task.ContextID
	}
	result, err := h.client.SendMessage(c.Context(), agentID, contextID, taskID, "", []service.ApprovalDecision{{ID: approvalID, Approved: req.Approved}})
	if err != nil {
		h.audit(userID, agentID, "approval", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	h.audit(userID, agentID, "approval", "success")
	return Success(c, archestraSendResponse(result))
}

func (h *ArchestraAdminHandler) Cancel(c fiber.Ctx) error {
	var req archestraTaskRequest
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid request")
		}
	}
	agentID := strings.TrimSpace(req.AgentID)
	taskID := strings.TrimSpace(c.Params("taskId"))
	userID, err := h.requireAdmin(c)
	if err != nil {
		return err
	}
	if agentID == "" || taskID == "" {
		return Error(c, fiber.StatusBadRequest, "agent_id and task_id are required")
	}
	task, err := h.client.CancelTask(c.Context(), agentID, taskID)
	if err != nil {
		h.audit(userID, agentID, "cancel_task", "error")
		return Error(c, fiber.StatusServiceUnavailable, "Archestra service unavailable")
	}
	h.audit(userID, agentID, "cancel_task", "success")
	return Success(c, task)
}

func archestraSendResponse(result service.ArchestraSendResult) fiber.Map {
	response := fiber.Map{}
	if result.Message != nil {
		response["message"] = result.Message
	}
	if result.Task != nil {
		response["task"] = result.Task
	}
	return response
}

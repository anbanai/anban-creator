package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type InteractiveHandler struct {
	svc               *service.InteractiveService
	heartbeatInterval time.Duration
}

func NewInteractiveHandler(svc *service.InteractiveService) *InteractiveHandler {
	return &InteractiveHandler{svc: svc}
}
func (h *InteractiveHandler) Agents(c fiber.Ctx) error { return Success(c, h.svc.Agents()) }

type createInteractiveRequest struct {
	AgentID   string `json:"agent_id"`
	Intent    string `json:"intent"`
	ProjectID string `json:"project_id,omitempty"`
}

func (h *InteractiveHandler) Create(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	var req createInteractiveRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	sess, err := h.svc.CreateSessionWithIdempotency(c.Context(), uid, req.AgentID, req.Intent, c.Get("Idempotency-Key"), req.ProjectID)
	if err != nil {
		return interactiveError(c, err)
	}
	status := "starting"
	if current, stateErr := h.svc.SessionState(uid, sess.ID); stateErr == nil && current != "" {
		status = current
	}
	return Success(c, fiber.Map{"id": sess.ID, "status": status, "agent_id": sess.AgentID, "intent": sess.Intent, "expires_at": sess.ExpiresAt})
}
func (h *InteractiveHandler) Events(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	return h.EventsForSession(c, uid, c.Params("id"))
}

// EventsForSession is shared by legacy session routes and the Workspace SSE
// route. The caller resolves the Server-owned session identity first.
func (h *InteractiveHandler) EventsForSession(c fiber.Ctx, uid, sessionID string) error {
	after := uint64(0)
	if raw := c.Get("Last-Event-ID"); raw != "" {
		after, _ = strconv.ParseUint(raw, 10, 64)
	}
	if err := h.svc.TouchSession(uid, sessionID); err != nil {
		return interactiveError(c, err)
	}
	history, ch, closeFn, err := h.svc.Events(uid, sessionID, after)
	if err != nil {
		return interactiveError(c, err)
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
	c.Abandon()
	requestDone := c.RequestCtx().Done()
	contextDone := c.Context().Done()
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer closeFn()
		heartbeatInterval := h.heartbeatInterval
		if heartbeatInterval <= 0 {
			heartbeatInterval = h.svc.ClientHeartbeatInterval()
		}
		heartbeat := time.NewTicker(heartbeatInterval)
		defer heartbeat.Stop()
		for _, ev := range history {
			ev = addWorkspaceEventScope(c, ev, sessionID)
			if err := writeInteractiveEvent(w, ev); err != nil {
				return
			}
		}
		for {
			select {
			case <-contextDone:
				return
			case <-requestDone:
				return
			case <-heartbeat.C:
				if err := h.svc.TouchSession(uid, sessionID); err != nil {
					return
				}
				if _, err := w.WriteString(": heartbeat\n\n"); err != nil || w.Flush() != nil {
					return
				}
			case ev, ok := <-ch:
				if !ok {
					return
				}
				ev = addWorkspaceEventScope(c, ev, sessionID)
				if err := writeInteractiveEvent(w, ev); err != nil {
					return
				}
			}
		}
	})
}

func addWorkspaceEventScope(c fiber.Ctx, ev service.InteractiveEvent, sessionID string) service.InteractiveEvent {
	modeID, _ := c.Locals("assistant_workspace_mode").(string)
	if modeID == "" {
		return ev
	}
	data, ok := ev.Data.(map[string]any)
	if !ok {
		if ev.Type == "draft.ready" || ev.Type == "draft.updated" {
			// Draft events are emitted as typed service values. Keep them under
			// the same envelope used by session snapshots so Studio can render
			// a confirmation card after both live delivery and replay.
			data = map[string]any{"draft": ev.Data}
		} else {
			data = map[string]any{"value": ev.Data}
		}
	} else if ev.Type == "draft.ready" || ev.Type == "draft.updated" {
		if _, exists := data["draft"]; !exists {
			data = map[string]any{"draft": ev.Data}
		}
	}
	copyData := make(map[string]any, len(data)+2)
	for key, value := range data {
		copyData[key] = value
	}
	copyData["mode_id"] = modeID
	copyData["session_id"] = sessionID
	ev.Data = copyData
	return ev
}
func writeInteractiveEvent(w *bufio.Writer, ev service.InteractiveEvent) error {
	b, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	if _, err = w.WriteString("id: "); err != nil {
		return err
	}
	if _, err = w.WriteString(strconv.FormatUint(ev.ID, 10)); err != nil {
		return err
	}
	if _, err = w.WriteString("\nevent: "); err != nil {
		return err
	}
	if _, err = w.WriteString(ev.Type); err != nil {
		return err
	}
	if _, err = w.WriteString("\ndata: "); err != nil {
		return err
	}
	if _, err = w.Write(b); err != nil {
		return err
	}
	if _, err = w.WriteString("\n\n"); err != nil {
		return err
	}
	return w.Flush()
}
func (h *InteractiveHandler) Message(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	if err := h.svc.Message(c.Context(), uid, c.Params("id"), req.Text); err != nil {
		return interactiveError(c, err)
	}
	return Success(c, nil)
}
func (h *InteractiveHandler) Answers(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	var req struct {
		Answers []service.InteractiveAnswer `json:"answers"`
	}
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	if err := h.svc.Answer(c.Context(), uid, c.Params("id"), c.Params("callId"), req.Answers); err != nil {
		return interactiveError(c, err)
	}
	return Success(c, nil)
}
func (h *InteractiveHandler) Commit(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	var req service.InteractiveCommitRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	result, err := h.svc.Commit(c.Context(), uid, c.Params("id"), req)
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, result)
}

func decodeInteractiveJSON(body []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON documents")
		}
		return err
	}
	return nil
}
func (h *InteractiveHandler) Delete(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if err := h.svc.Close(uid, c.Params("id")); err != nil {
		return interactiveError(c, err)
	}
	return Success(c, nil)
}
func interactiveError(c fiber.Ctx, err error) error {
	if interactionErr := service.NormalizeInteractionError(err); interactionErr != nil {
		var structuredErr *service.InteractionError
		if errors.Is(err, service.ErrInteractiveSessionNotFound) || errors.As(err, &structuredErr) {
			status := interactionErr.Status
			if status < 400 {
				status = fiber.StatusInternalServerError
			}
			return c.Status(status).JSON(Response{
				Code:             status * 100,
				Msg:              interactionErr.Message,
				ErrorCode:        interactionErr.Code,
				AvailableActions: interactionErr.AvailableActions,
				Data:             interactionErr.Snapshot,
			})
		}
	}
	switch {
	case errors.Is(err, service.ErrAssistantCheckpointNotFound):
		return Error(c, 404, err.Error())
	case errors.Is(err, service.ErrAssistantCheckpointInvalid):
		return Error(c, 400, err.Error())
	case errors.Is(err, service.ErrAssistantCreationInputsMissing), errors.Is(err, service.ErrAssistantCreationAssetUnavailable), errors.Is(err, service.ErrUserMediaUnavailable):
		return Error(c, 400, err.Error())
	case errors.Is(err, service.ErrAssistantModeUnavailable):
		return ErrorWithCode(c, 409, "assistant_mode_unavailable", err.Error())
	case errors.Is(err, service.ErrAssistantCheckpointConflict):
		return ErrorWithCode(c, 409, "assistant_checkpoint_conflict", err.Error())
	case errors.Is(err, service.ErrAssistantWorkspaceBusy):
		return ErrorWithCode(c, 409, "assistant_workspace_busy", err.Error())
	case errors.Is(err, service.ErrAssistantWorkspaceModeMismatch):
		return ErrorWithCode(c, 409, "assistant_workspace_mode_mismatch", err.Error())
	case errors.Is(err, service.ErrAssistantWorkspaceExpired):
		return ErrorWithCode(c, 410, "assistant_workspace_expired", err.Error())
	case errors.Is(err, service.ErrAssistantWorkspaceNotFound):
		return ErrorWithCode(c, 404, "assistant_workspace_not_found", err.Error())
	case errors.Is(err, service.ErrAssistantRuntimeContextTooLarge):
		return Error(c, 413, err.Error())
	case errors.Is(err, service.ErrInteractiveSessionNotFound):
		return Error(c, 404, err.Error())
	case errors.Is(err, service.ErrInteractiveSessionOwner):
		return Forbidden(c, err.Error())
	case errors.Is(err, service.ErrInteractivePending):
		return ErrorWithCode(c, 409, "interactive_question_pending", err.Error())
	case errors.Is(err, service.ErrInteractiveBusy):
		return ErrorWithCode(c, 409, "interactive_session_busy", err.Error())
	case errors.Is(err, service.ErrInteractiveToken):
		return ErrorWithCode(c, 409, "interactive_confirmation_stale", err.Error())
	case errors.Is(err, service.ErrInteractiveIdempotency):
		return ErrorWithCode(c, 409, "interactive_idempotency_conflict", err.Error())
	case errors.Is(err, service.ErrInteractiveSessionClosed):
		return ErrorWithCode(c, 410, "interactive_session_closed", err.Error())
	case errors.Is(err, service.ErrInteractiveEventGap):
		return ErrorWithCode(c, 409, "interactive_event_gap", err.Error())
	case errors.Is(err, service.ErrProjectProfileVersionConflict), errors.Is(err, service.ErrProjectMemoryConflict):
		return ErrorWithCode(c, 409, "project_profile_revision_conflict", err.Error())
	case errors.Is(err, service.ErrInteractiveAgent):
		return Error(c, 503, err.Error())
	case errors.Is(err, service.ErrInteractivePersistence):
		return Error(c, 503, err.Error())
	default:
		if strings.Contains(err.Error(), "invalid") || errors.Is(err, service.ErrInteractiveDraftInvalid) {
			return Error(c, 400, err.Error())
		}
		return Error(c, 500, err.Error())
	}
}

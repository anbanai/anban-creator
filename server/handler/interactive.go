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
	after := uint64(0)
	if raw := c.Get("Last-Event-ID"); raw != "" {
		after, _ = strconv.ParseUint(raw, 10, 64)
	}
	sessionID := c.Params("id")
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
				if err := writeInteractiveEvent(w, ev); err != nil {
					return
				}
			}
		}
	})
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
	switch {
	case errors.Is(err, service.ErrInteractiveSessionNotFound):
		return Error(c, 404, err.Error())
	case errors.Is(err, service.ErrInteractiveSessionOwner):
		return Forbidden(c, err.Error())
	case errors.Is(err, service.ErrInteractivePending), errors.Is(err, service.ErrInteractiveBusy), errors.Is(err, service.ErrInteractiveToken), errors.Is(err, service.ErrInteractiveIdempotency):
		return Error(c, 409, err.Error())
	case errors.Is(err, service.ErrInteractiveSessionClosed):
		return Error(c, 410, err.Error())
	case errors.Is(err, service.ErrInteractiveEventGap):
		return Error(c, 409, err.Error())
	case errors.Is(err, service.ErrAssistantModeUnavailable), errors.Is(err, service.ErrAssistantCheckpointConflict):
		return Error(c, 409, err.Error())
	case errors.Is(err, service.ErrAssistantCheckpointNotFound):
		return Error(c, 404, err.Error())
	case errors.Is(err, service.ErrAssistantRuntimeContextTooLarge):
		return Error(c, 413, err.Error())
	case errors.Is(err, service.ErrAssistantCheckpointInvalid):
		return Error(c, 400, err.Error())
	case errors.Is(err, service.ErrProjectProfileVersionConflict), errors.Is(err, service.ErrProjectMemoryConflict):
		return Error(c, 409, err.Error())
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

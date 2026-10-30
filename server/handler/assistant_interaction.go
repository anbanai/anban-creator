package handler

import (
	"bufio"
	"encoding/json"
	"strconv"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

type assistantInteractionCreateRequest struct {
	ModeID              string `json:"mode_id"`
	ProjectID           string `json:"project_id,omitempty"`
	ContentID           string `json:"content_id,omitempty"`
	ResumeInteractionID string `json:"resume_interaction_id,omitempty"`
	AgentID             string `json:"agent_id,omitempty"`
	Intent              string `json:"intent,omitempty"`
}

func (h *AssistantHandler) InteractionCreate(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	var req assistantInteractionCreateRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	snapshot, err := h.interactionSvc.CreateOrResume(c.Context(), uid, service.AssistantInteractionInput{ModeID: req.ModeID, ProjectID: req.ProjectID, ContentID: req.ContentID, ResumeInteractionID: req.ResumeInteractionID, AgentID: req.AgentID, Intent: req.Intent}, c.Get("Idempotency-Key"))
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, snapshot)
}

func (h *AssistantHandler) InteractionSnapshot(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	snapshot, err := h.interactionSvc.Snapshot(c.Context(), uid, c.Params("id"))
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, snapshot)
}

func (h *AssistantHandler) InteractionMessage(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	var req struct {
		Generation      int64  `json:"generation"`
		ClientMessageID string `json:"client_message_id"`
		Text            string `json:"text"`
	}
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	err := h.interactionSvc.SendMessage(c.Context(), uid, c.Params("id"), service.InteractionMessageInput{Generation: req.Generation, ClientMessageID: req.ClientMessageID, Text: req.Text})
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, fiber.Map{"accepted": true})
}

func (h *AssistantHandler) InteractionAnswer(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	var req struct {
		CallID     string                      `json:"call_id"`
		Generation int64                       `json:"generation"`
		AttemptID  string                      `json:"attempt_id"`
		Answers    []service.InteractiveAnswer `json:"answers"`
	}
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	err := h.interactionSvc.AnswerQuestion(c.Context(), uid, c.Params("id"), service.InteractionAnswerInput{Generation: req.Generation, AttemptID: req.AttemptID, CallID: req.CallID, Answers: req.Answers})
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, fiber.Map{"accepted": true})
}

func (h *AssistantHandler) InteractionCommit(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	var req service.InteractionCommitRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	result, err := h.interactionSvc.CommitInteraction(c.Context(), uid, c.Params("id"), req)
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, result)
}

func (h *AssistantHandler) InteractionContinue(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	var req service.InteractionContinueRequest
	if err := decodeInteractiveJSON(c.Body(), &req); err != nil {
		return Error(c, 400, "invalid request body")
	}
	snapshot, err := h.interactionSvc.ContinueInteraction(c.Context(), uid, c.Params("id"), req)
	if err != nil {
		return interactiveError(c, err)
	}
	return Success(c, snapshot)
}

func (h *AssistantHandler) InteractionDelete(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	if err := h.interactionSvc.Abandon(c.Context(), uid, c.Params("id")); err != nil {
		return interactiveError(c, err)
	}
	return Success(c, nil)
}

func (h *AssistantHandler) InteractionEvents(c fiber.Ctx) error {
	uid := GetUserID(c)
	if uid == "" {
		return Error(c, 401, "unauthorized")
	}
	if h.interactionSvc == nil {
		return Error(c, 503, "assistant unavailable")
	}
	if _, err := h.interactionSvc.Snapshot(c.Context(), uid, c.Params("id")); err != nil {
		return interactiveError(c, err)
	}
	after, _ := strconv.ParseUint(c.Get("Last-Event-ID"), 10, 64)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Abandon()
	return c.SendStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(750 * time.Millisecond)
		defer ticker.Stop()
		for {
			events, err := h.interactionSvc.Events(c.Context(), uid, c.Params("id"), after)
			if err != nil {
				return
			}
			for _, event := range events {
				if _, err := w.WriteString("id: " + strconv.FormatUint(event.Sequence, 10) + "\nevent: " + event.Type + "\ndata: "); err != nil {
					return
				}
				encoded := interactionEventData(event)
				if _, err := w.Write(encoded); err != nil {
					return
				}
				if _, err := w.WriteString("\n\n"); err != nil || w.Flush() != nil {
					return
				}
				after = event.Sequence
			}
			select {
			case <-c.RequestCtx().Done():
				return
			case <-c.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
}

func interactionEventData(event *model.AssistantInteractionEvent) []byte {
	if event == nil {
		return []byte(`{}`)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil || fields == nil {
		return event.Data
	}
	generation, _ := json.Marshal(event.Generation)
	fields["generation"] = generation
	encoded, err := json.Marshal(fields)
	if err != nil {
		return event.Data
	}
	return encoded
}

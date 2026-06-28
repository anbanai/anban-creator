package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/wcf"
)

// wcfSender is the subset of the wcf client used by the notifier, extracted so
// the notifier can be unit-tested without spinning an HTTP server.
type wcfSender interface {
	SendText(ctx context.Context, accountID, toUserID, text string) error
}

// WCFNotifier pushes task terminal-state notifications and command replies to a
// user's WeChat via the wcfLink sidecar. It mirrors the Redis pub/sub notifier's
// nil-safe, best-effort philosophy: a missing binding or a sidecar hiccup must
// never fail the task pipeline. Construct only when wcf is enabled; otherwise
// leave the TaskService.wcfNotifier field nil and all call sites no-op.
type WCFNotifier struct {
	sender  wcfSender
	repo    repository.Repository
	logger  *zerolog.Logger
	enabled bool
}

// NewWCFNotifier constructs a notifier. Pass enabled=false (or a nil sender) to
// keep every method a silent no-op.
func NewWCFNotifier(client *wcf.Client, repo repository.Repository, enabled bool, logger *zerolog.Logger) *WCFNotifier {
	n := &WCFNotifier{repo: repo, logger: logger, enabled: enabled}
	if client != nil {
		n.sender = client
	}
	return n
}

// Available reports whether notifications can be delivered at all.
func (n *WCFNotifier) Available() bool {
	return n != nil && n.enabled && n.sender != nil
}

// SendToBinding sends a text to a specific binding's captured peer. Used by the
// command dispatcher to reply in the same conversation. Best-effort: errors are
// logged but NOT returned to callers that ignore them; returns the error so the
// dispatcher can surface a "发送失败" hint when it cares.
func (n *WCFNotifier) SendToBinding(ctx context.Context, binding *model.WCFBinding, text string) error {
	if !n.Available() {
		return nil
	}
	if binding == nil || binding.WCFAccountID == "" || binding.PeerID == "" {
		return nil
	}
	return n.send(ctx, binding.WCFAccountID, binding.PeerID, text)
}

// NotifyTerminal sends a task success/failure/cancel message to the task owner's
// WeChat. Safe to call from any terminal-state hook: no-op when unavailable, no
// binding, binding not active, or peer not yet captured. Never returns an error
// to the caller — sending is fire-and-forget from the task pipeline's view.
func (n *WCFNotifier) NotifyTerminal(ctx context.Context, task *model.Task, status, errMsg string) {
	if !n.Available() || task == nil {
		return
	}
	binding, err := n.repo.WCFBindings().FindByUserID(ctx, task.UserID)
	if err != nil || binding == nil {
		return // user has no WeChat binding — silent skip
	}
	if binding.Status != model.WCFBindingStatusActive || binding.PeerID == "" {
		return // channel not active / not yet activated by an inbound message
	}
	text := formatTerminalMessage(task, status, errMsg)
	if err := n.send(ctx, binding.WCFAccountID, binding.PeerID, text); err != nil {
		if n.logger != nil {
			n.logger.Warn().Err(err).
				Str("task_id", task.ID).
				Str("user_id", task.UserID).
				Msg("wcf terminal notification send failed")
		}
	}
}

func (n *WCFNotifier) send(ctx context.Context, accountID, peerID, text string) error {
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return n.sender.SendText(sendCtx, accountID, peerID, text)
}

// formatTerminalMessage builds the Chinese notification body for a terminal task.
func formatTerminalMessage(task *model.Task, status, errMsg string) string {
	label := taskTypeLabel(task.Type)
	subject := strings.TrimSpace(task.Title)
	if subject == "" {
		subject = firstLine(task.Prompt)
	}
	if subject == "" {
		subject = task.ID
	}
	switch status {
	case model.TaskStatusCompleted:
		return fmt.Sprintf("✅ %s已完成\n《%s》\n任务ID: %s", label, subject, task.ID)
	case model.TaskStatusFailed:
		reason := strings.TrimSpace(errMsg)
		if reason == "" {
			reason = "未知错误"
		}
		// Truncate by RUNE so multi-byte (Chinese) error text is never split
		// mid-character and produces invalid UTF-8 in the WeChat message body.
		if runes := []rune(reason); len(runes) > 200 {
			reason = string(runes[:200]) + "…"
		}
		return fmt.Sprintf("❌ %s失败\n《%s》\n原因: %s\n任务ID: %s", label, subject, reason, task.ID)
	case model.TaskStatusCancelled:
		return fmt.Sprintf("🚫 %s已取消\n《%s》\n任务ID: %s", label, subject, task.ID)
	default:
		return fmt.Sprintf("%s状态变更: %s\n任务ID: %s", label, status, task.ID)
	}
}

func taskTypeLabel(t string) string {
	switch t {
	case model.PlatformArticle:
		return "文章"
	case model.PlatformSeednote:
		return "种草笔记"
	case model.PlatformEcommerce:
		return "电商图"
	case "poster":
		return "海报"
	default:
		if t == "" {
			return "任务"
		}
		return t
	}
}

// firstLine returns the first line of a prompt, truncated for notification subjects.
func firstLine(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if i := strings.IndexAny(prompt, "\r\n"); i >= 0 {
		prompt = prompt[:i]
	}
	prompt = strings.TrimSpace(prompt) // drop trailing spaces on the extracted line
	const max = 40
	if runes := []rune(prompt); len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return prompt
}

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/wcf"
)

// maxWeChatPromptRunes caps the prompt a user can submit via WeChat. Mirrors the
// handler-layer maxTaskPromptCharacters so the bot and Studio enforce the same
// ceiling. Kept local (not imported) because the handler constant is unexported.
const maxWeChatPromptRunes = 5120

// rateLimitPerMinute caps how many commands a single user may issue per minute
// via WeChat. Commands create billable tasks, so this guards against a runaway
// loop or accidental paste-flood.
const rateLimitPerMinute = 10

// wcfCommandKind enumerates the recognized WeChat commands.
type wcfCommandKind int

const (
	cmdUnknown wcfCommandKind = iota
	cmdHelp
	cmdRecent
	cmdStatus
	cmdCancel
	cmdCreate
)

// parsedCommand is the output of parseCommand: a kind plus the trailing args
// (trimmed). For cmdCreate args is the prompt; for cmdStatus/cmdCancel the id.
type parsedCommand struct {
	kind wcfCommandKind
	args string
}

// createPrefixes are the trigger words for "create a task". The task TYPE comes
// from the default project's platform — these words are pure aliases, ordered
// longest-first. Bare high-frequency single/ambiguous words ("写", "文章",
// "任务") are deliberately excluded: a create command is BILLABLE (credits are
// charged), so a casual phrase like "写得好" or "任务太多了" must NOT spin up a
// task. Only explicit genre verbs survive — keep it that way when adding aliases.
var createPrefixes = []string{
	"写文章", "写种草", "种草", "海报", "创建",
}

// statusPrefixes / cancelPrefixes trigger id-taking commands.
var (
	statusPrefixes = []string{"状态", "查询", "status"}
	cancelPrefixes = []string{"取消", "cancel"}
)

// parseCommand classifies an inbound WeChat message into a command + args. It is
// pure and table-driven so it can be unit-tested without the dispatcher.
func parseCommand(raw string) parsedCommand {
	text := strings.TrimSpace(raw)
	if text == "" {
		return parsedCommand{kind: cmdUnknown}
	}
	lower := strings.ToLower(text)

	switch lower {
	case "?", "？", "help", "帮助":
		return parsedCommand{kind: cmdHelp}
	case "最近", "列表", "list", "recent":
		return parsedCommand{kind: cmdRecent}
	}

	if kind, args, ok := matchPrefix(text, statusPrefixes, cmdStatus); ok {
		return parsedCommand{kind: kind, args: args}
	}
	if kind, args, ok := matchPrefix(text, cancelPrefixes, cmdCancel); ok {
		return parsedCommand{kind: kind, args: args}
	}
	if kind, args, ok := matchPrefix(text, createPrefixes, cmdCreate); ok {
		return parsedCommand{kind: kind, args: args}
	}
	return parsedCommand{kind: cmdUnknown}
}

// matchPrefix returns the command + trailing args if text starts with one of the
// prefixes (followed by a separator or end-of-string); args are trimmed.
func matchPrefix(text string, prefixes []string, kind wcfCommandKind) (wcfCommandKind, string, bool) {
	for _, p := range prefixes {
		if text == p {
			return kind, "", true
		}
		if strings.HasPrefix(text, p) {
			rest := strings.TrimLeft(text[len(p):], " \t,，:：\r\n")
			if rest != "" {
				return kind, rest, true
			}
		}
	}
	return cmdUnknown, "", false
}

// wcfRateLimiter is a minimal per-key sliding-window limiter (in-memory). Keys
// are user ids. Lost on restart, which is acceptable for a coarse abuse guard.
type wcfRateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newWCFRateLimiter(max int, window time.Duration) *wcfRateLimiter {
	return &wcfRateLimiter{hits: make(map[string][]time.Time), window: window, max: max}
}

// Allow reports whether key is under the limit, recording the attempt either way.
func (l *wcfRateLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	fresh := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= l.max {
		l.hits[key] = fresh
		return false
	}
	fresh = append(fresh, now)
	l.hits[key] = fresh
	return true
}

// WCFCommandDispatcher turns inbound WeChat text events into task actions and
// replies. Each command is scoped to the binding's user; status/cancel re-check
// task ownership because TaskService.Cancel does not.
type WCFCommandDispatcher struct {
	taskSvc  *TaskService
	notifier *WCFNotifier
	limiter  *wcfRateLimiter
	logger   *zerolog.Logger
	nowFunc  func() time.Time
}

// NewWCFCommandDispatcher constructs the dispatcher. nowFunc defaults to
// time.Now when nil (kept injectable for tests).
func NewWCFCommandDispatcher(taskSvc *TaskService, notifier *WCFNotifier, logger *zerolog.Logger) *WCFCommandDispatcher {
	return &WCFCommandDispatcher{
		taskSvc:  taskSvc,
		notifier: notifier,
		limiter:  newWCFRateLimiter(rateLimitPerMinute, time.Minute),
		logger:   logger,
		nowFunc:  time.Now,
	}
}

// Handle parses one inbound event and replies to the binding's peer. It never
// returns an error that would stop the poller — failures become user-facing
// reply text. binding carries the resolved user + account + peer.
func (d *WCFCommandDispatcher) Handle(ctx context.Context, binding *model.WCFBinding, event wcf.Event) {
	if d == nil || binding == nil {
		return
	}
	// Self-defense: only inbound text can be a command. The poller already
	// filters, but Handle is a public method — guard it so a future caller (e.g.
	// a webhook path) can't accidentally parse outbound messages as commands.
	if !event.IsInboundText() {
		return
	}
	now := time.Now()
	if d.nowFunc != nil {
		now = d.nowFunc()
	}
	if !d.limiter.Allow(binding.UserID, now) {
		d.reply(ctx, binding, "⏳ 命令太频繁，请稍后再试。")
		return
	}

	cmd := parseCommand(event.BodyText)
	var reply string
	switch cmd.kind {
	case cmdHelp, cmdUnknown:
		reply = wcfHelpText()
	case cmdRecent:
		reply = d.handleRecent(ctx, binding.UserID)
	case cmdStatus:
		reply = d.handleStatus(ctx, binding.UserID, cmd.args)
	case cmdCancel:
		reply = d.handleCancel(ctx, binding.UserID, cmd.args)
	case cmdCreate:
		reply = d.handleCreate(ctx, binding, cmd.args)
	}
	if reply != "" {
		d.reply(ctx, binding, reply)
	}
}

func (d *WCFCommandDispatcher) reply(ctx context.Context, binding *model.WCFBinding, text string) {
	if d.notifier == nil {
		return
	}
	if err := d.notifier.SendToBinding(ctx, binding, text); err != nil && d.logger != nil {
		d.logger.Warn().Err(err).Str("user_id", binding.UserID).Msg("wcf command reply failed")
	}
}

func (d *WCFCommandDispatcher) handleCreate(ctx context.Context, binding *model.WCFBinding, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "请提供任务主题，例如：\n写文章 夏日防晒指南"
	}
	if utf8.RuneCountInString(prompt) > maxWeChatPromptRunes {
		return fmt.Sprintf("主题过长，请控制在 %d 字以内。", maxWeChatPromptRunes)
	}
	if binding.DefaultProjectID == "" {
		return "请先在 Studio 设置页绑定「默认项目」，再来发命令创建任务。"
	}
	if d.taskSvc == nil {
		return "任务服务暂不可用，请稍后再试。"
	}

	tasks, err := d.taskSvc.CreateManual(ctx, CreateManualParams{
		UserID:    binding.UserID,
		ProjectID: binding.DefaultProjectID,
		Prompt:    prompt,
		Quantity:  1,
	})
	if err != nil {
		// CreateManual wraps the sentinel as "积分不足: %w", so errors.Is is the
		// reliable match (a substring check on "insufficient" would be dead code).
		if errors.Is(err, ErrInsufficientCredits) {
			return "❌ 积分不足，无法创建任务。"
		}
		if d.logger != nil {
			d.logger.Warn().Err(err).Str("user_id", binding.UserID).Msg("wcf create command failed")
		}
		return "❌ 创建任务失败：" + cleanErr(err.Error())
	}
	if len(tasks) == 0 {
		return "❌ 创建任务失败：未生成任务。"
	}
	t := tasks[0]
	return fmt.Sprintf("✅ 已创建%s任务\n《%s》\n任务ID: %s\n查询回复「状态 %s」",
		taskTypeLabel(t.Type), taskSubject(t), t.ID, t.ID)
}

func (d *WCFCommandDispatcher) handleStatus(ctx context.Context, userID, taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "请提供任务ID，例如：\n状态 <任务ID>"
	}
	if d.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	task, err := d.taskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return "未找到该任务。"
	}
	if task.UserID != userID {
		return "未找到该任务。" // do not leak existence of others' tasks
	}
	return fmt.Sprintf("📋 任务状态\n《%s》\n状态: %s\n任务ID: %s",
		taskSubject(task), taskStatusLabel(task.Status), task.ID)
}

func (d *WCFCommandDispatcher) handleCancel(ctx context.Context, userID, taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "请提供任务ID，例如：\n取消 <任务ID>"
	}
	if d.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	task, err := d.taskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return "未找到该任务。"
	}
	if task.UserID != userID {
		return "未找到该任务。"
	}
	// Only pending/running are cancellable; a terminal task is a no-op reply.
	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		return fmt.Sprintf("该任务当前为「%s」，无法取消。", taskStatusLabel(task.Status))
	}
	if err := d.taskSvc.Cancel(ctx, taskID); err != nil {
		if d.logger != nil {
			d.logger.Warn().Err(err).Str("task_id", taskID).Msg("wcf cancel command failed")
		}
		return "❌ 取消失败：" + cleanErr(err.Error())
	}
	return fmt.Sprintf("🚫 已取消任务\n《%s》", taskSubject(task))
}

func (d *WCFCommandDispatcher) handleRecent(ctx context.Context, userID string) string {
	if d.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	tasks, _, err := d.taskSvc.List(ctx, userID, 0, 5, "", "")
	if err != nil || len(tasks) == 0 {
		return "暂无最近任务。"
	}
	var b strings.Builder
	b.WriteString("📋 最近任务\n")
	for i, t := range tasks {
		fmt.Fprintf(&b, "%d. 《%s》— %s\n", i+1, taskSubject(t), taskStatusLabel(t.Status))
	}
	return strings.TrimRight(b.String(), "\n")
}

// taskSubject returns a short label for a task: its title, else the first line of
// its prompt, else the id.
func taskSubject(t *model.Task) string {
	if s := strings.TrimSpace(t.Title); s != "" {
		return truncateRunes(s, 30)
	}
	if s := firstLine(t.Prompt); s != "" {
		return truncateRunes(s, 30)
	}
	return t.ID
}

func taskStatusLabel(status string) string {
	switch status {
	case model.TaskStatusPending:
		return "排队中"
	case model.TaskStatusRunning:
		return "执行中"
	case model.TaskStatusCompleted:
		return "已完成"
	case model.TaskStatusFailed:
		return "失败"
	case model.TaskStatusCancelled:
		return "已取消"
	default:
		if status == "" {
			return "未知"
		}
		return status
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// cleanErr strips noisy wrapping from an error string for a user-facing reply.
// Truncates by RUNE so multi-byte (Chinese) text is never split mid-character.
func cleanErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if runes := []rune(msg); len(runes) > 120 {
		msg = string(runes[:120]) + "…"
	}
	return msg
}

func wcfHelpText() string {
	return strings.Join([]string{
		"🤖 指令助手",
		"• 写文章/种草/海报 <主题> — 创建任务",
		"• 状态 <任务ID> — 查询任务状态",
		"• 最近 — 列出最近 5 条任务",
		"• 取消 <任务ID> — 取消任务",
		"• 帮助 / ? — 显示本帮助",
		"",
		"任务类型由绑定的「默认项目」决定；请先在 Studio 设置默认项目。",
	}, "\n")
}

package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/model"
)

const maxWeChatPromptRunes = 5120

type ilinkCommandKind int

const (
	cmdUnknown ilinkCommandKind = iota
	cmdHelp
	cmdRecent
	cmdStatus
	cmdCancel
	cmdCreate
)

type parsedCommand struct {
	kind ilinkCommandKind
	args string
}

var createPrefixes = []string{"写文章", "写种草", "种草", "海报", "创建"}

var (
	statusPrefixes = []string{"状态", "查询", "status"}
	cancelPrefixes = []string{"取消", "cancel"}
)

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

func matchPrefix(text string, prefixes []string, kind ilinkCommandKind) (ilinkCommandKind, string, bool) {
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
	return string([]rune(s)[:max]) + "..."
}

func cleanErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if runes := []rune(msg); len(runes) > 120 {
		msg = string(runes[:120]) + "..."
	}
	return msg
}

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
		return fmt.Sprintf("%s已完成\n《%s》\n任务ID: %s", label, subject, task.ID)
	case model.TaskStatusFailed:
		reason := strings.TrimSpace(errMsg)
		if reason == "" {
			reason = "未知错误"
		}
		if runes := []rune(reason); len(runes) > 200 {
			reason = string(runes[:200]) + "..."
		}
		return fmt.Sprintf("%s失败\n《%s》\n原因: %s\n任务ID: %s", label, subject, reason, task.ID)
	case model.TaskStatusCancelled:
		return fmt.Sprintf("%s已取消\n《%s》\n任务ID: %s", label, subject, task.ID)
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

func firstLine(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if i := strings.IndexAny(prompt, "\r\n"); i >= 0 {
		prompt = prompt[:i]
	}
	prompt = strings.TrimSpace(prompt)
	const max = 40
	if runes := []rune(prompt); len(runes) > max {
		return string(runes[:max]) + "..."
	}
	return prompt
}

func stringPtr(s string) *string {
	return &s
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/wcf"
)

type IlinkConversationService struct {
	taskSvc *TaskService
	sender  ilinkSender
	logger  *zerolog.Logger
}

func NewIlinkConversationService(taskSvc *TaskService, sender ilinkSender, logger *zerolog.Logger) *IlinkConversationService {
	return &IlinkConversationService{taskSvc: taskSvc, sender: sender, logger: logger}
}

func (s *IlinkConversationService) Handle(ctx context.Context, binding *model.IlinkBinding, event wcf.Event) {
	if s == nil || binding == nil || !event.IsInboundText() {
		return
	}
	cmd := parseIlinkIntent(event.BodyText)
	var reply string
	switch cmd.kind {
	case cmdHelp, cmdUnknown:
		reply = ilinkHelpText()
	case cmdRecent:
		reply = s.handleRecent(ctx, binding.UserID)
	case cmdStatus:
		reply = s.handleStatus(ctx, binding.UserID, cmd.args)
	case cmdCancel:
		reply = s.handleCancel(ctx, binding.UserID, cmd.args)
	case cmdCreate:
		reply = s.handleCreate(ctx, binding, cmd.args)
	}
	s.reply(ctx, binding, reply)
}

func parseIlinkIntent(raw string) parsedCommand {
	cmd := parseCommand(raw)
	if cmd.kind != cmdUnknown {
		return cmd
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return cmd
	}
	for _, p := range []string{"帮我写", "帮我做", "帮我生成", "生成", "做一份", "写一篇"} {
		if strings.HasPrefix(text, p) {
			return parsedCommand{kind: cmdCreate, args: strings.TrimSpace(strings.TrimLeft(text[len(p):], " \t,，:：\r\n"))}
		}
	}
	return cmd
}

func (s *IlinkConversationService) reply(ctx context.Context, binding *model.IlinkBinding, text string) {
	if s.sender == nil || binding == nil || text == "" {
		return
	}
	if err := s.sender.SendText(ctx, stringValue(binding.PlatformAccountID), stringValue(binding.ExternalUserID), text); err != nil && s.logger != nil {
		s.logger.Warn().Err(err).Str("user_id", binding.UserID).Msg("ilink conversation reply failed")
	}
}

func (s *IlinkConversationService) handleCreate(ctx context.Context, binding *model.IlinkBinding, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "请告诉我任务主题，例如：帮我写一篇夏日防晒指南"
	}
	if utf8.RuneCountInString(prompt) > maxWeChatPromptRunes {
		return fmt.Sprintf("主题过长，请控制在 %d 字以内。", maxWeChatPromptRunes)
	}
	if binding.DefaultProjectID == "" {
		return "请先在 Studio 设置页选择默认项目，然后再通过微信创建任务。"
	}
	if s.taskSvc == nil {
		return "任务服务暂不可用，请稍后再试。"
	}
	tasks, err := s.taskSvc.CreateManual(ctx, CreateManualParams{
		UserID:    binding.UserID,
		ProjectID: binding.DefaultProjectID,
		Prompt:    prompt,
		Quantity:  1,
	})
	if err != nil {
		return "创建任务失败：" + cleanErr(err.Error())
	}
	if len(tasks) == 0 {
		return "创建任务失败：未生成任务。"
	}
	t := tasks[0]
	return fmt.Sprintf("已创建%s任务\n《%s》\n任务ID: %s\n完成或失败后我会在这里通知你。", taskTypeLabel(t.Type), taskSubject(t), t.ID)
}

func (s *IlinkConversationService) handleStatus(ctx context.Context, userID, taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "请提供任务ID，例如：状态 <任务ID>"
	}
	if s.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	task, err := s.taskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil || task.UserID != userID {
		return "未找到该任务。"
	}
	return fmt.Sprintf("任务状态\n《%s》\n状态: %s\n任务ID: %s", taskSubject(task), taskStatusLabel(task.Status), task.ID)
}

func (s *IlinkConversationService) handleCancel(ctx context.Context, userID, taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "请提供任务ID，例如：取消 <任务ID>"
	}
	if s.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	task, err := s.taskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil || task.UserID != userID {
		return "未找到该任务。"
	}
	if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
		return fmt.Sprintf("该任务当前为「%s」，无法取消。", taskStatusLabel(task.Status))
	}
	if err := s.taskSvc.Cancel(ctx, taskID); err != nil {
		return "取消失败：" + cleanErr(err.Error())
	}
	return fmt.Sprintf("已取消任务\n《%s》", taskSubject(task))
}

func (s *IlinkConversationService) handleRecent(ctx context.Context, userID string) string {
	if s.taskSvc == nil {
		return "任务服务暂不可用。"
	}
	tasks, _, err := s.taskSvc.List(ctx, userID, 0, 5, "", "", "")
	if err != nil || len(tasks) == 0 {
		return "暂无最近任务。"
	}
	var b strings.Builder
	b.WriteString("最近任务\n")
	for i, t := range tasks {
		fmt.Fprintf(&b, "%d. 《%s》 - %s\n", i+1, taskSubject(t), taskStatusLabel(t.Status))
	}
	return strings.TrimRight(b.String(), "\n")
}

func ilinkHelpText() string {
	return strings.Join([]string{
		"我是 Anban 微信助手，可以帮你创建和跟踪任务。",
		"你可以直接说：帮我写一篇夏日防晒指南",
		"也可以发送：最近、状态 <任务ID>、取消 <任务ID>",
		"任务成功或失败后，我会在这里通知你。",
	}, "\n")
}

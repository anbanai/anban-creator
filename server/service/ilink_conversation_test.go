package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/wcf"
)

type fakeIlinkConversationSender struct {
	texts []string
}

func (f *fakeIlinkConversationSender) SendText(_ context.Context, _ string, _ string, text string) error {
	f.texts = append(f.texts, text)
	return nil
}

type fakeIlinkAIEntry struct {
	req AIEntrySubmitRequest
	res AIEntrySubmitResult
}

func (f *fakeIlinkAIEntry) Submit(_ context.Context, req AIEntrySubmitRequest) (*AIEntrySubmitResult, error) {
	f.req = req
	return &f.res, nil
}

func TestIlinkConversationNaturalLanguageCreateUsesAIEntryService(t *testing.T) {
	logger := zerolog.New(io.Discard)
	sender := &fakeIlinkConversationSender{}
	created := &model.Task{ID: uuid.NewString(), Type: model.PlatformArticle, Prompt: "夏日防晒指南"}
	entry := &fakeIlinkAIEntry{res: AIEntrySubmitResult{
		Status:  AIEntryStatusCreated,
		Tasks:   []*model.Task{created},
		Message: "已创建任务",
	}}
	svc := NewIlinkConversationService(nil, sender, &logger)
	svc.SetAIEntryService(entry)
	binding := &model.IlinkBinding{
		UserID:            "user-1",
		DefaultProjectID:  "project-1",
		PlatformAccountID: stringPtr("assistant"),
		ExternalUserID:    stringPtr("wx-user"),
		Status:            model.IlinkBindingStatusActive,
	}

	svc.Handle(context.Background(), binding, wcf.Event{
		AccountID:  "assistant",
		Direction:  "inbound",
		EventType:  "text",
		FromUserID: "wx-user",
		BodyText:   "帮我写一篇夏日防晒指南",
	})

	if entry.req.UserID != "user-1" || entry.req.ProjectID != "project-1" || entry.req.Channel != "ilink" {
		t.Fatalf("entry req = %#v", entry.req)
	}
	if entry.req.Text != "帮我写一篇夏日防晒指南" {
		t.Fatalf("entry text = %q", entry.req.Text)
	}
	if entry.req.Quantity != 0 {
		t.Fatalf("iLink should omit quantity and rely on the one-task default, got %d", entry.req.Quantity)
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], created.ID) {
		t.Fatalf("texts = %#v, want task id", sender.texts)
	}
}

func TestIlinkConversationNaturalLanguageCreateRejectsMissingTask(t *testing.T) {
	for _, tt := range []struct {
		name  string
		tasks []*model.Task
	}{
		{name: "empty collection"},
		{name: "nil first task", tasks: []*model.Task{nil}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			sender := &fakeIlinkConversationSender{}
			entry := &fakeIlinkAIEntry{res: AIEntrySubmitResult{Status: AIEntryStatusCreated, Tasks: tt.tasks}}
			svc := NewIlinkConversationService(nil, sender, &logger)
			svc.SetAIEntryService(entry)
			binding := &model.IlinkBinding{
				UserID:            "user-1",
				DefaultProjectID:  "project-1",
				PlatformAccountID: stringPtr("assistant"),
				ExternalUserID:    stringPtr("wx-user"),
				Status:            model.IlinkBindingStatusActive,
			}

			svc.Handle(context.Background(), binding, wcf.Event{
				Direction: "inbound", EventType: "text", BodyText: "帮我写一篇文章",
			})

			if len(sender.texts) != 1 || sender.texts[0] != "创建任务失败：未生成任务。" {
				t.Fatalf("texts = %#v", sender.texts)
			}
		})
	}
}

func TestIlinkConversationCommandStillBypassesAIEntryService(t *testing.T) {
	logger := zerolog.New(io.Discard)
	sender := &fakeIlinkConversationSender{}
	entry := &fakeIlinkAIEntry{}
	svc := NewIlinkConversationService(nil, sender, &logger)
	svc.SetAIEntryService(entry)
	binding := &model.IlinkBinding{
		UserID:            "user-1",
		DefaultProjectID:  "project-1",
		PlatformAccountID: stringPtr("assistant"),
		ExternalUserID:    stringPtr("wx-user"),
		Status:            model.IlinkBindingStatusActive,
	}

	svc.Handle(context.Background(), binding, wcf.Event{
		AccountID:  "assistant",
		Direction:  "inbound",
		EventType:  "text",
		FromUserID: "wx-user",
		BodyText:   "状态 task-1",
	})

	if entry.req.UserID != "" {
		t.Fatalf("entry should not be called for status command, got %#v", entry.req)
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "任务服务暂不可用") {
		t.Fatalf("texts = %#v", sender.texts)
	}
}

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// fakeWCFSender records the last SendText call. Implements wcfSender.
type fakeWCFSender struct {
	lastAccount string
	lastPeer    string
	lastText    string
	calls       int
	err         error
}

func (f *fakeWCFSender) SendText(_ context.Context, accountID, toUserID, text string) error {
	f.calls++
	f.lastAccount = accountID
	f.lastPeer = toUserID
	f.lastText = text
	return f.err
}

// stubWCFBindingsRepo embeds the (nil) interface so only FindByUserID is
// implemented — every other method panics if called, which the notifier never
// does. Keeps the stub to the one method under test.
type stubWCFBindingsRepo struct {
	repository.WCFBindingRepository
	binding *model.WCFBinding
	err     error
}

func (s *stubWCFBindingsRepo) FindByUserID(_ context.Context, _ string) (*model.WCFBinding, error) {
	return s.binding, s.err
}

// stubRepo embeds the (nil) Repository interface; only WCFBindings is overridden.
type stubRepo struct {
	repository.Repository
	bindings repository.WCFBindingRepository
}

func (s *stubRepo) WCFBindings() repository.WCFBindingRepository { return s.bindings }

func newTestNotifier(sender wcfSender, binding *model.WCFBinding) (*WCFNotifier, *fakeWCFSender) {
	if sender == nil {
		fs := &fakeWCFSender{}
		return &WCFNotifier{
			sender:  fs,
			repo:    &stubRepo{bindings: &stubWCFBindingsRepo{binding: binding}},
			enabled: true,
		}, fs
	}
	return &WCFNotifier{
		sender:  sender,
		repo:    &stubRepo{bindings: &stubWCFBindingsRepo{binding: binding}},
		enabled: true,
	}, sender.(*fakeWCFSender)
}

func TestWCFNotifier_Available(t *testing.T) {
	// Nil receiver and disabled/nil-sender notifiers are unavailable.
	var nilNotifier *WCFNotifier
	if nilNotifier.Available() {
		t.Fatal("nil notifier should be unavailable")
	}
	if (&WCFNotifier{enabled: true}).Available() { // sender nil
		t.Fatal("notifier with nil sender should be unavailable")
	}
}

func TestWCFNotifier_NotifyTerminalNoBinding(t *testing.T) {
	n, fs := newTestNotifier(nil, nil) // no binding for the user
	n.NotifyTerminal(context.Background(), &model.Task{ID: "t1", UserID: "u1", Type: model.PlatformArticle}, model.TaskStatusCompleted, "")
	if fs.calls != 0 {
		t.Fatalf("expected no send when binding absent, got %d", fs.calls)
	}
}

func TestWCFNotifier_NotifyTerminalInactiveBinding(t *testing.T) {
	binding := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "peer", Status: model.WCFBindingStatusPending}
	n, fs := newTestNotifier(nil, binding)
	n.NotifyTerminal(context.Background(), &model.Task{ID: "t1", UserID: "u1"}, model.TaskStatusCompleted, "")
	if fs.calls != 0 {
		t.Fatalf("expected no send for inactive binding, got %d", fs.calls)
	}
}

func TestWCFNotifier_NotifyTerminalMissingPeer(t *testing.T) {
	binding := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "", Status: model.WCFBindingStatusActive}
	n, fs := newTestNotifier(nil, binding)
	n.NotifyTerminal(context.Background(), &model.Task{ID: "t1", UserID: "u1"}, model.TaskStatusCompleted, "")
	if fs.calls != 0 {
		t.Fatalf("expected no send when peer not captured, got %d", fs.calls)
	}
}

func TestWCFNotifier_NotifyTerminalSendsFormatted(t *testing.T) {
	binding := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc-1", PeerID: "peer-1", Status: model.WCFBindingStatusActive}
	task := &model.Task{ID: "task-xyz", UserID: "u1", Type: model.PlatformSeednote, Title: "防晒测评"}

	cases := []struct {
		status   string
		errMsg   string
		contains []string
	}{
		{model.TaskStatusCompleted, "", []string{"✅", "种草笔记", "已完成", "防晒测评", "task-xyz"}},
		{model.TaskStatusFailed, "rate limit", []string{"❌", "失败", "rate limit", "task-xyz"}},
		{model.TaskStatusFailed, "", []string{"❌", "未知错误"}},
		{model.TaskStatusCancelled, "", []string{"🚫", "已取消", "task-xyz"}},
	}
	for _, tc := range cases {
		n, fs := newTestNotifier(nil, binding)
		n.NotifyTerminal(context.Background(), task, tc.status, tc.errMsg)
		if fs.calls != 1 {
			t.Fatalf("%s: expected 1 send, got %d", tc.status, fs.calls)
		}
		if fs.lastAccount != "acc-1" || fs.lastPeer != "peer-1" {
			t.Fatalf("%s: sent to account=%q peer=%q", tc.status, fs.lastAccount, fs.lastPeer)
		}
		for _, want := range tc.contains {
			if !strings.Contains(fs.lastText, want) {
				t.Errorf("%s: message %q missing %q", tc.status, fs.lastText, want)
			}
		}
	}
}

func TestWCFNotifier_NotifyTerminalFallsBackToPromptFirstLine(t *testing.T) {
	binding := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "peer", Status: model.WCFBindingStatusActive}
	task := &model.Task{ID: "t9", UserID: "u1", Type: model.PlatformArticle, Title: "", Prompt: "第一行主题\n第二行内容"}
	n, fs := newTestNotifier(nil, binding)
	n.NotifyTerminal(context.Background(), task, model.TaskStatusCompleted, "")
	if fs.calls != 1 {
		t.Fatal("expected 1 send")
	}
	if !strings.Contains(fs.lastText, "第一行主题") {
		t.Errorf("expected subject to fall back to prompt first line; got %q", fs.lastText)
	}
}

func TestWCFNotifier_SendToBinding(t *testing.T) {
	fs := &fakeWCFSender{}
	n := &WCFNotifier{sender: fs, enabled: true}

	// Nil binding and empty peer/peer are silent no-ops.
	n.SendToBinding(context.Background(), nil, "hi")
	if fs.calls != 0 {
		t.Fatal("nil binding should not send")
	}
	n.SendToBinding(context.Background(), &model.WCFBinding{}, "hi")
	if fs.calls != 0 {
		t.Fatal("binding missing account/peer should not send")
	}

	binding := &model.WCFBinding{UserID: "u1", WCFAccountID: "acc", PeerID: "peer", Status: model.WCFBindingStatusActive}
	n.SendToBinding(context.Background(), binding, "hello")
	if fs.calls != 1 || fs.lastText != "hello" || fs.lastPeer != "peer" {
		t.Fatalf("unexpected send: calls=%d text=%q peer=%q", fs.calls, fs.lastText, fs.lastPeer)
	}
}

func TestFormatTerminalMessageUnknownStatus(t *testing.T) {
	task := &model.Task{ID: "t1", Type: model.PlatformArticle, Title: "X"}
	msg := formatTerminalMessage(task, "weird-status", "")
	if !strings.Contains(msg, "状态变更") {
		t.Errorf("unknown status should render generic line; got %q", msg)
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"单行":            "单行",
		"第一行\n第二行":      "第一行",
		"   首尾空白  \n次行": "首尾空白",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
	// Long line is truncated to 40 runes + ellipsis.
	long := strings.Repeat("字", 50)
	got := firstLine(long)
	if len([]rune(got)) != 41 {
		t.Errorf("firstLine long rune count = %d, want 41", len([]rune(got)))
	}
}

package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/wcf"
)

type fakeIlinkSender struct {
	accountID string
	toUserID  string
	text      string
}

func (f *fakeIlinkSender) SendText(_ context.Context, accountID, toUserID, text string) error {
	f.accountID = accountID
	f.toUserID = toUserID
	f.text = text
	return nil
}

func TestIlinkGateway_BindsContactFromBindCodeEvent(t *testing.T) {
	repo := newIlinkTestRepo(t)
	userID := uuid.NewString()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         userID,
		Email:      "gateway@example.com",
		Password:   "x",
		InviteCode: "invite3",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	log := zerolog.Nop()
	bindSvc := NewIlinkBindingService(repo, true, nil, &log)
	res, err := bindSvc.CreateBindCode(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateBindCode: %v", err)
	}
	sender := &fakeIlinkSender{}
	gateway := NewIlinkGateway(repo, bindSvc, nil, sender, &log)

	gateway.ProcessEvent(context.Background(), wcf.Event{
		ID:         1,
		AccountID:  "platform-1",
		Direction:  "inbound",
		EventType:  "text",
		FromUserID: "wx-user-1",
		BodyText:   res.BindCode,
	})

	binding, err := repo.IlinkBindings().FindByUserID(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if binding.Status != model.IlinkBindingStatusActive || stringValue(binding.PlatformAccountID) != "platform-1" || stringValue(binding.ExternalUserID) != "wx-user-1" {
		t.Fatalf("unexpected binding after event: %+v", binding)
	}
	if sender.accountID != "platform-1" || sender.toUserID != "wx-user-1" || sender.text == "" {
		t.Fatalf("expected bind reply, got account=%q to=%q text=%q", sender.accountID, sender.toUserID, sender.text)
	}
}

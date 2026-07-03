package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func newIlinkTestRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.New(db)
}

func TestIlinkBindingService_CreateBindCodeAndCompleteFromContact(t *testing.T) {
	repo := newIlinkTestRepo(t)
	userID := uuid.NewString()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         userID,
		Email:      "ilink@example.com",
		Password:   "x",
		InviteCode: "invite",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	log := zerolog.Nop()
	svc := NewIlinkBindingService(repo, true, nil, &log)

	res, err := svc.CreateBindCode(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateBindCode: %v", err)
	}
	if res.BindCode == "" || !res.ExpiresAt.After(time.Now()) {
		t.Fatalf("invalid bind code result: %+v", res)
	}

	binding, err := svc.CompleteBindByCode(context.Background(), res.BindCode, "platform-1", "wx-user-1")
	if err != nil {
		t.Fatalf("CompleteBindByCode: %v", err)
	}
	if binding.UserID != userID || stringValue(binding.PlatformAccountID) != "platform-1" || stringValue(binding.ExternalUserID) != "wx-user-1" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	if binding.Status != model.IlinkBindingStatusActive {
		t.Fatalf("status = %q, want active", binding.Status)
	}

	status, err := svc.GetStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Bound || status.Status != model.IlinkBindingStatusActive {
		t.Fatalf("status result = %+v, want active bound", status)
	}
}

func TestIlinkBindingService_AllowsMultiplePendingBindCodes(t *testing.T) {
	repo := newIlinkTestRepo(t)
	log := zerolog.Nop()
	svc := NewIlinkBindingService(repo, true, nil, &log)

	for i, email := range []string{"pending1@example.com", "pending2@example.com"} {
		userID := uuid.NewString()
		if err := repo.Users().Create(context.Background(), &model.User{
			ID:         userID,
			Email:      email,
			Password:   "x",
			InviteCode: "invite-pending-" + string(rune('0'+i)),
		}); err != nil {
			t.Fatalf("create user %d: %v", i, err)
		}
		if _, err := svc.CreateBindCode(context.Background(), userID); err != nil {
			t.Fatalf("CreateBindCode user %d: %v", i, err)
		}
	}
}

func TestIlinkBindingService_CreateBindCodeKeepsActiveBindingDeliverable(t *testing.T) {
	repo := newIlinkTestRepo(t)
	userID := uuid.NewString()
	log := zerolog.Nop()
	svc := NewIlinkBindingService(repo, true, nil, &log)
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         userID,
		Email:      "active@example.com",
		Password:   "x",
		InviteCode: "active",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create binding: %v", err)
	}

	if _, err := svc.CreateBindCode(context.Background(), userID); err != nil {
		t.Fatalf("CreateBindCode: %v", err)
	}
	binding, err := repo.IlinkBindings().FindByUserID(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if binding.Status != model.IlinkBindingStatusActive {
		t.Fatalf("status = %q, want active while waiting for rebind code", binding.Status)
	}
	if stringValue(binding.PlatformAccountID) != "platform-1" || stringValue(binding.ExternalUserID) != "wx-user-1" {
		t.Fatalf("existing delivery target changed: %+v", binding)
	}
}

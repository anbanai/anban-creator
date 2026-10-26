package repository

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestWechatOpenPlatformAuthorizationStateCanBeConsumedOnlyOnce(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	now := time.Now().UTC()
	state := &model.WechatOpenPlatformAuthorizationState{
		StateHash: "hashed-state", ProjectID: uuid.NewString(), UserID: uuid.NewString(), ExpiresAt: now.Add(time.Minute),
	}
	if err := repo.WechatOpenPlatformAuthorizations().CreateState(context.Background(), state); err != nil {
		t.Fatal(err)
	}

	consumed, ok, err := repo.WechatOpenPlatformAuthorizations().ConsumeState(context.Background(), state.StateHash, now)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v, %v)", ok, err)
	}
	if consumed.ProjectID != state.ProjectID || consumed.UserID != state.UserID {
		t.Fatalf("consumed state = %#v", consumed)
	}
	if _, ok, err := repo.WechatOpenPlatformAuthorizations().ConsumeState(context.Background(), state.StateHash, now); err != nil || ok {
		t.Fatalf("replay consume = (%v, %v), want false", ok, err)
	}
}

func TestWechatOpenPlatformAuthorizationStateRejectsExpiredState(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	now := time.Now().UTC()
	state := &model.WechatOpenPlatformAuthorizationState{
		StateHash: "expired-state", ProjectID: uuid.NewString(), UserID: uuid.NewString(), ExpiresAt: now.Add(-time.Second),
	}
	if err := repo.WechatOpenPlatformAuthorizations().CreateState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.WechatOpenPlatformAuthorizations().ConsumeState(context.Background(), state.StateHash, now); err != nil || ok {
		t.Fatalf("expired consume = (%v, %v), want false", ok, err)
	}
}

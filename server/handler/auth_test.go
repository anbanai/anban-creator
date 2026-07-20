package handler

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGenerateTokenPairDoesNotExposeWalletLedger(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := "auth-wallet-ledger"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "auth-billing@example.com",
		Password:   "hashed",
		InviteCode: "authmult",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	jwtSvc, err := auth.NewJWTService("test-secret", "1h", "24h")
	if err != nil {
		t.Fatalf("jwt service: %v", err)
	}
	logger := zerolog.New(io.Discard)
	h := NewAuthHandler(jwtSvc, nil, nil, repo, nil, &logger, nil, false, 0, nil)

	resp, err := h.generateTokenPair(ctx, userID)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(data), "credits_balance") || strings.Contains(string(data), "billing_multiplier") {
		t.Fatalf("token response leaked wallet projection: %s", data)
	}
}

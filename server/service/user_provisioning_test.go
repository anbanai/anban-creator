package service

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestUserProvisioningCreateCreatesUserWalletAndInvite(t *testing.T) {
	db, repo := newUserProvisioningTestRepository(t)
	ctx := context.Background()
	inviter := &model.User{
		ID:         uuid.NewString(),
		Email:      "inviter@example.com",
		Password:   "hashed",
		InviteCode: "INVITER1",
	}
	if err := repo.Users().Create(ctx, inviter); err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	user := newUserProvisioningTestUser("invitee@example.com", "INVITEE1")
	user.InvitedBy = "stale-inviter"
	service := NewUserProvisioningService(repo)
	if err := service.Create(ctx, user, inviter.ID, 3); err != nil {
		t.Fatalf("Create: %v", err)
	}

	createdUser, err := repo.Users().FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("created user missing: %v", err)
	}
	if createdUser.InvitedBy != inviter.ID {
		t.Fatalf("created user InvitedBy = %q, want %q", createdUser.InvitedBy, inviter.ID)
	}
	account, err := repo.Billing().FindAccount(ctx, user.ID)
	if err != nil {
		t.Fatalf("created wallet missing: %v", err)
	}
	if account.UserID != user.ID || account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 || account.Version != 0 {
		t.Fatalf("wallet = %+v, want empty account for user %q", account, user.ID)
	}

	var inviteCount int
	if err := db.Model(&model.User{}).Select("invite_count").Where("id = ?", inviter.ID).Scan(&inviteCount).Error; err != nil {
		t.Fatalf("read inviter count: %v", err)
	}
	if inviteCount != 1 {
		t.Fatalf("inviter count = %d, want 1", inviteCount)
	}
}

func TestUserProvisioningCreateRollsBackUserWhenWalletCreationFails(t *testing.T) {
	db, repo := newUserProvisioningTestRepository(t)
	ctx := context.Background()
	if err := db.Exec(`CREATE TRIGGER reject_user_wallet BEFORE INSERT ON billing_wallet_accounts
		BEGIN SELECT RAISE(ABORT, 'forced wallet creation failure'); END`).Error; err != nil {
		t.Fatalf("create wallet trigger: %v", err)
	}

	user := newUserProvisioningTestUser("wallet-failure@example.com", "WALLET01")
	service := NewUserProvisioningService(repo)
	if err := service.Create(ctx, user, "", 3); err == nil {
		t.Fatal("Create error = nil, want wallet creation failure")
	}

	assertUserProvisioningUserAndWalletMissing(t, repo, user.ID)
}

func TestUserProvisioningCreateRollsBackWhenInviteLimitReached(t *testing.T) {
	_, repo := newUserProvisioningTestRepository(t)
	ctx := context.Background()
	inviter := &model.User{
		ID:          uuid.NewString(),
		Email:       "limited-inviter@example.com",
		Password:    "hashed",
		InviteCode:  "LIMITED1",
		InviteCount: 1,
	}
	if err := repo.Users().Create(ctx, inviter); err != nil {
		t.Fatalf("create inviter: %v", err)
	}

	user := newUserProvisioningTestUser("limited-invitee@example.com", "LIMITEE1")
	service := NewUserProvisioningService(repo)
	err := service.Create(ctx, user, inviter.ID, 1)
	if !errors.Is(err, ErrInviteLimitReached) {
		t.Fatalf("Create error = %v, want ErrInviteLimitReached", err)
	}

	assertUserProvisioningUserAndWalletMissing(t, repo, user.ID)
	foundInviter, err := repo.Users().FindByID(ctx, inviter.ID)
	if err != nil {
		t.Fatalf("find inviter: %v", err)
	}
	if foundInviter.InviteCount != 1 {
		t.Fatalf("inviter count = %d, want 1", foundInviter.InviteCount)
	}
}

func newUserProvisioningTestRepository(t *testing.T) (*gorm.DB, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db, repository.New(db)
}

func newUserProvisioningTestUser(email, inviteCode string) *model.User {
	return &model.User{
		ID:         uuid.NewString(),
		Email:      email,
		Password:   "hashed",
		InviteCode: inviteCode,
	}
}

func assertUserProvisioningUserAndWalletMissing(t *testing.T, repo repository.Repository, userID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.Users().FindByID(ctx, userID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("user lookup error = %v, want record not found", err)
	}
	if _, err := repo.Billing().FindAccount(ctx, userID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("wallet lookup error = %v, want record not found", err)
	}
}

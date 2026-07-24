package service

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrInviteLimitReached = errors.New("invite limit reached")

// UserProvisioningService atomically creates a user and their empty wallet.
type UserProvisioningService struct {
	repo repository.Repository
}

func NewUserProvisioningService(repo repository.Repository) *UserProvisioningService {
	return &UserProvisioningService{repo: repo}
}

func (s *UserProvisioningService) Create(ctx context.Context, user *model.User, inviterID string, maxInvites int) error {
	user.InvitedBy = inviterID
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.Users().Create(ctx, user); err != nil {
			return err
		}
		if err := tx.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: user.ID}); err != nil {
			return err
		}
		if inviterID == "" {
			return nil
		}

		incremented, err := tx.Users().IncrementInviteCount(ctx, inviterID, maxInvites)
		if err != nil {
			return err
		}
		if !incremented {
			return ErrInviteLimitReached
		}
		return nil
	})
}

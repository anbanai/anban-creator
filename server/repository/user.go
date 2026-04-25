package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type userRepository struct {
	db *gorm.DB
}

func newUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByOpenID(ctx context.Context, openID string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("open_id = ?", openID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByInviteCode(ctx context.Context, code string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("invite_code = ?", code).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) Update(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

// IncrementInviteCount atomically increments the invite count for a user,
// but only if the current count is below maxCount. Returns true if the
// increment succeeded, false if the limit was already reached.
func (r *userRepository) IncrementInviteCount(ctx context.Context, userID string, maxCount int) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND invite_count < ?", userID, maxCount).
		Update("invite_count", gorm.Expr("invite_count + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// AdjustBalance atomically adjusts a user's credit balance by delta and returns the new balance.
func (r *userRepository) AdjustBalance(ctx context.Context, userID string, delta int) (int, error) {
	result := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("credits_balance", gorm.Expr("credits_balance + ?", delta))
	if result.Error != nil {
		return 0, result.Error
	}

	var user model.User
	if err := r.db.WithContext(ctx).Select("credits_balance").First(&user, "id = ?", userID).Error; err != nil {
		return 0, err
	}
	return user.CreditsBalance, nil
}

// DeductCredits atomically deducts credits only if the user has sufficient balance.
// Returns the new balance and whether the deduction succeeded.
func (r *userRepository) DeductCredits(ctx context.Context, userID string, amount int) (int, bool, error) {
	result := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND credits_balance >= ?", userID, amount).
		Update("credits_balance", gorm.Expr("credits_balance - ?", amount))
	if result.Error != nil {
		return 0, false, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, false, nil // insufficient balance
	}

	var user model.User
	if err := r.db.WithContext(ctx).Select("credits_balance").First(&user, "id = ?", userID).Error; err != nil {
		return 0, false, err
	}
	return user.CreditsBalance, true, nil
}

package repository

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"
	mysqlDriver "github.com/go-sql-driver/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// IsDuplicateKeyError reports duplicate-key violations across supported databases.
func IsDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}

	var sqliteErr interface{ Code() int }
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == sqliteConstraintPrimaryKey || sqliteErr.Code() == sqliteConstraintUnique
	}
	return false
}

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

func (r *userRepository) LockByID(ctx context.Context, id string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&user).Error; err != nil {
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
		Where("id = ? AND COALESCE(invite_count, 0) < ?", userID, maxCount).
		Update("invite_count", gorm.Expr("COALESCE(invite_count, 0) + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

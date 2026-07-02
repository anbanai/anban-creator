package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// WCFBindingRepository provides access to the wcf_bindings table.
type WCFBindingRepository interface {
	Create(ctx context.Context, binding *model.WCFBinding) error
	FindByID(ctx context.Context, id string) (*model.WCFBinding, error)
	FindByUserID(ctx context.Context, userID string) (*model.WCFBinding, error)
	FindByWCFAccountID(ctx context.Context, accountID string) (*model.WCFBinding, error)
	Update(ctx context.Context, binding *model.WCFBinding) error
	UpdatePeerID(ctx context.Context, userID, peerID string) error
	UpdateStatus(ctx context.Context, userID, status string) error
	UpdateDefaultProject(ctx context.Context, userID, projectID string) error
	TouchLastSeen(ctx context.Context, userID string) error
	Delete(ctx context.Context, userID string) error
	ListActive(ctx context.Context) ([]*model.WCFBinding, error)
}

type wcfBindingRepository struct {
	db *gorm.DB
}

func newWCFBindingRepository(db *gorm.DB) WCFBindingRepository {
	return &wcfBindingRepository{db: db}
}

func (r *wcfBindingRepository) Create(ctx context.Context, binding *model.WCFBinding) error {
	return r.db.WithContext(ctx).Create(binding).Error
}

func (r *wcfBindingRepository) FindByID(ctx context.Context, id string) (*model.WCFBinding, error) {
	var b model.WCFBinding
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *wcfBindingRepository) FindByUserID(ctx context.Context, userID string) (*model.WCFBinding, error) {
	var b model.WCFBinding
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *wcfBindingRepository) FindByWCFAccountID(ctx context.Context, accountID string) (*model.WCFBinding, error) {
	var b model.WCFBinding
	// Resolve only ACTIVE bindings: a stale pending/unbound row must not block a
	// fresh bind or false-positive a conflict, and only active accounts should
	// receive commands.
	//
	// ORDER BY created_at + First makes resolution deterministic. This matters
	// because cross-user account uniqueness is NOT DB-enforced: StartBind only
	// refuses a re-bind for the SAME user, and the cross-user conflict check in
	// PollBindStatus (this lookup then Update) is check-then-write, not atomic;
	// wcf_account_id is intentionally non-unique so pending rows can coexist.
	// In the "bind your own WeChat" model, two users sharing one account (the
	// only way to hit the TOCTOU) is a narrow, recoverable edge, not a
	// stranger-billing path — peer capture guards that. Deterministic
	// earliest-wins keeps poller/notifier resolution stable either way.
	if err := r.db.WithContext(ctx).
		Where("wcf_account_id = ? AND status = ?", accountID, model.WCFBindingStatusActive).
		Order("created_at").
		First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// Update overwrites the ENTIRE row via Save (not a partial column update):
// callers must load the row first and mutate only the fields they intend to
// change, so unrelated columns (e.g. LoginSessionID) aren't clobbered with zero
// values.
func (r *wcfBindingRepository) Update(ctx context.Context, binding *model.WCFBinding) error {
	return r.db.WithContext(ctx).Save(binding).Error
}

func (r *wcfBindingRepository) UpdatePeerID(ctx context.Context, userID, peerID string) error {
	return r.db.WithContext(ctx).Model(&model.WCFBinding{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"peer_id": peerID, "updated_at": time.Now()}).Error
}

func (r *wcfBindingRepository) UpdateStatus(ctx context.Context, userID, status string) error {
	updates := map[string]any{"status": status, "updated_at": time.Now()}
	if status == model.WCFBindingStatusActive {
		now := time.Now()
		updates["bound_at"] = &now
	}
	return r.db.WithContext(ctx).Model(&model.WCFBinding{}).
		Where("user_id = ?", userID).Updates(updates).Error
}

func (r *wcfBindingRepository) UpdateDefaultProject(ctx context.Context, userID, projectID string) error {
	return r.db.WithContext(ctx).Model(&model.WCFBinding{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"default_project_id": projectID, "updated_at": time.Now()}).Error
}

func (r *wcfBindingRepository) TouchLastSeen(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Model(&model.WCFBinding{}).
		Where("user_id = ?", userID).
		Update("last_seen_at", time.Now()).Error
}

func (r *wcfBindingRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).
		Delete(&model.WCFBinding{}).Error
}

func (r *wcfBindingRepository) ListActive(ctx context.Context) ([]*model.WCFBinding, error) {
	var items []*model.WCFBinding
	if err := r.db.WithContext(ctx).
		Where("status = ?", model.WCFBindingStatusActive).
		Order("bound_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

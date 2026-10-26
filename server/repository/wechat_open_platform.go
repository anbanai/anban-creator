package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WechatOpenPlatformAuthorizationRepository interface {
	FindByProjectID(ctx context.Context, projectID string) (*model.WechatOpenPlatformAuthorization, error)
	FindByAuthorizerAppID(ctx context.Context, appID string) ([]*model.WechatOpenPlatformAuthorization, error)
	Upsert(ctx context.Context, authorization *model.WechatOpenPlatformAuthorization) error
	UpdateAuthorizerInfo(ctx context.Context, projectID, authorizerAppID, accountName, avatarURL string, accountType int, grantedScopes []int, expectedAuthorizedAt time.Time, expectedLastEventAt *time.Time, eventAt, updatedAt time.Time) error
	UpdateRefreshToken(ctx context.Context, projectID string, expectedVersion uint64, ciphertext string, updatedAt time.Time) (bool, error)
	RevokeProjectForEvent(ctx context.Context, projectID, appID string, expectedAuthorizedAt time.Time, expectedLastEventAt *time.Time, eventAt, updatedAt time.Time) (bool, error)
	DisconnectProject(ctx context.Context, projectID string, at time.Time) error
	CreateState(ctx context.Context, state *model.WechatOpenPlatformAuthorizationState) error
	ConsumeState(ctx context.Context, stateHash string, now time.Time) (*model.WechatOpenPlatformAuthorizationState, bool, error)
}

type wechatOpenPlatformAuthorizationRepository struct{ db *gorm.DB }

func newWechatOpenPlatformAuthorizationRepository(db *gorm.DB) WechatOpenPlatformAuthorizationRepository {
	return &wechatOpenPlatformAuthorizationRepository{db: db}
}

func (r *wechatOpenPlatformAuthorizationRepository) FindByProjectID(ctx context.Context, projectID string) (*model.WechatOpenPlatformAuthorization, error) {
	var authorization model.WechatOpenPlatformAuthorization
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).First(&authorization).Error
	return &authorization, err
}

func (r *wechatOpenPlatformAuthorizationRepository) FindByAuthorizerAppID(ctx context.Context, appID string) ([]*model.WechatOpenPlatformAuthorization, error) {
	var authorizations []*model.WechatOpenPlatformAuthorization
	err := r.db.WithContext(ctx).Where("authorizer_app_id = ?", appID).Find(&authorizations).Error
	return authorizations, err
}

func (r *wechatOpenPlatformAuthorizationRepository) Upsert(ctx context.Context, authorization *model.WechatOpenPlatformAuthorization) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"user_id", "authorizer_app_id", "account_name", "avatar_url", "account_type",
			"granted_scopes", "status", "refresh_token_ciphertext", "token_version",
			"authorized_at", "revoked_at", "last_event_at", "updated_at",
		}),
	}).Create(authorization).Error
}

func (r *wechatOpenPlatformAuthorizationRepository) UpdateAuthorizerInfo(ctx context.Context, projectID, authorizerAppID, accountName, avatarURL string, accountType int, grantedScopes []int, expectedAuthorizedAt time.Time, expectedLastEventAt *time.Time, eventAt, updatedAt time.Time) error {
	query := r.eventCASQuery(ctx, projectID, authorizerAppID, expectedAuthorizedAt, expectedLastEventAt)
	return query.
		Select("account_name", "avatar_url", "account_type", "granted_scopes", "last_event_at", "updated_at").
		Updates(&model.WechatOpenPlatformAuthorization{
			AccountName: accountName, AvatarURL: avatarURL, AccountType: accountType,
			GrantedScopes: grantedScopes, LastEventAt: &eventAt, UpdatedAt: updatedAt,
		}).Error
}

func (r *wechatOpenPlatformAuthorizationRepository) UpdateRefreshToken(ctx context.Context, projectID string, expectedVersion uint64, ciphertext string, updatedAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.WechatOpenPlatformAuthorization{}).
		Where("project_id = ? AND status = ? AND token_version = ?", projectID, model.WechatAuthorizationStatusConnected, expectedVersion).
		Updates(map[string]any{"refresh_token_ciphertext": ciphertext, "token_version": gorm.Expr("token_version + 1"), "updated_at": updatedAt})
	return result.RowsAffected == 1, result.Error
}

func (r *wechatOpenPlatformAuthorizationRepository) RevokeProjectForEvent(ctx context.Context, projectID, appID string, expectedAuthorizedAt time.Time, expectedLastEventAt *time.Time, eventAt, updatedAt time.Time) (bool, error) {
	result := r.eventCASQuery(ctx, projectID, appID, expectedAuthorizedAt, expectedLastEventAt).
		Updates(map[string]any{
			"status":                   model.WechatAuthorizationStatusRevoked,
			"refresh_token_ciphertext": "",
			"token_version":            gorm.Expr("token_version + 1"),
			"revoked_at":               updatedAt,
			"last_event_at":            eventAt,
			"updated_at":               updatedAt,
		})
	return result.RowsAffected > 0, result.Error
}

func (r *wechatOpenPlatformAuthorizationRepository) eventCASQuery(ctx context.Context, projectID, appID string, authorizedAt time.Time, lastEventAt *time.Time) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&model.WechatOpenPlatformAuthorization{}).
		Where("project_id = ? AND authorizer_app_id = ? AND status = ? AND authorized_at = ?", projectID, appID, model.WechatAuthorizationStatusConnected, authorizedAt)
	if lastEventAt == nil {
		return query.Where("last_event_at IS NULL")
	}
	return query.Where("last_event_at = ?", *lastEventAt)
}

func (r *wechatOpenPlatformAuthorizationRepository) DisconnectProject(ctx context.Context, projectID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.WechatOpenPlatformAuthorization{}).
		Where("project_id = ?", projectID).
		Updates(map[string]any{
			"status":                   model.WechatAuthorizationStatusDisconnected,
			"refresh_token_ciphertext": "",
			"token_version":            gorm.Expr("token_version + 1"),
			"updated_at":               at,
		}).Error
}

func (r *wechatOpenPlatformAuthorizationRepository) CreateState(ctx context.Context, state *model.WechatOpenPlatformAuthorizationState) error {
	return r.db.WithContext(ctx).Create(state).Error
}

func (r *wechatOpenPlatformAuthorizationRepository) ConsumeState(ctx context.Context, stateHash string, now time.Time) (*model.WechatOpenPlatformAuthorizationState, bool, error) {
	var state model.WechatOpenPlatformAuthorizationState
	consumed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.WechatOpenPlatformAuthorizationState{}).
			Where("state_hash = ? AND consumed_at IS NULL AND expires_at > ?", stateHash, now).
			Update("consumed_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		consumed = true
		return tx.Where("state_hash = ?", stateHash).First(&state).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &state, consumed, nil
}

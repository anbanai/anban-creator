package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type sessionRepository struct {
	db *gorm.DB
}

func newSessionRepository(db *gorm.DB) SessionRepository {
	return &sessionRepository{db: db}
}

func (r *sessionRepository) Create(ctx context.Context, session *model.LoginSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *sessionRepository) FindByToken(ctx context.Context, token string) (*model.LoginSession, error) {
	var session model.LoginSession
	if err := r.db.WithContext(ctx).Where("token = ?", token).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *sessionRepository) FindByRefreshToken(ctx context.Context, token string) (*model.LoginSession, error) {
	var session model.LoginSession
	if err := r.db.WithContext(ctx).Where("refresh_token = ?", token).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *sessionRepository) Update(ctx context.Context, session *model.LoginSession) error {
	return r.db.WithContext(ctx).Save(session).Error
}

func (r *sessionRepository) Delete(ctx context.Context, token string) error {
	return r.db.WithContext(ctx).Where("token = ?", token).Delete(&model.LoginSession{}).Error
}

func (r *sessionRepository) DeleteExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&model.LoginSession{}).Error
}

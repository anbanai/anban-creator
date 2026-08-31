package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type wechatPublicationRepository struct{ db *gorm.DB }

func newWechatPublicationRepository(db *gorm.DB) WechatPublicationRepository {
	return &wechatPublicationRepository{db: db}
}

func (r *wechatPublicationRepository) Create(ctx context.Context, publication *model.WechatPublication) error {
	return r.db.WithContext(ctx).Create(publication).Error
}

func (r *wechatPublicationRepository) FindByID(ctx context.Context, id string) (*model.WechatPublication, error) {
	var publication model.WechatPublication
	err := retryWechatSQLiteBusy(ctx, r.db.Dialector.Name(), func() error {
		return r.db.WithContext(ctx).Where("id = ?", id).First(&publication).Error
	})
	return &publication, err
}

func (r *wechatPublicationRepository) FindByTaskID(ctx context.Context, taskID string) (*model.WechatPublication, error) {
	var publication model.WechatPublication
	err := retryWechatSQLiteBusy(ctx, r.db.Dialector.Name(), func() error {
		return r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&publication).Error
	})
	return &publication, err
}

func (r *wechatPublicationRepository) FindByArticleID(ctx context.Context, projectID, articleID string) (*model.WechatPublication, error) {
	var binding model.WechatPublicationBinding
	if err := retryWechatSQLiteBusy(ctx, r.db.Dialector.Name(), func() error {
		return r.db.WithContext(ctx).Where("project_id = ? AND article_id = ?", projectID, articleID).First(&binding).Error
	}); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, binding.PublicationID)
}

func (r *wechatPublicationRepository) FindPendingByProject(ctx context.Context, projectID string) ([]*model.WechatPublication, error) {
	var publications []*model.WechatPublication
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Where("status IN ?", []string{
			model.WechatPublicationStatusDrafting,
			model.WechatPublicationStatusDrafted,
			model.WechatPublicationStatusPublishSubmitting,
			model.WechatPublicationStatusNeedsSelection,
		}).
		Order("created_at ASC").Find(&publications).Error
	return publications, err
}

func (r *wechatPublicationRepository) ClaimProjectReconcile(ctx context.Context, projectID string, lease time.Duration) (bool, error) {
	if projectID == "" || lease <= 0 {
		return false, fmt.Errorf("project reconcile lease requires project_id and positive duration")
	}
	initial := &model.WechatProjectReconcileLease{ProjectID: projectID, LeaseUntilMicros: 1}
	if _, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(initial)
	}); err != nil {
		return false, err
	}
	query := r.db.WithContext(ctx).Model(&model.WechatProjectReconcileLease{}).Where("project_id = ?", projectID)
	var until clause.Expr
	switch r.db.Dialector.Name() {
	case "mysql":
		clock := "CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(6)) * 1000000 AS SIGNED)"
		query = query.Where("lease_until_micros <= " + clock)
		until = clause.Expr{SQL: clock + " + ?", Vars: []any{lease.Microseconds()}}
	case "sqlite":
		clock := "CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER)"
		query = query.Where("lease_until_micros <= " + clock)
		until = clause.Expr{SQL: clock + " + ?", Vars: []any{lease.Microseconds()}}
	default:
		return false, fmt.Errorf("project reconcile leases are unsupported for database dialect %q", r.db.Dialector.Name())
	}
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return query.Update("lease_until_micros", until)
	})
	return rows == 1, err
}

func (r *wechatPublicationRepository) ClaimArticleBinding(ctx context.Context, projectID, articleID, publicationID string) (bool, error) {
	if projectID == "" || articleID == "" || publicationID == "" {
		return false, fmt.Errorf("article binding requires project_id, article_id, and publication_id")
	}
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.WechatPublicationBinding{
			ProjectID: projectID, ArticleID: articleID, PublicationID: publicationID,
		})
	})
	return rows == 1, err
}

func (r *wechatPublicationRepository) TransitionToNeedsSelection(ctx context.Context, id, expectedStatus string, expectedUpdatedAt time.Time, source string, candidates []byte, nextCheckAt *time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatPublication{}).
			Where("id = ? AND status = ? AND updated_at = ?", id, expectedStatus, expectedUpdatedAt).
			Updates(map[string]any{
				"status": model.WechatPublicationStatusNeedsSelection, "source": source,
				"candidates": candidates, "next_check_at": nextCheckAt,
			})
	})
	return rows == 1, err
}

func (r *wechatPublicationRepository) TransitionToPublishSubmitting(ctx context.Context, id, expectedStatus string, expectedUpdatedAt time.Time, lastError string, nextCheckAt *time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatPublication{}).
			Where("id = ? AND status = ? AND updated_at = ?", id, expectedStatus, expectedUpdatedAt).
			Updates(map[string]any{
				"status":     model.WechatPublicationStatusPublishSubmitting,
				"last_error": lastError, "next_check_at": nextCheckAt,
			})
	})
	return rows == 1, err
}

func (r *wechatPublicationRepository) UpdateReconciliation(ctx context.Context, publication *model.WechatPublication, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatPublication{}).
			Where("id = ? AND status = ? AND updated_at = ?", publication.ID, expectedStatus, expectedUpdatedAt).
			Updates(map[string]any{
				"status":             publication.Status,
				"source":             publication.Source,
				"wechat_status_code": publication.WechatStatusCode,
				"next_check_at":      publication.NextCheckAt,
				"last_checked_at":    publication.LastCheckedAt,
				"check_attempts":     publication.CheckAttempts,
				"last_error":         publication.LastError,
				"candidates":         publication.Candidates,
			})
	})
	return rows == 1, err
}

func (r *wechatPublicationRepository) TransitionToPublished(ctx context.Context, publication *model.WechatPublication, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatPublication{}).
			Where("id = ? AND status = ? AND updated_at = ?", publication.ID, expectedStatus, expectedUpdatedAt).
			Updates(map[string]any{
				"status":             publication.Status,
				"source":             publication.Source,
				"msg_id":             publication.MsgID,
				"article_id":         publication.ArticleID,
				"article_url":        publication.ArticleURL,
				"article_index":      publication.ArticleIndex,
				"wechat_status_code": publication.WechatStatusCode,
				"published_at":       publication.PublishedAt,
				"next_check_at":      publication.NextCheckAt,
				"last_checked_at":    publication.LastCheckedAt,
				"check_attempts":     publication.CheckAttempts,
				"last_error":         publication.LastError,
				"candidates":         publication.Candidates,
			})
	})
	return rows == 1, err
}

func runWechatClaimWrite(ctx context.Context, dialect string, operation func() *gorm.DB) (int64, error) {
	var rows int64
	err := retryWechatSQLiteBusy(ctx, dialect, func() error {
		result := operation()
		rows = result.RowsAffected
		return result.Error
	})
	return rows, err
}

func retryWechatSQLiteBusy(ctx context.Context, dialect string, operation func() error) error {
	for attempt := 0; ; attempt++ {
		err := operation()
		if err == nil || dialect != "sqlite" || !isSQLiteBusyError(err) || attempt == 5 {
			return err
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isSQLiteBusyError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func (r *wechatPublicationRepository) Update(ctx context.Context, publication *model.WechatPublication) error {
	return retryWechatSQLiteBusy(ctx, r.db.Dialector.Name(), func() error {
		return r.db.WithContext(ctx).Save(publication).Error
	})
}

func (r *wechatPublicationRepository) BindMsgID(ctx context.Context, id, msgID string) (bool, error) {
	rows, err := runWechatClaimWrite(ctx, r.db.Dialector.Name(), func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.WechatPublication{}).
			Where("id = ? AND (msg_id = '' OR msg_id = ?)", id, msgID).
			Update("msg_id", msgID)
	})
	if err != nil || rows == 1 {
		return rows == 1, err
	}
	publication, err := r.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	return publication.MsgID == msgID, nil
}

func (r *wechatPublicationRepository) ClaimPublish(ctx context.Context, id, token string, now, staleBefore time.Time, nextCheckAt *time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.WechatPublication{}).
		Where("id = ? AND status = ? AND draft_media_id <> ''", id, model.WechatPublicationStatusDrafted).
		Where("claim_token = '' OR claimed_at IS NULL OR claimed_at < ?", staleBefore).
		Updates(map[string]any{
			"status":              model.WechatPublicationStatusPublishSubmitting,
			"claim_token":         token,
			"claimed_at":          &now,
			"submit_attempted_at": &now,
			"next_check_at":       nextCheckAt,
			"last_error":          "",
		})
	return result.RowsAffected == 1, result.Error
}

func (r *wechatPublicationRepository) UpdateClaimed(ctx context.Context, publication *model.WechatPublication, token string) (bool, error) {
	updates := map[string]any{
		"status": publication.Status, "publish_id": publication.PublishID, "msg_data_id": publication.MsgDataID,
		"wechat_status_code": publication.WechatStatusCode, "next_check_at": publication.NextCheckAt,
		"last_error": publication.LastError, "claim_token": "", "claimed_at": nil,
	}
	result := r.db.WithContext(ctx).Model(&model.WechatPublication{}).
		Where("id = ? AND claim_token = ? AND status = ?", publication.ID, token, model.WechatPublicationStatusPublishSubmitting).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

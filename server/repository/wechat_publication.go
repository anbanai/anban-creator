package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
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
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&publication).Error
	return &publication, err
}

func (r *wechatPublicationRepository) FindByTaskID(ctx context.Context, taskID string) (*model.WechatPublication, error) {
	var publication model.WechatPublication
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&publication).Error
	return &publication, err
}

func (r *wechatPublicationRepository) FindByArticleID(ctx context.Context, articleID string) (*model.WechatPublication, error) {
	var publication model.WechatPublication
	err := r.db.WithContext(ctx).Where("article_id = ?", articleID).First(&publication).Error
	return &publication, err
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

func (r *wechatPublicationRepository) ClaimProjectReconcile(ctx context.Context, projectID string, now, staleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`
UPDATE wechat_publications
SET last_checked_at = ?, updated_at = ?
WHERE id = (
	SELECT id FROM (
		SELECT id FROM wechat_publications
		WHERE project_id = ? AND status IN (?, ?, ?, ?)
		ORDER BY created_at ASC
		LIMIT 1
	) AS reconcile_target
)
AND NOT EXISTS (
	SELECT 1 FROM (
		SELECT last_checked_at FROM wechat_publications
		WHERE project_id = ? AND last_checked_at IS NOT NULL
	) AS recent_checks
	WHERE recent_checks.last_checked_at > ?
)`, now, now, projectID,
		model.WechatPublicationStatusDrafting,
		model.WechatPublicationStatusDrafted,
		model.WechatPublicationStatusPublishSubmitting,
		model.WechatPublicationStatusNeedsSelection,
		projectID, staleBefore,
	)
	return result.RowsAffected == 1, result.Error
}

func (r *wechatPublicationRepository) Update(ctx context.Context, publication *model.WechatPublication) error {
	return r.db.WithContext(ctx).Save(publication).Error
}

func (r *wechatPublicationRepository) ClaimPublish(ctx context.Context, id, token string, now, staleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.WechatPublication{}).
		Where("id = ? AND status = ? AND draft_media_id <> ''", id, model.WechatPublicationStatusDrafted).
		Where("claim_token = '' OR claimed_at IS NULL OR claimed_at < ?", staleBefore).
		Updates(map[string]any{
			"status":      model.WechatPublicationStatusPublishSubmitting,
			"claim_token": token,
			"claimed_at":  &now,
			"last_error":  "",
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

package service

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

var ErrAnalyticsImportRevoked = errors.New("该导入批次已撤销，请重新导入正确文件")

func (s *SeednoteImportService) Revoke(ctx context.Context, userID, projectID, batchID string) (*SeednoteImportSummary, error) {
	if err := s.checkProject(ctx, userID, projectID); err != nil {
		return nil, err
	}
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.SeednoteImports().LockBatch(ctx, projectID, batchID); err != nil {
			return err
		}
		batch, err := tx.SeednoteImports().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if batch.UserID != userID {
			return errors.New("batch does not belong to user")
		}
		if batch.RevokedAt != nil {
			return nil
		}
		now := s.now()
		batch.Status, batch.RevokedAt = "revoked", &now
		return tx.SeednoteImports().UpdateBatch(ctx, batch)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBatch(ctx, userID, projectID, batchID)
}

func (s *WechatAnalyticsImportService) Revoke(ctx context.Context, userID, projectID, batchID string) (*WechatAnalyticsImportSummary, error) {
	// Check ownership before taking a write lock. Recheck inside the transaction.
	batch, err := s.repo.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.UserID != userID {
		return nil, errors.New("import does not belong to user")
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.WechatAnalyticsImports().LockProject(ctx, projectID); err != nil {
			return err
		}
		if err := tx.WechatAnalyticsImports().LockBatch(ctx, projectID, batchID); err != nil {
			return err
		}
		batch, err := tx.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if batch.UserID != userID {
			return errors.New("import does not belong to user")
		}
		if batch.RevokedAt != nil {
			return nil
		}
		now := s.now()
		batch.Status, batch.RevokedAt = "revoked", &now
		if err := tx.WechatAnalyticsImports().UpdateBatch(ctx, batch); err != nil {
			return err
		}
		// Scan the project's publications, including links sourced by this batch
		// whose rows were subsequently relinked. Only import-owned URLs can change.
		publications, err := tx.WechatPublications().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		for _, publication := range publications {
			snapshots, err := tx.WechatAnalyticsImports().FindSnapshotsByPublicationID(ctx, projectID, publication.ID)
			if err != nil {
				return err
			}
			url := historicalWechatArticleURL(snapshots)
			sourceBatchID := ""
			if url != "" && len(snapshots) > 0 {
				// Choose an active observation that actually supplies this URL.
				for _, snapshot := range snapshots {
					if historicalWechatArticleURL([]*model.WechatAnalyticsSnapshot{snapshot}) == url {
						sourceBatchID = snapshot.BatchID
						break
					}
				}
			}
			status := "import_available"
			if len(snapshots) == 0 {
				status, err = wechatAnalyticsStatusWithoutImports(ctx, tx, publication)
				if err != nil {
					return err
				}
			}
			if err := tx.WechatPublications().RevokeImportedAnalytics(ctx, projectID, publication.ID, batchID, url, sourceBatchID, status); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetBatch(ctx, userID, projectID, batchID)
}

func wechatAnalyticsStatusWithoutImports(ctx context.Context, repo repository.Repository, publication *model.WechatPublication) (string, error) {
	metrics, err := repo.WechatMetricSnapshots().FindByTaskID(ctx, publication.TaskID)
	if err != nil {
		return "", err
	}
	if len(metrics) > 0 {
		return "official_available", nil
	}
	tracking, err := repo.WechatTrackings().FindByTaskID(ctx, publication.TaskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "not_available", nil
	}
	if err != nil {
		return "", err
	}
	switch tracking.Status {
	case model.WechatTrackingStatusUnsupported:
		return "unsupported", nil
	case model.WechatTrackingStatusError:
		return "partial", nil
	case model.WechatTrackingStatusExpired:
		return "not_available", nil
	default:
		return "official_fetching", nil
	}
}

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
	var revision int64
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		batch, err := tx.SeednoteImports().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if batch.UserID != userID {
			return errors.New("batch does not belong to user")
		}
		revision, err = NewAnalyticsService(tx).RevokeBatch(ctx, projectID, batchID)
		if err != nil {
			return err
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
	result, err := s.GetBatch(ctx, userID, projectID, batchID)
	if result != nil {
		result.Revision = revision
	}
	return result, err
}
func (s *WechatAnalyticsImportService) Revoke(ctx context.Context, userID, projectID, batchID string) (*WechatAnalyticsImportSummary, error) {
	var revision int64
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		batch, err := tx.WechatAnalyticsImports().FindBatchByID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if batch.UserID != userID {
			return errors.New("import does not belong to user")
		}
		revision, err = NewAnalyticsService(tx).RevokeBatch(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		if batch.RevokedAt != nil {
			return nil
		}
		now := s.now()
		batch.Status, batch.RevokedAt = "revoked", &now
		if err := tx.WechatAnalyticsImports().UpdateBatch(ctx, batch); err != nil {
			return err
		}
		// Reconcile the operational URL/status from the remaining valid import
		// observations. The analytics projection is already recomputed above;
		// this keeps publication lifecycle metadata consistent for existing task
		// and publication views without making it part of the read path.
		rows, err := tx.WechatAnalyticsImports().FindRowsByBatchID(ctx, projectID, batchID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		// A URL can be linked by RecordImportedAnalytics before a row is
		// materialized (for example a manually selected publication). Include
		// those durable ownership links in the same reconciliation pass.
		publications, err := tx.WechatPublications().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		for _, publication := range publications {
			if publication.ArticleURLImportBatchID == batchID {
				rows = append(rows, &model.WechatAnalyticsImportRow{PublicationID: publication.ID})
			}
		}
		for _, row := range rows {
			if row.PublicationID == "" || seen[row.PublicationID] {
				continue
			}
			seen[row.PublicationID] = true
			snapshots, err := tx.WechatAnalyticsImports().FindSnapshotsByPublicationID(ctx, projectID, row.PublicationID)
			if err != nil {
				return err
			}
			replacementURL := historicalWechatArticleURL(snapshots)
			replacementBatchID := ""
			if len(snapshots) > 0 {
				replacementBatchID = snapshots[0].BatchID
			}
			publication, err := tx.WechatPublications().FindByID(ctx, row.PublicationID)
			if err != nil {
				return err
			}
			status, err := wechatAnalyticsStatusWithoutImports(ctx, tx, publication)
			if err != nil {
				return err
			}
			if err := tx.WechatPublications().RevokeImportedAnalytics(ctx, projectID, row.PublicationID, batchID, replacementURL, replacementBatchID, status); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result, err := s.GetBatch(ctx, userID, projectID, batchID)
	if result != nil {
		result.Revision = revision
	}
	return result, err
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

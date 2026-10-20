package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// A draft-success task outcome is not evidence of formal publication.
func (s *ContentAssetService) reconcileReusePublication(ctx context.Context, run *model.ContentReuseRun, task *model.Task) (bool, error) {
	run.Stage = model.ContentReuseStagePublish
	if run.TargetPlatform != model.PlatformWechat {
		run.Status = model.ContentReuseRunStatusPaused
		run.ErrorCode, run.ErrorMessage = "publication_capability_unavailable", "等待平台发布能力"
		return false, nil
	}
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		run.ErrorCode, run.ErrorMessage = "publication_pending", "等待 Server 创建发布记录"
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if publication.UserID != run.UserID || publication.ProjectID != run.TargetProjectID {
		return false, fmt.Errorf("publication does not belong to reuse run")
	}
	if publication.Status != model.WechatPublicationStatusPublished {
		run.ErrorCode, run.ErrorMessage = "publication_pending", "等待 Server 发布确认与对账"
		if publication.Status == model.WechatPublicationStatusPublishFailed || publication.Status == model.WechatPublicationStatusUnsupported || publication.Status == model.WechatPublicationStatusAwaitingManual || publication.Status == model.WechatPublicationStatusSkipped {
			run.Status = model.ContentReuseRunStatusPaused
			run.ErrorCode, run.ErrorMessage = "publication_not_ready", "发布尚未通过 Server readiness"
		}
		return false, nil
	}
	_, err = s.LinkContent(ctx, LinkContentAssetParams{UserID: run.UserID, AssetID: run.AssetID, ProjectID: run.TargetProjectID, ContentKind: "wechat_publication", ContentID: publication.ID, Platform: run.TargetPlatform, Relation: model.ContentAssetLinkDerivative})
	return err == nil, err
}

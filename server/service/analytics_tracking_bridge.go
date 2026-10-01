package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func analyticsCaptureID(source, tracking, day string, at time.Time) string {
	sum := sha256.Sum256([]byte(source + "|" + tracking + "|" + day + "|" + at.UTC().Format(time.RFC3339Nano)))
	return source + ":" + hex.EncodeToString(sum[:])
}
func analyticsInt(n int) *int64 { v := int64(n); return &v }
func analyticsIntValue(n *int64) int {
	if n == nil {
		return 0
	}
	return int(*n)
}
func analyticsFloatValue(n *model.AnalyticsDecimal) float64 {
	if n == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(string(*n), 64)
	return v
}
func analyticsWechatCaptureInput(t *model.WechatArticleTracking, p *model.WechatPublication, s *model.WechatMetricSnapshot) (AnalyticsObservationInput, error) {
	date, err := time.ParseInLocation("2006-01-02", s.StatDate, seednoteAnalyticsLocation)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	metrics := model.AnalyticsMetrics{ReadUsers: analyticsInt(s.ReadUsers), ShareUsers: analyticsInt(s.ShareUsers), CollectionUsers: analyticsInt(s.CollectionUsers), LikeUsers: analyticsInt(s.LikeUsers), ZaikanUsers: analyticsInt(s.ZaikanUsers), CommentCount: analyticsInt(s.CommentCount), ReadToFollowUsers: analyticsInt(s.ReadToSubscribeUsers)}
	metrics.ReadCompletionRate, err = analyticsDecimal("", &s.ReadFinishRate, true)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	metrics.AverageReadActiveTime, err = analyticsDecimal("", &s.AverageReadActiveTime, false)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	contentType := normalizeWechatContentType(p.DraftArticleType)
	content := model.AnalyticsContent{ID: "task:" + t.TaskID, TaskID: t.TaskID, PublicationID: t.PublicationID, ProjectID: t.ProjectID, Channel: model.ChannelArticle, Platform: model.PlatformWechat, Title: p.DraftTitle, ContentType: contentType, Status: p.Status, URL: p.ArticleURL, Date: p.PublishedAt}
	return AnalyticsObservationInput{Content: content, Observation: model.AnalyticsObservation{ID: analyticsCaptureID("wechat_api", t.ID, s.StatDate, s.CapturedAt), TrackingID: t.ID, ProjectID: t.ProjectID, ContentID: content.ID, MetricBasis: "cumulative", StatDate: s.StatDate, Source: "wechat_api", SourcePriority: 200, EffectiveAt: date, ReceivedAt: s.CapturedAt, AnalyticsMetrics: metrics}, RawPayload: string(s.RawResponse)}, nil
}
func analyticsWechatTrackingSnapshots(ctx context.Context, repo repository.Repository, taskID string) ([]*model.WechatMetricSnapshot, error) {
	rows, err := repo.Analytics().TrackingObservations(ctx, taskID, "wechat_api")
	if err != nil {
		return nil, err
	}
	result := make([]*model.WechatMetricSnapshot, 0, len(rows))
	for _, r := range rows {
		result = append(result, &model.WechatMetricSnapshot{ID: r.ID, TrackingID: r.TrackingID, TaskID: taskID, StatDate: r.StatDate, CapturedAt: r.ReceivedAt, ReadUsers: analyticsIntValue(r.ReadUsers), ShareUsers: analyticsIntValue(r.ShareUsers), CollectionUsers: analyticsIntValue(r.CollectionUsers), LikeUsers: analyticsIntValue(r.LikeUsers), ZaikanUsers: analyticsIntValue(r.ZaikanUsers), CommentCount: analyticsIntValue(r.CommentCount), ReadToSubscribeUsers: analyticsIntValue(r.ReadToFollowUsers), ReadFinishRate: analyticsFloatValue(r.ReadCompletionRate), AverageReadActiveTime: analyticsFloatValue(r.AverageReadActiveTime)})
	}
	return result, nil
}
func analyticsSeednoteTrackingSnapshots(ctx context.Context, repo repository.Repository, taskID string) ([]*model.SeednoteMetricSnapshot, error) {
	// Operational snapshots are retained for lifecycle calculations and make
	// same-day idempotence independent of the projection generation.
	legacy, err := repo.SeednoteMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	tracking, trackingErr := repo.SeednoteTrackings().FindByTaskID(ctx, taskID)
	if trackingErr == nil && tracking.TrackingStartedAt != nil {
		filtered := legacy[:0]
		for _, item := range legacy {
			if item.CapturedAt.Before(*tracking.TrackingStartedAt) {
				continue
			}
			filtered = append(filtered, item)
		}
		legacy = filtered
	}
	if len(legacy) > 0 {
		return legacy, nil
	}
	rows, err := repo.Analytics().TrackingObservations(ctx, taskID, "seednote_public")
	if err != nil {
		return nil, err
	}
	if trackingErr == nil && tracking.TrackingStartedAt != nil {
		filtered := rows[:0]
		for _, row := range rows {
			if row.ReceivedAt.Before(*tracking.TrackingStartedAt) {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
	}
	result := make([]*model.SeednoteMetricSnapshot, 0, len(rows))
	for _, r := range rows {
		item := &model.SeednoteMetricSnapshot{ID: r.ID, TrackingID: r.TrackingID, TaskID: taskID, CapturedDate: r.StatDate, CapturedAt: r.ReceivedAt, LikeCount: analyticsIntValue(r.LikeCount), CollectCount: analyticsIntValue(r.CollectCount), CommentCount: analyticsIntValue(r.CommentCount), ShareCount: analyticsIntValue(r.ShareCount)}
		if r.ViewCount != nil {
			v := int(*r.ViewCount)
			item.ViewCount = &v
		}
		result = append(result, item)
	}
	return result, nil
}

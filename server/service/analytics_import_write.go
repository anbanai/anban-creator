package service

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func analyticsDecimal(raw string, fallback *float64, rate bool) (*model.AnalyticsDecimal, error) {
	if fallback == nil {
		return nil, nil
	}
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if raw == "" {
		raw = strconv.FormatFloat(*fallback, 'f', -1, 64)
	}
	percent := strings.HasSuffix(raw, "%")
	raw = strings.TrimSuffix(raw, "%")
	n, ok := new(big.Rat).SetString(raw)
	if !ok || n.Sign() < 0 {
		return nil, fmt.Errorf("invalid analytics decimal")
	}
	if rate && (percent || n.Cmp(big.NewRat(1, 1)) > 0) {
		n.Quo(n, big.NewRat(100, 1))
	}
	value := strings.TrimRight(strings.TrimRight(n.FloatString(18), "0"), ".")
	parsed, _ := new(big.Rat).SetString(value)
	if parsed == nil || parsed.Cmp(n) != 0 {
		return nil, fmt.Errorf("analytics decimal exceeds 18 decimal places")
	}
	result := model.AnalyticsDecimal(value)
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}
func analyticsWechatInput(candidate AnalyticsCandidate, row *model.WechatAnalyticsImportRow, batch *model.WechatAnalyticsImportBatch, now time.Time) (AnalyticsObservationInput, error) {
	content := model.AnalyticsContent{ProjectID: batch.ProjectID, Channel: model.ChannelArticle, Platform: model.PlatformWechat, Title: candidate.Title, ContentType: candidate.ContentType, Status: candidate.Status, URL: candidate.URL, Date: candidate.Date}
	if candidate.Task != nil {
		content.ID = "task:" + candidate.Task.ID
		content.TaskID = candidate.Task.ID
	}
	if candidate.Publication != nil {
		content.PublicationID = candidate.Publication.ID
		if content.ID == "" {
			content.ID = "wechat_publication:" + candidate.Publication.ID
		}
	}
	metrics := model.AnalyticsMetrics{ReadUsers: row.ReadUsers, ShareUsers: row.ShareUsers, ReadToFollowUsers: row.ReadToFollowUsers, DeliveredUsers: row.DeliveredUsers}
	var raw map[string]string
	_ = json.Unmarshal([]byte(row.RawData), &raw)
	var err error
	metrics.ReadCompletionRate, err = analyticsDecimal(raw["阅读完成率"], row.ReadCompletionRate, true)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	metrics.DeliveryCompletionRate, err = analyticsDecimal(raw["送达完成率"], row.DeliveryCompletionRate, true)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	return AnalyticsObservationInput{Content: content, Observation: model.AnalyticsObservation{ID: "wechat_import:" + row.ID, ProjectID: batch.ProjectID, ContentID: content.ID, BatchID: batch.ID, MetricBasis: "cumulative", StatDate: seednoteAnalyticsDate(batch.DataAsOfAt), Source: "wechat_import", SourcePriority: 100, EffectiveAt: batch.DataAsOfAt, ReceivedAt: now, AnalyticsMetrics: metrics}, RawPayload: row.RawData}, nil
}
func analyticsSeednoteInput(post *model.SeednotePost, row *model.SeednoteImportRow, batch *model.SeednoteImportBatch, basis string, now time.Time) (AnalyticsObservationInput, error) {
	content := model.AnalyticsContent{ID: "seednote_post:" + post.ID, PostID: post.ID, TaskID: post.TaskID, ProjectID: batch.ProjectID, Channel: model.ChannelSeednote, Platform: model.PlatformSeednote, Title: post.Title, ContentType: analyticsSeednoteType(post.Genre), URL: post.NoteURL, Date: post.FirstPublishedAt}
	metrics := model.AnalyticsMetrics{ExposureCount: row.ExposureCount, ViewCount: row.ViewCount, LikeCount: row.LikeCount, CommentCount: row.CommentCount, CollectCount: row.CollectCount, FollowerGainCount: row.FollowerGainCount, ShareCount: row.ShareCount, BarrageCount: row.BarrageCount}
	var raw map[string]string
	_ = json.Unmarshal([]byte(row.RawData), &raw)
	var err error
	metrics.CoverClickRate, err = analyticsDecimal(raw["封面点击率"], row.CoverClickRate, true)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	metrics.AvgWatchDuration, err = analyticsDecimal(raw["人均观看时长"], row.AvgWatchDuration, false)
	if err != nil {
		return AnalyticsObservationInput{}, err
	}
	return AnalyticsObservationInput{Content: content, Observation: model.AnalyticsObservation{ID: "seednote_import:" + row.ID, ProjectID: batch.ProjectID, ContentID: content.ID, BatchID: batch.ID, MetricBasis: basis, StatDate: seednoteAnalyticsDate(batch.DataAsOfAt), Source: "seednote_import", SourcePriority: 200, EffectiveAt: batch.DataAsOfAt, ReceivedAt: now, AnalyticsMetrics: metrics}, RawPayload: row.RawData}, nil
}

func analyticsSeednoteType(genre string) string {
	switch genre {
	case "video", "视频":
		return "video"
	case "image", "image_text", "图文", "图文笔记":
		return "image_text"
	default:
		return "unknown"
	}
}

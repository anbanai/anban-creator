package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const WechatCaptureMetricsTaskType = "wechat:capture_metrics"

var wechatAnalyticsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

var errWechatPublicationMsgIDConflict = errors.New("WeChat publication msgid conflicts with analytics response")

type WechatAnalyticsPlatform interface {
	FetchArticleTotalDetail(ctx context.Context, project *model.Project, publicationDate string) (*appwechat.ArticleTotalDetailResponse, error)
}

type WechatTrackingService struct {
	repo     repository.Repository
	platform WechatAnalyticsPlatform
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
	now      func() time.Time
}

func NewWechatTrackingService(repo repository.Repository, provider WechatAnalyticsPlatform, enqueuer TaskEnqueuer, logger *zerolog.Logger) *WechatTrackingService {
	return &WechatTrackingService{repo: repo, platform: provider, enqueuer: enqueuer, logger: logger, now: time.Now}
}

type WechatAnalytics struct {
	Tracking *WechatTrackingInfo      `json:"tracking,omitempty"`
	Metrics  *WechatMetricInfo        `json:"metrics,omitempty"`
	Trend    []*WechatMetricTrendItem `json:"trend"`
}

type WechatTrackingInfo struct {
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	ArticleURL   string     `json:"article_url,omitempty"`
	PublishedAt  time.Time  `json:"published_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	LastFetchAt  *time.Time `json:"last_fetch_at,omitempty"`
	NextFetchAt  *time.Time `json:"next_fetch_at,omitempty"`
	RunCount     int        `json:"run_count"`
	FailureCount int        `json:"failure_count"`
	LastError    string     `json:"last_error,omitempty"`
}

type WechatMetricInfo struct {
	StatDate              string    `json:"stat_date"`
	CapturedAt            time.Time `json:"captured_at"`
	ReadUsers             int       `json:"read_users"`
	ShareUsers            int       `json:"share_users"`
	CollectionUsers       int       `json:"collection_users"`
	LikeUsers             int       `json:"like_users"`
	ZaikanUsers           int       `json:"zaikan_users"`
	CommentCount          int       `json:"comment_count"`
	ReadFinishRate        float64   `json:"read_finish_rate"`
	AverageReadActiveTime float64   `json:"average_read_active_time"`
	ReadToSubscribeUsers  int       `json:"read_to_subscribe_users"`
}

type WechatMetricTrendItem WechatMetricInfo

func (s *WechatTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.WechatTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if !wechatTrackingActive(tracking.Status) {
		return nil
	}
	now := s.currentTime()
	if !now.Before(tracking.ExpiresAt) {
		return s.expireTracking(ctx, tracking, now)
	}
	nextFetchAt := now.Add(24 * time.Hour)
	if nextFetchAt.After(tracking.ExpiresAt) {
		nextFetchAt = tracking.ExpiresAt
	}
	claimed, err := s.repo.WechatTrackings().TryClaimDailyFetch(ctx, tracking.ID, now, wechatCalendarDayStart(now), nextFetchAt)
	if err != nil {
		return fmt.Errorf("claim daily WeChat analytics fetch: %w", err)
	}
	if !claimed {
		return nil
	}
	tracking.LastFetchAt = &now
	tracking.NextFetchAt = &nextFetchAt
	tracking.RecoveryClaimToken = ""
	tracking.RecoveryClaimedAt = nil
	project, err := s.repo.Projects().FindByID(ctx, tracking.ProjectID)
	if err != nil {
		return s.recordFetchError(ctx, tracking, now, fmt.Errorf("find project: %w", err))
	}
	if s.platform == nil {
		return s.recordFetchError(ctx, tracking, now, errors.New("WeChat analytics platform unavailable"))
	}
	response, err := s.platform.FetchArticleTotalDetail(ctx, project, tracking.PublishedAt.In(wechatAnalyticsLocation).Format("2006-01-02"))
	if err != nil {
		if code, ok := publicationWechatErrCode(err); ok && code == 48001 {
			tracking.Status = model.WechatTrackingStatusUnsupported
			tracking.LastFetchAt = &now
			tracking.NextFetchAt = nil
			tracking.RunCount++
			tracking.FailureCount++
			tracking.LastError = err.Error()
			return s.repo.WechatTrackings().Update(ctx, tracking)
		}
		return s.recordFetchError(ctx, tracking, now, fmt.Errorf("fetch official article detail: %w", err))
	}
	if response == nil {
		response = &appwechat.ArticleTotalDetailResponse{}
	}
	raw := append([]byte(nil), response.RawResponse...)
	if len(raw) == 0 {
		raw, err = json.Marshal(response)
		if err != nil {
			return s.recordFetchError(ctx, tracking, now, fmt.Errorf("encode official article detail: %w", err))
		}
	}
	var manualMsgID string
	var snapshots []*model.WechatMetricSnapshot
	if !response.IsDelay {
		matched := matchWechatArticleDetailItems(tracking, response.List)
		manualMsgID, err = matchedManualMsgID(tracking, matched)
		if err != nil {
			return s.recordFetchError(ctx, tracking, now, err)
		}
		snapshots, err = detailSnapshots(tracking, matched, raw, now)
		if err != nil {
			return s.recordFetchError(ctx, tracking, now, err)
		}
	}
	updatedTracking := *tracking
	updatedTracking.LastFetchAt = &now
	updatedTracking.RunCount++
	updatedTracking.FailureCount = 0
	updatedTracking.LastError = ""
	if len(snapshots) == 0 {
		updatedTracking.Status = model.WechatTrackingStatusWaitingData
	} else {
		updatedTracking.Status = model.WechatTrackingStatusTracking
	}
	s.scheduleNextFetch(&updatedTracking, now)
	if manualMsgID != "" {
		updatedTracking.MsgID = manualMsgID
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		for _, snapshot := range snapshots {
			if err := tx.WechatMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot); err != nil {
				return err
			}
		}
		if manualMsgID != "" {
			bound, err := tx.WechatPublications().BindMsgID(ctx, tracking.PublicationID, manualMsgID)
			if err != nil {
				return fmt.Errorf("bind manual WeChat publication msgid: %w", err)
			}
			if !bound {
				return errWechatPublicationMsgIDConflict
			}
		}
		return tx.WechatTrackings().Update(ctx, &updatedTracking)
	}); err != nil {
		if errors.Is(err, errWechatPublicationMsgIDConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
			return s.recordFetchError(ctx, tracking, now, err)
		}
		return fmt.Errorf("persist WeChat article detail: %w", err)
	}
	*tracking = updatedTracking
	s.enqueueNext(tracking, now)
	return nil
}

func matchWechatArticleDetailItems(tracking *model.WechatArticleTracking, items []appwechat.ArticleTotalDetailItem) []appwechat.ArticleTotalDetailItem {
	matched := make([]appwechat.ArticleTotalDetailItem, 0, len(items))
	for _, item := range items {
		switch tracking.Source {
		case model.WechatPublicationSourceAnbanAPI:
			if tracking.MsgDataID != "" && item.MsgID == tracking.MsgDataID+"_1" {
				matched = append(matched, item)
			}
		case model.WechatPublicationSourceWechatConsole:
			if tracking.ArticleURL != "" && item.ContentURL == tracking.ArticleURL {
				matched = append(matched, item)
			}
		}
	}
	return matched
}

func matchedManualMsgID(tracking *model.WechatArticleTracking, items []appwechat.ArticleTotalDetailItem) (string, error) {
	if tracking.Source != model.WechatPublicationSourceWechatConsole {
		return "", nil
	}
	msgID := tracking.MsgID
	for _, item := range items {
		if item.MsgID == "" {
			continue
		}
		if msgID != "" && msgID != item.MsgID {
			return "", fmt.Errorf("official content URL matched multiple msgids")
		}
		msgID = item.MsgID
	}
	if msgID == tracking.MsgID {
		return "", nil
	}
	return msgID, nil
}

func detailSnapshots(tracking *model.WechatArticleTracking, items []appwechat.ArticleTotalDetailItem, raw []byte, capturedAt time.Time) ([]*model.WechatMetricSnapshot, error) {
	metricsByDate := make(map[string]appwechat.ArticleTotalDetailMetric)
	for _, item := range items {
		for _, detail := range item.DetailList {
			if detail.StatDate == "" {
				continue
			}
			if existing, ok := metricsByDate[detail.StatDate]; ok {
				if !reflect.DeepEqual(existing, detail) {
					return nil, fmt.Errorf("official article detail has conflicting metrics for stat_date %s", detail.StatDate)
				}
				continue
			}
			metricsByDate[detail.StatDate] = detail
		}
	}
	dates := make([]string, 0, len(metricsByDate))
	for statDate := range metricsByDate {
		dates = append(dates, statDate)
	}
	sort.Strings(dates)
	snapshots := make([]*model.WechatMetricSnapshot, 0, len(dates))
	for _, statDate := range dates {
		detail := metricsByDate[statDate]
		snapshots = append(snapshots, &model.WechatMetricSnapshot{
			ID: uuid.NewString(), TrackingID: tracking.ID, TaskID: tracking.TaskID, StatDate: detail.StatDate, CapturedAt: capturedAt,
			ReadUsers: detail.ReadUser, ShareUsers: detail.ShareUser, CollectionUsers: detail.CollectionUser,
			LikeUsers: detail.LikeUser, ZaikanUsers: detail.ZaikanUser, CommentCount: detail.CommentCount,
			ReadFinishRate: detail.ReadFinishRate, AverageReadActiveTime: detail.ReadAvgActiveTime,
			ReadToSubscribeUsers: detail.ReadSubscribeUser, RawResponse: append([]byte(nil), raw...),
		})
	}
	return snapshots, nil
}

func (s *WechatTrackingService) recordFetchError(ctx context.Context, tracking *model.WechatArticleTracking, now time.Time, cause error) error {
	tracking.Status = model.WechatTrackingStatusError
	tracking.LastFetchAt = &now
	tracking.RunCount++
	tracking.FailureCount++
	tracking.LastError = cause.Error()
	s.scheduleNextFetch(tracking, now)
	if err := s.repo.WechatTrackings().Update(ctx, tracking); err != nil {
		return err
	}
	s.enqueueNext(tracking, now)
	return nil
}

func (s *WechatTrackingService) expireTracking(ctx context.Context, tracking *model.WechatArticleTracking, now time.Time) error {
	tracking.Status = model.WechatTrackingStatusExpired
	tracking.NextFetchAt = nil
	tracking.ExpiredAt = &now
	tracking.LastError = ""
	return s.repo.WechatTrackings().Update(ctx, tracking)
}

func (s *WechatTrackingService) scheduleNextFetch(tracking *model.WechatArticleTracking, now time.Time) {
	next := now.Add(24 * time.Hour)
	if next.After(tracking.ExpiresAt) {
		next = tracking.ExpiresAt
	}
	tracking.NextFetchAt = &next
}

func (s *WechatTrackingService) enqueueNext(tracking *model.WechatArticleTracking, now time.Time) {
	if tracking.NextFetchAt == nil {
		return
	}
	if err := s.enqueueCapture(tracking.ID, tracking.NextFetchAt.Sub(now)); err != nil && s.logger != nil {
		s.logger.Warn().Err(err).Str("tracking_id", tracking.ID).Msg("enqueue WeChat analytics capture failed; database recovery will retry when due")
	}
}

func (s *WechatTrackingService) enqueueCapture(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(WechatCaptureMetricsTaskType, payload, delay)
}

func (s *WechatTrackingService) RecoverDue(ctx context.Context, limit int) error {
	if s.enqueuer == nil {
		return nil
	}
	now := s.currentTime()
	trackings, err := s.repo.WechatTrackings().FindDue(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, tracking := range trackings {
		lease := now.Add(15 * time.Minute)
		token := uuid.NewString()
		claimed, err := s.repo.WechatTrackings().TryClaimDueDispatch(ctx, tracking.ID, tracking.UpdatedAt, now, lease, token)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		payload, _ := json.Marshal(map[string]string{"tracking_id": tracking.ID})
		if err := s.enqueuer.Enqueue(WechatCaptureMetricsTaskType, payload); err != nil {
			if _, releaseErr := s.repo.WechatTrackings().ReleaseDueDispatch(ctx, tracking.ID, token, now); releaseErr != nil {
				return fmt.Errorf("enqueue WeChat analytics capture: %v; release recovery claim: %w", err, releaseErr)
			}
			return err
		}
	}
	return nil
}

func (s *WechatTrackingService) GetTaskAnalytics(ctx context.Context, userID, taskID string) (*WechatAnalytics, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to user")
	}
	tracking, err := s.repo.WechatTrackings().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &WechatAnalytics{Trend: []*WechatMetricTrendItem{}}, nil
	}
	if err != nil {
		return nil, err
	}
	snapshots, err := s.repo.WechatMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	analytics := &WechatAnalytics{
		Tracking: &WechatTrackingInfo{
			Status: tracking.Status, Source: tracking.Source, ArticleURL: tracking.ArticleURL,
			PublishedAt: tracking.PublishedAt, ExpiresAt: tracking.ExpiresAt,
			LastFetchAt: tracking.LastFetchAt, NextFetchAt: tracking.NextFetchAt,
			RunCount: tracking.RunCount, FailureCount: tracking.FailureCount, LastError: tracking.LastError,
		},
		Trend: make([]*WechatMetricTrendItem, 0, len(snapshots)),
	}
	for _, snapshot := range snapshots {
		if snapshot.TrackingID != tracking.ID {
			continue
		}
		metric := wechatMetricInfo(snapshot)
		trend := WechatMetricTrendItem(*metric)
		analytics.Trend = append(analytics.Trend, &trend)
		analytics.Metrics = metric
	}
	return analytics, nil
}

func wechatMetricInfo(snapshot *model.WechatMetricSnapshot) *WechatMetricInfo {
	return &WechatMetricInfo{
		StatDate: snapshot.StatDate, CapturedAt: snapshot.CapturedAt,
		ReadUsers: snapshot.ReadUsers, ShareUsers: snapshot.ShareUsers, CollectionUsers: snapshot.CollectionUsers,
		LikeUsers: snapshot.LikeUsers, ZaikanUsers: snapshot.ZaikanUsers, CommentCount: snapshot.CommentCount,
		ReadFinishRate: snapshot.ReadFinishRate, AverageReadActiveTime: snapshot.AverageReadActiveTime,
		ReadToSubscribeUsers: snapshot.ReadToSubscribeUsers,
	}
}

func wechatTrackingActive(status string) bool {
	return status == model.WechatTrackingStatusWaitingData || status == model.WechatTrackingStatusTracking || status == model.WechatTrackingStatusError
}

func wechatCalendarDayStart(value time.Time) time.Time {
	local := value.In(wechatAnalyticsLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, wechatAnalyticsLocation)
}

func (s *WechatTrackingService) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

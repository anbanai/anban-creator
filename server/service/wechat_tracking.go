package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
)

const WechatCaptureMetricsTaskType = "wechat:capture_metrics"

var wechatAnalyticsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

var (
	ErrWechatArticleURLInvalid        = errors.New("invalid WeChat article URL")
	ErrWechatArticleNotFound          = errors.New("article was not found in the configured official account")
	ErrWechatArticleOutsideDataWindow = errors.New("article is outside the WeChat official data window")
)

type WechatAnalyticsPlatform interface {
	ResolvePublishedArticle(ctx context.Context, project *model.Project, articleURL string) (*platform.WechatPublishedArticle, error)
	FetchArticleTotals(ctx context.Context, project *model.Project, publicationDate string) ([]platform.WechatArticleTotal, error)
}

type WechatTrackingService struct {
	repo     repository.Repository
	platform WechatAnalyticsPlatform
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
}

func NewWechatTrackingService(repo repository.Repository, provider WechatAnalyticsPlatform, enqueuer TaskEnqueuer, logger *zerolog.Logger) *WechatTrackingService {
	return &WechatTrackingService{repo: repo, platform: provider, enqueuer: enqueuer, logger: logger}
}

type WechatAnalytics struct {
	Tracking *WechatTrackingInfo       `json:"tracking,omitempty"`
	Latest   *WechatMetricInfo         `json:"latest,omitempty"`
	Deltas   *WechatMetricDelta        `json:"deltas,omitempty"`
	Series   []*WechatMetricSeriesItem `json:"series"`
}

type WechatTrackingInfo struct {
	Status        string     `json:"status"`
	ArticleURL    string     `json:"article_url"`
	ArticleTitle  string     `json:"article_title,omitempty"`
	PublishedDate string     `json:"published_date"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty"`
	RunCount      int        `json:"run_count"`
	StopReason    string     `json:"stop_reason,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type WechatMetricInfo struct {
	TargetUser       int        `json:"target_user"`
	IntPageReadUser  int        `json:"int_page_read_user"`
	IntPageReadCount int        `json:"int_page_read_count"`
	OriPageReadUser  int        `json:"ori_page_read_user"`
	OriPageReadCount int        `json:"ori_page_read_count"`
	ShareUser        int        `json:"share_user"`
	ShareCount       int        `json:"share_count"`
	AddToFavUser     int        `json:"add_to_fav_user"`
	AddToFavCount    int        `json:"add_to_fav_count"`
	StatDate         string     `json:"stat_date"`
	CapturedAt       *time.Time `json:"captured_at,omitempty"`
}

type WechatMetricDelta struct {
	IntPageReadUser  int `json:"int_page_read_user"`
	IntPageReadCount int `json:"int_page_read_count"`
	ShareCount       int `json:"share_count"`
	AddToFavCount    int `json:"add_to_fav_count"`
}

type WechatMetricSeriesItem struct {
	CapturedAt       time.Time `json:"captured_at"`
	StatDate         string    `json:"stat_date"`
	IntPageReadUser  int       `json:"int_page_read_user"`
	IntPageReadCount int       `json:"int_page_read_count"`
	OriPageReadUser  int       `json:"ori_page_read_user"`
	OriPageReadCount int       `json:"ori_page_read_count"`
	ShareCount       int       `json:"share_count"`
	AddToFavCount    int       `json:"add_to_fav_count"`
}

// BindTask validates ownership through WeChat's official published-article
// list, synchronously attempts the first DataCube capture, and then schedules
// daily collection. A successful bind never waits for the background worker to
// make the first official metrics request.
func (s *WechatTrackingService) BindTask(ctx context.Context, userID, taskID, articleURL string) error {
	normalizedURL, err := platform.NormalizeWechatArticleURL(articleURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWechatArticleURLInvalid, err)
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("task does not belong to user")
	}
	if task.Type != model.PlatformArticle {
		return fmt.Errorf("task is not an article task")
	}
	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return fmt.Errorf("project does not belong to user")
	}
	if s.platform == nil {
		return fmt.Errorf("WeChat analytics platform unavailable")
	}
	article, err := s.platform.ResolvePublishedArticle(ctx, project, normalizedURL)
	if err != nil {
		if errors.Is(err, platform.ErrWechatPublishedArticleNotFound) {
			return fmt.Errorf("%w: %v", ErrWechatArticleNotFound, err)
		}
		return fmt.Errorf("resolve published article: %w", err)
	}
	publishedDate := article.PublishedAt.In(wechatAnalyticsLocation).Format("2006-01-02")
	if !wechatDateInWindow(publishedDate, time.Now()) {
		return ErrWechatArticleOutsideDataWindow
	}

	now := time.Now()
	tracking, err := s.repo.WechatTrackings().FindByTaskID(ctx, taskID)
	isNew := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		isNew = true
		tracking = &model.WechatArticleTracking{ID: uuid.NewString(), TaskID: taskID, UserID: userID, ProjectID: task.ProjectID}
	} else if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	tracking.Status = model.WechatTrackingStatusWaitingData
	tracking.ArticleID = article.ArticleID
	tracking.MsgID = ""
	tracking.ArticleURL = article.ArticleURL
	tracking.ArticleTitle = article.ArticleTitle
	tracking.PublishedDate = publishedDate
	tracking.BoundAt = now
	tracking.LastRunAt = nil
	tracking.NextRunAt = nil
	tracking.TrackingStoppedAt = nil
	tracking.RunCount = 0
	tracking.FailureCount = 0
	tracking.StopReason = ""
	tracking.LastError = ""
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if isNew {
			return tx.WechatTrackings().Create(ctx, tracking)
		}
		if err := tx.WechatTrackings().Update(ctx, tracking); err != nil {
			return err
		}
		return tx.WechatMetricSnapshots().DeleteByTrackingID(ctx, tracking.ID)
	}); err != nil {
		return fmt.Errorf("persist tracking binding: %w", err)
	}
	return s.CaptureMetrics(ctx, tracking.ID)
}

func (s *WechatTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.WechatTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.Status != model.WechatTrackingStatusWaitingData && tracking.Status != model.WechatTrackingStatusTracking {
		return nil
	}
	now := time.Now()
	snapshots, err := s.repo.WechatMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		return fmt.Errorf("find snapshots: %w", err)
	}
	capturedDate := now.In(wechatAnalyticsLocation).Format("2006-01-02")
	for _, snapshot := range snapshots {
		if snapshot.TrackingID == tracking.ID && snapshot.CapturedDate == capturedDate {
			return nil
		}
	}
	project, err := s.repo.Projects().FindByID(ctx, tracking.ProjectID)
	if err != nil {
		return s.recordFailure(ctx, tracking, fmt.Errorf("find project: %w", err))
	}
	if s.platform == nil {
		return s.recordFailure(ctx, tracking, fmt.Errorf("WeChat analytics platform unavailable"))
	}
	totals, err := s.platform.FetchArticleTotals(ctx, project, tracking.PublishedDate)
	if err != nil {
		return s.recordFailure(ctx, tracking, fmt.Errorf("fetch official article metrics: %w", err))
	}
	total, matchErr := matchWechatArticleTotal(tracking, totals)
	if matchErr != nil {
		return s.recordFailure(ctx, tracking, matchErr)
	}
	if total == nil {
		return s.recordWaitingData(ctx, tracking, now)
	}
	metric, ok := platform.LatestWechatMetric(total.Details)
	if !ok {
		return s.recordWaitingData(ctx, tracking, now)
	}
	tracking.MsgID = total.MsgID
	raw, _ := json.Marshal(metric)
	snapshot := &model.WechatMetricSnapshot{
		ID: uuid.NewString(), TrackingID: tracking.ID, TaskID: tracking.TaskID,
		CapturedAt: now, CapturedDate: capturedDate, StatDate: metric.StatDate,
		TargetUser: metric.TargetUser, IntPageReadUser: metric.IntPageReadUser, IntPageReadCount: metric.IntPageReadCount,
		OriPageReadUser: metric.OriPageReadUser, OriPageReadCount: metric.OriPageReadCount,
		ShareUser: metric.ShareUser, ShareCount: metric.ShareCount,
		AddToFavUser: metric.AddToFavUser, AddToFavCount: metric.AddToFavCount, RawData: string(raw),
	}
	if err := s.repo.WechatMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	tracking.Status = model.WechatTrackingStatusTracking
	tracking.RunCount++
	tracking.FailureCount = 0
	tracking.LastRunAt = &now
	tracking.LastError = ""
	return s.scheduleNext(ctx, tracking, now)
}

func (s *WechatTrackingService) recordWaitingData(ctx context.Context, tracking *model.WechatArticleTracking, now time.Time) error {
	tracking.Status = model.WechatTrackingStatusWaitingData
	tracking.LastRunAt = &now
	tracking.LastError = "微信官方文章数据通常在次日生成"
	return s.scheduleNext(ctx, tracking, now)
}

func (s *WechatTrackingService) scheduleNext(ctx context.Context, tracking *model.WechatArticleTracking, now time.Time) error {
	if !wechatDateInWindow(tracking.PublishedDate, now.Add(24*time.Hour)) {
		tracking.Status = model.WechatTrackingStatusStopped
		tracking.StopReason = model.WechatStopReasonDataWindowEnded
		tracking.TrackingStoppedAt = &now
		tracking.NextRunAt = nil
		return s.repo.WechatTrackings().Update(ctx, tracking)
	}
	next := now.Add(24 * time.Hour)
	tracking.NextRunAt = &next
	if err := s.repo.WechatTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking: %w", err)
	}
	if err := s.enqueueCapture(tracking.ID, 24*time.Hour); err != nil {
		s.logEnqueueFailure(err, tracking.ID)
	}
	return nil
}

func (s *WechatTrackingService) recordFailure(ctx context.Context, tracking *model.WechatArticleTracking, cause error) error {
	now := time.Now()
	tracking.FailureCount++
	tracking.LastRunAt = &now
	tracking.LastError = cause.Error()
	if tracking.FailureCount >= model.WechatTrackingMaxFailures {
		tracking.Status = model.WechatTrackingStatusFailed
		tracking.StopReason = model.WechatStopReasonTooManyFailures
		tracking.NextRunAt = nil
		if err := s.repo.WechatTrackings().Update(ctx, tracking); err != nil {
			return err
		}
		return nil
	}
	return s.scheduleNext(ctx, tracking, now)
}

func (s *WechatTrackingService) enqueueCapture(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(WechatCaptureMetricsTaskType, payload, delay)
}

func (s *WechatTrackingService) logEnqueueFailure(err error, trackingID string) {
	if s.logger != nil {
		s.logger.Warn().Err(err).Str("tracking_id", trackingID).Msg("enqueue WeChat analytics capture failed; database recovery will retry when due")
	}
}

func (s *WechatTrackingService) RecoverDue(ctx context.Context, limit int) error {
	if s.enqueuer == nil {
		return nil
	}
	now := time.Now()
	trackings, err := s.repo.WechatTrackings().FindDue(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, tracking := range trackings {
		payload, _ := json.Marshal(map[string]string{"tracking_id": tracking.ID})
		if err := s.enqueuer.Enqueue(WechatCaptureMetricsTaskType, payload); err != nil {
			return err
		}
		lease := now.Add(15 * time.Minute)
		tracking.NextRunAt = &lease
		if err := s.repo.WechatTrackings().Update(ctx, tracking); err != nil {
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
		return &WechatAnalytics{Series: []*WechatMetricSeriesItem{}}, nil
	}
	if err != nil {
		return nil, err
	}
	snapshots, err := s.repo.WechatMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	filtered := make([]*model.WechatMetricSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.TrackingID == tracking.ID {
			filtered = append(filtered, snapshot)
		}
	}
	analytics := &WechatAnalytics{
		Tracking: &WechatTrackingInfo{
			Status: tracking.Status, ArticleURL: tracking.ArticleURL, ArticleTitle: tracking.ArticleTitle,
			PublishedDate: tracking.PublishedDate, LastRunAt: tracking.LastRunAt, NextRunAt: tracking.NextRunAt,
			RunCount: tracking.RunCount, StopReason: tracking.StopReason, LastError: tracking.LastError,
		},
		Series: make([]*WechatMetricSeriesItem, 0, len(filtered)),
	}
	for _, snapshot := range filtered {
		analytics.Series = append(analytics.Series, &WechatMetricSeriesItem{
			CapturedAt: snapshot.CapturedAt, StatDate: snapshot.StatDate,
			IntPageReadUser: snapshot.IntPageReadUser, IntPageReadCount: snapshot.IntPageReadCount,
			OriPageReadUser: snapshot.OriPageReadUser, OriPageReadCount: snapshot.OriPageReadCount,
			ShareCount: snapshot.ShareCount, AddToFavCount: snapshot.AddToFavCount,
		})
	}
	if len(filtered) > 0 {
		latest := filtered[len(filtered)-1]
		analytics.Latest = wechatMetricInfo(latest)
		analytics.Deltas = &WechatMetricDelta{}
		if len(filtered) > 1 {
			previous := filtered[len(filtered)-2]
			analytics.Deltas = &WechatMetricDelta{
				IntPageReadUser:  latest.IntPageReadUser - previous.IntPageReadUser,
				IntPageReadCount: latest.IntPageReadCount - previous.IntPageReadCount,
				ShareCount:       latest.ShareCount - previous.ShareCount,
				AddToFavCount:    latest.AddToFavCount - previous.AddToFavCount,
			}
		}
	}
	return analytics, nil
}

func matchWechatArticleTotal(tracking *model.WechatArticleTracking, totals []platform.WechatArticleTotal) (*platform.WechatArticleTotal, error) {
	var matches []*platform.WechatArticleTotal
	for i := range totals {
		item := &totals[i]
		if tracking.MsgID != "" && item.MsgID == tracking.MsgID {
			return item, nil
		}
		if tracking.ArticleID != "" && item.MsgID == tracking.ArticleID {
			return item, nil
		}
		if strings.TrimSpace(item.Title) == strings.TrimSpace(tracking.ArticleTitle) {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple official articles share the title %q", tracking.ArticleTitle)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, nil
}

func wechatDateInWindow(publicationDate string, now time.Time) bool {
	published, err := time.ParseInLocation("2006-01-02", publicationDate, wechatAnalyticsLocation)
	if err != nil {
		return false
	}
	today, _ := time.ParseInLocation("2006-01-02", now.In(wechatAnalyticsLocation).Format("2006-01-02"), wechatAnalyticsLocation)
	days := int(today.Sub(published).Hours() / 24)
	return days >= 0 && days < model.WechatTrackingMaxDays
}

func wechatMetricInfo(snapshot *model.WechatMetricSnapshot) *WechatMetricInfo {
	return &WechatMetricInfo{
		TargetUser: snapshot.TargetUser, IntPageReadUser: snapshot.IntPageReadUser, IntPageReadCount: snapshot.IntPageReadCount,
		OriPageReadUser: snapshot.OriPageReadUser, OriPageReadCount: snapshot.OriPageReadCount,
		ShareUser: snapshot.ShareUser, ShareCount: snapshot.ShareCount,
		AddToFavUser: snapshot.AddToFavUser, AddToFavCount: snapshot.AddToFavCount,
		StatDate: snapshot.StatDate, CapturedAt: &snapshot.CapturedAt,
	}
}

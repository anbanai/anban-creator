package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/worldtree"
)

const ChannelsCaptureMetricsTaskType = "channels:capture_metrics"

var (
	ErrChannelsVideoURLInvalid  = errors.New("invalid WeChat Channels video URL")
	ErrChannelsProviderDisabled = errors.New("WeChat Channels analytics provider is not configured")
)

var channelsAnalyticsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type ChannelsAnalyticsPlatform interface {
	GetVideoInfoByURL(ctx context.Context, videoURL string) (*worldtree.VideoInfo, error)
}

type ChannelsTrackingService struct {
	repo     repository.Repository
	platform ChannelsAnalyticsPlatform
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
}

func NewChannelsTrackingService(repo repository.Repository, provider ChannelsAnalyticsPlatform, enqueuer TaskEnqueuer, logger *zerolog.Logger) *ChannelsTrackingService {
	return &ChannelsTrackingService{repo: repo, platform: provider, enqueuer: enqueuer, logger: logger}
}

type ChannelsAnalytics struct {
	Tracking *ChannelsTrackingInfo       `json:"tracking,omitempty"`
	Latest   *ChannelsMetricInfo         `json:"latest,omitempty"`
	Deltas   *ChannelsMetricDelta        `json:"deltas,omitempty"`
	Series   []*ChannelsMetricSeriesItem `json:"series"`
}

type ChannelsTrackingInfo struct {
	Status       string     `json:"status"`
	VideoURL     string     `json:"video_url"`
	VideoTitle   string     `json:"video_title,omitempty"`
	AuthorName   string     `json:"author_name,omitempty"`
	CoverURL     string     `json:"cover_url,omitempty"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	RunCount     int        `json:"run_count"`
	StopReason   string     `json:"stop_reason,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	ProviderName string     `json:"provider_name"`
}

type ChannelsMetricInfo struct {
	LikeCount     int        `json:"like_count"`
	FavoriteCount int        `json:"favorite_count"`
	CommentCount  int        `json:"comment_count"`
	ForwardCount  int        `json:"forward_count"`
	CapturedAt    *time.Time `json:"captured_at,omitempty"`
}

type ChannelsMetricDelta struct {
	LikeCount     int `json:"like_count"`
	FavoriteCount int `json:"favorite_count"`
	CommentCount  int `json:"comment_count"`
	ForwardCount  int `json:"forward_count"`
}

type ChannelsMetricSeriesItem struct {
	CapturedAt    time.Time `json:"captured_at"`
	LikeCount     int       `json:"like_count"`
	FavoriteCount int       `json:"favorite_count"`
	CommentCount  int       `json:"comment_count"`
	ForwardCount  int       `json:"forward_count"`
}

func NormalizeChannelsVideoURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return "", ErrChannelsVideoURLInvalid
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "channels.weixin.qq.com":
		if parsed.Path != "/web/pages/feed" {
			return "", ErrChannelsVideoURLInvalid
		}
		stable := url.Values{}
		if value := parsed.Query().Get("oid"); value != "" {
			stable.Set("oid", value)
		} else if value := parsed.Query().Get("eid"); value != "" {
			stable.Set("eid", value)
		} else {
			return "", ErrChannelsVideoURLInvalid
		}
		parsed.RawQuery = stable.Encode()
	case "weixin.qq.com":
		if !strings.HasPrefix(parsed.Path, "/sph/") || strings.TrimPrefix(parsed.Path, "/sph/") == "" {
			return "", ErrChannelsVideoURLInvalid
		}
		parsed.RawQuery = ""
	default:
		return "", ErrChannelsVideoURLInvalid
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (s *ChannelsTrackingService) BindTask(ctx context.Context, userID, taskID, rawURL string) error {
	videoURL, err := NormalizeChannelsVideoURL(rawURL)
	if err != nil {
		return err
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("task does not belong to user")
	}
	if task.Type != model.PlatformMontage && task.Type != model.TaskTypeLiveSlicer {
		return fmt.Errorf("task is not a video task")
	}
	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return fmt.Errorf("project does not belong to user")
	}
	if s.platform == nil {
		return ErrChannelsProviderDisabled
	}
	info, err := s.platform.GetVideoInfoByURL(ctx, videoURL)
	if err != nil {
		if errors.Is(err, worldtree.ErrNotConfigured) {
			return ErrChannelsProviderDisabled
		}
		return fmt.Errorf("fetch WeChat Channels video details: %w", err)
	}

	now := time.Now()
	tracking, err := s.repo.ChannelsTrackings().FindByTaskID(ctx, taskID)
	isNew := errors.Is(err, gorm.ErrRecordNotFound)
	if isNew {
		tracking = &model.ChannelsVideoTracking{ID: uuid.NewString(), TaskID: taskID, UserID: userID, ProjectID: task.ProjectID}
	} else if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	applyChannelsVideoInfo(tracking, videoURL, info, now)
	snapshot := newChannelsSnapshot(tracking, info, now)
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if isNew {
			if err := tx.ChannelsTrackings().Create(ctx, tracking); err != nil {
				return err
			}
		} else {
			if err := tx.ChannelsTrackings().Update(ctx, tracking); err != nil {
				return err
			}
			if err := tx.ChannelsMetricSnapshots().DeleteByTrackingID(ctx, tracking.ID); err != nil {
				return err
			}
		}
		return tx.ChannelsMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot)
	}); err != nil {
		return fmt.Errorf("persist WeChat Channels tracking: %w", err)
	}
	return s.finishCapture(ctx, tracking, now)
}

func applyChannelsVideoInfo(tracking *model.ChannelsVideoTracking, videoURL string, info *worldtree.VideoInfo, now time.Time) {
	tracking.Status = model.ChannelsTrackingStatusTracking
	tracking.VideoURL = videoURL
	tracking.VideoID = info.ObjectID
	tracking.VideoTitle = strings.TrimSpace(info.Title)
	tracking.AuthorUsername = strings.TrimSpace(info.Username)
	tracking.AuthorName = strings.TrimSpace(info.AuthorName)
	tracking.CoverURL = strings.TrimSpace(info.CoverURL)
	tracking.PublishedAt = nil
	if info.CreateTime > 0 {
		publishedAt := time.Unix(info.CreateTime, 0)
		tracking.PublishedAt = &publishedAt
	}
	tracking.BoundAt = now
	tracking.LastRunAt = nil
	tracking.NextRunAt = nil
	tracking.CaptureClaimUntil = nil
	tracking.CaptureClaimToken = ""
	tracking.TrackingStoppedAt = nil
	tracking.RunCount = 0
	tracking.FailureCount = 0
	tracking.StopReason = ""
	tracking.LastError = ""
}

func newChannelsSnapshot(tracking *model.ChannelsVideoTracking, info *worldtree.VideoInfo, now time.Time) *model.ChannelsMetricSnapshot {
	raw, _ := json.Marshal(info)
	return &model.ChannelsMetricSnapshot{
		ID: uuid.NewString(), TrackingID: tracking.ID, TaskID: tracking.TaskID,
		CapturedAt: now, CapturedDate: now.In(channelsAnalyticsLocation).Format("2006-01-02"),
		LikeCount: info.LikeCount, FavoriteCount: info.FavoriteCount,
		CommentCount: info.CommentCount, ForwardCount: info.ForwardCount, RawData: string(raw),
	}
}

func (s *ChannelsTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.ChannelsTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.Status != model.ChannelsTrackingStatusTracking {
		return nil
	}
	now := time.Now()
	claimToken := uuid.NewString()
	claimed, err := s.repo.ChannelsTrackings().TryClaimCapture(ctx, tracking.ID, claimToken, now, now.Add(15*time.Minute))
	if err != nil {
		return fmt.Errorf("claim WeChat Channels capture: %w", err)
	}
	if !claimed {
		return nil
	}
	defer func() {
		if err := s.repo.ChannelsTrackings().ReleaseCaptureClaim(context.Background(), tracking.ID, claimToken); err != nil && s.logger != nil {
			s.logger.Warn().Err(err).Str("tracking_id", tracking.ID).Msg("release WeChat Channels capture claim failed")
		}
	}()
	if now.Sub(tracking.BoundAt) >= model.ChannelsTrackingMaxDays*24*time.Hour {
		tracking.Status = model.ChannelsTrackingStatusStopped
		tracking.StopReason = model.ChannelsStopReasonMaxDurationReached
		tracking.TrackingStoppedAt = &now
		tracking.NextRunAt = nil
		tracking.CaptureClaimUntil = nil
		tracking.CaptureClaimToken = ""
		return s.repo.ChannelsTrackings().Update(ctx, tracking)
	}
	snapshots, err := s.repo.ChannelsMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		return fmt.Errorf("find snapshots: %w", err)
	}
	today := now.In(channelsAnalyticsLocation).Format("2006-01-02")
	for _, snapshot := range snapshots {
		if snapshot.TrackingID == tracking.ID && snapshot.CapturedDate == today {
			if tracking.LastRunAt == nil || tracking.LastRunAt.Before(snapshot.CapturedAt) {
				return s.finishCapture(ctx, tracking, now)
			}
			return nil
		}
	}
	if s.platform == nil {
		return s.recordFailure(ctx, tracking, ErrChannelsProviderDisabled)
	}
	info, err := s.platform.GetVideoInfoByURL(ctx, tracking.VideoURL)
	if err != nil {
		return s.recordFailure(ctx, tracking, fmt.Errorf("fetch WeChat Channels metrics: %w", err))
	}
	current, err := s.repo.ChannelsTrackings().FindByID(ctx, tracking.ID)
	if err != nil {
		return fmt.Errorf("reload tracking after capture: %w", err)
	}
	if current.VideoURL != tracking.VideoURL || !current.BoundAt.Equal(tracking.BoundAt) {
		return nil
	}
	tracking = current
	tracking.CaptureClaimUntil = nil
	tracking.CaptureClaimToken = ""
	tracking.VideoTitle = strings.TrimSpace(info.Title)
	tracking.AuthorName = strings.TrimSpace(info.AuthorName)
	tracking.CoverURL = strings.TrimSpace(info.CoverURL)
	if err := s.repo.ChannelsMetricSnapshots().UpsertByTrackingAndDate(ctx, newChannelsSnapshot(tracking, info, now)); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	return s.finishCapture(ctx, tracking, now)
}

func (s *ChannelsTrackingService) finishCapture(ctx context.Context, tracking *model.ChannelsVideoTracking, now time.Time) error {
	tracking.RunCount++
	tracking.FailureCount = 0
	tracking.LastRunAt = &now
	tracking.LastError = ""
	if now.Sub(tracking.BoundAt) >= model.ChannelsTrackingMaxDays*24*time.Hour {
		tracking.Status = model.ChannelsTrackingStatusStopped
		tracking.StopReason = model.ChannelsStopReasonMaxDurationReached
		tracking.TrackingStoppedAt = &now
		tracking.NextRunAt = nil
		return s.repo.ChannelsTrackings().Update(ctx, tracking)
	}
	next := now.Add(24 * time.Hour)
	tracking.NextRunAt = &next
	if err := s.repo.ChannelsTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking: %w", err)
	}
	if err := s.enqueueCapture(tracking.ID, 24*time.Hour); err != nil {
		tracking.LastError = "enqueue WeChat Channels capture failed"
		_ = s.repo.ChannelsTrackings().Update(ctx, tracking)
		s.logEnqueueFailure(err, tracking.ID)
	}
	return nil
}

func (s *ChannelsTrackingService) recordFailure(ctx context.Context, tracking *model.ChannelsVideoTracking, cause error) error {
	now := time.Now()
	tracking.FailureCount++
	tracking.LastRunAt = &now
	tracking.LastError = cause.Error()
	if tracking.FailureCount >= model.ChannelsTrackingMaxFailures {
		tracking.Status = model.ChannelsTrackingStatusFailed
		tracking.StopReason = model.ChannelsStopReasonTooManyFailures
		tracking.NextRunAt = nil
	} else {
		next := now.Add(24 * time.Hour)
		tracking.NextRunAt = &next
	}
	if err := s.repo.ChannelsTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking failure: %w", err)
	}
	if tracking.Status == model.ChannelsTrackingStatusTracking {
		if err := s.enqueueCapture(tracking.ID, 24*time.Hour); err != nil {
			s.logEnqueueFailure(err, tracking.ID)
		}
	}
	return nil
}

func (s *ChannelsTrackingService) logEnqueueFailure(err error, trackingID string) {
	if s.logger != nil {
		s.logger.Warn().Err(err).Str("tracking_id", trackingID).Msg("enqueue WeChat Channels capture failed; database recovery will retry when due")
	}
}

func (s *ChannelsTrackingService) enqueueCapture(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(ChannelsCaptureMetricsTaskType, payload, delay)
}

func (s *ChannelsTrackingService) RecoverDue(ctx context.Context, limit int) error {
	if s.enqueuer == nil {
		return nil
	}
	now := time.Now()
	trackings, err := s.repo.ChannelsTrackings().FindDue(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, tracking := range trackings {
		payload, _ := json.Marshal(map[string]string{"tracking_id": tracking.ID})
		if err := s.enqueuer.Enqueue(ChannelsCaptureMetricsTaskType, payload); err != nil {
			return err
		}
		lease := now.Add(15 * time.Minute)
		tracking.NextRunAt = &lease
		if err := s.repo.ChannelsTrackings().Update(ctx, tracking); err != nil {
			return err
		}
	}
	return nil
}

func (s *ChannelsTrackingService) GetTaskAnalytics(ctx context.Context, userID, taskID string) (*ChannelsAnalytics, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to user")
	}
	tracking, err := s.repo.ChannelsTrackings().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &ChannelsAnalytics{Series: []*ChannelsMetricSeriesItem{}}, nil
	}
	if err != nil {
		return nil, err
	}
	snapshots, err := s.repo.ChannelsMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	filtered := make([]*model.ChannelsMetricSnapshot, 0, len(snapshots))
	analytics := &ChannelsAnalytics{
		Tracking: &ChannelsTrackingInfo{
			Status: tracking.Status, VideoURL: tracking.VideoURL, VideoTitle: tracking.VideoTitle,
			AuthorName: tracking.AuthorName, CoverURL: tracking.CoverURL, PublishedAt: tracking.PublishedAt,
			LastRunAt: tracking.LastRunAt, NextRunAt: tracking.NextRunAt, RunCount: tracking.RunCount,
			StopReason: tracking.StopReason, LastError: tracking.LastError, ProviderName: "世界树科技",
		},
		Series: []*ChannelsMetricSeriesItem{},
	}
	for _, snapshot := range snapshots {
		if snapshot.TrackingID != tracking.ID || snapshot.CapturedAt.Before(tracking.BoundAt) {
			continue
		}
		filtered = append(filtered, snapshot)
		analytics.Series = append(analytics.Series, &ChannelsMetricSeriesItem{
			CapturedAt: snapshot.CapturedAt, LikeCount: snapshot.LikeCount, FavoriteCount: snapshot.FavoriteCount,
			CommentCount: snapshot.CommentCount, ForwardCount: snapshot.ForwardCount,
		})
	}
	if len(filtered) > 0 {
		latest := filtered[len(filtered)-1]
		analytics.Latest = channelsMetricInfo(latest)
		analytics.Deltas = &ChannelsMetricDelta{}
		if len(filtered) > 1 {
			previous := filtered[len(filtered)-2]
			analytics.Deltas = &ChannelsMetricDelta{
				LikeCount:     latest.LikeCount - previous.LikeCount,
				FavoriteCount: latest.FavoriteCount - previous.FavoriteCount,
				CommentCount:  latest.CommentCount - previous.CommentCount,
				ForwardCount:  latest.ForwardCount - previous.ForwardCount,
			}
		}
	}
	return analytics, nil
}

func channelsMetricInfo(snapshot *model.ChannelsMetricSnapshot) *ChannelsMetricInfo {
	return &ChannelsMetricInfo{
		LikeCount: snapshot.LikeCount, FavoriteCount: snapshot.FavoriteCount,
		CommentCount: snapshot.CommentCount, ForwardCount: snapshot.ForwardCount,
		CapturedAt: &snapshot.CapturedAt,
	}
}

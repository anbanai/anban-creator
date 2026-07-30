package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/platform"
	"github.com/anbanai/anban-creator/server/repository"
)

const (
	SeednoteCaptureMetricsTaskType = "seednote:capture_metrics"
)

const unresolvedSeednoteTrackingMessage = "缺少公开笔记 ID 或链接，尚未建立追踪关联"

var (
	ErrSeednotePublicationIDInvalid        = errors.New("invalid seednote publication note id")
	ErrSeednotePublicationURLInvalid       = errors.New("invalid seednote publication note url")
	ErrSeednotePublicationIdentityMismatch = errors.New("seednote publication identity mismatch")
	seednotePublicationIDPattern           = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
)

type SeednotePublicationIdentity struct {
	NoteID  string `json:"note_id,omitempty"`
	NoteURL string `json:"note_url,omitempty"`
}

type SeednotePublicPlatform interface {
	FetchPostMetrics(ctx context.Context, noteURL string) (*platform.SeednotePostMetrics, error)
}

type SeednoteTrackingService struct {
	repo     repository.Repository
	platform SeednotePublicPlatform
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
}

func NewSeednoteTrackingService(repo repository.Repository, platform SeednotePublicPlatform, enqueuer TaskEnqueuer, logger *zerolog.Logger) *SeednoteTrackingService {
	return &SeednoteTrackingService{
		repo:     repo,
		platform: platform,
		enqueuer: enqueuer,
		logger:   logger,
	}
}

type SeednoteAnalytics struct {
	Tracking *SeednoteTrackingInfo       `json:"tracking,omitempty"`
	Latest   *SeednoteMetricInfo         `json:"latest,omitempty"`
	Deltas   *SeednoteMetricDelta        `json:"deltas,omitempty"`
	Series   []*SeednoteMetricSeriesItem `json:"series"`
}

type SeednoteTrackingInfo struct {
	Status       string     `json:"status"`
	NoteURL      string     `json:"note_url,omitempty"`
	NoteTitle    string     `json:"note_title,omitempty"`
	NoteCoverURL string     `json:"note_cover_url,omitempty"`
	DiscoveredAt *time.Time `json:"discovered_at,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	RunCount     int        `json:"run_count"`
	StopReason   string     `json:"stop_reason,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

type SeednoteMetricInfo struct {
	LikeCount    int        `json:"like_count"`
	CollectCount int        `json:"collect_count"`
	CommentCount int        `json:"comment_count"`
	ShareCount   int        `json:"share_count"`
	ViewCount    *int       `json:"view_count"`
	CapturedAt   *time.Time `json:"captured_at,omitempty"`
}

type SeednoteMetricDelta struct {
	LikeCount    int `json:"like_count"`
	CollectCount int `json:"collect_count"`
	CommentCount int `json:"comment_count"`
	ShareCount   int `json:"share_count"`
}

type SeednoteMetricSeriesItem struct {
	CapturedAt   time.Time `json:"captured_at"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	CommentCount int       `json:"comment_count"`
	ShareCount   int       `json:"share_count"`
	ViewCount    *int      `json:"view_count"`
}

func NormalizeSeednotePublicationIdentity(identity SeednotePublicationIdentity) (SeednotePublicationIdentity, error) {
	identity.NoteID = strings.TrimSpace(identity.NoteID)
	identity.NoteURL = strings.TrimSpace(identity.NoteURL)
	if identity.NoteID != "" && !seednotePublicationIDPattern.MatchString(identity.NoteID) {
		return SeednotePublicationIdentity{}, ErrSeednotePublicationIDInvalid
	}
	if identity.NoteURL != "" {
		parsed, err := url.Parse(identity.NoteURL)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !isSeednotePublicationHost(parsed.Hostname()) {
			return SeednotePublicationIdentity{}, ErrSeednotePublicationURLInvalid
		}
		urlID := platform.ExtractSeednoteNoteID(parsed.EscapedPath())
		if !seednotePublicationIDPattern.MatchString(urlID) {
			return SeednotePublicationIdentity{}, ErrSeednotePublicationURLInvalid
		}
		if identity.NoteID != "" && identity.NoteID != urlID {
			return SeednotePublicationIdentity{}, ErrSeednotePublicationIdentityMismatch
		}
		identity.NoteID = urlID
	}
	if identity.NoteID != "" && identity.NoteURL == "" {
		identity.NoteURL = "https://www.xiaohongshu.com/explore/" + url.PathEscape(identity.NoteID)
	}
	return identity, nil
}

func isSeednotePublicationHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com")
}

func (s *SeednoteTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string, identity SeednotePublicationIdentity) error {
	identity, err := NormalizeSeednotePublicationIdentity(identity)
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
	if task.Type != model.PlatformSeednote {
		return nil
	}

	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return fmt.Errorf("project does not belong to user")
	}

	now := time.Now()
	status := model.SeednoteTrackingStatusUnresolved
	lastError := unresolvedSeednoteTrackingMessage
	var trackingStartedAt *time.Time
	if identity.NoteID != "" {
		status = model.SeednoteTrackingStatusTracking
		lastError = ""
		trackingStartedAt = &now
	}
	existing, err := s.repo.SeednoteTrackings().FindByTaskID(ctx, taskID)
	if err == nil {
		existing.Status = status
		existing.ProfileURL = strings.TrimSpace(project.ProfileURL)
		existing.PublishedMarkedAt = now
		existing.NextRunAt = nil
		existing.LastRunAt = nil
		existing.DiscoveredAt = nil
		existing.TrackingStartedAt = trackingStartedAt
		existing.TrackingStoppedAt = nil
		existing.NoteID = identity.NoteID
		existing.NoteURL = identity.NoteURL
		existing.NoteTitle = ""
		existing.NoteCoverURL = ""
		existing.RunCount = 0
		existing.ConsecutiveLowGrowthCount = 0
		existing.FailureCount = 0
		existing.DiscoveryAttemptCount = 0
		existing.MatchConfidence = 0
		existing.MatchReason = ""
		existing.StopReason = ""
		existing.LastError = lastError
		if updateErr := s.repo.SeednoteTrackings().Update(ctx, existing); updateErr != nil {
			return fmt.Errorf("update tracking: %w", updateErr)
		}
		if status == model.SeednoteTrackingStatusTracking {
			return s.CaptureMetrics(ctx, existing.ID)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find tracking: %w", err)
	}

	tracking := &model.SeednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ProjectID:         task.ProjectID,
		Status:            status,
		ProfileURL:        strings.TrimSpace(project.ProfileURL),
		NoteID:            identity.NoteID,
		NoteURL:           identity.NoteURL,
		PublishedMarkedAt: now,
		TrackingStartedAt: trackingStartedAt,
		LastError:         lastError,
	}
	if err := s.repo.SeednoteTrackings().Create(ctx, tracking); err != nil {
		return fmt.Errorf("create tracking: %w", err)
	}
	if status == model.SeednoteTrackingStatusTracking {
		return s.CaptureMetrics(ctx, tracking.ID)
	}
	return nil
}

func (s *SeednoteTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.SeednoteTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.Status != model.SeednoteTrackingStatusTracking {
		return nil
	}
	if tracking.NoteURL == "" {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("tracking has no note URL"))
	}
	now := time.Now()
	if snapshot, ok := s.findSnapshotForDate(ctx, tracking, model.SeednoteCapturedDate(now)); ok {
		if tracking.LastRunAt != nil && model.SeednoteCapturedDate(*tracking.LastRunAt) == snapshot.CapturedDate {
			if tracking.LastError != "" && tracking.NextRunAt != nil {
				if err := s.enqueueCapture(tracking.ID, delayUntil(*tracking.NextRunAt, now)); err != nil {
					return s.recordEnqueueFailure(ctx, tracking, err)
				}
				tracking.LastError = ""
				if err := s.repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
					return fmt.Errorf("clear enqueue failure: %w", err)
				}
			}
			return nil
		}
		return s.finishCaptureLifecycle(ctx, tracking, snapshot, now, snapshot.CapturedAt)
	}
	if s.platform == nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("seednote platform unavailable"))
	}
	metrics, err := s.platform.FetchPostMetrics(ctx, tracking.NoteURL)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("fetch post metrics: %w", err))
	}
	raw, _ := json.Marshal(metrics)
	snapshot := &model.SeednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       tracking.TaskID,
		CapturedAt:   now,
		CapturedDate: model.SeednoteCapturedDate(now),
		LikeCount:    metrics.LikeCount,
		CollectCount: metrics.CollectCount,
		CommentCount: metrics.CommentCount,
		ShareCount:   metrics.ShareCount,
		ViewCount:    metrics.ViewCount,
		RawData:      string(raw),
	}
	if err := s.repo.SeednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}

	return s.finishCaptureLifecycle(ctx, tracking, snapshot, now, now)
}

func (s *SeednoteTrackingService) finishCaptureLifecycle(ctx context.Context, tracking *model.SeednotePostTracking, snapshot *model.SeednoteMetricSnapshot, now, previousBefore time.Time) error {
	tracking.RunCount++
	tracking.FailureCount = 0
	tracking.LastRunAt = &now
	tracking.LastError = ""
	previous, prevErr := s.findPreviousLifecycleSnapshot(ctx, tracking, previousBefore)
	if prevErr == nil && previous != nil {
		growth := totalGrowth(snapshot, previous)
		if growth < model.SeednoteLowGrowthThreshold {
			tracking.ConsecutiveLowGrowthCount++
		} else {
			tracking.ConsecutiveLowGrowthCount = 0
		}
	} else if prevErr != nil && !errors.Is(prevErr, gorm.ErrRecordNotFound) && s.logger != nil {
		s.logger.Warn().Err(prevErr).Str("tracking_id", tracking.ID).Msg("failed to load previous seednote metrics")
	}

	if s.shouldStopTracking(tracking, now) {
		tracking.Status = model.SeednoteTrackingStatusStopped
		tracking.TrackingStoppedAt = &now
		tracking.NextRunAt = nil
		if err := s.repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
			return fmt.Errorf("update stopped tracking: %w", err)
		}
		return nil
	}
	next := now.Add(24 * time.Hour)
	tracking.NextRunAt = &next
	if err := s.repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking after capture: %w", err)
	}
	if err := s.enqueueCapture(tracking.ID, 24*time.Hour); err != nil {
		return s.recordEnqueueFailure(ctx, tracking, err)
	}
	return nil
}

func delayUntil(runAt, now time.Time) time.Duration {
	if !runAt.After(now) {
		return 0
	}
	return runAt.Sub(now)
}

func (s *SeednoteTrackingService) shouldStopTracking(tracking *model.SeednotePostTracking, now time.Time) bool {
	if now.Sub(tracking.PublishedMarkedAt) >= model.SeednoteTrackingMaxDays*24*time.Hour {
		tracking.StopReason = model.SeednoteStopReasonMaxDurationReached
		return true
	}
	if shouldStopForLowGrowth(tracking.ConsecutiveLowGrowthCount, model.SeednoteLowGrowthConsecutiveCaptures) {
		tracking.StopReason = model.SeednoteStopReasonLowGrowth
		return true
	}
	return false
}

func shouldStopForLowGrowth(count, threshold int) bool {
	return count >= threshold
}

func totalGrowth(current, previous *model.SeednoteMetricSnapshot) int {
	return (current.LikeCount - previous.LikeCount) +
		(current.CollectCount - previous.CollectCount) +
		(current.CommentCount - previous.CommentCount) +
		(current.ShareCount - previous.ShareCount)
}

func lifecycleStartAt(tracking *model.SeednotePostTracking) time.Time {
	if tracking.TrackingStartedAt != nil {
		return *tracking.TrackingStartedAt
	}
	return tracking.PublishedMarkedAt
}

func filterLifecycleSnapshots(tracking *model.SeednotePostTracking, snapshots []*model.SeednoteMetricSnapshot) []*model.SeednoteMetricSnapshot {
	startedAt := lifecycleStartAt(tracking)
	filtered := make([]*model.SeednoteMetricSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.TrackingID != tracking.ID {
			continue
		}
		if snapshot.CapturedAt.Before(startedAt) {
			continue
		}
		filtered = append(filtered, snapshot)
	}
	return filtered
}

func (s *SeednoteTrackingService) findSnapshotForDate(ctx context.Context, tracking *model.SeednotePostTracking, capturedDate string) (*model.SeednoteMetricSnapshot, bool) {
	snapshots, err := s.repo.SeednoteMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("tracking_id", tracking.ID).Msg("failed to check seednote same-day snapshot")
		}
		return nil, false
	}
	for _, snapshot := range filterLifecycleSnapshots(tracking, snapshots) {
		if snapshot.CapturedDate == capturedDate {
			return snapshot, true
		}
	}
	return nil, false
}

func (s *SeednoteTrackingService) findPreviousLifecycleSnapshot(ctx context.Context, tracking *model.SeednotePostTracking, capturedAt time.Time) (*model.SeednoteMetricSnapshot, error) {
	snapshots, err := s.repo.SeednoteMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		return nil, err
	}
	var previous *model.SeednoteMetricSnapshot
	for _, snapshot := range filterLifecycleSnapshots(tracking, snapshots) {
		if !snapshot.CapturedAt.Before(capturedAt) {
			continue
		}
		if previous == nil || snapshot.CapturedAt.After(previous.CapturedAt) {
			previous = snapshot
		}
	}
	return previous, nil
}

func (s *SeednoteTrackingService) GetTaskAnalytics(ctx context.Context, userID, taskID string) (*SeednoteAnalytics, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to user")
	}
	tracking, err := s.repo.SeednoteTrackings().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &SeednoteAnalytics{Series: []*SeednoteMetricSeriesItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tracking: %w", err)
	}
	snapshots, err := s.repo.SeednoteMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find snapshots: %w", err)
	}
	snapshots = filterLifecycleSnapshots(tracking, snapshots)

	analytics := &SeednoteAnalytics{
		Tracking: &SeednoteTrackingInfo{
			Status:       tracking.Status,
			NoteURL:      tracking.NoteURL,
			NoteTitle:    tracking.NoteTitle,
			NoteCoverURL: tracking.NoteCoverURL,
			DiscoveredAt: tracking.DiscoveredAt,
			LastRunAt:    tracking.LastRunAt,
			NextRunAt:    tracking.NextRunAt,
			RunCount:     tracking.RunCount,
			StopReason:   tracking.StopReason,
			LastError:    tracking.LastError,
		},
		Series: make([]*SeednoteMetricSeriesItem, 0, len(snapshots)),
	}
	for _, snapshot := range snapshots {
		analytics.Series = append(analytics.Series, &SeednoteMetricSeriesItem{
			CapturedAt:   snapshot.CapturedAt,
			LikeCount:    snapshot.LikeCount,
			CollectCount: snapshot.CollectCount,
			CommentCount: snapshot.CommentCount,
			ShareCount:   snapshot.ShareCount,
			ViewCount:    snapshot.ViewCount,
		})
	}
	if len(snapshots) > 0 {
		latest := snapshots[len(snapshots)-1]
		analytics.Latest = metricInfoFromSnapshot(latest)
		if len(snapshots) > 1 {
			analytics.Deltas = metricDelta(latest, snapshots[len(snapshots)-2])
		} else {
			analytics.Deltas = &SeednoteMetricDelta{}
		}
	}
	return analytics, nil
}

func (s *SeednoteTrackingService) recordTrackingFailure(ctx context.Context, tracking *model.SeednotePostTracking, cause error) error {
	now := time.Now()
	tracking.FailureCount++
	tracking.LastRunAt = &now
	tracking.LastError = cause.Error()
	if tracking.FailureCount >= model.SeednoteTrackingMaxFailures {
		tracking.Status = model.SeednoteTrackingStatusFailed
		tracking.StopReason = model.SeednoteStopReasonTooManyFailures
		tracking.NextRunAt = nil
	} else {
		next := now.Add(24 * time.Hour)
		tracking.NextRunAt = &next
	}
	if err := s.repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking failure: %w", err)
	}
	if tracking.Status == model.SeednoteTrackingStatusTracking {
		return s.enqueueCapture(tracking.ID, 24*time.Hour)
	}
	return nil
}

func (s *SeednoteTrackingService) recordEnqueueFailure(ctx context.Context, tracking *model.SeednotePostTracking, cause error) error {
	tracking.LastError = fmt.Sprintf("enqueue seednote capture: %s", cause.Error())
	if err := s.repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("record enqueue failure: %w", err)
	}
	return cause
}

func (s *SeednoteTrackingService) enqueueCapture(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(SeednoteCaptureMetricsTaskType, payload, delay)
}

func metricInfoFromSnapshot(snapshot *model.SeednoteMetricSnapshot) *SeednoteMetricInfo {
	return &SeednoteMetricInfo{
		LikeCount:    snapshot.LikeCount,
		CollectCount: snapshot.CollectCount,
		CommentCount: snapshot.CommentCount,
		ShareCount:   snapshot.ShareCount,
		ViewCount:    snapshot.ViewCount,
		CapturedAt:   &snapshot.CapturedAt,
	}
}

func metricDelta(current, previous *model.SeednoteMetricSnapshot) *SeednoteMetricDelta {
	return &SeednoteMetricDelta{
		LikeCount:    current.LikeCount - previous.LikeCount,
		CollectCount: current.CollectCount - previous.CollectCount,
		CommentCount: current.CommentCount - previous.CommentCount,
		ShareCount:   current.ShareCount - previous.ShareCount,
	}
}

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

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
)

const (
	RednoteDiscoverTaskType       = "rednote:discover"
	RednoteCaptureMetricsTaskType = "rednote:capture_metrics"
)

type RednotePublicPlatform interface {
	FetchProfilePosts(ctx context.Context, profileURL string) ([]platform.RednotePost, error)
	FetchPostMetrics(ctx context.Context, noteURL string) (*platform.RednotePostMetrics, error)
}

type RednoteTrackingService struct {
	repo     repository.Repository
	platform RednotePublicPlatform
	llm      LLMClient
	enqueuer TaskEnqueuer
	logger   *zerolog.Logger
}

func NewRednoteTrackingService(repo repository.Repository, platform RednotePublicPlatform, llm LLMClient, enqueuer TaskEnqueuer, logger *zerolog.Logger) *RednoteTrackingService {
	return &RednoteTrackingService{
		repo:     repo,
		platform: platform,
		llm:      llm,
		enqueuer: enqueuer,
		logger:   logger,
	}
}

type RednoteAnalytics struct {
	Tracking *RednoteTrackingInfo       `json:"tracking,omitempty"`
	Latest   *RednoteMetricInfo         `json:"latest,omitempty"`
	Deltas   *RednoteMetricDelta        `json:"deltas,omitempty"`
	Series   []*RednoteMetricSeriesItem `json:"series"`
}

type RednoteTrackingInfo struct {
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

type RednoteMetricInfo struct {
	LikeCount    int        `json:"like_count"`
	CollectCount int        `json:"collect_count"`
	CommentCount int        `json:"comment_count"`
	ShareCount   int        `json:"share_count"`
	ViewCount    *int       `json:"view_count"`
	CapturedAt   *time.Time `json:"captured_at,omitempty"`
}

type RednoteMetricDelta struct {
	LikeCount    int `json:"like_count"`
	CollectCount int `json:"collect_count"`
	CommentCount int `json:"comment_count"`
	ShareCount   int `json:"share_count"`
}

type RednoteMetricSeriesItem struct {
	CapturedAt   time.Time `json:"captured_at"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	CommentCount int       `json:"comment_count"`
	ShareCount   int       `json:"share_count"`
	ViewCount    *int      `json:"view_count"`
}

type rednoteAIMatch struct {
	Matched    bool    `json:"matched"`
	NoteURL    string  `json:"note_url"`
	NoteID     string  `json:"note_id"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func (s *RednoteTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("task does not belong to user")
	}
	if task.Type != model.PlatformRednote {
		return nil
	}

	channel, err := s.repo.Channels().FindByID(ctx, task.ChannelID)
	if err != nil {
		return fmt.Errorf("find channel: %w", err)
	}
	if channel.UserID != userID {
		return fmt.Errorf("channel does not belong to user")
	}
	profileURL := strings.TrimSpace(channel.ProfileURL)
	if profileURL == "" {
		return fmt.Errorf("rednote channel profile URL is required")
	}

	now := time.Now()
	nextRun := now.Add(24 * time.Hour)
	existing, err := s.repo.RednoteTrackings().FindByTaskID(ctx, taskID)
	if err == nil {
		existing.Status = model.RednoteTrackingStatusWaitingDiscovery
		existing.ProfileURL = profileURL
		existing.PublishedMarkedAt = now
		existing.NextRunAt = &nextRun
		existing.LastRunAt = nil
		existing.DiscoveredAt = nil
		existing.TrackingStartedAt = nil
		existing.TrackingStoppedAt = nil
		existing.NoteID = ""
		existing.NoteURL = ""
		existing.NoteTitle = ""
		existing.NoteCoverURL = ""
		existing.RunCount = 0
		existing.ConsecutiveLowGrowthCount = 0
		existing.FailureCount = 0
		existing.DiscoveryAttemptCount = 0
		existing.MatchConfidence = 0
		existing.MatchReason = ""
		existing.StopReason = ""
		existing.LastError = ""
		if updateErr := s.repo.RednoteTrackings().Update(ctx, existing); updateErr != nil {
			return fmt.Errorf("update tracking: %w", updateErr)
		}
		return s.enqueueDiscover(existing.ID, 24*time.Hour)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find tracking: %w", err)
	}

	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         task.ChannelID,
		Status:            model.RednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        profileURL,
		PublishedMarkedAt: now,
		NextRunAt:         &nextRun,
	}
	if err := s.repo.RednoteTrackings().Create(ctx, tracking); err != nil {
		return fmt.Errorf("create tracking: %w", err)
	}
	return s.enqueueDiscover(tracking.ID, 24*time.Hour)
}

func (s *RednoteTrackingService) DiscoverPublishedNote(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.RednoteTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.Status != model.RednoteTrackingStatusWaitingDiscovery {
		return nil
	}
	tracking.DiscoveryAttemptCount++

	if s.platform == nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("rednote platform unavailable"))
	}
	posts, err := s.platform.FetchProfilePosts(ctx, tracking.ProfileURL)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("fetch profile posts: %w", err))
	}
	task, err := s.repo.Tasks().FindByID(ctx, tracking.TaskID)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("find task: %w", err))
	}
	match, err := s.matchPublishedNote(ctx, task, posts)
	candidate, ok := findMatchedCandidate(match, posts)
	if err != nil || !validPublishedNoteMatch(match) || !ok {
		return s.recordDiscoveryMiss(ctx, tracking, err)
	}

	now := time.Now()
	tracking.Status = model.RednoteTrackingStatusTracking
	tracking.NoteID = candidate.NoteID
	tracking.NoteURL = candidate.URL
	tracking.NoteTitle = candidate.Title
	tracking.NoteCoverURL = candidate.CoverURL
	tracking.MatchConfidence = match.Confidence
	tracking.MatchReason = match.Reason
	tracking.DiscoveredAt = &now
	tracking.TrackingStartedAt = &now
	tracking.LastError = ""
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update matched tracking: %w", err)
	}
	return s.CaptureMetrics(ctx, tracking.ID)
}

func (s *RednoteTrackingService) matchPublishedNote(ctx context.Context, task *model.Task, posts []platform.RednotePost) (*rednoteAIMatch, error) {
	if s.llm == nil {
		return &rednoteAIMatch{Matched: false, Reason: "AI client unavailable"}, nil
	}
	payload := map[string]any{
		"task": map[string]any{
			"id":     task.ID,
			"title":  task.Title,
			"prompt": task.Prompt,
		},
		"candidates": posts,
	}
	raw, _ := json.Marshal(payload)
	resp, err := s.llm.Complete(ctx, "你是小红书笔记匹配助手，只返回严格 JSON。", string(raw))
	if err != nil {
		return nil, err
	}
	var match rednoteAIMatch
	if err := json.Unmarshal([]byte(resp), &match); err != nil {
		return nil, err
	}
	return &match, nil
}

func validPublishedNoteMatch(match *rednoteAIMatch) bool {
	return match != nil &&
		match.Matched &&
		match.Confidence >= 0.8
}

func findMatchedCandidate(match *rednoteAIMatch, posts []platform.RednotePost) (*platform.RednotePost, bool) {
	if match == nil {
		return nil, false
	}
	noteID := strings.TrimSpace(match.NoteID)
	noteURL := strings.TrimSpace(match.NoteURL)
	if noteID == "" && noteURL == "" {
		return nil, false
	}
	for i := range posts {
		postID := strings.TrimSpace(posts[i].NoteID)
		postURL := strings.TrimSpace(posts[i].URL)
		if noteID != "" && noteURL != "" {
			if postID == noteID && postURL == noteURL {
				return &posts[i], true
			}
			continue
		}
		if noteID != "" && postID == noteID {
			return &posts[i], true
		}
		if noteURL != "" && postURL == noteURL {
			return &posts[i], true
		}
	}
	return nil, false
}

func (s *RednoteTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.RednoteTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.Status != model.RednoteTrackingStatusTracking {
		return nil
	}
	if tracking.NoteURL == "" {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("tracking has no note URL"))
	}
	now := time.Now()
	if snapshot, ok := s.findSnapshotForDate(ctx, tracking, model.RednoteCapturedDate(now)); ok {
		if tracking.LastRunAt != nil && model.RednoteCapturedDate(*tracking.LastRunAt) == snapshot.CapturedDate {
			if tracking.LastError != "" && tracking.NextRunAt != nil {
				if err := s.enqueueCapture(tracking.ID, delayUntil(*tracking.NextRunAt, now)); err != nil {
					return s.recordEnqueueFailure(ctx, tracking, err)
				}
				tracking.LastError = ""
				if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
					return fmt.Errorf("clear enqueue failure: %w", err)
				}
			}
			return nil
		}
		return s.finishCaptureLifecycle(ctx, tracking, snapshot, now, snapshot.CapturedAt)
	}
	if s.platform == nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("rednote platform unavailable"))
	}
	metrics, err := s.platform.FetchPostMetrics(ctx, tracking.NoteURL)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("fetch post metrics: %w", err))
	}
	raw, _ := json.Marshal(metrics)
	snapshot := &model.RednoteMetricSnapshot{
		ID:           uuid.New().String(),
		TrackingID:   tracking.ID,
		TaskID:       tracking.TaskID,
		CapturedAt:   now,
		CapturedDate: model.RednoteCapturedDate(now),
		LikeCount:    metrics.LikeCount,
		CollectCount: metrics.CollectCount,
		CommentCount: metrics.CommentCount,
		ShareCount:   metrics.ShareCount,
		ViewCount:    metrics.ViewCount,
		RawData:      string(raw),
	}
	if err := s.repo.RednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}

	return s.finishCaptureLifecycle(ctx, tracking, snapshot, now, now)
}

func (s *RednoteTrackingService) finishCaptureLifecycle(ctx context.Context, tracking *model.RednotePostTracking, snapshot *model.RednoteMetricSnapshot, now, previousBefore time.Time) error {
	tracking.RunCount++
	tracking.FailureCount = 0
	tracking.LastRunAt = &now
	tracking.LastError = ""
	previous, prevErr := s.findPreviousLifecycleSnapshot(ctx, tracking, previousBefore)
	if prevErr == nil && previous != nil {
		growth := totalGrowth(snapshot, previous)
		if growth < model.RednoteLowGrowthThreshold {
			tracking.ConsecutiveLowGrowthCount++
		} else {
			tracking.ConsecutiveLowGrowthCount = 0
		}
	} else if prevErr != nil && !errors.Is(prevErr, gorm.ErrRecordNotFound) && s.logger != nil {
		s.logger.Warn().Err(prevErr).Str("tracking_id", tracking.ID).Msg("failed to load previous rednote metrics")
	}

	if s.shouldStopTracking(tracking, now) {
		tracking.Status = model.RednoteTrackingStatusStopped
		tracking.TrackingStoppedAt = &now
		tracking.NextRunAt = nil
		if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
			return fmt.Errorf("update stopped tracking: %w", err)
		}
		return nil
	}
	next := now.Add(24 * time.Hour)
	tracking.NextRunAt = &next
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
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

func (s *RednoteTrackingService) shouldStopTracking(tracking *model.RednotePostTracking, now time.Time) bool {
	if now.Sub(tracking.PublishedMarkedAt) >= model.RednoteTrackingMaxDays*24*time.Hour {
		tracking.StopReason = model.RednoteStopReasonMaxDurationReached
		return true
	}
	if shouldStopForLowGrowth(tracking.ConsecutiveLowGrowthCount, model.RednoteLowGrowthConsecutiveCaptures) {
		tracking.StopReason = model.RednoteStopReasonLowGrowth
		return true
	}
	return false
}

func shouldStopForLowGrowth(count, threshold int) bool {
	return count >= threshold
}

func totalGrowth(current, previous *model.RednoteMetricSnapshot) int {
	return (current.LikeCount - previous.LikeCount) +
		(current.CollectCount - previous.CollectCount) +
		(current.CommentCount - previous.CommentCount) +
		(current.ShareCount - previous.ShareCount)
}

func lifecycleStartAt(tracking *model.RednotePostTracking) time.Time {
	if tracking.TrackingStartedAt != nil {
		return *tracking.TrackingStartedAt
	}
	return tracking.PublishedMarkedAt
}

func filterLifecycleSnapshots(tracking *model.RednotePostTracking, snapshots []*model.RednoteMetricSnapshot) []*model.RednoteMetricSnapshot {
	startedAt := lifecycleStartAt(tracking)
	filtered := make([]*model.RednoteMetricSnapshot, 0, len(snapshots))
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

func (s *RednoteTrackingService) findSnapshotForDate(ctx context.Context, tracking *model.RednotePostTracking, capturedDate string) (*model.RednoteMetricSnapshot, bool) {
	snapshots, err := s.repo.RednoteMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("tracking_id", tracking.ID).Msg("failed to check rednote same-day snapshot")
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

func (s *RednoteTrackingService) findPreviousLifecycleSnapshot(ctx context.Context, tracking *model.RednotePostTracking, capturedAt time.Time) (*model.RednoteMetricSnapshot, error) {
	snapshots, err := s.repo.RednoteMetricSnapshots().FindByTaskID(ctx, tracking.TaskID)
	if err != nil {
		return nil, err
	}
	var previous *model.RednoteMetricSnapshot
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

func (s *RednoteTrackingService) GetTaskAnalytics(ctx context.Context, userID, taskID string) (*RednoteAnalytics, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task does not belong to user")
	}
	tracking, err := s.repo.RednoteTrackings().FindByTaskID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &RednoteAnalytics{Series: []*RednoteMetricSeriesItem{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tracking: %w", err)
	}
	snapshots, err := s.repo.RednoteMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find snapshots: %w", err)
	}
	snapshots = filterLifecycleSnapshots(tracking, snapshots)

	analytics := &RednoteAnalytics{
		Tracking: &RednoteTrackingInfo{
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
		Series: make([]*RednoteMetricSeriesItem, 0, len(snapshots)),
	}
	for _, snapshot := range snapshots {
		analytics.Series = append(analytics.Series, &RednoteMetricSeriesItem{
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
			analytics.Deltas = &RednoteMetricDelta{}
		}
	}
	return analytics, nil
}

func (s *RednoteTrackingService) recordDiscoveryMiss(ctx context.Context, tracking *model.RednotePostTracking, cause error) error {
	now := time.Now()
	tracking.LastRunAt = &now
	if cause != nil {
		tracking.LastError = cause.Error()
	} else {
		tracking.LastError = "published note was not matched"
	}
	if tracking.DiscoveryAttemptCount >= model.RednoteDiscoveryMaxAttempts {
		tracking.Status = model.RednoteTrackingStatusFailed
		tracking.StopReason = model.RednoteStopReasonDiscoveryTimeout
		tracking.NextRunAt = nil
	} else {
		tracking.Status = model.RednoteTrackingStatusWaitingDiscovery
		next := now.Add(24 * time.Hour)
		tracking.NextRunAt = &next
	}
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update discovery retry: %w", err)
	}
	if tracking.Status == model.RednoteTrackingStatusWaitingDiscovery {
		return s.enqueueDiscover(tracking.ID, 24*time.Hour)
	}
	return nil
}

func (s *RednoteTrackingService) recordTrackingFailure(ctx context.Context, tracking *model.RednotePostTracking, cause error) error {
	now := time.Now()
	tracking.FailureCount++
	tracking.LastRunAt = &now
	tracking.LastError = cause.Error()
	if tracking.FailureCount >= model.RednoteTrackingMaxFailures {
		tracking.Status = model.RednoteTrackingStatusFailed
		tracking.StopReason = model.RednoteStopReasonTooManyFailures
		tracking.NextRunAt = nil
	} else {
		next := now.Add(24 * time.Hour)
		tracking.NextRunAt = &next
	}
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking failure: %w", err)
	}
	if tracking.Status == model.RednoteTrackingStatusWaitingDiscovery {
		return s.enqueueDiscover(tracking.ID, 24*time.Hour)
	}
	if tracking.Status == model.RednoteTrackingStatusTracking {
		return s.enqueueCapture(tracking.ID, 24*time.Hour)
	}
	return nil
}

func (s *RednoteTrackingService) recordEnqueueFailure(ctx context.Context, tracking *model.RednotePostTracking, cause error) error {
	tracking.LastError = fmt.Sprintf("enqueue rednote capture: %s", cause.Error())
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("record enqueue failure: %w", err)
	}
	return cause
}

func (s *RednoteTrackingService) enqueueDiscover(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(RednoteDiscoverTaskType, payload, delay)
}

func (s *RednoteTrackingService) enqueueCapture(trackingID string, delay time.Duration) error {
	if s.enqueuer == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"tracking_id": trackingID})
	return s.enqueuer.EnqueueIn(RednoteCaptureMetricsTaskType, payload, delay)
}

func metricInfoFromSnapshot(snapshot *model.RednoteMetricSnapshot) *RednoteMetricInfo {
	return &RednoteMetricInfo{
		LikeCount:    snapshot.LikeCount,
		CollectCount: snapshot.CollectCount,
		CommentCount: snapshot.CommentCount,
		ShareCount:   snapshot.ShareCount,
		ViewCount:    snapshot.ViewCount,
		CapturedAt:   &snapshot.CapturedAt,
	}
}

func metricDelta(current, previous *model.RednoteMetricSnapshot) *RednoteMetricDelta {
	return &RednoteMetricDelta{
		LikeCount:    current.LikeCount - previous.LikeCount,
		CollectCount: current.CollectCount - previous.CollectCount,
		CommentCount: current.CommentCount - previous.CommentCount,
		ShareCount:   current.ShareCount - previous.ShareCount,
	}
}

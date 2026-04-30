# Rednote Post Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build automatic post-publication RedNote tracking: discover a newly published note from the account homepage, bind it to the local task, collect public metrics daily, and show trends on the task detail page.

**Architecture:** Add a focused tracking subsystem beside the existing task/channel flow. Backend state lives in dedicated GORM models and repositories; `RednoteTrackingService` orchestrates discovery, AI matching, metric capture, stop rules, and scheduling. Studio reads analytics through a task-scoped endpoint and renders a compact panel inside `TaskDetailPage`.

**Tech Stack:** Go, Fiber v3, GORM, Asynq, zerolog, existing OpenAI-compatible `service.LLMClient`, React, TanStack Query, Recharts, Vitest.

---

## File Structure

Create:

- `server/model/rednote_tracking.go`: tracking and metric snapshot models, statuses, stop reasons.
- `server/repository/rednote_tracking.go`: GORM repository implementations for tracking records and snapshots.
- `server/platform/rednote_metrics_test.go`: public RedNote note ID, metric parsing, profile candidate parsing, note metrics parsing tests.
- `server/service/rednote_tracking.go`: orchestration service, AI match parsing, stop evaluation, analytics DTOs.
- `server/service/rednote_tracking_test.go`: service tests using fake repository data, fake platform, fake AI, and fake enqueuer.
- `server/handler/rednote_analytics.go`: task-scoped analytics handler method.
- `server/handler/rednote_analytics_test.go`: endpoint ownership and response tests.
- `studio/src/types/rednote-analytics.ts`: frontend analytics response types.
- `studio/src/lib/api/rednote-analytics.ts`: API client for task analytics.
- `studio/src/components/tasks/RednoteAnalyticsPanel.tsx`: RedNote task analytics UI.

Modify:

- `server/model/model.go`: migrate new models.
- `server/repository/repository.go`: expose new repositories.
- `server/repository/repository_test.go`: assert new repositories are wired.
- `server/platform/rednote.go`: add public profile post and note metric parsing APIs.
- `server/scheduler/scheduler.go`: add `rednote:discover` and `rednote:capture_metrics` task types and handlers.
- `server/main.go`: instantiate tracking service, wire Asynq handlers, pass handler dependencies.
- `server/router/router.go`: add analytics endpoint and service fields.
- `server/service/task.go`: call tracking service from `SetPublished` for RedNote published tasks.
- `server/service/task_test.go`: verify RedNote publish creates tracking and non-RedNote publish does not.
- `studio/src/lib/api/index.ts`: export rednote analytics API.
- `studio/src/pages/TaskDetailPage.tsx`: embed analytics panel only for published RedNote tasks.
- `studio/src/lib/query-keys.ts`: add stable analytics query key.

---

### Task 1: Add Tracking Models and Migration

**Files:**

- Create: `server/model/rednote_tracking.go`
- Modify: `server/model/model.go`
- Test: `server/repository/repository_test.go`

- [ ] **Step 1: Write the failing migration test**

Add this test to `server/repository/repository_test.go`:

```go
func TestNew_RednoteTrackingRepositories(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)

	if repo.RednoteTrackings() == nil {
		t.Fatal("RednoteTrackings() should not be nil")
	}
	if repo.RednoteMetricSnapshots() == nil {
		t.Fatal("RednoteMetricSnapshots() should not be nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./server/repository -run TestNew_RednoteTrackingRepositories -count=1
```

Expected: compile failure because `Repository` does not define `RednoteTrackings` or `RednoteMetricSnapshots`.

- [ ] **Step 3: Add tracking models and constants**

Create `server/model/rednote_tracking.go`:

```go
package model

import "time"

const (
	RednoteTrackingStatusWaitingDiscovery = "waiting_discovery"
	RednoteTrackingStatusTracking         = "tracking"
	RednoteTrackingStatusStopped          = "stopped"
	RednoteTrackingStatusFailed           = "failed"
)

const (
	RednoteStopReasonMaxDurationReached = "max_duration_reached"
	RednoteStopReasonLowGrowth          = "low_growth"
	RednoteStopReasonDiscoveryTimeout   = "discovery_timeout"
	RednoteStopReasonTooManyFailures    = "too_many_failures"
	RednoteStopReasonManualStop         = "manual_stop"
)

const (
	RednoteTrackingMaxDays              = 14
	RednoteDiscoveryMaxAttempts         = 7
	RednoteTrackingMaxFailures          = 5
	RednoteLowGrowthThreshold           = 3
	RednoteLowGrowthConsecutiveCaptures = 3
)

type RednotePostTracking struct {
	ID                        string     `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID                    string     `gorm:"type:char(36);uniqueIndex;not null" json:"task_id"`
	UserID                    string     `gorm:"type:char(36);index;not null" json:"user_id"`
	ChannelID                 string     `gorm:"type:char(36);index;not null" json:"channel_id"`
	Status                    string     `gorm:"type:varchar(32);index;not null" json:"status"`
	ProfileURL                string     `gorm:"type:varchar(500)" json:"profile_url"`
	NoteID                    string     `gorm:"type:varchar(100);index" json:"note_id"`
	NoteURL                   string     `gorm:"type:varchar(500)" json:"note_url"`
	NoteTitle                 string     `gorm:"type:varchar(500)" json:"note_title"`
	NoteCoverURL              string     `gorm:"type:varchar(500)" json:"note_cover_url"`
	PublishedMarkedAt         time.Time  `gorm:"index" json:"published_marked_at"`
	DiscoveredAt              *time.Time `gorm:"index" json:"discovered_at,omitempty"`
	TrackingStartedAt         *time.Time `gorm:"index" json:"tracking_started_at,omitempty"`
	TrackingStoppedAt         *time.Time `gorm:"index" json:"tracking_stopped_at,omitempty"`
	NextRunAt                 *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	LastRunAt                 *time.Time `gorm:"index" json:"last_run_at,omitempty"`
	RunCount                  int        `gorm:"default:0" json:"run_count"`
	ConsecutiveLowGrowthCount int        `gorm:"default:0" json:"consecutive_low_growth_count"`
	FailureCount              int        `gorm:"default:0" json:"failure_count"`
	DiscoveryAttemptCount     int        `gorm:"default:0" json:"discovery_attempt_count"`
	MatchConfidence           float64    `gorm:"default:0" json:"match_confidence"`
	MatchReason               string     `gorm:"type:text" json:"match_reason"`
	StopReason                string     `gorm:"type:varchar(64)" json:"stop_reason"`
	LastError                 string     `gorm:"type:text" json:"last_error"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

func (RednotePostTracking) TableName() string { return "rednote_post_trackings" }

type RednoteMetricSnapshot struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	TrackingID   string     `gorm:"type:char(36);uniqueIndex:idx_rednote_tracking_date,priority:1;index;not null" json:"tracking_id"`
	TaskID       string     `gorm:"type:char(36);index;not null" json:"task_id"`
	CapturedAt   time.Time  `gorm:"index" json:"captured_at"`
	CapturedDate string     `gorm:"type:char(10);uniqueIndex:idx_rednote_tracking_date,priority:2;not null" json:"captured_date"`
	LikeCount    int        `json:"like_count"`
	CollectCount int        `json:"collect_count"`
	CommentCount int        `json:"comment_count"`
	ShareCount   int        `json:"share_count"`
	ViewCount     *int       `json:"view_count,omitempty"`
	RawData      string     `gorm:"type:json" json:"raw_data,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (RednoteMetricSnapshot) TableName() string { return "rednote_metric_snapshots" }

func RednoteCapturedDate(t time.Time) string {
	return t.Format("2006-01-02")
}
```

- [ ] **Step 4: Add models to AutoMigrate**

Modify `server/model/model.go` so `AutoMigrate` includes the new models:

```go
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Channel{},
		&Plan{},
		&Task{},
		&TaskFile{},
		&CreditTransaction{},
		&APIKey{},
		&Feedback{},
		&UserModelConfig{},
		&RednotePostTracking{},
		&RednoteMetricSnapshot{},
	)
	if err != nil {
		return err
	}

	return nil
}
```

- [ ] **Step 5: Run the model package tests**

Run:

```bash
go test ./server/model -count=1
```

Expected: pass or report no test files.

- [ ] **Step 6: Commit**

```bash
git add server/model/rednote_tracking.go server/model/model.go server/repository/repository_test.go
git commit -m "feat: add rednote tracking models"
```

---

### Task 2: Add Tracking Repositories

**Files:**

- Create: `server/repository/rednote_tracking.go`
- Modify: `server/repository/repository.go`
- Test: `server/repository/repository_test.go`

- [ ] **Step 1: Write repository CRUD tests**

Add these tests to `server/repository/repository_test.go`:

```go
func TestRednoteTrackingRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	nextRun := now.Add(24 * time.Hour)

	tracking := &model.RednotePostTracking{
		ID:                "tracking-1",
		TaskID:            "task-1",
		UserID:            "user-1",
		ChannelID:         "channel-1",
		Status:            model.RednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/abc",
		PublishedMarkedAt: now,
		NextRunAt:         &nextRun,
	}

	if err := repo.RednoteTrackings().Create(ctx, tracking); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.RednoteTrackings().FindByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if found.ID != "tracking-1" {
		t.Fatalf("ID = %q, want tracking-1", found.ID)
	}

	due, err := repo.RednoteTrackings().FindDue(ctx, nextRun.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("FindDue: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due length = %d, want 1", len(due))
	}

	found.Status = model.RednoteTrackingStatusTracking
	found.NoteID = "note-1"
	if err := repo.RednoteTrackings().Update(ctx, found); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(ctx, "tracking-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.RednoteTrackingStatusTracking || updated.NoteID != "note-1" {
		t.Fatalf("updated tracking = %+v", updated)
	}
}

func TestRednoteMetricSnapshotRepository_UpsertAndSeries(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()
	captured := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)

	first := &model.RednoteMetricSnapshot{
		ID:           "snapshot-1",
		TrackingID:   "tracking-1",
		TaskID:       "task-1",
		CapturedAt:   captured,
		CapturedDate: model.RednoteCapturedDate(captured),
		LikeCount:    10,
		CollectCount: 2,
		CommentCount: 1,
		ShareCount:   0,
	}
	if err := repo.RednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := *first
	second.ID = "snapshot-2"
	second.LikeCount = 15
	if err := repo.RednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, &second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	series, err := repo.RednoteMetricSnapshots().FindByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0].LikeCount != 15 {
		t.Fatalf("LikeCount = %d, want 15", series[0].LikeCount)
	}

	latest, err := repo.RednoteMetricSnapshots().FindLatestByTrackingID(ctx, "tracking-1")
	if err != nil {
		t.Fatalf("FindLatestByTrackingID: %v", err)
	}
	if latest.ID != "snapshot-1" {
		t.Fatalf("latest ID = %q, want snapshot-1 because upsert keeps primary ID", latest.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./server/repository -run 'TestNew_RednoteTrackingRepositories|TestRednoteTrackingRepository|TestRednoteMetricSnapshotRepository' -count=1
```

Expected: compile failure because repository interfaces and methods are missing.

- [ ] **Step 3: Extend repository interfaces and constructor**

Modify `server/repository/repository.go`:

```go
type Repository interface {
	Users() UserRepository
	Sessions() SessionRepository
	Plans() PlanRepository
	Tasks() TaskRepository
	TaskFiles() TaskFileRepository
	Channels() ChannelRepository
	Credits() CreditRepository
	APIKeys() APIKeyRepository
	Feedbacks() FeedbackRepository
	ModelConfigs() ModelConfigRepository
	RednoteTrackings() RednoteTrackingRepository
	RednoteMetricSnapshots() RednoteMetricSnapshotRepository
	WithTx(ctx context.Context, fn func(Repository) error) error
	Close() error
}

type RednoteTrackingRepository interface {
	Create(ctx context.Context, tracking *model.RednotePostTracking) error
	FindByTaskID(ctx context.Context, taskID string) (*model.RednotePostTracking, error)
	FindByID(ctx context.Context, id string) (*model.RednotePostTracking, error)
	FindDue(ctx context.Context, now time.Time, limit int) ([]*model.RednotePostTracking, error)
	Update(ctx context.Context, tracking *model.RednotePostTracking) error
	UpdateStatus(ctx context.Context, id, status string) error
}

type RednoteMetricSnapshotRepository interface {
	Create(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error
	UpsertByTrackingAndDate(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error
	FindByTaskID(ctx context.Context, taskID string) ([]*model.RednoteMetricSnapshot, error)
	FindLatestByTrackingID(ctx context.Context, trackingID string) (*model.RednoteMetricSnapshot, error)
	FindPreviousByTrackingID(ctx context.Context, trackingID string, capturedAt time.Time) (*model.RednoteMetricSnapshot, error)
}
```

Add fields to both `repository` and `txRepository`, initialize them in `New` and `newTxRepository`, and add accessors:

```go
func (r *repository) RednoteTrackings() RednoteTrackingRepository {
	return r.rednoteTrackings
}

func (r *repository) RednoteMetricSnapshots() RednoteMetricSnapshotRepository {
	return r.rednoteMetricSnapshots
}

func (r *txRepository) RednoteTrackings() RednoteTrackingRepository {
	return r.rednoteTrackings
}

func (r *txRepository) RednoteMetricSnapshots() RednoteMetricSnapshotRepository {
	return r.rednoteMetricSnapshots
}
```

- [ ] **Step 4: Implement repositories**

Create `server/repository/rednote_tracking.go`:

```go
package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type rednoteTrackingRepository struct {
	db *gorm.DB
}

func newRednoteTrackingRepository(db *gorm.DB) RednoteTrackingRepository {
	return &rednoteTrackingRepository{db: db}
}

func (r *rednoteTrackingRepository) Create(ctx context.Context, tracking *model.RednotePostTracking) error {
	return r.db.WithContext(ctx).Create(tracking).Error
}

func (r *rednoteTrackingRepository) FindByTaskID(ctx context.Context, taskID string) (*model.RednotePostTracking, error) {
	var tracking model.RednotePostTracking
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *rednoteTrackingRepository) FindByID(ctx context.Context, id string) (*model.RednotePostTracking, error) {
	var tracking model.RednotePostTracking
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tracking).Error; err != nil {
		return nil, err
	}
	return &tracking, nil
}

func (r *rednoteTrackingRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.RednotePostTracking, error) {
	var trackings []*model.RednotePostTracking
	q := r.db.WithContext(ctx).
		Where("next_run_at IS NOT NULL AND next_run_at <= ?", now).
		Where("status IN ?", []string{
			model.RednoteTrackingStatusWaitingDiscovery,
			model.RednoteTrackingStatusTracking,
		}).
		Order("next_run_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&trackings).Error; err != nil {
		return nil, err
	}
	return trackings, nil
}

func (r *rednoteTrackingRepository) Update(ctx context.Context, tracking *model.RednotePostTracking) error {
	return r.db.WithContext(ctx).Save(tracking).Error
}

func (r *rednoteTrackingRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.RednotePostTracking{}).
		Where("id = ?", id).
		Update("status", status).Error
}

type rednoteMetricSnapshotRepository struct {
	db *gorm.DB
}

func newRednoteMetricSnapshotRepository(db *gorm.DB) RednoteMetricSnapshotRepository {
	return &rednoteMetricSnapshotRepository{db: db}
}

func (r *rednoteMetricSnapshotRepository) Create(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *rednoteMetricSnapshotRepository) UpsertByTrackingAndDate(ctx context.Context, snapshot *model.RednoteMetricSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tracking_id"},
			{Name: "captured_date"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"captured_at",
			"like_count",
			"collect_count",
			"comment_count",
			"share_count",
			"view_count",
			"raw_data",
		}),
	}).Create(snapshot).Error
}

func (r *rednoteMetricSnapshotRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.RednoteMetricSnapshot, error) {
	var snapshots []*model.RednoteMetricSnapshot
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("captured_at ASC").
		Find(&snapshots).Error
	return snapshots, err
}

func (r *rednoteMetricSnapshotRepository) FindLatestByTrackingID(ctx context.Context, trackingID string) (*model.RednoteMetricSnapshot, error) {
	var snapshot model.RednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ?", trackingID).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *rednoteMetricSnapshotRepository) FindPreviousByTrackingID(ctx context.Context, trackingID string, capturedAt time.Time) (*model.RednoteMetricSnapshot, error) {
	var snapshot model.RednoteMetricSnapshot
	if err := r.db.WithContext(ctx).
		Where("tracking_id = ? AND captured_at < ?", trackingID, capturedAt).
		Order("captured_at DESC").
		First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}
```

- [ ] **Step 5: Run repository tests**

Run:

```bash
go test ./server/repository -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add server/repository/repository.go server/repository/rednote_tracking.go server/repository/repository_test.go
git commit -m "feat: add rednote tracking repositories"
```

---

### Task 3: Add RedNote Public Parsing

**Files:**

- Modify: `server/platform/rednote.go`
- Create: `server/platform/rednote_metrics_test.go`

- [ ] **Step 1: Write parsing tests**

Create `server/platform/rednote_metrics_test.go`:

```go
package platform

import "testing"

func TestExtractRednoteNoteID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"explore URL", "https://www.xiaohongshu.com/explore/65f123abc456?xsec_token=abc", "65f123abc456"},
		{"discovery item URL", "https://www.xiaohongshu.com/discovery/item/65f123abc456", "65f123abc456"},
		{"relative URL", "/explore/65f123abc456", "65f123abc456"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractRednoteNoteID(tt.raw); got != tt.want {
				t.Fatalf("ExtractRednoteNoteID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeRednoteMetricCount(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"1.2万", 12000},
		{"3千", 3000},
		{"2,345", 2345},
		{"88", 88},
		{"点赞 456", 456},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeRednoteMetricCount(tt.raw); got != tt.want {
				t.Fatalf("NormalizeRednoteMetricCount(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseRednotePostsIncludesNoteIDAndMetrics(t *testing.T) {
	html := `
	<a href="/explore/65f123abc456">
		<img src="https://img.example/cover.jpg">
		<span>早起效率翻倍的方法</span>
		<span>点赞 1.2万</span>
		<span>收藏 300</span>
		<span>评论 45</span>
		<span>分享 6</span>
	</a>`

	posts := parseRednotePosts(html)
	if len(posts) != 1 {
		t.Fatalf("len(posts) = %d, want 1", len(posts))
	}
	post := posts[0]
	if post.NoteID != "65f123abc456" {
		t.Fatalf("NoteID = %q, want 65f123abc456", post.NoteID)
	}
	if post.URL != "https://www.xiaohongshu.com/explore/65f123abc456" {
		t.Fatalf("URL = %q", post.URL)
	}
	if post.LikeCount != 12000 || post.CollectCount != 300 || post.CommentCount != 45 || post.ShareCount != 6 {
		t.Fatalf("metrics = %+v", post)
	}
}

func TestParseRednotePostMetrics(t *testing.T) {
	html := `
	<html>
		<body>
			<div>点赞 123</div>
			<div>收藏 45</div>
			<div>评论 6</div>
			<div>分享 2</div>
		</body>
	</html>`

	metrics := parseRednotePostMetrics(html)
	if metrics.LikeCount != 123 || metrics.CollectCount != 45 || metrics.CommentCount != 6 || metrics.ShareCount != 2 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics.ViewCount != nil {
		t.Fatalf("ViewCount = %v, want nil", *metrics.ViewCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./server/platform -run 'TestExtractRednoteNoteID|TestNormalizeRednoteMetricCount|TestParseRednotePostsIncludesNoteIDAndMetrics|TestParseRednotePostMetrics' -count=1
```

Expected: compile failure for missing exported functions and missing `RednotePost.NoteID`.

- [ ] **Step 3: Extend platform types and parsing helpers**

Modify `server/platform/rednote.go` so `RednotePost` has `NoteID`. If the type is already lower in the file, update the existing definition rather than adding a second one:

```go
type RednotePost struct {
	Title           string `json:"title"`
	URL             string `json:"url"`
	NoteID          string `json:"note_id"`
	CoverURL        string `json:"cover_url"`
	LikeCount       int    `json:"like_count"`
	CollectCount    int    `json:"collect_count"`
	CommentCount    int    `json:"comment_count"`
	ShareCount      int    `json:"share_count"`
	EngagementScore int    `json:"engagement_score"`
}

type RednotePostMetrics struct {
	LikeCount    int  `json:"like_count"`
	CollectCount int  `json:"collect_count"`
	CommentCount int  `json:"comment_count"`
	ShareCount   int  `json:"share_count"`
	ViewCount     *int `json:"view_count,omitempty"`
}
```

Add exported helpers:

```go
func ExtractRednoteNoteID(raw string) string {
	raw = strings.ReplaceAll(raw, `\u002F`, "/")
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/explore/([^/?#]+)`),
		regexp.MustCompile(`/discovery/item/([^/?#]+)`),
	}
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(raw)
		if len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func NormalizeRednoteMetricCount(text string) int {
	text = strings.TrimSpace(strings.ReplaceAll(text, ",", ""))
	if text == "" {
		return 0
	}
	match := rednoteNumberPattern.FindString(text)
	if match == "" {
		return 0
	}
	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	switch {
	case strings.Contains(text, "万"):
		value *= 10000
	case strings.Contains(text, "千"):
		value *= 1000
	}
	return int(value)
}
```

Update `parseMetricAfterLabels` to call `NormalizeRednoteMetricCount` for the matched fragment. Update `parseRednotePosts` so it sets `NoteID: ExtractRednoteNoteID(postURL)`.

- [ ] **Step 4: Add profile and note metric fetch methods**

Add methods to `server/platform/rednote.go`:

```go
func (p *RednoteProvider) FetchProfilePosts(ctx context.Context, profileURL string) ([]RednotePost, error) {
	profile, err := p.FetchProfile(ctx, profileURL)
	if err != nil {
		return nil, err
	}
	posts, ok := profile.RawData["posts"].([]RednotePost)
	if !ok {
		return []RednotePost{}, nil
	}
	return posts, nil
}

func (p *RednoteProvider) FetchPostMetrics(ctx context.Context, noteURL string) (*RednotePostMetrics, error) {
	if strings.TrimSpace(noteURL) == "" {
		return nil, fmt.Errorf("note URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, noteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	setRednoteHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch note page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, rednoteMaxProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(body) > rednoteMaxProfileBytes {
		return nil, fmt.Errorf("note page is too large")
	}
	metrics := parseRednotePostMetrics(string(body))
	return &metrics, nil
}

func parseRednotePostMetrics(html string) RednotePostMetrics {
	text := normalizeRednoteText(rednoteTagPattern.ReplaceAllString(html, " "))
	return RednotePostMetrics{
		LikeCount:    parseMetricAfterLabels(text, "点赞", "赞", "喜欢", "like"),
		CollectCount: parseMetricAfterLabels(text, "收藏", "collect"),
		CommentCount: parseMetricAfterLabels(text, "评论", "comment"),
		ShareCount:   parseMetricAfterLabels(text, "分享", "share"),
		ViewCount:     nil,
	}
}
```

- [ ] **Step 5: Run platform tests**

Run:

```bash
go test ./server/platform -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add server/platform/rednote.go server/platform/rednote_metrics_test.go
git commit -m "feat: parse rednote public metrics"
```

---

### Task 4: Add RedNote Tracking Service

**Files:**

- Create: `server/service/rednote_tracking.go`
- Create: `server/service/rednote_tracking_test.go`

- [ ] **Step 1: Write service tests**

Create `server/service/rednote_tracking_test.go`:

```go
package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/platform"
	"github.com/royalrick/anbanwriter/server/repository"
)

type fakeRednotePlatform struct {
	posts   []platform.RednotePost
	metrics platform.RednotePostMetrics
	err     error
}

func (f *fakeRednotePlatform) FetchProfilePosts(ctx context.Context, profileURL string) ([]platform.RednotePost, error) {
	return f.posts, f.err
}

func (f *fakeRednotePlatform) FetchPostMetrics(ctx context.Context, noteURL string) (*platform.RednotePostMetrics, error) {
	return &f.metrics, f.err
}

type fakeRednoteLLM struct {
	response string
	err      error
}

func (f *fakeRednoteLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return f.response, f.err
}

type fakeTrackingEnqueuer struct {
	delayed []string
	now     []string
}

func (f *fakeTrackingEnqueuer) Enqueue(taskType string, payload []byte) error {
	f.now = append(f.now, taskType)
	return nil
}

func (f *fakeTrackingEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	f.delayed = append(f.delayed, taskType)
	return nil
}

func setupRednoteTrackingServiceTest(t *testing.T) (*RednoteTrackingService, repository.Repository, *fakeTrackingEnqueuer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enq := &fakeTrackingEnqueuer{}
	svc := NewRednoteTrackingService(repo, &fakeRednotePlatform{}, &fakeRednoteLLM{}, enq, &logger)
	return svc, repo, enq
}

func createRednoteTrackingFixtures(t *testing.T, repo repository.Repository) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Nickname: "User", Password: "hashed"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Channels().Create(ctx, &model.Channel{
		ID:         channelID,
		UserID:     userID,
		Platform:   model.PlatformRednote,
		Name:       "RedNote",
		ProfileURL: "https://www.xiaohongshu.com/user/profile/profile-1",
		Status:     model.ChannelStatusActive,
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformRednote,
		Status:    model.TaskStatusCompleted,
		Title:     "早起效率翻倍的方法",
		Prompt:    "早起效率",
		Published: true,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return userID, channelID, taskID
}

func TestRednoteTrackingService_EnsureTrackingForPublishedTask(t *testing.T) {
	svc, repo, enq := setupRednoteTrackingServiceTest(t)
	userID, _, taskID := createRednoteTrackingFixtures(t, repo)

	if err := svc.EnsureTrackingForPublishedTask(context.Background(), userID, taskID); err != nil {
		t.Fatalf("EnsureTrackingForPublishedTask: %v", err)
	}

	tracking, err := repo.RednoteTrackings().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID: %v", err)
	}
	if tracking.Status != model.RednoteTrackingStatusWaitingDiscovery {
		t.Fatalf("Status = %q", tracking.Status)
	}
	if len(enq.delayed) != 1 || enq.delayed[0] != "rednote:discover" {
		t.Fatalf("delayed jobs = %+v", enq.delayed)
	}
}

func TestRednoteTrackingService_DiscoverPublishedNoteBindsAndCaptures(t *testing.T) {
	svc, repo, _ := setupRednoteTrackingServiceTest(t)
	userID, channelID, taskID := createRednoteTrackingFixtures(t, repo)
	platformFake := &fakeRednotePlatform{
		posts: []platform.RednotePost{
			{Title: "早起效率翻倍的方法", URL: "https://www.xiaohongshu.com/explore/note-1", NoteID: "note-1", CoverURL: "https://img.example/1.jpg"},
		},
		metrics: platform.RednotePostMetrics{LikeCount: 10, CollectCount: 3, CommentCount: 1, ShareCount: 0},
	}
	llmFake := &fakeRednoteLLM{response: `{"matched":true,"note_url":"https://www.xiaohongshu.com/explore/note-1","note_id":"note-1","confidence":0.91,"reason":"标题和主题一致"}`}
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc = NewRednoteTrackingService(repo, platformFake, llmFake, &fakeTrackingEnqueuer{}, &logger)

	tracking := &model.RednotePostTracking{
		ID:                uuid.New().String(),
		TaskID:            taskID,
		UserID:            userID,
		ChannelID:         channelID,
		Status:            model.RednoteTrackingStatusWaitingDiscovery,
		ProfileURL:        "https://www.xiaohongshu.com/user/profile/profile-1",
		PublishedMarkedAt: time.Now().Add(-24 * time.Hour),
	}
	if err := repo.RednoteTrackings().Create(context.Background(), tracking); err != nil {
		t.Fatalf("create tracking: %v", err)
	}

	if err := svc.DiscoverPublishedNote(context.Background(), tracking.ID); err != nil {
		t.Fatalf("DiscoverPublishedNote: %v", err)
	}

	updated, err := repo.RednoteTrackings().FindByID(context.Background(), tracking.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated.Status != model.RednoteTrackingStatusTracking || updated.NoteID != "note-1" {
		t.Fatalf("tracking = %+v", updated)
	}
	snapshots, err := repo.RednoteMetricSnapshots().FindByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("FindByTaskID snapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].LikeCount != 10 {
		t.Fatalf("snapshots = %+v", snapshots)
	}
}

func TestRednoteTrackingService_ShouldStopForLowGrowth(t *testing.T) {
	if !shouldStopForLowGrowth(3, model.RednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("expected low growth stop")
	}
	if shouldStopForLowGrowth(2, model.RednoteLowGrowthConsecutiveCaptures) {
		t.Fatal("did not expect low growth stop")
	}
}
```

- [ ] **Step 2: Run service tests to verify they fail**

Run:

```bash
go test ./server/service -run 'TestRednoteTrackingService' -count=1
```

Expected: compile failure because `RednoteTrackingService` does not exist.

- [ ] **Step 3: Implement service interfaces and constructor**

Create `server/service/rednote_tracking.go` with the package, imports, interfaces, DTOs, and constructor:

```go
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
	ViewCount     *int       `json:"view_count"`
	CapturedAt    *time.Time `json:"captured_at,omitempty"`
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
	ViewCount     *int      `json:"view_count"`
}

type rednoteAIMatch struct {
	Matched    bool    `json:"matched"`
	NoteURL    string  `json:"note_url"`
	NoteID     string  `json:"note_id"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}
```

- [ ] **Step 4: Implement publish tracking creation and discovery**

Add these methods to `server/service/rednote_tracking.go`:

```go
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
	if strings.TrimSpace(channel.ProfileURL) == "" {
		return fmt.Errorf("rednote channel profile URL is required")
	}
	now := time.Now()
	nextRun := now.Add(24 * time.Hour)
	existing, err := s.repo.RednoteTrackings().FindByTaskID(ctx, taskID)
	if err == nil {
		existing.Status = model.RednoteTrackingStatusWaitingDiscovery
		existing.ProfileURL = channel.ProfileURL
		existing.PublishedMarkedAt = now
		existing.NextRunAt = &nextRun
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
		ProfileURL:        channel.ProfileURL,
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
	tracking.DiscoveryAttemptCount++
	posts, err := s.platform.FetchProfilePosts(ctx, tracking.ProfileURL)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("fetch profile posts: %w", err))
	}
	task, err := s.repo.Tasks().FindByID(ctx, tracking.TaskID)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("find task: %w", err))
	}
	match, err := s.matchPublishedNote(ctx, task, posts)
	if err != nil || !match.Matched || match.Confidence < 0.8 || !matchExistsInCandidates(match, posts) {
		tracking.LastRunAt = ptrTime(time.Now())
		if tracking.DiscoveryAttemptCount >= model.RednoteDiscoveryMaxAttempts {
			tracking.Status = model.RednoteTrackingStatusFailed
			tracking.StopReason = model.RednoteStopReasonDiscoveryTimeout
			tracking.LastError = "discovery timed out"
			tracking.NextRunAt = nil
		} else {
			next := time.Now().Add(24 * time.Hour)
			tracking.NextRunAt = &next
		}
		if updateErr := s.repo.RednoteTrackings().Update(ctx, tracking); updateErr != nil {
			return fmt.Errorf("update discovery retry: %w", updateErr)
		}
		if tracking.Status == model.RednoteTrackingStatusWaitingDiscovery {
			return s.enqueueDiscover(tracking.ID, 24*time.Hour)
		}
		return nil
	}
	now := time.Now()
	tracking.Status = model.RednoteTrackingStatusTracking
	tracking.NoteID = match.NoteID
	tracking.NoteURL = match.NoteURL
	tracking.MatchConfidence = match.Confidence
	tracking.MatchReason = match.Reason
	tracking.DiscoveredAt = &now
	tracking.TrackingStartedAt = &now
	tracking.LastError = ""
	for _, post := range posts {
		if post.NoteID == match.NoteID || post.URL == match.NoteURL {
			tracking.NoteTitle = post.Title
			tracking.NoteCoverURL = post.CoverURL
			break
		}
	}
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update matched tracking: %w", err)
	}
	return s.CaptureMetrics(ctx, tracking.ID)
}
```

- [ ] **Step 5: Implement AI match, capture, stop evaluation, analytics**

Add these methods to `server/service/rednote_tracking.go`:

```go
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

func matchExistsInCandidates(match *rednoteAIMatch, posts []platform.RednotePost) bool {
	for _, post := range posts {
		if match.NoteID != "" && post.NoteID == match.NoteID {
			return true
		}
		if match.NoteURL != "" && post.URL == match.NoteURL {
			return true
		}
	}
	return false
}

func (s *RednoteTrackingService) CaptureMetrics(ctx context.Context, trackingID string) error {
	tracking, err := s.repo.RednoteTrackings().FindByID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("find tracking: %w", err)
	}
	if tracking.NoteURL == "" {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("tracking has no note URL"))
	}
	metrics, err := s.platform.FetchPostMetrics(ctx, tracking.NoteURL)
	if err != nil {
		return s.recordTrackingFailure(ctx, tracking, fmt.Errorf("fetch post metrics: %w", err))
	}
	now := time.Now()
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
		ViewCount:     metrics.ViewCount,
		RawData:      string(raw),
	}
	if err := s.repo.RednoteMetricSnapshots().UpsertByTrackingAndDate(ctx, snapshot); err != nil {
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	tracking.RunCount++
	tracking.FailureCount = 0
	tracking.LastRunAt = &now
	tracking.LastError = ""
	previous, prevErr := s.repo.RednoteMetricSnapshots().FindPreviousByTrackingID(ctx, tracking.ID, now)
	if prevErr == nil {
		growth := totalGrowth(snapshot, previous)
		if growth < model.RednoteLowGrowthThreshold {
			tracking.ConsecutiveLowGrowthCount++
		} else {
			tracking.ConsecutiveLowGrowthCount = 0
		}
	}
	if s.shouldStopTracking(tracking, now) {
		tracking.Status = model.RednoteTrackingStatusStopped
		stoppedAt := now
		tracking.TrackingStoppedAt = &stoppedAt
		tracking.NextRunAt = nil
		if tracking.StopReason == "" {
			tracking.StopReason = model.RednoteStopReasonLowGrowth
		}
		return s.repo.RednoteTrackings().Update(ctx, tracking)
	}
	next := now.Add(24 * time.Hour)
	tracking.NextRunAt = &next
	if err := s.repo.RednoteTrackings().Update(ctx, tracking); err != nil {
		return fmt.Errorf("update tracking after capture: %w", err)
	}
	return s.enqueueCapture(tracking.ID, 24*time.Hour)
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
			ViewCount:     snapshot.ViewCount,
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
```

- [ ] **Step 6: Implement helpers for failures, enqueueing, DTO conversion**

Add these helper methods:

```go
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
		ViewCount:     snapshot.ViewCount,
		CapturedAt:    &snapshot.CapturedAt,
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

func ptrTime(t time.Time) *time.Time {
	return &t
}
```

- [ ] **Step 7: Run service tests**

Run:

```bash
go test ./server/service -run 'TestRednoteTrackingService' -count=1
```

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add server/service/rednote_tracking.go server/service/rednote_tracking_test.go
git commit -m "feat: add rednote tracking service"
```

---

### Task 5: Wire Publishing Hook and Scheduler

**Files:**

- Modify: `server/service/task.go`
- Modify: `server/service/task_test.go`
- Modify: `server/scheduler/scheduler.go`
- Modify: `server/main.go`

- [ ] **Step 1: Add task service publish hook test**

Add this test to `server/service/task_test.go`:

```go
type fakePublishTrackingService struct {
	called bool
	taskID string
}

func (f *fakePublishTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error {
	f.called = true
	f.taskID = taskID
	return nil
}

func TestTaskService_SetPublishedStartsRednoteTracking(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	tracking := &fakePublishTrackingService{}
	svc.SetRednoteTrackingService(tracking)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformRednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformRednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "rednote topic",
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(context.Background(), userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished: %v", err)
	}

	if !tracking.called || tracking.taskID != task.ID {
		t.Fatalf("tracking called=%v taskID=%q", tracking.called, tracking.taskID)
	}
}
```

- [ ] **Step 2: Run hook test to verify it fails**

Run:

```bash
go test ./server/service -run TestTaskService_SetPublishedStartsRednoteTracking -count=1
```

Expected: compile failure because `SetRednoteTrackingService` does not exist.

- [ ] **Step 3: Add task service dependency and hook**

Modify `server/service/task.go`:

```go
type PublishedTrackingService interface {
	EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error
}
```

Add a field to `TaskService`:

```go
rednoteTrackingSvc PublishedTrackingService
```

Add setter:

```go
func (s *TaskService) SetRednoteTrackingService(trackingSvc PublishedTrackingService) {
	s.rednoteTrackingSvc = trackingSvc
}
```

Modify `SetPublished` after `repo.Tasks().SetPublished` succeeds:

```go
if err := s.repo.Tasks().SetPublished(ctx, taskID, published); err != nil {
	return err
}
if published && task.Type == model.PlatformRednote && s.rednoteTrackingSvc != nil {
	if err := s.rednoteTrackingSvc.EnsureTrackingForPublishedTask(ctx, userID, taskID); err != nil {
		return fmt.Errorf("ensure rednote tracking: %w", err)
	}
}
return nil
```

- [ ] **Step 4: Add scheduler handler signatures and task types**

Modify `server/scheduler/scheduler.go`:

```go
const (
	TypeContentGenerate       = "content:generate"
	TypePlanTrigger           = "plan:trigger"
	TypeTaskCleanup           = "task:cleanup"
	TypeRednoteDiscover       = "rednote:discover"
	TypeRednoteCaptureMetrics = "rednote:capture_metrics"
)

type RednoteDiscoverHandler func(ctx context.Context, trackingID string) error
type RednoteCaptureMetricsHandler func(ctx context.Context, trackingID string) error
```

Add parameters to `NewTaskProcessor` after `cleanupHandler`:

```go
rednoteDiscoverHandler RednoteDiscoverHandler,
rednoteCaptureHandler RednoteCaptureMetricsHandler,
```

Register handlers:

```go
mux.HandleFunc(TypeRednoteDiscover, func(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		TrackingID string `json:"tracking_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Error().Err(err).Msg("failed to unmarshal rednote discover payload")
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if rednoteDiscoverHandler == nil {
		return fmt.Errorf("rednote discover handler is not configured")
	}
	logger.Info().Str("tracking_id", payload.TrackingID).Msg("processing rednote discovery")
	return rednoteDiscoverHandler(ctx, payload.TrackingID)
})

mux.HandleFunc(TypeRednoteCaptureMetrics, func(ctx context.Context, t *asynq.Task) error {
	var payload struct {
		TrackingID string `json:"tracking_id"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Error().Err(err).Msg("failed to unmarshal rednote capture payload")
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if rednoteCaptureHandler == nil {
		return fmt.Errorf("rednote capture handler is not configured")
	}
	logger.Info().Str("tracking_id", payload.TrackingID).Msg("processing rednote metrics capture")
	return rednoteCaptureHandler(ctx, payload.TrackingID)
})
```

- [ ] **Step 5: Wire service in main**

Modify `server/main.go`:

1. Add a `rednoteTrackingSvc *service.RednoteTrackingService` variable near other services.
2. After `taskSvc` is created and `writingLLMClient` is available, instantiate this before the `// 15. Start Asynq worker if Redis is available.` block:

```go
rednoteTrackingSvc = service.NewRednoteTrackingService(
	repo,
	platform.NewRednoteProvider(),
	writingLLMClient,
	asynqClient,
	log,
)
taskSvc.SetRednoteTrackingService(rednoteTrackingSvc)
```

3. Add `github.com/royalrick/anbanwriter/server/platform` to imports.
4. Change `startAsynqServer` signature:

```go
func startAsynqServer(taskSvc *service.TaskService, rednoteTrackingSvc *service.RednoteTrackingService, cfg *config.Config, log *zerolog.Logger) *scheduler.TaskProcessor
```

5. Pass rednote handlers into `scheduler.NewTaskProcessor`:

```go
func(ctx context.Context, trackingID string) error {
	if rednoteTrackingSvc == nil {
		return fmt.Errorf("rednote tracking service is not configured")
	}
	return rednoteTrackingSvc.DiscoverPublishedNote(ctx, trackingID)
},
func(ctx context.Context, trackingID string) error {
	if rednoteTrackingSvc == nil {
		return fmt.Errorf("rednote tracking service is not configured")
	}
	return rednoteTrackingSvc.CaptureMetrics(ctx, trackingID)
},
```

- [ ] **Step 6: Run service and scheduler tests**

Run:

```bash
go test ./server/service ./server/scheduler -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add server/service/task.go server/service/task_test.go server/scheduler/scheduler.go server/main.go
git commit -m "feat: schedule rednote tracking jobs"
```

---

### Task 6: Add Analytics API Endpoint

**Files:**

- Create: `server/handler/rednote_analytics.go`
- Create: `server/handler/rednote_analytics_test.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

- [ ] **Step 1: Write handler tests**

Create `server/handler/rednote_analytics_test.go`:

```go
package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

type fakeRednoteAnalyticsService struct {
	analytics *service.RednoteAnalytics
	err       error
	userID    string
	taskID    string
}

func (f *fakeRednoteAnalyticsService) GetTaskAnalytics(ctx context.Context, userID, taskID string) (*service.RednoteAnalytics, error) {
	f.userID = userID
	f.taskID = taskID
	return f.analytics, f.err
}

func TestRednoteAnalyticsHandler_GetTaskAnalytics(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	fake := &fakeRednoteAnalyticsService{
		analytics: &service.RednoteAnalytics{
			Tracking: &service.RednoteTrackingInfo{Status: "tracking", NoteURL: "https://www.xiaohongshu.com/explore/note-1"},
			Latest:   &service.RednoteMetricInfo{LikeCount: 12, CapturedAt: &now},
			Deltas:   &service.RednoteMetricDelta{LikeCount: 3},
			Series: []*service.RednoteMetricSeriesItem{
				{CapturedAt: now, LikeCount: 12},
			},
		},
	}
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	h := NewRednoteAnalyticsHandler(fake, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/rednote-analytics", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.GetTaskAnalytics(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/tasks/task-1/rednote-analytics", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if fake.userID != "user-1" || fake.taskID != "task-1" {
		t.Fatalf("service userID=%q taskID=%q", fake.userID, fake.taskID)
	}
}
```

- [ ] **Step 2: Run handler test to verify it fails**

Run:

```bash
go test ./server/handler -run TestRednoteAnalyticsHandler_GetTaskAnalytics -count=1
```

Expected: compile failure because handler does not exist.

- [ ] **Step 3: Implement handler**

Create `server/handler/rednote_analytics.go`:

```go
package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

type RednoteAnalyticsService interface {
	GetTaskAnalytics(ctx context.Context, userID, taskID string) (*service.RednoteAnalytics, error)
}

type RednoteAnalyticsHandler struct {
	service RednoteAnalyticsService
	logger  *zerolog.Logger
}

func NewRednoteAnalyticsHandler(svc RednoteAnalyticsService, logger *zerolog.Logger) *RednoteAnalyticsHandler {
	return &RednoteAnalyticsHandler{service: svc, logger: logger}
}

func (h *RednoteAnalyticsHandler) GetTaskAnalytics(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	taskID := c.Params("id")
	if taskID == "" {
		return Error(c, fiber.StatusBadRequest, "task id is required")
	}
	analytics, err := h.service.GetTaskAnalytics(c.Context(), userID, taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Str("user_id", userID).Msg("get rednote analytics failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get rednote analytics")
	}
	return Success(c, analytics)
}
```

- [ ] **Step 4: Wire router and main**

Modify `server/router/router.go`:

```go
RednoteAnalyticsHandler *handler.RednoteAnalyticsHandler
```

Add route in the task endpoints group:

```go
if svc.RednoteAnalyticsHandler != nil {
	apiV1.Get("/tasks/:id/rednote-analytics", svc.RednoteAnalyticsHandler.GetTaskAnalytics)
}
```

Modify `server/main.go` to create and pass the handler:

```go
var rednoteAnalyticsHandler *handler.RednoteAnalyticsHandler
if rednoteTrackingSvc != nil {
	rednoteAnalyticsHandler = handler.NewRednoteAnalyticsHandler(rednoteTrackingSvc, log)
}
```

Add `RednoteAnalyticsHandler: rednoteAnalyticsHandler,` to `router.Services`.

- [ ] **Step 5: Run handler and router tests**

Run:

```bash
go test ./server/handler ./server/router -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add server/handler/rednote_analytics.go server/handler/rednote_analytics_test.go server/router/router.go server/main.go
git commit -m "feat: expose rednote analytics API"
```

---

### Task 7: Add Studio Analytics Panel

**Files:**

- Create: `studio/src/types/rednote-analytics.ts`
- Create: `studio/src/lib/api/rednote-analytics.ts`
- Create: `studio/src/components/tasks/RednoteAnalyticsPanel.tsx`
- Modify: `studio/src/lib/api/index.ts`
- Modify: `studio/src/lib/query-keys.ts`
- Modify: `studio/src/pages/TaskDetailPage.tsx`

- [ ] **Step 1: Add frontend types**

Create `studio/src/types/rednote-analytics.ts`:

```ts
export type RednoteTrackingStatus = 'waiting_discovery' | 'tracking' | 'stopped' | 'failed'

export interface RednoteTrackingInfo {
  status: RednoteTrackingStatus
  note_url?: string
  note_title?: string
  note_cover_url?: string
  discovered_at?: string | null
  last_run_at?: string | null
  next_run_at?: string | null
  run_count: number
  stop_reason?: string
  last_error?: string
}

export interface RednoteMetricInfo {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
  captured_at?: string | null
}

export interface RednoteMetricDelta {
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
}

export interface RednoteMetricSeriesItem {
  captured_at: string
  like_count: number
  collect_count: number
  comment_count: number
  share_count: number
  view_count: number | null
}

export interface RednoteAnalytics {
  tracking?: RednoteTrackingInfo
  latest?: RednoteMetricInfo
  deltas?: RednoteMetricDelta
  series: RednoteMetricSeriesItem[]
}
```

- [ ] **Step 2: Add API client**

Create `studio/src/lib/api/rednote-analytics.ts`:

```ts
import { http, unwrap } from '@/lib/http-client'
import type { RednoteAnalytics } from '@/types/rednote-analytics'

export const rednoteAnalyticsApi = {
  getForTask: (taskId: string) =>
    unwrap<RednoteAnalytics>(http.get(`/tasks/${taskId}/rednote-analytics`)),
}
```

Modify `studio/src/lib/api/index.ts`:

```ts
import { rednoteAnalyticsApi } from './rednote-analytics'

export const api = {
  auth: authApi,
  plans: plansApi,
  tasks: tasksApi,
  timeline: timelineApi,
  channels: channelsApi,
  credits: creditsApi,
  apiKeys: apiKeysApi,
  usage: usageApi,
  feedback: feedbackApi,
  modelConfig: modelConfigApi,
  rednoteAnalytics: rednoteAnalyticsApi,
}
```

- [ ] **Step 3: Add query key**

Modify `studio/src/lib/query-keys.ts` to include:

```ts
rednoteAnalytics: {
  task: (taskId: string) => ['rednote-analytics', taskId] as const,
},
```

Keep the existing exported `queryKeys` object shape intact.

- [ ] **Step 4: Build the panel component**

Create `studio/src/components/tasks/RednoteAnalyticsPanel.tsx`:

```tsx
import { ExternalLink, Loader2 } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { formatFullDateTimeCN } from '@/lib/labels'
import { Card, CardBody } from '@/components/ui/Card'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import type { RednoteAnalytics, RednoteMetricInfo } from '@/types/rednote-analytics'

const statusText = {
  waiting_discovery: '明天将从账号主页自动识别这篇笔记',
  tracking: '正在每日采集公开数据',
  stopped: '数据变化已趋缓，已停止自动采集',
  failed: '暂时无法识别或采集这篇笔记',
}

const statusVariant = {
  waiting_discovery: 'warning',
  tracking: 'success',
  stopped: 'secondary',
  failed: 'destructive',
} as const

function formatMetric(value: number | null | undefined) {
  if (value === null || value === undefined) return '暂无公开数据'
  return value.toLocaleString('zh-CN')
}

function MetricCell({ label, value, delta }: { label: string; value: number | null | undefined; delta?: number }) {
  return (
    <div className="rounded-md border border-border bg-background/40 px-3 py-2">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-lg font-semibold text-foreground">{formatMetric(value)}</p>
      {typeof delta === 'number' && (
        <p className="text-xs text-muted-foreground">较上次 {delta >= 0 ? '+' : ''}{delta}</p>
      )}
    </div>
  )
}

function LatestMetrics({ latest, deltas }: { latest?: RednoteMetricInfo; deltas?: RednoteAnalytics['deltas'] }) {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
      <MetricCell label="点赞" value={latest?.like_count ?? 0} delta={deltas?.like_count} />
      <MetricCell label="收藏" value={latest?.collect_count ?? 0} delta={deltas?.collect_count} />
      <MetricCell label="评论" value={latest?.comment_count ?? 0} delta={deltas?.comment_count} />
      <MetricCell label="分享" value={latest?.share_count ?? 0} delta={deltas?.share_count} />
      <MetricCell label="曝光" value={latest?.view_count ?? null} />
    </div>
  )
}

export function RednoteAnalyticsPanel({ taskId }: { taskId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: queryKeys.rednoteAnalytics.task(taskId),
    queryFn: () => api.rednoteAnalytics.getForTask(taskId),
    refetchInterval: 60_000,
  })

  if (isLoading) {
    return (
      <Card>
        <CardBody>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            正在加载小红书数据
          </div>
        </CardBody>
      </Card>
    )
  }

  const tracking = data?.tracking
  if (!tracking) {
    return (
      <Card>
        <CardBody>
          <p className="text-sm text-muted-foreground">小红书数据追踪尚未创建</p>
        </CardBody>
      </Card>
    )
  }

  const hasSeries = Boolean(data?.series?.length)

  return (
    <Card>
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-foreground">小红书数据</h2>
          <Badge variant={statusVariant[tracking.status]}>{statusText[tracking.status]}</Badge>
        </div>
        {tracking.note_url && (
          <Button size="sm" variant="outline" onClick={() => window.open(tracking.note_url, '_blank', 'noopener,noreferrer')}>
            <ExternalLink className="h-4 w-4" />
            打开笔记
          </Button>
        )}
      </div>
      <CardBody className="space-y-4">
        {(tracking.note_title || tracking.note_cover_url) && (
          <div className="flex items-center gap-3">
            {tracking.note_cover_url && <img src={tracking.note_cover_url} alt="" className="h-14 w-14 rounded-md object-cover" />}
            <div>
              <p className="text-sm font-medium text-foreground">{tracking.note_title || '已绑定小红书笔记'}</p>
              <p className="text-xs text-muted-foreground">采集 {tracking.run_count} 次</p>
            </div>
          </div>
        )}

        <LatestMetrics latest={data?.latest} deltas={data?.deltas} />

        {hasSeries ? (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data?.series ?? []}>
                <XAxis dataKey="captured_at" tickFormatter={(value) => new Date(value).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })} />
                <YAxis />
                <Tooltip labelFormatter={(value) => formatFullDateTimeCN(String(value))} />
                <Line type="monotone" dataKey="like_count" name="点赞" stroke="#22c55e" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="collect_count" name="收藏" stroke="#0ea5e9" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="comment_count" name="评论" stroke="#f59e0b" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="share_count" name="分享" stroke="#a855f7" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">还没有采集到趋势数据</p>
        )}

        <div className="grid grid-cols-1 gap-2 text-xs text-muted-foreground sm:grid-cols-3">
          <p>最后采集：{formatFullDateTimeCN(tracking.last_run_at)}</p>
          <p>下次采集：{formatFullDateTimeCN(tracking.next_run_at)}</p>
          <p>停止原因：{tracking.stop_reason || '自动追踪中'}</p>
        </div>
        {tracking.status === 'failed' && tracking.last_error && (
          <p className="rounded-md border border-red-900/30 bg-red-900/20 px-3 py-2 text-xs text-red-300">
            {tracking.last_error}
          </p>
        )}
      </CardBody>
    </Card>
  )
}
```

- [ ] **Step 5: Embed panel in task detail**

Modify `studio/src/pages/TaskDetailPage.tsx`:

1. Add import:

```tsx
import { RednoteAnalyticsPanel } from '@/components/tasks/RednoteAnalyticsPanel'
```

2. Add the panel after the details cards and before generated files:

```tsx
{task.type === 'rednote' && task.published && (
  <RednoteAnalyticsPanel taskId={task.id} />
)}
```

- [ ] **Step 6: Run frontend checks**

Run:

```bash
npm --prefix studio run build
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add studio/src/types/rednote-analytics.ts studio/src/lib/api/rednote-analytics.ts studio/src/components/tasks/RednoteAnalyticsPanel.tsx studio/src/lib/api/index.ts studio/src/lib/query-keys.ts studio/src/pages/TaskDetailPage.tsx
git commit -m "feat: show rednote analytics panel"
```

---

### Task 8: Final Verification

**Files:**

- Verify all changed backend and frontend files.

- [ ] **Step 1: Run backend tests**

Run:

```bash
go test ./server/model ./server/repository ./server/platform ./server/service ./server/handler ./server/router ./server/scheduler -count=1
```

Expected: pass.

- [ ] **Step 2: Run frontend build**

Run:

```bash
npm --prefix studio run build
```

Expected: pass.

- [ ] **Step 3: Run full Go test suite if time allows**

Run:

```bash
go test ./... -count=1
```

Expected: pass. If unrelated packages fail, capture the failing package, test name, and error output before deciding whether the failure is related to this feature.

- [ ] **Step 4: Inspect final diff**

Run:

```bash
git diff --stat HEAD
git diff --check
```

Expected: diff only contains RedNote tracking changes; `git diff --check` prints no output.

- [ ] **Step 5: Commit final fixes**

If Step 1 through Step 4 required fixes, commit them:

```bash
git add server studio
git commit -m "fix: stabilize rednote tracking integration"
```

If no fixes were required, skip this commit.

---

## Self-Review

Spec coverage:

- Automatic discovery after marking published: Task 5 hooks `SetPublished`; Task 4 creates tracking and queues discovery.
- Public homepage discovery and AI matching: Task 3 adds profile candidate parsing; Task 4 adds AI matching and binding rules.
- Daily metric snapshots: Task 4 captures and stores snapshots; Task 5 wires Asynq task types.
- Stop rules: Task 4 implements max duration, low growth, discovery timeout, and repeated failures.
- Task detail analytics: Task 6 exposes API; Task 7 renders Studio panel.
- Public-only metrics and nullable exposure: Task 1 models nullable `ViewCount`; Task 7 displays unavailable exposure explicitly.
- Tests: each task starts with failing tests or build verification.

Type consistency:

- Backend status constants use `RednoteTrackingStatusWaitingDiscovery`, `RednoteTrackingStatusTracking`, `RednoteTrackingStatusStopped`, and `RednoteTrackingStatusFailed`.
- Scheduler task types use `rednote:discover` and `rednote:capture_metrics`.
- Frontend API path is `/tasks/:id/rednote-analytics`, matching router route.
- Metric names are consistent across model, service DTO, API response, and Studio types: `like_count`, `collect_count`, `comment_count`, `share_count`, `view_count`.

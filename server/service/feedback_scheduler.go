package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FeedbackTaskEnqueuer interface {
	EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error)
}

type feedbackDelayedTaskEnqueuer interface {
	EnqueueUniqueIn(taskType string, payload []byte, uniqueKey string, delay time.Duration) (bool, error)
}

type FeedbackScheduler struct {
	repo          repository.Repository
	enqueuer      FeedbackTaskEnqueuer
	now           func() time.Time
	maxPerUser    int
	maxPerProject int
	maxPerAccount int
	maxPerRun     int
	windowMu      sync.Mutex
	windowRuns    map[string]time.Time
}

func NewFeedbackScheduler(repo repository.Repository, enqueuer FeedbackTaskEnqueuer) *FeedbackScheduler {
	return &FeedbackScheduler{repo: repo, enqueuer: enqueuer, now: time.Now, maxPerUser: 3, maxPerProject: 2, maxPerAccount: 1, maxPerRun: 100, windowRuns: make(map[string]time.Time)}
}

type FeedbackScanResult struct{ Created, Enqueued, Skipped int }

type FeedbackPeriodOverride struct {
	Start string
	End   string
}

// RunCadence performs the deterministic eligibility pass. A skipped row is
// persisted for auditability, but no Agent task is created for it.
func (s *FeedbackScheduler) RunCadence(ctx context.Context, cadence string, at time.Time) (FeedbackScanResult, error) {
	return s.runCadence(ctx, cadence, at, false, "", nil)
}

// RunProjectCadence is the explicit, auditable manual rerun path. It scopes
// the same eligibility pass to one project instead of accidentally scanning a
// tenant's other accounts.
func (s *FeedbackScheduler) RunProjectCadence(ctx context.Context, projectID, cadence string, at time.Time) (FeedbackScanResult, error) {
	return s.runCadence(ctx, cadence, at, false, projectID, nil)
}

func (s *FeedbackScheduler) RunProjectCadencePeriod(ctx context.Context, projectID, cadence string, at time.Time, period FeedbackPeriodOverride) (FeedbackScanResult, error) {
	if _, err := time.Parse("2006-01-02", period.Start); err != nil {
		return FeedbackScanResult{}, fmt.Errorf("invalid feedback period start: %w", err)
	}
	if _, err := time.Parse("2006-01-02", period.End); err != nil {
		return FeedbackScanResult{}, fmt.Errorf("invalid feedback period end: %w", err)
	}
	if period.Start > period.End {
		return FeedbackScanResult{}, errors.New("feedback period start must not be after end")
	}
	return s.runCadence(ctx, cadence, at, false, projectID, &period)
}

// RunCadenceWindowed is used by the background loop. It only considers
// projects whose local low-traffic window is open; manual re-runs use
// RunCadence and deliberately bypass the clock gate.
func (s *FeedbackScheduler) RunCadenceWindowed(ctx context.Context, cadence string, at time.Time) (FeedbackScanResult, error) {
	return s.runCadence(ctx, cadence, at, true, "", nil)
}

func (s *FeedbackScheduler) runCadence(ctx context.Context, cadence string, at time.Time, windowed bool, onlyProjectID string, override *FeedbackPeriodOverride) (FeedbackScanResult, error) {
	if s == nil || s.repo == nil {
		return FeedbackScanResult{}, errors.New("feedback scheduler repository unavailable")
	}
	if at.IsZero() {
		at = s.now()
	}
	var projects []*model.Project
	var err error
	if strings.TrimSpace(onlyProjectID) != "" {
		project, findErr := s.repo.Projects().FindByID(ctx, onlyProjectID)
		if findErr != nil {
			return FeedbackScanResult{}, findErr
		}
		projects = []*model.Project{project}
	} else {
		projects, err = s.repo.Projects().ListActiveProjects(ctx)
		if err != nil {
			return FeedbackScanResult{}, err
		}
	}
	projects = fairProjectOrder(projects)
	userCounts := map[string]int{}
	projectCounts := map[string]int{}
	accountCounts := map[string]int{}
	// Load durable queued/running work before this scan. The in-memory counts
	// below only cover jobs admitted by this pass; these counts enforce the
	// same fairness limits across scheduler ticks and server instances.
	for _, p := range projects {
		if p == nil {
			continue
		}
		if s.maxPerUser > 0 && p.UserID != "" {
			if _, ok := userCounts[p.UserID]; !ok {
				running, loadErr := s.repo.FeedbackLoop().ListRunningByUser(ctx, p.UserID, s.maxPerUser)
				if loadErr != nil {
					return FeedbackScanResult{}, loadErr
				}
				userCounts[p.UserID] = len(running)
			}
		}
		if s.maxPerProject > 0 {
			if _, ok := projectCounts[p.ID]; !ok {
				running, loadErr := s.repo.FeedbackLoop().ListRunningByProject(ctx, p.ID, s.maxPerProject)
				if loadErr != nil {
					return FeedbackScanResult{}, loadErr
				}
				projectCounts[p.ID] = len(running)
			}
		}
	}
	scopes, err := feedbackProjectScopes(ctx, s.repo, projects)
	if err != nil {
		return FeedbackScanResult{}, err
	}
	var out FeedbackScanResult
	for _, scope := range scopes {
		p := scope.Project
		if s.maxPerRun > 0 && out.Enqueued >= s.maxPerRun {
			break
		}
		if windowed && !feedbackWindowOpen(cadence, at, p.Timezone) {
			continue
		}
		if windowed && s.windowAlreadyRun(cadence, p, scope.Channel, at) {
			continue
		}
		accountID := scope.AccountID
		if feedbackLimitReached(userCounts[p.UserID], s.maxPerUser) || feedbackLimitReached(projectCounts[p.ID], s.maxPerProject) || feedbackLimitReached(accountCounts[accountID], s.maxPerAccount) {
			continue
		}
		jobs, err := s.buildCadenceJobs(ctx, scope, cadence, at, override)
		if err != nil {
			return out, err
		}
		for i := range jobs {
			job := &jobs[i]
			job.AccountID = accountID
			accountBlocked := false
			// Daily, weekly, and monthly jobs for one external account share a
			// single lease. This prevents a busy account from consuming workers
			// across cadences while preserving tenant/project isolation.
			if job.Status == model.FeedbackJobQueued {
				running, err := s.repo.FeedbackLoop().ListRunningByAccount(ctx, accountID, "", 1)
				if err != nil {
					return out, err
				}
				if len(running) > 0 || (s.maxPerAccount > 0 && accountCounts[accountID] >= s.maxPerAccount) {
					job.Status = model.FeedbackJobSkipped
					job.SkipReason = model.FeedbackSkipResourceLimit
					accountBlocked = true
				}
			}
			// AccountID is part of the durable identity. Recompute after setting
			// it so two projects sharing an account cannot collide accidentally.
			job.Fingerprint = FeedbackJobFingerprint(FeedbackEligibilityInput{
				ProjectID: job.ProjectID, Platform: job.Platform, AccountID: job.AccountID,
				Operation: job.Operation, Cadence: job.Cadence, PeriodStart: job.PeriodStart,
				PeriodEnd: job.PeriodEnd, AnalyticsRevision: job.AnalyticsRevision,
				ContentSetDigest: job.ContentSetDigest, StrategyRevision: job.StrategyRevision,
			})
			created, err := s.repo.FeedbackLoop().CreateJob(ctx, job)
			if err != nil {
				return out, err
			}
			if !created {
				requeued := false
				if job.Status == model.FeedbackJobSkipped && job.SkipReason == model.FeedbackSkipResourceLimit && !accountBlocked {
					job.Status = model.FeedbackJobQueued
					job.SkipReason = ""
					job.LastError = ""
					now := at.UTC()
					job.QueuedAt = &now
					if err := s.repo.FeedbackLoop().UpdateJob(ctx, job); err != nil {
						return out, err
					}
					requeued = true
				}
				if !requeued && job.Status == model.FeedbackJobFailed && strings.HasPrefix(job.LastError, "enqueue:") {
					job.Status = model.FeedbackJobQueued
					job.LastError = ""
					job.SkipReason = ""
					now := at.UTC()
					job.QueuedAt = &now
					if err := s.repo.FeedbackLoop().UpdateJob(ctx, job); err != nil {
						return out, err
					}
					requeued = true
				}
				if !requeued {
					out.Skipped++
					continue
				}
			}
			if !created && job.Status != model.FeedbackJobQueued {
				out.Skipped++
				continue
			}
			if created {
				out.Created++
			}
			if job.Status != model.FeedbackJobQueued || s.enqueuer == nil {
				out.Skipped++
				continue
			}
			payload, _ := json.Marshal(map[string]string{"fingerprint": job.Fingerprint, "operation": job.Operation})
			if _, err := enqueueFeedbackTask(s.enqueuer, feedbackTaskType(job.Operation), payload, "feedback:"+job.Fingerprint, feedbackJitter(job.Fingerprint)); err != nil {
				job.Status = model.FeedbackJobFailed
				job.LastError = "enqueue: " + err.Error()
				if updateErr := s.repo.FeedbackLoop().UpdateJob(ctx, job); updateErr != nil {
					return out, fmt.Errorf("enqueue feedback job: %v; persist enqueue failure: %w", err, updateErr)
				}
				return out, err
			}
			out.Enqueued++
			userCounts[p.UserID]++
			projectCounts[p.ID]++
			accountCounts[accountID]++
			if feedbackLimitReached(userCounts[p.UserID], s.maxPerUser) || feedbackLimitReached(projectCounts[p.ID], s.maxPerProject) || feedbackLimitReached(accountCounts[accountID], s.maxPerAccount) {
				break
			}
		}
		if windowed {
			s.markWindowRun(cadence, p, scope.Channel, at)
		}
	}
	return out, nil
}

func feedbackLimitReached(current, limit int) bool {
	return limit > 0 && current >= limit
}

func feedbackWindowKey(cadence string, p *model.Project, channel string, at time.Time) string {
	if p == nil {
		return ""
	}
	loc, err := time.LoadLocation(strings.TrimSpace(p.Timezone))
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return cadence + "|" + p.ID + "|" + channel + "|" + at.In(loc).Format("2006-01-02")
}

func (s *FeedbackScheduler) windowAlreadyRun(cadence string, p *model.Project, channel string, at time.Time) bool {
	if s == nil || p == nil {
		return false
	}
	key := feedbackWindowKey(cadence, p, channel, at)
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	for existing, seenAt := range s.windowRuns {
		if at.Sub(seenAt) > 48*time.Hour {
			delete(s.windowRuns, existing)
		}
	}
	if _, exists := s.windowRuns[key]; exists {
		return true
	}
	return false
}

// markWindowRun makes the frequent background wake-up idempotent per project
// and project-local calendar day. Durable fingerprints still protect against
// duplicate work after a process restart or across multiple server instances.
func (s *FeedbackScheduler) markWindowRun(cadence string, p *model.Project, channel string, at time.Time) {
	if s == nil || p == nil {
		return
	}
	key := feedbackWindowKey(cadence, p, channel, at)
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	if s.windowRuns == nil {
		s.windowRuns = make(map[string]time.Time)
	}
	for existing, seenAt := range s.windowRuns {
		if at.Sub(seenAt) > 48*time.Hour {
			delete(s.windowRuns, existing)
		}
	}
	s.windowRuns[key] = at
}

func enqueueFeedbackTask(enqueuer FeedbackTaskEnqueuer, taskType string, payload []byte, uniqueKey string, delay time.Duration) (bool, error) {
	if delayed, ok := enqueuer.(feedbackDelayedTaskEnqueuer); ok {
		return delayed.EnqueueUniqueIn(taskType, payload, uniqueKey, delay)
	}
	return enqueuer.EnqueueUnique(taskType, payload, uniqueKey)
}

func feedbackJitter(fingerprint string) time.Duration {
	if len(fingerprint) < 2 {
		return 0
	}
	value := (hexValue(fingerprint[0]) << 4) | hexValue(fingerprint[1])
	return time.Duration(value%31) * time.Second
}

func hexValue(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10
	default:
		return 0
	}
}

func fairProjectOrder(projects []*model.Project) []*model.Project {
	byUser := map[string][]*model.Project{}
	users := make([]string, 0)
	for _, project := range projects {
		if project == nil {
			continue
		}
		if _, ok := byUser[project.UserID]; !ok {
			users = append(users, project.UserID)
		}
		byUser[project.UserID] = append(byUser[project.UserID], project)
	}
	sort.Strings(users)
	for _, user := range users {
		sort.Slice(byUser[user], func(i, j int) bool { return byUser[user][i].ID < byUser[user][j].ID })
	}
	ordered := make([]*model.Project, 0, len(projects))
	for round := 0; ; round++ {
		added := false
		for _, user := range users {
			if round >= len(byUser[user]) {
				continue
			}
			ordered = append(ordered, byUser[user][round])
			added = true
		}
		if !added {
			return ordered
		}
	}
}

func feedbackWindowOpen(cadence string, at time.Time, timezone string) bool {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	local := at.In(loc)
	startHour, endHour := 3, 5
	switch cadence {
	case FeedbackCadenceWeekly:
		startHour, endHour = 4, 6
	case FeedbackCadenceMonthly:
		startHour, endHour = 4, 7
	case FeedbackCadenceDaily:
		// Daily scans run every day in the 03:00-05:00 local window.
	default:
		return false
	}
	if local.Hour() < startHour || local.Hour() >= endHour {
		return false
	}
	switch cadence {
	case FeedbackCadenceWeekly:
		return local.Weekday() == time.Monday
	case FeedbackCadenceMonthly:
		return local.Day() <= 7 && local.Weekday() >= time.Monday && local.Weekday() <= time.Friday
	default:
		return true
	}
}

func (s *FeedbackScheduler) buildCadenceJobs(ctx context.Context, scope feedbackProjectScope, cadence string, at time.Time, override *FeedbackPeriodOverride) ([]model.FeedbackJob, error) {
	p := scope.Project
	state, err := s.repo.Analytics().State(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	from, to := feedbackPeriodForProject(cadence, at, p.Timezone)
	if override != nil {
		from, to = override.Start, override.End
	}
	if state.Status != "ready" {
		return []model.FeedbackJob{s.skippedJob(scope, cadence, at, "", FeedbackSkipAnalyticsRebuilding, 0, state.Revision)}, nil
	}
	if p.FeedbackPaused || p.Status != model.ProjectStatusActive {
		return []model.FeedbackJob{s.skippedJob(scope, cadence, at, "", FeedbackSkipProjectPaused, 0, state.Revision)}, nil
	}
	var contentCount int64
	db := s.repo.Analytics().DB().WithContext(ctx)
	defaultMaturityCutoff := feedbackMaturityCutoffForProject(at, p.Timezone, 24*time.Hour)
	if err := scopeFeedbackContents(db.Model(&model.AnalyticsContent{}), "", scope.Channel).
		Where("project_id = ? AND date IS NOT NULL AND date(date) <= ? AND date(date) BETWEEN ? AND ?", p.ID, defaultMaturityCutoff, from, to).
		Count(&contentCount).Error; err != nil {
		return nil, err
	}
	validObservationPredicate := analyticsValidMetricPredicate(model.PlatformForChannel(scope.Channel))
	operations := feedbackOperations(cadence)
	jobs := make([]model.FeedbackJob, 0, len(operations))
	for _, operation := range operations {
		minSample := feedbackSampleThreshold(operation)
		matureContentCount := contentCount
		maturityCutoff := defaultMaturityCutoff
		if operation == "content_postmortem" {
			maturityCutoff = feedbackMaturityCutoffForProject(at, p.Timezone, 7*24*time.Hour)
			matureContentCount = s.countMatureContents(ctx, db, scope, from, to, maturityCutoff)
		}
		observationCount, err := feedbackObservationCount(ctx, db, p.ID, scope.Channel, from, to, maturityCutoff, validObservationPredicate)
		if err != nil {
			return nil, err
		}
		contentIDs, err := feedbackContentIDs(ctx, db, p.ID, scope.Channel, from, to, maturityCutoff)
		if err != nil {
			return nil, err
		}
		digest := contentDigest(contentIDs, observationCount, from, to, state.Revision)
		strategyRevision := int64(0)
		if strategy, strategyErr := s.repo.FeedbackLoop().FindActiveStrategy(ctx, p.ID, scope.Channel); strategyErr != nil {
			return nil, strategyErr
		} else if strategy != nil {
			strategyRevision = strategy.Revision
		}
		fingerprintInput := FeedbackEligibilityInput{ProjectID: p.ID, Platform: scope.Channel, AccountID: scope.AccountID, Operation: operation, Cadence: cadence, PeriodStart: from, PeriodEnd: to, AnalyticsRevision: state.Revision, ContentSetDigest: digest, StrategyRevision: strategyRevision}
		fingerprint := FeedbackJobFingerprint(fingerprintInput)
		alreadySucceeded, running := false, false
		prior, err := s.repo.FeedbackLoop().FindJobByFingerprint(ctx, fingerprint)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if prior != nil && prior.Status == model.FeedbackJobSucceeded {
			alreadySucceeded = true
		}
		runningJob, err := s.repo.FeedbackLoop().FindRunningJob(ctx, p.ID, operation, scope.Channel)
		if err != nil {
			return nil, err
		}
		if runningJob != nil && (prior == nil || runningJob.ID != prior.ID) {
			running = true
		}
		in := fingerprintInput
		in.HasNewRevision = state.Revision > 0 && !alreadySucceeded
		in.HasMatureContent = matureContentCount > 0
		in.HasValidObservations = observationCount > 0
		in.MeetsSampleThreshold = observationCount >= minSample
		in.ProjectActive = true
		in.AnalyticsReady = true
		in.AlreadySucceeded = alreadySucceeded
		in.RunningDuplicate = running
		decision := EvaluateFeedbackEligibility(in)
		job := model.FeedbackJob{ID: uuid.NewString(), UserID: p.UserID, ProjectID: p.ID, Platform: scope.Channel, AccountID: scope.AccountID, Operation: operation, Cadence: cadence, Trigger: cadence, PeriodStart: from, PeriodEnd: to, MaturityCutoff: maturityCutoff, AnalyticsRevision: state.Revision, ContentSetDigest: digest, StrategyRevision: strategyRevision, Fingerprint: fingerprint, Status: model.FeedbackJobQueued, SampleCount: int(observationCount), Coverage: "partial"}
		if decision.Status != FeedbackJobEligible {
			job.Status = model.FeedbackJobSkipped
			job.SkipReason = decision.SkipReason
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *FeedbackScheduler) countMatureContents(ctx context.Context, db *gorm.DB, scope feedbackProjectScope, from, to, cutoff string) int64 {
	p := scope.Project
	var count int64
	if db == nil || p == nil {
		return 0
	}
	_ = scopeFeedbackContents(db.WithContext(ctx).Model(&model.AnalyticsContent{}), "", scope.Channel).
		Where("project_id = ? AND date IS NOT NULL AND date(date) <= ? AND date(date) BETWEEN ? AND ?", p.ID, cutoff, from, to).
		Count(&count).Error
	return count
}

func feedbackMaturityCutoffForProject(now time.Time, timezone string, age time.Duration) string {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return now.In(loc).Add(-age).Format("2006-01-02")
}

func analyticsValidMetricPredicate(platform string) string {
	family, err := model.AnalyticsMetricFamilyForPlatform(platform)
	if err != nil {
		return ""
	}
	columns := make([]string, 0, len(model.AnalyticsMetricColumns()))
	for _, column := range model.AnalyticsMetricColumns() {
		if family.Contains(column) {
			columns = append(columns, column+" IS NOT NULL")
		}
	}
	return strings.Join(columns, " OR ")
}

func feedbackContentIDs(ctx context.Context, db *gorm.DB, projectID, channel, from, to, cutoff string) ([]string, error) {
	var ids []string
	err := scopeFeedbackContents(db.WithContext(ctx).Model(&model.AnalyticsContent{}), "", channel).
		Where("project_id = ? AND date IS NOT NULL AND date(date) <= ? AND date(date) BETWEEN ? AND ?", projectID, cutoff, from, to).
		Order("id asc").Pluck("id", &ids).Error
	return ids, err
}

func feedbackObservationCount(ctx context.Context, db *gorm.DB, projectID, channel, from, to, cutoff, validPredicate string) (int64, error) {
	if validPredicate == "" {
		return 0, nil
	}
	var count int64
	err := scopeFeedbackContents(db.WithContext(ctx).Model(&model.AnalyticsObservation{}), "ac.", channel).
		Joins("JOIN analytics_contents ac ON ac.id = analytics_observations.content_id AND ac.project_id = analytics_observations.project_id").
		Where("analytics_observations.project_id = ? AND analytics_observations.stat_date BETWEEN ? AND ? AND analytics_observations.metric_basis IN ? AND analytics_observations.revoked_at IS NULL AND analytics_observations.content_id <> '' AND ac.date IS NOT NULL AND date(ac.date) <= ? AND ("+validPredicate+")", projectID, from, to, []string{"daily", "cumulative"}, cutoff).
		Distinct("content_id").Count(&count).Error
	return count, err
}

func contentDigest(contents []string, observations int64, from, to string, revision int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%d", strings.Join(contents, "\x00"), observations, from, to, revision)))
	return hex.EncodeToString(sum[:])
}

func (s *FeedbackScheduler) skippedJob(scope feedbackProjectScope, cadence string, at time.Time, operation, reason string, sample int, revision int64) model.FeedbackJob {
	p := scope.Project
	from, to := feedbackPeriodForProject(cadence, at, p.Timezone)
	if operation == "" {
		operation = feedbackOperations(cadence)[0]
	}
	in := FeedbackEligibilityInput{ProjectID: p.ID, Platform: scope.Channel, AccountID: scope.AccountID, Operation: operation, Cadence: cadence, PeriodStart: from, PeriodEnd: to, AnalyticsRevision: revision}
	return model.FeedbackJob{ID: uuid.NewString(), UserID: p.UserID, ProjectID: p.ID, Platform: scope.Channel, AccountID: scope.AccountID, Operation: operation, Cadence: cadence, PeriodStart: from, PeriodEnd: to, MaturityCutoff: feedbackMaturityCutoffForProject(at, p.Timezone, 24*time.Hour), AnalyticsRevision: revision, Fingerprint: FeedbackJobFingerprint(in), Status: model.FeedbackJobSkipped, SkipReason: reason, SampleCount: sample}
}

func feedbackAccountID(p *model.Project) string {
	if p == nil {
		return ""
	}
	if p.Platform == model.ScopeWechat || p.Platform == model.PlatformWechat {
		id := strings.TrimSpace(mapStringValue(p.RuntimeChannelConfig, "wechat_app_id"))
		if id != "" {
			return "wechat:" + id
		}
	}
	if profile := strings.TrimSpace(p.ProfileURL); profile != "" {
		return p.Platform + ":" + profile
	}
	return p.Platform + ":project:" + p.ID
}

func feedbackOperations(cadence string) []string {
	switch cadence {
	case FeedbackCadenceDaily:
		return []string{"data_tracker"}
	case FeedbackCadenceWeekly:
		return []string{"publish_analytics", "content_postmortem"}
	case FeedbackCadenceMonthly:
		return []string{"performance_review", "strategy_advisor"}
	default:
		return []string{"data_tracker"}
	}
}

func feedbackTaskType(operation string) string {
	switch operation {
	case "data_tracker":
		return "feedback:daily"
	case "publish_analytics", "content_postmortem":
		return "feedback:weekly"
	default:
		return "feedback:monthly"
	}
}

func feedbackPeriodForProject(cadence string, at time.Time, timezone string) (string, string) {
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return feedbackPeriod(cadence, at.In(loc))
}

func feedbackPeriod(cadence string, loc time.Time) (string, string) {
	switch cadence {
	case FeedbackCadenceDaily:
		d := loc.AddDate(0, 0, -1).Format("2006-01-02")
		return d, d
	case FeedbackCadenceWeekly:
		weekday := (int(loc.Weekday()) + 6) % 7
		start := loc.AddDate(0, 0, -weekday-7)
		return start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
	default:
		start := time.Date(loc.Year(), loc.Month()-1, 1, 0, 0, 0, 0, loc.Location())
		return start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02")
	}
}

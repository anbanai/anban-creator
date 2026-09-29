package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// FeedbackDashboard is the low-cost project status surface for periodic
// feedback. It never runs an Agent or performs an analysis itself.
type FeedbackDashboardHandler struct {
	projects  *service.ProjectService
	repo      repository.Repository
	scheduler *service.FeedbackScheduler
}

func NewFeedbackDashboardHandler(projects *service.ProjectService, repo repository.Repository, scheduler *service.FeedbackScheduler) *FeedbackDashboardHandler {
	return &FeedbackDashboardHandler{projects: projects, repo: repo, scheduler: scheduler}
}

func (h *FeedbackDashboardHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	project, _, err := h.projects.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to load project feedback")
	}

	jobs, err := h.repo.FeedbackLoop().ListJobs(c.Context(), project.ID, "", 200)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to load feedback queue")
	}
	status := map[string]int{"queued": 0, "running": 0, "succeeded": 0, "failed": 0, "skipped": 0, "blocked": 0}
	var lastSuccess *time.Time
	var latestSkipped *model.FeedbackJob
	for _, job := range jobs {
		if job == nil {
			continue
		}
		status[job.Status]++
		if job.Status == model.FeedbackJobSucceeded && job.CompletedAt != nil && (lastSuccess == nil || job.CompletedAt.After(*lastSuccess)) {
			value := *job.CompletedAt
			lastSuccess = &value
		}
		if job.Status == model.FeedbackJobSkipped && (latestSkipped == nil || job.CreatedAt.After(latestSkipped.CreatedAt)) {
			latestSkipped = job
		}
	}

	state, err := h.repo.Analytics().State(c.Context(), project.ID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to load analytics state")
	}
	var contentCount, observationCount int64
	db := h.repo.Analytics().DB().WithContext(c.Context())
	if err := db.Model(&model.AnalyticsContent{}).Where("project_id = ?", project.ID).Count(&contentCount).Error; err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to load feedback coverage")
	}
	if err := db.Model(&model.AnalyticsObservation{}).Where("project_id = ? AND revoked_at IS NULL AND content_id <> ''").Count(&observationCount).Error; err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to load feedback coverage")
	}

	strategy, err := h.repo.FeedbackLoop().FindActiveStrategy(c.Context(), project.ID, project.Platform)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusInternalServerError, "failed to load feedback strategy")
	}
	now := time.Now().UTC()
	next := map[string]time.Time{}
	for _, cadence := range []string{service.FeedbackCadenceDaily, service.FeedbackCadenceWeekly, service.FeedbackCadenceMonthly} {
		if value, err := service.NextFeedbackRun(now, project.Timezone, cadence); err == nil {
			next[cadence] = value
		}
	}

	response := fiber.Map{
		"project_id":      project.ID,
		"platform":        project.Platform,
		"timezone":        project.Timezone,
		"feedback_paused": project.FeedbackPaused,
		"analytics":       fiber.Map{"revision": state.Revision, "status": state.Status, "content_count": contentCount, "valid_observation_count": observationCount},
		"queue":           fiber.Map{"counts": status, "last_success_at": lastSuccess},
		"next_runs":       next,
		"strategy":        fiber.Map{"id": "", "revision": int64(0), "status": "unavailable"},
	}
	if latestSkipped != nil {
		response["queue"].(fiber.Map)["latest_skip"] = fiber.Map{"reason": latestSkipped.SkipReason, "cadence": latestSkipped.Cadence, "operation": latestSkipped.Operation, "created_at": latestSkipped.CreatedAt}
	}
	if strategy != nil {
		response["strategy"] = fiber.Map{"id": strategy.ID, "revision": strategy.Revision, "source_revision": strategy.SourceRevision, "status": strategy.Status, "digest": strategy.Digest, "expires_at": strategy.ExpiresAt}
	}
	return Success(c, response)
}

func (h *FeedbackDashboardHandler) Rerun(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	cadence := strings.TrimSpace(c.Query("cadence", ""))
	if cadence != service.FeedbackCadenceDaily && cadence != service.FeedbackCadenceWeekly && cadence != service.FeedbackCadenceMonthly {
		return Error(c, fiber.StatusBadRequest, "cadence must be daily, weekly, or monthly")
	}
	periodStart := strings.TrimSpace(c.Query("period_start", ""))
	periodEnd := strings.TrimSpace(c.Query("period_end", ""))
	if cadence == service.FeedbackCadenceMonthly && (periodStart == "" || periodEnd == "") {
		return Error(c, fiber.StatusBadRequest, "monthly rerun requires period_start and period_end")
	}
	if (periodStart == "") != (periodEnd == "") {
		return Error(c, fiber.StatusBadRequest, "period_start and period_end must be provided together")
	}
	if _, _, err := h.projects.Get(c.Context(), userID, c.Params("id")); err != nil {
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to load project")
	}
	if h.scheduler == nil {
		return Error(c, fiber.StatusServiceUnavailable, "feedback scheduler unavailable")
	}
	var result service.FeedbackScanResult
	var err error
	if periodStart != "" {
		result, err = h.scheduler.RunProjectCadencePeriod(c.Context(), c.Params("id"), cadence, time.Now().UTC(), service.FeedbackPeriodOverride{Start: periodStart, End: periodEnd})
	} else {
		result, err = h.scheduler.RunProjectCadence(c.Context(), c.Params("id"), cadence, time.Now().UTC())
	}
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "feedback rerun failed")
	}
	return Success(c, result)
}

func (h *FeedbackDashboardHandler) SetPaused(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	paused := c.Params("state") == "pause"
	if c.Params("state") != "pause" && c.Params("state") != "resume" {
		return Error(c, fiber.StatusBadRequest, "feedback state must be pause or resume")
	}
	project, err := h.projects.SetFeedbackPaused(c.Context(), userID, c.Params("id"), paused)
	if err != nil {
		if errors.Is(err, service.ErrProjectOwnedByUser) {
			return Forbidden(c, "you do not have access to this project")
		}
		if errors.Is(err, service.ErrProjectNotFound) {
			return Error(c, fiber.StatusNotFound, "project not found")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to update feedback state")
	}
	return Success(c, fiber.Map{"project_id": project.ID, "feedback_paused": project.FeedbackPaused})
}

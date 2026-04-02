package handler

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/repository"
)

// TimelineItem represents a unified item in the timeline view.
type TimelineItem struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`          // "task" or "plan"
	ContentType  string     `json:"content_type"`  // "rednote", "article", "xls"
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TimelineHandler handles the timeline API endpoint.
type TimelineHandler struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewTimelineHandler creates a new TimelineHandler.
func NewTimelineHandler(repo repository.Repository, logger *zerolog.Logger) *TimelineHandler {
	return &TimelineHandler{repo: repo, logger: logger}
}

// GetTimeline handles GET /api/v1/timeline.
// Query params: from (date), to (date). Both required.
// Returns a merged timeline of tasks and active plans within the date range.
func (h *TimelineHandler) GetTimeline(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		return Error(c, fiber.StatusBadRequest, "from and to query parameters are required (format: YYYY-MM-DD)")
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid 'from' date format, expected YYYY-MM-DD")
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid 'to' date format, expected YYYY-MM-DD")
	}

	// Ensure 'to' covers the full day.
	to = to.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	ctx := c.Context()
	var items []TimelineItem

	// 1. Past tasks within the date range.
	tasks, err := h.repo.Tasks().FindByCreatedAtRange(ctx, from, to, 0, 0)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch tasks for timeline")
		return Error(c, fiber.StatusInternalServerError, "failed to fetch timeline data")
	}

	for _, t := range tasks {
		if t.UserID != userID {
			continue
		}
		title := t.Topic
		if title == "" {
			title = t.Type + " task"
		}
		items = append(items, TimelineItem{
			ID:          t.ID,
			Type:        "task",
			ContentType: t.Type,
			Title:       title,
			Status:      t.Status,
			CompletedAt: t.CompletedAt,
			CreatedAt:   t.CreatedAt,
		})
	}

	// 2. Active plans with next_run_at within range.
	plans, err := h.repo.Plans().ListActiveByUserID(ctx, userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch plans for timeline")
		// Non-fatal: return what we have.
	} else {
		for _, p := range plans {
			if p.NextRunAt != nil && p.NextRunAt.After(from) && p.NextRunAt.Before(to) {
				title := p.Title
				if title == "" {
					title = p.TopicHint
				}
				if title == "" {
					title = p.Type + " plan"
				}
				items = append(items, TimelineItem{
					ID:          p.ID,
					Type:        "plan",
					ContentType: p.Type,
					Title:       title,
					Status:      p.Status,
					ScheduledAt: p.NextRunAt,
					CreatedAt:   p.CreatedAt,
				})
			}
		}
	}

	// 3. Running tasks (always included if owned by user).
	running, err := h.repo.Tasks().FindRunning(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch running tasks")
	} else {
		for _, t := range running {
			if t.UserID != userID {
				continue
			}
			// Avoid duplicates with items already added.
			alreadyAdded := false
			for _, item := range items {
				if item.ID == t.ID && item.Type == "task" {
					alreadyAdded = true
					break
				}
			}
			if alreadyAdded {
				continue
			}
			title := t.Topic
			if title == "" {
				title = t.Type + " task"
			}
			items = append(items, TimelineItem{
				ID:          t.ID,
				Type:        "task",
				ContentType: t.Type,
				Title:       title,
				Status:      t.Status,
				CreatedAt:   t.CreatedAt,
			})
		}
	}

	if items == nil {
		items = []TimelineItem{}
	}

	return Success(c, fiber.Map{
		"items": items,
	})
}

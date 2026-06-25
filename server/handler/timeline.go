package handler

import (
	"sort"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// TimelineItem represents a unified item in the timeline view.
type TimelineItem struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`         // "task" or "plan"
	ContentType string     `json:"content_type"` // "seednote", "article"
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	ProjectID   string     `json:"project_id,omitempty"`
	ProjectName string     `json:"project_name,omitempty"`
	Platform    string     `json:"platform,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
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
// Query params: from (date), to (date). Both required. Optional: project_id.
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

	projectID := c.Query("project_id", "")
	itemType := c.Query("item_type", "")       // "task" or "plan"
	contentType := c.Query("content_type", "") // "article", "seednote"
	statusFilter := c.Query("status", "")      // any valid task or plan status

	ctx := c.Context()

	// Fetch all user's projects once for lookup.
	projects, err := h.repo.Projects().ListByUserID(ctx, userID, repository.ProjectListOptions{})
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch projects for timeline")
		// Non-fatal: proceed without project info.
	}
	projectMap := make(map[string]*model.Project)
	for _, ch := range projects {
		projectMap[ch.ID] = ch
	}

	var items []TimelineItem

	// 1. Past tasks within the date range (scoped to user at query level).
	tasks, err := h.repo.Tasks().FindByUserIDAndCreatedAtRange(ctx, userID, from, to, 0, 0)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch tasks for timeline")
		return Error(c, fiber.StatusInternalServerError, "failed to fetch timeline data")
	}

	for _, t := range tasks {
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		title := t.Title
		if title == "" {
			title = t.Prompt
		}
		if title == "" {
			title = t.Type + " task"
		}
		var projectName, platform string
		if ch, ok := projectMap[t.ProjectID]; ok {
			projectName = ch.Name
			platform = ch.Platform
		}
		items = append(items, TimelineItem{
			ID:          t.ID,
			Type:        "task",
			ContentType: t.Type,
			Title:       title,
			Status:      t.Status,
			ProjectID:   t.ProjectID,
			ProjectName: projectName,
			Platform:    platform,
			CompletedAt: t.CompletedAt,
			CreatedAt:   t.CreatedAt,
		})
	}

	// 2. Active plans with next_run_at within range.
	plans, err := h.repo.Plans().ListActiveByUserID(ctx, userID, projectID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch plans for timeline")
		// Non-fatal: return what we have.
	} else {
		for _, p := range plans {
			if p.NextRunAt != nil && p.NextRunAt.After(from) && p.NextRunAt.Before(to) {
				title := p.Title
				if title == "" {
					title = p.Prompt
				}
				if title == "" {
					title = p.Type + " plan"
				}
				var projectName, platform string
				if ch, ok := projectMap[p.ProjectID]; ok {
					projectName = ch.Name
					platform = ch.Platform
				}
				items = append(items, TimelineItem{
					ID:          p.ID,
					Type:        "plan",
					ContentType: p.Type,
					Title:       title,
					Status:      p.Status,
					ProjectID:   p.ProjectID,
					ProjectName: projectName,
					Platform:    platform,
					ScheduledAt: p.NextRunAt,
					CreatedAt:   p.CreatedAt,
				})
			}
		}
	}

	// 3. Running tasks for this user (scoped query to prevent information leak).
	running, err := h.repo.Tasks().FindRunningByUser(ctx, userID, projectID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to fetch running tasks")
	} else {
		for _, t := range running {
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
			title := t.Title
			if title == "" {
				title = t.Prompt
			}
			if title == "" {
				title = t.Type + " task"
			}
			var projectName, platform string
			if ch, ok := projectMap[t.ProjectID]; ok {
				projectName = ch.Name
				platform = ch.Platform
			}
			items = append(items, TimelineItem{
				ID:          t.ID,
				Type:        "task",
				ContentType: t.Type,
				Title:       title,
				Status:      t.Status,
				ProjectID:   t.ProjectID,
				ProjectName: projectName,
				Platform:    platform,
				CreatedAt:   t.CreatedAt,
			})
		}
	}

	// Apply server-side filters.
	if itemType != "" || contentType != "" || statusFilter != "" {
		filtered := make([]TimelineItem, 0, len(items))
		for _, item := range items {
			if itemType != "" && item.Type != itemType {
				continue
			}
			if contentType != "" && item.ContentType != contentType {
				continue
			}
			if statusFilter != "" && item.Status != statusFilter {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}

	if items == nil {
		items = []TimelineItem{}
	}

	// Sort items by date (scheduled_at > created_at) for consistent ordering.
	sort.Slice(items, func(i, j int) bool {
		getDate := func(item TimelineItem) time.Time {
			if item.ScheduledAt != nil {
				return *item.ScheduledAt
			}
			return item.CreatedAt
		}
		return getDate(items[i]).Before(getDate(items[j]))
	})

	return Success(c, fiber.Map{
		"items": items,
	})
}
